package route

import (
	"strings"
	"testing"

	"dependency-workbench/internal/domain"
)

func mutualManifest() domain.Manifest {
	return domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
			{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^3.0.0"}}},
		}},
		{Name: "lib", Versions: []domain.Version{
			{Version: "1.0.0"},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: "^2.0.0"}}},
			{Version: "3.0.0", Requirements: []domain.Requirement{{Component: "app", Constraint: ">=2.0.0"}}},
		}},
	}}
}

func TestMutualDependenciesSwitchTogether(t *testing.T) {
	report := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
	})
	if report.Status != "feasible" {
		t.Fatalf("expected feasible, got %s: %s", report.Status, report.ConflictMessage)
	}
	if len(report.Steps) != 1 {
		t.Fatalf("expected a single step, got %d", len(report.Steps))
	}
	step := report.Steps[0]
	if len(step.Together) != 2 {
		t.Fatalf("expected app and lib to switch together, got %v", step.Together)
	}
	if step.Verification.Status != "feasible" {
		t.Fatalf("intermediate state must verify, got %s", step.Verification.Status)
	}
	if step.Rollback.State["app"] != "1.0.0" || step.Rollback.State["lib"] != "1.0.0" {
		t.Fatalf("rollback state must be the compatible starting state, got %v", step.Rollback.State)
	}
}

func TestTransitionVersionsAreUsedAndExplained(t *testing.T) {
	report := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
	})
	if report.Status != "feasible" {
		t.Fatalf("expected feasible, got %s: %s", report.Status, report.ConflictMessage)
	}
	if len(report.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d: %+v", len(report.Steps), report.Steps)
	}
	first := report.Steps[0]
	if first.State["app"] != "2.0.0" || first.State["lib"] != "2.0.0" {
		t.Fatalf("first step must move to transition versions, got %v", first.State)
	}
	if len(first.Transition) == 0 {
		t.Fatalf("transition versions must be flagged, got %+v", first)
	}
	if !strings.Contains(first.Note, "过渡版本") {
		t.Fatalf("transition must be explained, got %q", first.Note)
	}
	for i, step := range report.Steps {
		if step.Verification.Status != "feasible" {
			t.Fatalf("step %d state must verify independently, got %s", i+1, step.Verification.Status)
		}
	}
	last := report.Steps[len(report.Steps)-1]
	if last.State["app"] != "3.0.0" || last.State["lib"] != "3.0.0" {
		t.Fatalf("final state must reach the goal, got %v", last.State)
	}
}

func TestOptionalExitIsStagedAndMarkedIrreversible(t *testing.T) {
	manifest := domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "cache", Constraint: "^1.0.0", Optional: true}}},
			{Version: "2.0.0"},
		}},
		{Name: "cache", Versions: []domain.Version{{Version: "1.0.0"}}},
	}}
	report := Plan(domain.RouteRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "cache": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
	})
	if report.Status != "feasible" {
		t.Fatalf("expected feasible, got %s: %s", report.Status, report.ConflictMessage)
	}
	var exitStep *domain.RouteStep
	for i := range report.Steps {
		for _, change := range report.Steps[i].Changes {
			if change.Component == "cache" && change.To == "" {
				exitStep = &report.Steps[i]
			}
		}
	}
	if exitStep == nil {
		t.Fatalf("expected a step removing cache, got %+v", report.Steps)
	}
	if _, present := exitStep.State["cache"]; present {
		t.Fatalf("cache must be gone after the exit step, got %v", exitStep.State)
	}
	if len(exitStep.Rollback.Irreversible) == 0 {
		t.Fatalf("exit impact must be listed as not directly revertible")
	}
}

func TestConfirmedStepsAreKeptAndPlanningResumes(t *testing.T) {
	initial := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
	})
	if initial.Status != "feasible" || len(initial.Steps) == 0 {
		t.Fatalf("initial plan failed: %s", initial.ConflictMessage)
	}
	confirmed := []domain.RouteStep{initial.Steps[0]}

	// Mid-way goal change: app 3.0.0 -> app 2.0.0 after the first step.
	replanned := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
		Confirmed:      confirmed,
	})
	if replanned.Status != "feasible" {
		t.Fatalf("replan failed: %s", replanned.ConflictMessage)
	}
	if len(replanned.ConfirmedSteps) != 1 || !replanned.ConfirmedSteps[0].Confirmed {
		t.Fatalf("history must be preserved, got %+v", replanned.ConfirmedSteps)
	}
	if replanned.ResumedFrom["app"] != "2.0.0" {
		t.Fatalf("planning must resume from the completed state, got %v", replanned.ResumedFrom)
	}
	if len(replanned.Steps) != 0 {
		t.Fatalf("goal already reached after confirmed step, expected no remaining steps, got %+v", replanned.Steps)
	}

	// Locking a not-yet-upgraded component also resumes from history.
	locked := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
		Locks:          map[string]string{"lib": "2.0.0"},
		Confirmed:      confirmed,
	})
	if locked.Status != "conflict" {
		t.Fatalf("locking lib 2.0.0 must conflict with app 3.0.0, got %s", locked.Status)
	}
	if len(locked.ConfirmedSteps) != 1 {
		t.Fatalf("history must survive a failed replan, got %+v", locked.ConfirmedSteps)
	}
	if locked.ConflictMessage == "" {
		t.Fatalf("remaining conflict must be explained")
	}
}

func TestNoRouteReportsRemainingConflicts(t *testing.T) {
	manifest := domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
		}},
		{Name: "lib", Versions: []domain.Version{{Version: "1.0.0"}}},
	}}
	report := Plan(domain.RouteRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
	})
	if report.Status != "conflict" {
		t.Fatalf("expected conflict, got %s", report.Status)
	}
	if report.ConflictMessage == "" {
		t.Fatalf("conflict must be explained")
	}
}

func TestConfirmedStepInconsistentWithCurrentState(t *testing.T) {
	bogus := domain.RouteStep{
		Index:   1,
		Changes: []domain.Change{{Component: "app", From: "1.0.0", To: "2.0.0"}},
		State:   map[string]string{"app": "2.0.0", "lib": "9.9.9"},
	}
	report := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
		Confirmed:      []domain.RouteStep{bogus},
	})
	if report.Status != "conflict" {
		t.Fatalf("forged history must be rejected, got %s", report.Status)
	}
}
