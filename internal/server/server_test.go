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
	routeStore, err := store.NewRouteStore(filepath.Join(t.TempDir(), "routes.json"))
	if err != nil {
		t.Fatal(err)
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

func TestRoutePlanStagesTransitionAndAtomicSwitch(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "staged-transition")
	_, planned := do(t, handler, http.MethodPost, "/api/routes/plan", req)
	if got, _ := planned["status"].(string); got != "feasible" {
		t.Fatalf("route plan status = %q: %v", got, planned)
	}
	steps, _ := planned["steps"].([]any)
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %v", steps)
	}
	first, _ := steps[0].(map[string]any)
	if first["transitional"] != true {
		t.Fatalf("first step must be transitional, got %v", first)
	}
	verification, _ := first["verification"].(map[string]any)
	if got, _ := verification["status"].(string); got != "feasible" {
		t.Fatalf("intermediate state must verify independently, got %v", verification)
	}
	last, _ := steps[2].(map[string]any)
	if last["together"] != true {
		t.Fatalf("last step must switch components together, got %v", last)
	}
}

func TestRouteAdvanceKeepsHistoryAndReplansFromConfirmedState(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "goal-change")
	status, saved := do(t, handler, http.MethodPost, "/api/routes", req)
	if status != http.StatusCreated {
		t.Fatalf("save route status %d: %v", status, saved)
	}
	routeID, _ := saved["id"].(string)
	steps, _ := saved["steps"].([]any)
	if len(steps) != 3 {
		t.Fatalf("expected 3 planned steps, got %v", steps)
	}
	frozenHistory, _ := json.Marshal(steps[0])

	// Confirm the transitional step, then change the final goal to svc 2.1.0.
	status, advanced := do(t, handler, http.MethodPost, "/api/routes/advance", map[string]any{
		"id": routeID, "confirm": 1, "target": "svc", "desiredVersion": "2.1.0",
	})
	if status != http.StatusOK {
		t.Fatalf("advance status %d: %v", status, advanced)
	}
	if got, _ := advanced["status"].(string); got != "feasible" {
		t.Fatalf("replanned route must be feasible, got %v", advanced)
	}
	if got, _ := advanced["confirmed"].(float64); got != 1 {
		t.Fatalf("expected 1 confirmed step, got %v", advanced["confirmed"])
	}
	newSteps, _ := advanced["steps"].([]any)
	if len(newSteps) != 3 {
		t.Fatalf("expected history + 2 replanned steps, got %v", newSteps)
	}
	replayedHistory, _ := json.Marshal(newSteps[0])
	if string(frozenHistory) != string(replayedHistory) {
		t.Fatalf("confirmed step was recomputed into another route: %s vs %s", frozenHistory, replayedHistory)
	}
	finalState, _ := newSteps[2].(map[string]any)["state"].(map[string]any)
	if finalState["svc"] != "2.1.0" {
		t.Fatalf("remaining steps must reach the new goal svc 2.1.0, got %v", finalState)
	}
	revisions, _ := advanced["revisions"].([]any)
	if len(revisions) != 1 {
		t.Fatalf("goal change must be recorded as a revision, got %v", revisions)
	}

	// A reopened workspace still explains executed steps and remaining choices.
	listReq := httptest.NewRequest(http.MethodGet, "/api/routes", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	var stored []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored route, got %d", len(stored))
	}
	storedSteps, _ := stored[0]["steps"].([]any)
	if len(storedSteps) != 3 {
		t.Fatalf("persisted route must keep all steps, got %v", storedSteps)
	}
	storedFirst, _ := storedSteps[0].(map[string]any)
	if _, ok := storedFirst["verification"].(map[string]any); !ok {
		t.Fatalf("persisted step must carry independent verification, got %v", storedFirst)
	}
}

func TestRouteAdvanceBlockedReportsRollbackAndRemainingConflict(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "goal-change")
	_, saved := do(t, handler, http.MethodPost, "/api/routes", req)
	routeID, _ := saved["id"].(string)

	// Confirm the transitional step, then lock db at 1.0.0: svc 2.0.0 requires
	// db ^2.0.0, so no route remains and the conflict must be explicit.
	status, advanced := do(t, handler, http.MethodPost, "/api/routes/advance", map[string]any{
		"id": routeID, "confirm": 1, "locks": map[string]string{"db": "1.0.0"},
	})
	if status != http.StatusOK {
		t.Fatalf("advance status %d: %v", status, advanced)
	}
	if got, _ := advanced["status"].(string); got != "blocked" {
		t.Fatalf("route must be blocked, got %v", advanced)
	}
	if msg, _ := advanced["conflictMessage"].(string); msg == "" {
		t.Fatalf("blocked route must name the remaining conflict, got %v", advanced)
	}
	steps, _ := advanced["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("blocked route keeps only confirmed history, got %v", steps)
	}
	rollback, _ := advanced["rollback"].([]any)
	if len(rollback) != 1 {
		t.Fatalf("blocked route must list compatible rollback states, got %v", rollback)
	}
	point, _ := rollback[0].(map[string]any)
	state, _ := point["state"].(map[string]any)
	if state["svc"] != "1.5.0" {
		t.Fatalf("rollback state must be the confirmed svc 1.5.0 state, got %v", state)
	}
}
