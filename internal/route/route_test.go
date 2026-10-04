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
	done := initial.Steps[0]
	for i := range done.Checks {
		done.Checks[i].Status = "success"
	}
	confirmed := []domain.RouteStep{done}

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
		Checks:  []domain.StepCheck{{ID: "c1", Label: "核对", Status: "success"}},
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

func TestPlannedStepsCarryPendingChecks(t *testing.T) {
	report := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
	})
	if report.Status != "feasible" {
		t.Fatalf("expected feasible, got %s", report.Status)
	}
	for _, step := range report.Steps {
		if len(step.Checks) == 0 {
			t.Fatalf("planned step %d must carry checks to record, got %+v", step.Index, step)
		}
		for _, check := range step.Checks {
			if check.Status != "pending" {
				t.Fatalf("fresh checks must start pending, got %q", check.Status)
			}
		}
	}
}

func TestExecutedStepWithIncompleteChecksStaysHeldButCounts(t *testing.T) {
	initial := Plan(domain.RouteRequest{
		Manifest:       mutualManifest(),
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
	})
	if initial.Status != "feasible" || len(initial.Steps) != 2 {
		t.Fatalf("initial plan failed: %s", initial.ConflictMessage)
	}
	cases := []struct {
		name   string
		checks []domain.StepCheck
	}{
		{"pending", []domain.StepCheck{{ID: "c1", Label: "核对", Status: "pending"}}},
		{"failed", []domain.StepCheck{{ID: "c1", Label: "核对", Status: "failed"}}},
		{"insufficient", []domain.StepCheck{{ID: "c1", Label: "核对", Status: "insufficient"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := initial.Steps[0]
			step.Checks = tc.checks
			report := Plan(domain.RouteRequest{
				Manifest:       mutualManifest(),
				Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
				Target:         "app",
				DesiredVersion: "3.0.0",
				Confirmed:      []domain.RouteStep{step},
			})
			if len(report.ConfirmedSteps) != 1 {
				t.Fatalf("executed step must stay in the confirmed history, got %+v", report.ConfirmedSteps)
			}
			if len(report.HeldSteps) != 1 || report.HeldSteps[0].Effective {
				t.Fatalf("step must be held with its record preserved, got %+v", report.HeldSteps)
			}
			// The conversion already ran: planning resumes from the real
			// state the step left behind, never re-scheduling it.
			if report.ResumedFrom["app"] != "2.0.0" {
				t.Fatalf("planning must resume from the executed state, got %v", report.ResumedFrom)
			}
			if len(report.Steps) != 1 {
				t.Fatalf("the executed conversion must not be planned again, got %+v", report.Steps)
			}
		})
	}
}

func TestOptionsNeverShowPlannedFutureAsCurrentState(t *testing.T) {
	manifest := mutualManifest()
	report := Plan(domain.RouteRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
	})
	if report.Status != "feasible" || len(report.Steps) != 2 {
		t.Fatalf("expected a feasible two-step route, got %s (%d steps)", report.Status, len(report.Steps))
	}
	if len(report.Options) == 0 {
		t.Fatal("options must be offered")
	}
	for _, option := range report.Options {
		if option.Kind == "rollback" {
			continue
		}
		if option.State["app"] != "1.0.0" {
			t.Fatalf("option %s must show the actual current state, not the planned final version, got %v", option.Kind, option.State)
		}
	}
	if report.FinalSelection["app"] != "3.0.0" {
		t.Fatalf("final selection must still plan app 3.0.0, got %v", report.FinalSelection)
	}
}

func TestFailedCheckDataConversionStillCountsAsHappened(t *testing.T) {
	manifest := mutualManifest()
	initial := Plan(domain.RouteRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
	})
	if initial.Status != "feasible" || len(initial.Steps) == 0 {
		t.Fatalf("initial plan failed: %s", initial.ConflictMessage)
	}
	// The step was executed (cross-major data migration ran) but its check
	// failed: planning must not treat the conversion as never happened.
	failed := initial.Steps[0]
	for i := range failed.Checks {
		failed.Checks[i].Status = "failed"
	}
	if len(failed.Rollback.Irreversible) == 0 {
		t.Fatalf("cross-major step must carry irreversible impacts, got %+v", failed.Rollback)
	}
	report := Plan(domain.RouteRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
		Confirmed:      []domain.RouteStep{failed},
	})
	if len(report.HeldSteps) != 1 {
		t.Fatalf("failed step must be held, got %+v", report.HeldSteps)
	}
	if len(report.Options) == 0 {
		t.Fatal("options must be offered after a failed check")
	}
	for _, option := range report.Options {
		joined := ""
		for _, impact := range option.Impacts {
			joined += impact
		}
		if !strings.Contains(joined, "数据迁移") && !strings.Contains(joined, "数据转换") {
			t.Fatalf("option %s must keep the irreversible data impact of the executed step, got %v", option.Kind, option.Impacts)
		}
	}
	// Version planning resumes from the real executed state: the failed
	// check does not resurrect app 1.0.0.
	if report.ResumedFrom["app"] != "2.0.0" {
		t.Fatalf("planning resumes from the executed state, got %v", report.ResumedFrom)
	}
	// A backfill option is offered:补齐资料只更新核对，不重复执行转换。
	foundBackfill := false
	for _, option := range report.Options {
		if option.Kind == "backfill" {
			foundBackfill = true
			if option.State["app"] != "2.0.0" {
				t.Fatalf("backfill keeps the executed state, got %v", option.State)
			}
		}
	}
	if !foundBackfill {
		t.Fatalf("held steps must offer a backfill option, got %+v", report.Options)
	}
}

func TestOptionsExplainUnresolvedImpacts(t *testing.T) {
	manifest := domain.Manifest{Components: []domain.Component{
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
	initial := Plan(domain.RouteRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
	})
	if initial.Status != "feasible" || len(initial.Steps) == 0 {
		t.Fatalf("initial plan failed: %s", initial.ConflictMessage)
	}
	done := initial.Steps[0]
	for i := range done.Checks {
		done.Checks[i].Status = "success"
	}
	report := Plan(domain.RouteRequest{
		Manifest:       manifest,
		Current:        map[string]string{"app": "1.0.0", "lib": "1.0.0"},
		Target:         "app",
		DesiredVersion: "3.0.0",
		Locks:          map[string]string{"lib": "2.0.0"},
		Confirmed:      []domain.RouteStep{done},
	})
	if report.Status != "conflict" {
		t.Fatalf("locking lib 2.0.0 must block app 3.0.0, got %s", report.Status)
	}
	if len(report.Options) < 2 {
		t.Fatalf("expected continue/stop/rollback options, got %+v", report.Options)
	}
	byKind := map[string]domain.RouteOption{}
	for _, option := range report.Options {
		byKind[option.Kind] = option
		if len(option.Impacts) == 0 {
			t.Fatalf("option %s must list unresolved impacts", option.Kind)
		}
	}
	cont, ok := byKind["continue"]
	if !ok || cont.State["app"] != "2.0.0" {
		t.Fatalf("continue must keep the current executed state, got %+v", cont)
	}
	joined := ""
	for _, impact := range cont.Impacts {
		joined += impact
	}
	if !strings.Contains(joined, "数据迁移") && !strings.Contains(joined, "不可") {
		t.Fatalf("continue impacts must mention irreversible data migration, got %v", cont.Impacts)
	}
	rollback, ok := byKind["rollback"]
	if !ok || rollback.State["app"] != "1.0.0" {
		t.Fatalf("rollback must return to the still-compatible old state, got %+v", rollback)
	}
	rollbackJoined := ""
	for _, impact := range rollback.Impacts {
		rollbackJoined += impact
	}
	if !strings.Contains(rollbackJoined, "数据转换") {
		t.Fatalf("rollback impacts must explain data transforms are not undone, got %v", rollback.Impacts)
	}
	if _, ok := byKind["stop"]; !ok {
		t.Fatalf("stop option must be offered, got %+v", report.Options)
	}
}
