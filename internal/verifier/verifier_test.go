package verifier_test

import (
	"testing"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/verifier"
)

func verifierManifest() domain.Manifest {
	return domain.Manifest{Components: []domain.Component{
		{Name: "app", Versions: []domain.Version{
			{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
			{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
		}},
		{Name: "lib", Versions: []domain.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}},
	}}
}

func TestVerifyRejectsEmptySelectionWhenMigrationGoalExists(t *testing.T) {
	req := domain.SolveRequest{Manifest: verifierManifest(), Target: "app", DesiredVersion: "2.0.0"}
	report := verifier.VerifyRequest(req, map[string]string{})
	if report.Status != "conflict" {
		t.Fatalf("empty selection must not be feasible, got %s", report.Status)
	}
	if len(report.GoalStatuses) != 1 || report.GoalStatuses[0].Status != "pending" {
		t.Fatalf("expected pending app goal, got %#v", report.GoalStatuses)
	}
}

func TestVerifyRejectsSelectionThatDoesNotCoverGoal(t *testing.T) {
	req := domain.SolveRequest{
		Manifest:       verifierManifest(),
		Target:         "app",
		DesiredVersion: "2.0.0",
		Goals:          []domain.MigrationGoal{{Component: "lib", Version: "2.0.0"}},
	}
	selection := map[string]string{"app": "1.0.0", "lib": "1.0.0"}
	report := verifier.VerifyRequest(req, selection)
	if report.Status != "conflict" {
		t.Fatalf("wrong selected versions must conflict, got %s", report.Status)
	}
	statuses := map[string]string{}
	for _, item := range report.GoalStatuses {
		statuses[item.Component] = item.Status
	}
	if statuses["app"] != "unmet" || statuses["lib"] != "unmet" {
		t.Fatalf("expected both goals unmet, got %#v", report.GoalStatuses)
	}
}

func TestVerifyAcceptsSelectionCoveringEveryGoalAndDependency(t *testing.T) {
	req := domain.SolveRequest{Manifest: verifierManifest(), Target: "app", DesiredVersion: "2.0.0"}
	selection := map[string]string{"app": "2.0.0", "lib": "2.0.0"}
	report := verifier.VerifyRequest(req, selection)
	if report.Status != "feasible" {
		t.Fatalf("complete selection should be feasible, got %s: %#v", report.Status, report.Checks)
	}
	if report.GoalStatuses[0].Status != "satisfied" {
		t.Fatalf("expected satisfied goal, got %#v", report.GoalStatuses)
	}
}
