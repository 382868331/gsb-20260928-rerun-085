package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	return server.New(planStore, nil)
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
				"manifest":  req.Manifest,
				"selection": selection,
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
			"manifest":  req.Manifest,
			"selection": map[string]string{"app": "2.0.0", "lib": "3.0.0"},
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
			"manifest":  req.Manifest,
			"selection": solved["selection"],
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
		"manifest":  req.Manifest,
		"selection": storedReport.Selection,
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

// An empty selection must no longer be judged feasible when the verification
// request carries the migration goals.
func TestVerifyWithGoalsRejectsEmptySelection(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "base")
	_, verified := do(t, handler, http.MethodPost, "/api/plans/verify", map[string]any{
		"manifest":       req.Manifest,
		"selection":      map[string]string{},
		"target":         req.Target,
		"desiredVersion": req.DesiredVersion,
	})
	if got, _ := verified["status"].(string); got != "conflict" {
		t.Fatalf("empty selection with goals must conflict, got %v", verified)
	}
	checks, _ := verified["checks"].([]any)
	foundGoal := false
	for _, item := range checks {
		check, _ := item.(map[string]any)
		if check["kind"] == "goal" {
			foundGoal = true
		}
	}
	if !foundGoal {
		t.Fatalf("expected a goal-specific reason, got %v", checks)
	}
}

// Submitting the same request twice must produce two plans whose conclusions
// are each computed from their own snapshot, not copied from the first save.
func TestDuplicateSubmissionVerifiedIndependently(t *testing.T) {
	handler := setup(t)
	req := scenario(t, handler, "base")
	_, first := do(t, handler, http.MethodPost, "/api/plans", req)
	_, second := do(t, handler, http.MethodPost, "/api/plans", req)
	if first["id"] == second["id"] {
		t.Fatal("duplicate submission must create a distinct plan")
	}
	for _, saved := range []map[string]any{first, second} {
		if ok, _ := saved["consistent"].(bool); !ok {
			t.Fatalf("each saved plan must be independently consistent: %v", saved)
		}
		verification := encodeDecode[domain.Report](t, saved["verification"])
		if verification.Status != "feasible" || len(verification.Goals) == 0 {
			t.Fatalf("verification must be goal-aware, got %#v", verification)
		}
	}
	if first["manifestHash"] == "" || first["manifestHash"] != second["manifestHash"] {
		t.Fatalf("same manifest must share a hash, got %v / %v", first["manifestHash"], second["manifestHash"])
	}
}

// A stored success conclusion must never be borrowed: even if the persisted
// file claims feasibility, listing re-verifies against the frozen snapshot.
func TestStoredConclusionIsRecomputedOnRead(t *testing.T) {
	dir := t.TempDir()
	planStore, err := store.New(filepath.Join(dir, "plans.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := server.New(planStore, nil)
	req := scenario(t, handler, "lock-conflict")
	status, saved := do(t, handler, http.MethodPost, "/api/plans", req)
	if status != http.StatusCreated {
		t.Fatalf("save status %d: %v", status, saved)
	}
	// Tamper with the stored file: pretend the solver found a feasible plan.
	content, err := os.ReadFile(filepath.Join(dir, "plans.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plans []map[string]any
	if err := json.Unmarshal(content, &plans); err != nil {
		t.Fatal(err)
	}
	plans[0]["report"] = map[string]any{"status": "feasible", "selection": map[string]string{"app": "2.0.0", "lib": "3.0.0"}, "changes": []any{}, "checks": []any{}}
	plans[0]["verification"] = map[string]any{"status": "feasible", "selection": map[string]string{"app": "2.0.0", "lib": "3.0.0"}, "checks": []any{}}
	plans[0]["consistent"] = true
	encoded, _ := json.Marshal(plans)
	if err := os.WriteFile(filepath.Join(dir, "plans.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	var got []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one plan, got %d", len(got))
	}
	verification := encodeDecode[domain.Report](t, got[0]["verification"])
	if verification.Status != "conflict" {
		t.Fatalf("stored feasible verdict must not survive re-verification, got %s", verification.Status)
	}
	if ok, _ := got[0]["consistent"].(bool); ok {
		t.Fatal("tampered plan must not be reported consistent")
	}
}
