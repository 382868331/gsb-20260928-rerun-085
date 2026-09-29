package verifier

import (
	"testing"

	"dependency-workbench/internal/model"
)

func catalog() model.Catalog {
	return model.Catalog{Components: []model.Component{
		{Name: "app", Versions: []model.Version{
			{Version: "1.0.0", Dependencies: []model.Dependency{
				{Component: "lib", Constraint: "^1.0.0"},
				{Component: "plugin", Constraint: "^1.0.0", Optional: true},
			}},
		}},
		{Name: "lib", Versions: []model.Version{{Version: "1.2.0"}, {Version: "2.0.0"}}},
		{Name: "plugin", Versions: []model.Version{{Version: "1.0.0"}, {Version: "2.0.0"}}},
	}}
}

func TestVerifyAllowsUnselectedOptionalDependency(t *testing.T) {
	index := Index(catalog())
	report := index.Verify(map[string]string{"app": "1.0.0", "lib": "1.2.0"}, nil, "app")
	if !report.Satisfied {
		t.Fatalf("expected satisfied report, got %+v", report.Conflicts)
	}
}

func TestVerifyChecksSelectedOptionalDependency(t *testing.T) {
	index := Index(catalog())
	report := index.Verify(map[string]string{"app": "1.0.0", "lib": "1.2.0", "plugin": "2.0.0"}, nil, "app")
	if report.Satisfied || len(report.Conflicts) != 1 || report.Conflicts[0].Kind != "constraint" {
		t.Fatalf("expected one optional dependency conflict, got %+v", report)
	}
	chain := report.Conflicts[0].Chain
	if len(chain) != 1 || chain[0].From != "app" || chain[0].To != "plugin" {
		t.Fatalf("expected conflict chain app -> plugin, got %+v", chain)
	}
}
