package solver_test

import (
	"testing"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/solver"
	"dependency-workbench/internal/verifier"
)

func sampleManifest() domain.Manifest {
	return domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}, {Component: "legacy", Constraint: "^1.0.0", Optional: true}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}, {Component: "legacy", Constraint: "^9.0.0", Optional: true}}},
		}},
		{Name: "lib", Versions: []domain.Version{
			{Version: "1.0.0"}, {Version: "1.5.0"}, {Version: "2.0.0"}, {Version: "2.1.0"}, {Version: "3.0.0"},
		}},
		{Name: "legacy", Versions: []domain.Version{{Version: "1.0.0"}}},
		{Name: "broken", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^3.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
		}},
	}}
}

func TestSolveUsesClosestFeasibleNotLatest(t *testing.T) {
	report := solver.Solve(domain.SolveRequest{
		Manifest: sampleManifest(),
		Current:  map[string]string{"app": "1.0.0", "lib": "1.5.0"},
		Target:   "app", DesiredVersion: "2.0.0",
	})
	t.Logf("selection=%#v checks=%#v", report.Selection, report.Checks)
	if report.Status != "feasible" {
		t.Fatalf("expected feasible, got %s: %s", report.Status, report.ConflictMessage)
	}
	if report.Selection["lib"] != "2.0.0" {
		t.Fatalf("expected closest lib 2.0.0, got %s", report.Selection["lib"])
	}
	if report.ChangedCount != 2 {
		t.Fatalf("expected app and lib changes, got %d", report.ChangedCount)
	}
	if len(report.OptionalOmitted) != 1 || report.OptionalOmitted[0] != "app -> legacy" {
		t.Fatalf("expected optional legacy omitted, got %#v", report.OptionalOmitted)
	}
}

func TestSolveHonorsLockAndRecalculates(t *testing.T) {
	report := solver.Solve(domain.SolveRequest{
		Manifest: sampleManifest(),
		Current:  map[string]string{"app": "1.0.0", "lib": "1.5.0", "broken": "1.0.0"},
		Target:   "app", DesiredVersion: "2.0.0", Locks: map[string]string{"lib": "2.1.0"},
	})
	if report.Status != "feasible" {
		t.Fatalf("expected feasible, got %s: %s", report.Status, report.ConflictMessage)
	}
	if report.Selection["lib"] != "2.1.0" || report.Selection["broken"] != "" {
		t.Fatalf("unexpected selection: %#v", report.Selection)
	}
}

func TestConflictReportsDependencyChain(t *testing.T) {
	manifest := sampleManifest()
	manifest.Components = append(manifest.Components, domain.Component{Name: "ring", Versions: []domain.Version{
		{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "ring", Constraint: "^2.0.0"}}},
		{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "ring", Constraint: "^1.0.0"}}},
	}})
	report := solver.Solve(domain.SolveRequest{Manifest: manifest, Target: "ring", DesiredVersion: "1.0.0"})
	if report.Status != "conflict" {
		t.Fatalf("expected conflict, got %s", report.Status)
	}
	if len(report.ConflictChain) == 0 {
		t.Fatal("expected conflict chain")
	}
}

func TestIndependentVerifierWarnsOnCycles(t *testing.T) {
	manifest := domain.Manifest{Components: []domain.Component{
		{Name: "a", Versions: []domain.Version{{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "b", Constraint: "^1.0.0"}}}}},
		{Name: "b", Versions: []domain.Version{{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "a", Constraint: "^1.0.0"}}}}},
	}}
	report := verifier.Verify(manifest, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	if report.Status != "feasible" {
		t.Fatalf("cycles should be warnings, got %s", report.Status)
	}
	foundCycle := false
	for _, check := range report.Checks {
		foundCycle = foundCycle || check.Kind == "cycle"
	}
	if !foundCycle {
		t.Fatal("expected cycle check")
	}
}
