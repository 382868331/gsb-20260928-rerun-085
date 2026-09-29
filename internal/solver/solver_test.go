package solver

import (
	"testing"

	"dependency-workbench/internal/model"
)

func testCatalog(t *testing.T) model.Catalog {
	t.Helper()
	return model.Catalog{Components: []model.Component{
		{
			Name: "app",
			Versions: []model.Version{
				{Version: "1.0.0", Dependencies: []model.Dependency{{Component: "lib", Constraint: "^1.0.0"}}},
				{Version: "2.0.0", Dependencies: []model.Dependency{{Component: "lib", Constraint: "^2.0.0"}, {Component: "util", Constraint: "^1.0.0"}}},
			},
		},
		{
			Name: "lib",
			Versions: []model.Version{
				{Version: "1.0.0"},
				{Version: "1.5.0"},
				{Version: "2.0.0", Dependencies: []model.Dependency{{Component: "util", Constraint: "^1.0.0"}}},
				{Version: "2.9.0", Dependencies: []model.Dependency{{Component: "util", Constraint: "^2.0.0"}}},
			},
		},
		{Name: "util", Versions: []model.Version{{Version: "1.0.0"}, {Version: "1.9.0"}, {Version: "2.0.0"}}},
		{
			Name:     "loop-a",
			Versions: []model.Version{{Version: "1.0.0", Dependencies: []model.Dependency{{Component: "loop-b", Constraint: "=1.0.0"}}}},
		},
		{
			Name:     "loop-b",
			Versions: []model.Version{{Version: "1.0.0", Dependencies: []model.Dependency{{Component: "loop-a", Constraint: "=1.0.0"}}}},
		},
	}}
}

func TestSolveKeepsOtherChangesMinimal(t *testing.T) {
	engine, err := New(testCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	choices, _, _, conflicts := engine.Solve(model.SolveRequest{
		TargetComponent: "app",
		TargetVersion:   "2.0.0",
		Current: map[string]string{
			"app":  "1.0.0",
			"lib":  "1.5.0",
			"util": "1.9.0",
		},
	})
	if conflicts != nil {
		t.Fatalf("expected solution, got %v", conflicts)
	}
	if choices["app"] != "2.0.0" || choices["lib"] != "2.0.0" || choices["util"] != "1.9.0" {
		t.Fatalf("unexpected choices: %v", choices)
	}
}

func TestSolveRespectsLocks(t *testing.T) {
	engine, err := New(testCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	choices, _, _, conflicts := engine.Solve(model.SolveRequest{
		TargetComponent: "app",
		TargetVersion:   "2.0.0",
		Current:         map[string]string{"app": "1.0.0", "lib": "1.5.0", "util": "1.0.0"},
		Locks:           map[string]string{"util": "2.0.0"},
	})
	if conflicts == nil {
		t.Fatalf("expected locked util 2.0.0 to conflict with app/lib constraints")
	}
	_ = choices
}

func TestSolveRejectsCycle(t *testing.T) {
	engine, err := New(testCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	choices, _, _, conflicts := engine.Solve(model.SolveRequest{
		TargetComponent: "loop-a",
		TargetVersion:   "1.0.0",
		Current:         map[string]string{"loop-a": "1.0.0"},
	})
	if choices != nil || len(conflicts) == 0 || conflicts[0].Kind != "cycle" {
		t.Fatalf("expected cycle conflict, choices=%v conflicts=%v", choices, conflicts)
	}
}
