package route_test

import (
	"testing"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/route"
	"dependency-workbench/internal/verifier"
)

func stagedManifest() domain.Manifest {
	return domain.Manifest{Components: []domain.Component{
		{Name: "top", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "mid", Constraint: "^1.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "mid", Constraint: "^2.0.0"}}},
		}},
		{Name: "mid", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "core", Constraint: "^1.0.0"}}},
			{Version: "1.5.0", Requirements: []domain.Requirement{{Component: "core", Constraint: ">=1.0.0 <3.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "core", Constraint: "^2.0.0"}}},
		}},
		{Name: "core", Versions: []domain.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}},
	}}
}

func TestPlanStagesTransitionalVersionAndAtomicSwitch(t *testing.T) {
	req := domain.SolveRequest{
		Manifest:       stagedManifest(),
		Current:        map[string]string{"top": "1.0.0", "mid": "1.0.0", "core": "1.0.0"},
		Target:         "top",
		DesiredVersion: "2.0.0",
	}
	result := route.Plan(req, req.Current)
	if result.Conflict != nil {
		t.Fatalf("expected route, got conflict: %s", result.Conflict.ConflictMessage)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %#v", len(result.Steps), result.Steps)
	}
	first := result.Steps[0]
	if !first.Transitional || first.Changes[0].Component != "mid" || first.Changes[0].To != "1.5.0" {
		t.Fatalf("first step must stage mid via transitional 1.5.0, got %#v", first)
	}
	if first.Reason == "" {
		t.Fatal("transitional step must explain why the transition is needed")
	}
	last := result.Steps[2]
	if !last.Together || len(last.Changes) != 2 {
		t.Fatalf("final step must switch mid and top together, got %#v", last)
	}
	if last.Reason == "" {
		t.Fatal("together step must explain which components switch together")
	}
	// Every intermediate state must verify independently.
	for _, step := range result.Steps {
		if route.HasErrors(step.Verification) {
			t.Fatalf("step %d state fails verification: %#v", step.Index, step.Verification.Checks)
		}
		fresh := verifier.CheckState(stagedManifest(), step.State)
		if route.HasErrors(fresh) {
			t.Fatalf("step %d state fails fresh verification: %#v", step.Index, fresh.Checks)
		}
	}
	if result.Steps[2].State["top"] != "2.0.0" || result.Steps[2].State["mid"] != "2.0.0" || result.Steps[2].State["core"] != "2.0.0" {
		t.Fatalf("final state must reach the goal, got %#v", result.Steps[2].State)
	}
}

func TestPlanGroupsInterdependentComponents(t *testing.T) {
	manifest := domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
		}},
		{Name: "lib", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^1.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
		}},
	}}
	req := domain.SolveRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
	}
	result := route.Plan(req, req.Current)
	if result.Conflict != nil {
		t.Fatalf("expected route, got conflict: %s", result.Conflict.ConflictMessage)
	}
	if len(result.Steps) != 1 || !result.Steps[0].Together {
		t.Fatalf("app and lib must switch in one atomic step, got %#v", result.Steps)
	}
}

func TestPlanAllowsOptionalExitWithWarning(t *testing.T) {
	manifest := domain.Manifest{Components: []domain.Component{
		{Name: "portal", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "plugin", Constraint: "^1.0.0", Optional: true}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "plugin", Constraint: "^2.0.0", Optional: true}}},
		}},
		{Name: "plugin", Versions: []domain.Version{{Version: "1.0.0"}}},
	}}
	req := domain.SolveRequest{
		Manifest:       manifest,
		Current:        map[string]string{"portal": "1.0.0", "plugin": "1.0.0"},
		Target:         "portal",
		DesiredVersion: "2.0.0",
	}
	result := route.Plan(req, req.Current)
	if result.Conflict != nil {
		t.Fatalf("optional exit must not block the route: %s", result.Conflict.ConflictMessage)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected a single step, got %#v", result.Steps)
	}
	foundWarning := false
	for _, check := range result.Steps[0].Verification.Checks {
		if check.Kind == "optional" && check.Status == "warning" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("expected optional-exit warning in step verification, got %#v", result.Steps[0].Verification.Checks)
	}
}

func TestPlanReportsConflictWhenNoRouteExists(t *testing.T) {
	manifest := domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
		}},
		{Name: "lib", Versions: []domain.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}},
	}}
	req := domain.SolveRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
		Locks:          map[string]string{"lib": "1.0.0"},
	}
	result := route.Plan(req, req.Current)
	if result.Conflict == nil {
		t.Fatalf("locked lib must block the route, got %#v", result.Steps)
	}
	if result.Conflict.ConflictMessage == "" {
		t.Fatal("conflict must explain the remaining blocker")
	}
}

func TestAnalyzeRollbackMarksStepsThatLaterStepsDependOn(t *testing.T) {
	req := domain.SolveRequest{
		Manifest:       stagedManifest(),
		Current:        map[string]string{"top": "1.0.0", "mid": "1.0.0", "core": "1.0.0"},
		Target:         "top",
		DesiredVersion: "2.0.0",
	}
	result := route.Plan(req, req.Current)
	if result.Conflict != nil {
		t.Fatalf("expected route, got conflict: %s", result.Conflict.ConflictMessage)
	}
	points := route.AnalyzeRollback(stagedManifest(), req.Current, result.Steps, 3)
	if len(points) != 3 {
		t.Fatalf("expected 3 rollback points, got %#v", points)
	}
	// Rolling back the core 2.0.0 step while keeping the final mid/top switch
	// strands the later confirmed state, so it must not be revertible.
	if points[1].Revertible {
		t.Fatalf("core upgrade must not be directly revertible: %#v", points[1])
	}
	if points[1].Reason == "" {
		t.Fatal("non-revertible step must explain the impact")
	}
	if !points[2].Revertible {
		t.Fatalf("last confirmed step has no later dependents, got %#v", points[2])
	}
}
