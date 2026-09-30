package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/sample"
	"dependency-workbench/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	planStore, err := store.New(filepath.Join(dir, "plans.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &server{store: planStore}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sample", s.handleSample)
	mux.HandleFunc("/api/solve", s.handleSolve)
	mux.HandleFunc("/api/plans/verify", s.handleVerify)
	mux.HandleFunc("/api/plans", s.handlePlans)
	return mux
}

func TestRootFallbackWhenFrontendMissing(t *testing.T) {
	dir := t.TempDir()
	fs := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fileExists(index) {
			http.ServeFile(w, r, index)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("API is running"))
		_ = fs
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "API is running") {
		t.Fatalf("unexpected fallback: %d %q", rec.Code, rec.Body.String())
	}
}

func postJSON(t *testing.T, h http.Handler, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func getJSON(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func TestScenariosMatchVerifierConclusions(t *testing.T) {
	h := newTestServer(t)
	cases := []struct {
		name             string
		scenario         string
		wantStatus       string
		wantMessageParts []string
	}{
		{"default feasible", "default", "feasible", nil},
		{"cycle feasible with warning", "cycle", "feasible", nil},
		{"lock conflict chain", "lock-conflict", "conflict", []string{"lib", "1.5.0", "^2.0.0", "app"}},
		{"missing candidate", "missing-candidate", "conflict", []string{"ghost-lib", "not present in the manifest"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqBody, _ := json.Marshal(sample.Get(tc.scenario))
			solveReq := httptest.NewRequest(http.MethodPost, "/api/solve", bytes.NewReader(reqBody))
			solveRec := httptest.NewRecorder()
			h.ServeHTTP(solveRec, solveReq)
			if solveRec.Code != http.StatusOK {
				t.Fatalf("solve status %d: %s", solveRec.Code, solveRec.Body.String())
			}
			var report domain.Report
			if err := json.Unmarshal(solveRec.Body.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Status != tc.wantStatus {
				t.Fatalf("status=%s want %s: %s", report.Status, tc.wantStatus, report.ConflictMessage)
			}
			for _, part := range tc.wantMessageParts {
				if !strings.Contains(report.ConflictMessage+chainText(report.ConflictChain), part) {
					t.Fatalf("conflict text %q chain %#v missing %q", report.ConflictMessage, report.ConflictChain, part)
				}
			}
			// Independent verify endpoint must reach the same status from raw selection.
			scenarioReq := sample.Get(tc.scenario)
			code, out := postJSON(t, h, "/api/plans/verify", map[string]any{
				"manifest":       scenarioReq.Manifest,
				"selection":      report.Selection,
				"target":         scenarioReq.Target,
				"desiredVersion": scenarioReq.DesiredVersion,
				"locks":          scenarioReq.Locks,
			})
			if code != http.StatusOK || out["status"] != tc.wantStatus {
				t.Fatalf("verify mismatch code=%d body=%v", code, out)
			}
			if tc.scenario == "cycle" {
				foundCycle := false
				for _, check := range report.Checks {
					if check.Kind == "cycle" {
						foundCycle = true
					}
				}
				if !foundCycle {
					t.Fatal("expected cycle warning check")
				}
			}
		})
	}
}

func chainText(links []domain.ChainLink) string {
	var b strings.Builder
	for _, link := range links {
		b.WriteString(link.From + " " + link.FromVersion + " " + link.To + " " + link.Constraint + " ")
	}
	return b.String()
}

func TestSavePlanRunsIndependentVerifyAndPersistsSnapshot(t *testing.T) {
	h := newTestServer(t)
	req := sample.Get("lock-conflict")
	code, saved := postJSON(t, h, "/api/plans", req)
	if code != http.StatusCreated {
		t.Fatalf("save status %d: %v", code, saved)
	}
	if saved["status"] != nil {
		t.Fatal("save must return the full plan envelope, got report-shaped body")
	}
	if saved["consistent"] != true {
		t.Fatalf("conflict solver and verifier should agree: %v", saved)
	}
	code, list := getJSON(t, h, "/api/plans")
	if code != http.StatusOK {
		t.Fatalf("list status %d", code)
	}
	if len(list) != 0 {
		t.Fatalf("unexpected decoded top-level map: %v", list)
	}
	reqList := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	recList := httptest.NewRecorder()
	h.ServeHTTP(recList, reqList)
	var plans []domain.Plan
	if err := json.Unmarshal(recList.Body.Bytes(), &plans); err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 persisted plan, got %d", len(plans))
	}
	if plans[0].Request.Target != "app" || plans[0].Request.Locks["lib"] != "1.5.0" {
		t.Fatalf("request snapshot mutated: %#v", plans[0].Request)
	}
	if plans[0].Report.Status != "conflict" || plans[0].Verify.Status != "conflict" {
		t.Fatalf("expected both reports conflict, got solve=%s verify=%s", plans[0].Report.Status, plans[0].Verify.Status)
	}
}

func TestSavedPlanKeepsOldVersionsWhenManifestChanges(t *testing.T) {
	h := newTestServer(t)
	// Save a feasible default plan.
	code, saved := postJSON(t, h, "/api/plans", sample.Get("default"))
	if code != http.StatusCreated {
		t.Fatalf("save status %d: %v", code, saved)
	}
	var plan domain.Plan
	raw, _ := json.Marshal(saved)
	_ = json.Unmarshal(raw, &plan)
	oldSelection := plan.Report.Selection["lib"]
	if oldSelection != "2.0.0" {
		t.Fatalf("expected closest lib 2.0.0, got %s", oldSelection)
	}
	// Re-check the saved selection against a new manifest that drops lib 2.x:
	// the stored snapshot must not be reinterpreted or rewritten.
	newManifest := domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
		}},
		{Name: "lib", Versions: []domain.Version{{Version: "1.0.0"}, {Version: "3.0.0"}}},
	}}
	code, out := postJSON(t, h, "/api/plans/verify", map[string]any{"manifest": newManifest, "selection": plan.Report.Selection, "target": "app", "desiredVersion": "2.0.0"})
	if code != http.StatusOK || out["status"] != "conflict" {
		t.Fatalf("old selection must fail verification under changed manifest, got %d %v", code, out)
	}
	// Re-list: saved plan still shows original versions.
	reqList := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	recList := httptest.NewRecorder()
	h.ServeHTTP(recList, reqList)
	var plans []domain.Plan
	_ = json.Unmarshal(recList.Body.Bytes(), &plans)
	if len(plans) != 1 || plans[0].Report.Selection["lib"] != "2.0.0" {
		t.Fatalf("saved snapshot changed: %#v", plans)
	}
}
