package sample

import "dependency-workbench/internal/domain"

// Names lists the built-in fixture scenarios exposed by GET /api/sample.
func Names() []string {
	return []string{"default", "cycle", "lock-conflict", "missing-candidate"}
}

// Get returns the named sample solve request. Unknown names fall back to default.
func Get(name string) domain.SolveRequest {
	switch name {
	case "cycle":
		return cycleRequest()
	case "lock-conflict":
		return lockConflictRequest()
	case "missing-candidate":
		return missingCandidateRequest()
	default:
		return defaultRequest()
	}
}

func defaultRequest() domain.SolveRequest {
	return domain.SolveRequest{
		Manifest: domain.Manifest{Components: []domain.Component{
			{Name: "app", Versions: []domain.Version{
				{Version: "1.0.0", Requirements: []domain.Requirement{
					{Component: "lib", Constraint: "^1.0.0"},
					{Component: "legacy", Constraint: "^1.0.0", Optional: true},
				}},
				{Version: "2.0.0", Requirements: []domain.Requirement{
					{Component: "lib", Constraint: "^2.0.0"},
					{Component: "legacy", Constraint: "^9.0.0", Optional: true},
				}},
			}},
			{Name: "lib", Versions: []domain.Version{
				{Version: "1.0.0"}, {Version: "1.5.0"}, {Version: "2.0.0"}, {Version: "2.1.0"}, {Version: "3.0.0"},
			}},
			{Name: "legacy", Versions: []domain.Version{{Version: "1.0.0"}}},
		}},
		Current:        map[string]string{"app": "1.0.0", "lib": "1.5.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
		Locks:          map[string]string{},
	}
}

func cycleRequest() domain.SolveRequest {
	return domain.SolveRequest{
		Manifest: domain.Manifest{Components: []domain.Component{
			{Name: "a", Versions: []domain.Version{
				{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "b", Constraint: "^1.0.0"}}},
			}},
			{Name: "b", Versions: []domain.Version{
				{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "a", Constraint: "^1.0.0"}}},
				{Version: "1.2.0", Requirements: []domain.Requirement{{Component: "a", Constraint: "^1.0.0"}}},
			}},
		}},
		Current:        map[string]string{"a": "1.0.0", "b": "1.0.0"},
		Target:         "a",
		DesiredVersion: "1.0.0",
		Locks:          map[string]string{},
	}
}

func lockConflictRequest() domain.SolveRequest {
	return domain.SolveRequest{
		Manifest: domain.Manifest{Components: []domain.Component{
			{Name: "app", Versions: []domain.Version{
				{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^1.0.0"}}},
				{Version: "2.0.0", Requirements: []domain.Requirement{{Component: "lib", Constraint: "^2.0.0"}}},
			}},
			{Name: "lib", Versions: []domain.Version{
				{Version: "1.0.0"}, {Version: "1.5.0"}, {Version: "2.0.0"}, {Version: "2.1.0"},
			}},
		}},
		Current:        map[string]string{"app": "1.0.0", "lib": "1.5.0"},
		Target:         "app",
		DesiredVersion: "2.0.0",
		// lib 1.5.0 cannot satisfy app 2.0.0's ^2.0.0 requirement.
		Locks: map[string]string{"lib": "1.5.0"},
	}
}

func missingCandidateRequest() domain.SolveRequest {
	return domain.SolveRequest{
		Manifest: domain.Manifest{Components: []domain.Component{
			{Name: "shell", Versions: []domain.Version{
				{Version: "1.0.0", Requirements: []domain.Requirement{{Component: "ghost-lib", Constraint: "^1.0.0"}}},
			}},
			{Name: "present-lib", Versions: []domain.Version{{Version: "1.0.0"}}},
		}},
		Current:        map[string]string{"shell": "1.0.0"},
		Target:         "shell",
		DesiredVersion: "1.0.0",
		Locks:          map[string]string{},
	}
}
