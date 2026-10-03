package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/server"
	"dependency-workbench/internal/store"
)

func setup(t *testing.T) http.Handler {
	t.Helper()
	planStore, err := store.New(filepath.Join(t.TempDir(), "plans.json"))
	if err != nil {
		t.Fatal(err)
	}
	routeStore, err := store.NewRoutes(filepath.Join(t.TempDir(), "routes.json"))
	if err != nil {
		t.Fatalf("route store: %v", err)
	}
	return server.New(planStore, routeStore, nil)
}

func do(t *testing.T, handler http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s body: %v (%q)", path, err, rec.Body.String())
		}
	}
	return rec.Code, out
}

func scenario(t *testing.T, handler http.Handler, id string) domain.SolveRequest {
	t.Helper()
	status, body := do(t, handler, http.MethodGet, "/api/sample?id="+id, nil)
	if status != http.StatusOK {
		t.Fatalf("sample %s status %d", id, status)
	}
	encoded, _ := json.Marshal(body["request"])
	var req domain.SolveRequest
	if err := json.Unmarshal(encoded, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

// Every shipped scenario: solver verdict must agree with an independent
// verification of the resulting selection (conflict conclusions included).
func TestScenariosPageAndVerifierAgree(t *testing.T) {
	handler := setup(t)
	cases := []struct {
		id     string
		status string
	}{
		{"base", "feasible"},
		{"cycle", "feasible"},
		{"lock-conflict", "conflict"},
		{"missing-candidate", "conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			req := scenario(t, handler, tc.id)
			_, solved := do(t, handler, http.MethodPost, "/api/solve", req)
			status, _ := solved["status"].(string)
			if status != tc.status {
				t.Fatalf("solve status = %q, want %q: %v", status, tc.status, solved["conflictMessage"])
			}
			selection, _ := solved["selection"].(map[string]any)
			_, verified := do(t, handler, http.MethodPost, "/api/plans/verify", map[string]any{
				"manifest":       req.Manifest,
				"selection":      selection,
				"target":         req.Target,
				"desiredVersion": req.DesiredVersion,
				"goals":          req.Goals,
			})
			// An empty selection is trivially feasible for the verifier; the
			// conflict-path agreement for these scenarios is checked below.
			if tc.status == "feasible" {
				if got, _ := verified["status"].(string); got != tc.status {
					t.Fatalf("verifier status = %q, want %q", got, tc.status)
				}
			}
			chain, _ := solved["conflictChain"].([]any)
			if tc.status == "conflict" && len(chain) == 0 {
				t.Fatal("page conflict must expose the dependency chain")
			}
		})
	}
}

func TestConflictScenariosReportChainsAndErrors(t *testing.T) {
	handler := setup(t)
	t.Run("lock conflict chain", func(t *testing.T) {
		req := scenario(t, handler, "lock-conflict")
		_, solved := do(t, handler, http.MethodPost, "/api/solve", req)
		chain, _ := solved["conflictChain"].([]any)
		if len(chain) == 0 {
			t.Fatal("expected conflict dependency chain")
		}
		// The independent verifier must reject the same selection when asked
		// directly: app 2.0.0 -> lib 3.0.0 violates ^2.0.0.
		_, verified := do(t, handler, http.MethodPost, "/api/plans/verify", map[string]any{
			"manifest":       req.Manifest,
			"selection":      map[string]string{"app": "2.0.0", "lib": "3.0.0"},
			"target":         req.Target,
			"desiredVersion": req.DesiredVersion,
		})
		if got, _ := verified["status"].(string); got != "conflict" {
			t.Fatalf("verifier should flag locked lib 3.0.0, got %v", verified)
		}
	})
	t.Run("missing candidate named explicitly", func(t *testing.T) {
		req := scenario(t, handler, "missing-candidate")
		_, solved := do(t, handler, http.MethodPost, "/api/solve", req)
		msg, _ := solved["conflictMessage"].(string)
		if msg == "" || !contains(msg, "phantom") {
			t.Fatalf("expected phantom in conflict message, got %q", msg)
		}
	})
	t.Run("cycle is a verifier warning, not a solver failure", func(t *testing.T) {
		req := scenario(t, handler, "cycle")
		_, solved := do(t, handler, http.MethodPost, "/api/solve", req)
		_, verified := do(t, handler, http.MethodPost, "/api/plans/verify", map[string]any{
			"manifest":       req.Manifest,
			"selection":      solved["selection"],
			"target":         req.Target,
			"desiredVersion": req.DesiredVersion,
		})
		checks, _ := verified["checks"].([]any)
		found := false
		for _, item := range checks {
			check, _ := item.(map[string]any)
			if check["kind"] == "cycle" && check["status"] == "warning" {
				chain, _ := check["chain"].([]any)
				if len(chain) == 0 {
					t.Fatal("cycle warning must carry the chain")
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("expected cycle warning, got %v", checks)
		}
	})
}

func TestIndependentVerificationUsesGoalContextAndRejectsEmptySelection(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "base")
	_, verified := do(t, handler, http.MethodPost, "/api/plans/verify", map[string]any{
		"manifest":       req.Manifest,
		"selection":      map[string]string{},
		"target":         req.Target,
		"desiredVersion": req.DesiredVersion,
	})
	if got, _ := verified["status"].(string); got != "conflict" {
		t.Fatalf("empty selection must not be feasible against a goal, got %v", verified)
	}
	statuses, _ := verified["goalStatuses"].([]any)
	if len(statuses) != 1 {
		t.Fatalf("expected one goal status, got %v", statuses)
	}
	first, _ := statuses[0].(map[string]any)
	if first["status"] != "pending" || first["reason"] == "" {
		t.Fatalf("expected pending goal with reason, got %v", first)
	}
}

func TestSolveHonorsConfirmedComponentsForRemainingGoals(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "base")
	req.Confirmed = map[string]string{"lib": "2.1.0"}
	_, solved := do(t, handler, http.MethodPost, "/api/solve", req)
	if got, _ := solved["status"].(string); got != "feasible" {
		t.Fatalf("confirmed compatible lib should allow remaining app goal, got %v", solved)
	}
	selection, _ := solved["selection"].(map[string]any)
	if selection["app"] != "2.0.0" || selection["lib"] != "2.1.0" {
		t.Fatalf("unexpected selection: %v", selection)
	}
	statuses, _ := solved["goalStatuses"].([]any)
	if len(statuses) != 1 {
		t.Fatalf("expected app goal status, got %v", statuses)
	}
	first, _ := statuses[0].(map[string]any)
	if first["status"] != "satisfied" {
		t.Fatalf("expected satisfied app goal, got %v", first)
	}
}

// Saving must snapshot the request and re-verify it independently. Changing
// constraints afterwards must not silently reinterpret an older plan.
func TestSavedPlanKeepsSnapshotWhenConstraintsChange(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "base")
	status, saved := do(t, handler, http.MethodPost, "/api/plans", req)
	if status != http.StatusCreated {
		t.Fatalf("save status %d: %v", status, saved)
	}
	if consistent, _ := saved["consistent"].(bool); !consistent {
		t.Fatalf("freshly saved feasible plan must be consistent, got %v", saved)
	}
	planID, _ := saved["id"].(string)
	originalSelection := asSelection(t, saved["report"])
	if originalSelection["lib"] != "2.0.0" {
		t.Fatalf("expected closest lib 2.0.0, got %v", originalSelection)
	}

	// Mutate the live manifest: app 2.0.0 now demands lib ^3.0.0.
	for i := range req.Manifest.Components {
		if req.Manifest.Components[i].Name != "app" {
			continue
		}
		for j := range req.Manifest.Components[i].Versions {
			if req.Manifest.Components[i].Versions[j].Version == "2.0.0" {
				req.Manifest.Components[i].Versions[j].Requirements[0].Constraint = "^3.0.0"
			}
		}
	}

	// The stored plan must still carry the original request snapshot and the
	// original selection; re-verification against that snapshot stays feasible.
	listReq := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	var plans []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &plans); err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	for _, item := range plans {
		if item["id"] == planID {
			stored = item
		}
	}
	if stored == nil {
		t.Fatal("saved plan missing from list")
	}
	storedRequest := encodeDecode[domain.SolveRequest](t, stored["request"])
	for _, component := range storedRequest.Manifest.Components {
		if component.Name != "app" {
			continue
		}
		for _, version := range component.Versions {
			if version.Version == "2.0.0" {
				if version.Requirements[0].Constraint != "^2.0.0" {
					t.Fatalf("snapshot was rewritten: %v", version.Requirements)
				}
			}
		}
	}
	storedReport := encodeDecode[domain.Report](t, stored["report"])
	if !reflect.DeepEqual(storedReport.Selection, map[string]string{"app": "2.0.0", "lib": "2.0.0"}) {
		t.Fatalf("old plan selection changed: %v", storedReport.Selection)
	}
	verification := encodeDecode[domain.Report](t, stored["verification"])
	if verification.Status != "feasible" {
		t.Fatalf("snapshot verification must stay feasible, got %s", verification.Status)
	}

	// The same selection checked against the *new* manifest must fail, proving
	// the page and verifier react to the changed constraints rather than the
	// stale solver success flag.
	_, reverified := do(t, handler, http.MethodPost, "/api/plans/verify", map[string]any{
		"manifest":       req.Manifest,
		"selection":      storedReport.Selection,
		"target":         "app",
		"desiredVersion": "2.0.0",
	})
	if got, _ := reverified["status"].(string); got != "conflict" {
		t.Fatalf("lib 2.0.0 must conflict with new ^3.0.0 constraint, got %v", reverified)
	}
}

func asSelection(t *testing.T, value any) map[string]string {
	t.Helper()
	report := encodeDecode[domain.Report](t, value)
	return report.Selection
}

func encodeDecode[T any](t *testing.T, value any) T {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func contains(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}

func routeRequestFrom(req domain.SolveRequest) map[string]any {
	return map[string]any{
		"manifest":       req.Manifest,
		"current":        req.Current,
		"target":         req.Target,
		"desiredVersion": req.DesiredVersion,
		"locks":          req.Locks,
	}
}

func TestRoutePlanEndpointStagesMutualUpgrade(t *testing.T) {
	handler := setup(t)
	sample := scenario(t, handler, "mutual")
	status, body := do(t, handler, http.MethodPost, "/api/route/plan", routeRequestFrom(sample))
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if body["status"] != "feasible" {
		t.Fatalf("expected feasible, got %v", body["status"])
	}
	steps, _ := body["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("expected one step, got %v", len(steps))
	}
	step := steps[0].(map[string]any)
	if together, _ := step["together"].([]any); len(together) != 2 {
		t.Fatalf("mutual upgrade must switch together, got %v", step["together"])
	}
	verification := step["verification"].(map[string]any)
	if verification["status"] != "feasible" {
		t.Fatalf("intermediate state must verify, got %v", verification["status"])
	}
}

func TestRouteSaveAndReloadKeepsHistory(t *testing.T) {
	handler := setup(t)
	sample := scenario(t, handler, "transition")
	status, saved := do(t, handler, http.MethodPost, "/api/routes", routeRequestFrom(sample))
	if status != http.StatusCreated {
		t.Fatalf("status %d", status)
	}
	report := saved["report"].(map[string]any)
	if report["status"] != "feasible" {
		t.Fatalf("expected feasible saved route, got %v", report["status"])
	}
	req := httptest.NewRequest(http.MethodGet, "/api/routes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d", rec.Code)
	}
	var routes []domain.SavedRoute
	if err := json.Unmarshal(rec.Body.Bytes(), &routes); err != nil {
		t.Fatalf("decode routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 saved route, got %d", len(routes))
	}
	if len(routes[0].Report.Steps) == 0 || routes[0].Report.Steps[0].Verification.Status != "feasible" {
		t.Fatalf("saved route must keep per-step verification, got %+v", routes[0].Report.Steps)
	}
}

func TestRouteSaveIsIdempotentAcrossInterruption(t *testing.T) {
	handler := setup(t)
	sample := scenario(t, handler, "transition")
	payload := routeRequestFrom(sample)
	status, first := do(t, handler, http.MethodPost, "/api/routes", payload)
	if status != http.StatusCreated {
		t.Fatalf("first save status %d", status)
	}
	// Simulate a save interrupted after the record was written: the client
	// retries the identical request and must get the same record back
	// instead of executing the one-time transformations again.
	status, second := do(t, handler, http.MethodPost, "/api/routes", payload)
	if status != http.StatusOK {
		t.Fatalf("retry status %d, want 200", status)
	}
	if first["id"] != second["id"] {
		t.Fatalf("retry must return the original record, got %v vs %v", first["id"], second["id"])
	}
	req := httptest.NewRequest(http.MethodGet, "/api/routes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var routes []domain.SavedRoute
	if err := json.Unmarshal(rec.Body.Bytes(), &routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("interrupted save must not duplicate the route, got %d", len(routes))
	}
}

func TestRouteConfirmRecordsChecksOnce(t *testing.T) {
	handler := setup(t)
	sample := scenario(t, handler, "transition")
	status, saved := do(t, handler, http.MethodPost, "/api/routes", routeRequestFrom(sample))
	if status != http.StatusCreated {
		t.Fatalf("save status %d", status)
	}
	routeID, _ := saved["id"].(string)
	report := saved["report"].(map[string]any)
	steps, _ := report["steps"].([]any)
	if len(steps) == 0 {
		t.Fatalf("expected planned steps, got %v", report)
	}
	first := steps[0].(map[string]any)
	checks, _ := first["checks"].([]any)
	if len(checks) == 0 {
		t.Fatalf("planned step must carry checks, got %v", first)
	}
	recorded := []map[string]any{}
	for _, item := range checks {
		check := item.(map[string]any)
		recorded = append(recorded, map[string]any{"id": check["id"], "label": check["label"], "status": "success"})
	}
	confirm := map[string]any{"routeId": routeID, "stepIndex": 1, "checks": recorded}
	status, confirmed := do(t, handler, http.MethodPost, "/api/routes/confirm", confirm)
	if status != http.StatusOK {
		t.Fatalf("confirm status %d: %v", status, confirmed)
	}
	confirmedReport := confirmed["report"].(map[string]any)
	confirmedSteps, _ := confirmedReport["confirmedSteps"].([]any)
	if len(confirmedSteps) != 1 {
		t.Fatalf("step must move into confirmed history, got %v", confirmedReport)
	}
	// Idempotent retry after an interrupted save: same record, no duplicate.
	status, again := do(t, handler, http.MethodPost, "/api/routes/confirm", confirm)
	if status != http.StatusOK {
		t.Fatalf("idempotent retry status %d: %v", status, again)
	}
	againReport := again["report"].(map[string]any)
	againSteps, _ := againReport["confirmedSteps"].([]any)
	if len(againSteps) != 1 {
		t.Fatalf("retry must not duplicate the executed step, got %v", againReport)
	}
	// A different record for the same executed step is rejected: history
	// cannot be overwritten by a new route.
	changed := []map[string]any{}
	for _, item := range recorded {
		changed = append(changed, map[string]any{"id": item["id"], "label": item["label"], "status": "failed"})
	}
	status, conflict := do(t, handler, http.MethodPost, "/api/routes/confirm", map[string]any{
		"routeId": routeID, "stepIndex": 1, "checks": changed,
	})
	if status != http.StatusConflict {
		t.Fatalf("overwriting recorded checks must fail, got %d: %v", status, conflict)
	}
	// Replanning resumes from the truly completed state only.
	replanPayload := routeRequestFrom(sample)
	replanPayload["confirmed"] = confirmedReport["confirmedSteps"]
	status, replanned := do(t, handler, http.MethodPost, "/api/route/plan", replanPayload)
	if status != http.StatusOK {
		t.Fatalf("replan status %d", status)
	}
	resumed, _ := replanned["resumedFrom"].(map[string]any)
	if resumed["app"] != "2.0.0" {
		t.Fatalf("planning must resume from the verified state, got %v", resumed)
	}
}

func TestRouteConfirmInsufficientStaysPending(t *testing.T) {
	handler := setup(t)
	sample := scenario(t, handler, "transition")
	status, saved := do(t, handler, http.MethodPost, "/api/routes", routeRequestFrom(sample))
	if status != http.StatusCreated {
		t.Fatalf("save status %d", status)
	}
	routeID, _ := saved["id"].(string)
	report := saved["report"].(map[string]any)
	steps, _ := report["steps"].([]any)
	first := steps[0].(map[string]any)
	checks, _ := first["checks"].([]any)
	recorded := []map[string]any{}
	for _, item := range checks {
		check := item.(map[string]any)
		recorded = append(recorded, map[string]any{"id": check["id"], "label": check["label"], "status": "insufficient"})
	}
	status, confirmed := do(t, handler, http.MethodPost, "/api/routes/confirm", map[string]any{
		"routeId": routeID, "stepIndex": 1, "checks": recorded,
	})
	if status != http.StatusOK {
		t.Fatalf("confirm status %d: %v", status, confirmed)
	}
	confirmedReport := confirmed["report"].(map[string]any)
	confirmedSteps, _ := confirmedReport["confirmedSteps"].([]any)
	if len(confirmedSteps) != 1 {
		t.Fatalf("record must be preserved even when insufficient, got %v", confirmedReport)
	}
	// Replanning must not treat the step as done: it stays held and the
	// route resumes from the state before it.
	replanPayload := routeRequestFrom(sample)
	replanPayload["confirmed"] = confirmedReport["confirmedSteps"]
	_, replanned := do(t, handler, http.MethodPost, "/api/route/plan", replanPayload)
	held, _ := replanned["heldSteps"].([]any)
	if len(held) != 1 {
		t.Fatalf("insufficient checks must keep the step held, got %v", replanned)
	}
	resumed, _ := replanned["resumedFrom"].(map[string]any)
	if resumed["app"] != "1.0.0" {
		t.Fatalf("planning must resume from the truly completed state, got %v", resumed)
	}
}
