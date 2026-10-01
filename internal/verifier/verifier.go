package verifier

import (
	"fmt"
	"sort"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/semver"
)

func Verify(manifest domain.Manifest, selection map[string]string) domain.Report {
	return CheckSelection(manifest, selection)
}

func VerifyRequest(req domain.SolveRequest, selection map[string]string) domain.Report {
	manifest := req.Manifest
	if selection == nil {
		selection = map[string]string{}
	}
	versions := map[string]map[string]domain.Version{}
	for _, component := range manifest.Components {
		versions[component.Name] = map[string]domain.Version{}
		for _, item := range component.Versions {
			versions[component.Name][item.Version] = item
		}
	}
	checks := []domain.Check{}
	goalStatuses := checkGoals(req, selection, versions)
	for _, goal := range goalStatuses {
		switch goal.Status {
		case "satisfied":
			checks = append(checks, domain.Check{Kind: "goal", Status: "ok", Message: fmt.Sprintf("migration goal %s %s is covered by the selection", goal.Component, goal.Required)})
		case "pending", "unmet":
			checks = append(checks, domain.Check{Kind: "goal", Status: "error", Message: goal.Reason})
		}
	}
	if len(goalStatuses) == 0 {
		checks = append(checks, domain.Check{Kind: "coverage", Status: "error", Message: "cannot prove migration coverage because no migration goal was supplied"})
	} else if len(selection) == 0 {
		message := "selection is empty, so no migration goal is covered"
		checks = append(checks, domain.Check{Kind: "coverage", Status: "error", Message: message})
	}
	selectedComponents := make([]string, 0, len(selection))
	for name := range selection {
		selectedComponents = append(selectedComponents, name)
	}
	sort.Strings(selectedComponents)

	for _, name := range selectedComponents {
		version := selection[name]
		item, ok := versions[name][version]
		if !ok {
			checks = append(checks, domain.Check{Kind: "selection", Status: "error", Message: fmt.Sprintf("%s %s does not exist in the manifest", name, version)})
			continue
		}
		for _, req := range item.Requirements {
			chosen, included := selection[req.Component]
			if !included {
				if req.Optional {
					checks = append(checks, domain.Check{Kind: "optional", Status: "warning", Message: fmt.Sprintf("optional dependency %s required by %s %s is omitted", req.Component, name, version)})
					continue
				}
				checks = append(checks, domain.Check{Kind: "dependency", Status: "error", Message: fmt.Sprintf("required dependency %s for %s %s is missing", req.Component, name, version), Chain: []domain.ChainLink{{From: name, FromVersion: version, To: req.Component, Constraint: req.Constraint}}})
				continue
			}
			v, _ := semver.Parse(chosen)
			if !semver.Satisfies(v, req.Constraint) {
				status := "error"
				kind := "dependency"
				if req.Optional {
					status, kind = "warning", "optional"
				}
				checks = append(checks, domain.Check{Kind: kind, Status: status, Message: fmt.Sprintf("%s %s does not satisfy %s required by %s %s", req.Component, chosen, req.Constraint, name, version), Chain: []domain.ChainLink{{From: name, FromVersion: version, To: req.Component, Constraint: req.Constraint, Optional: req.Optional}}})
			}
		}
	}
	if cycles := findCycles(manifest, selection); len(cycles) > 0 {
		for _, cycle := range cycles {
			checks = append(checks, domain.Check{Kind: "cycle", Status: "warning", Message: "dependency cycle detected", Chain: cycle})
		}
	}
	status := "feasible"
	for _, check := range checks {
		if check.Status == "error" {
			status = "conflict"
			break
		}
	}
	return domain.Report{Status: status, Selection: clone(selection), GoalStatuses: goalStatuses, Changes: []domain.Change{}, Checks: checks}
}

func CheckSelection(manifest domain.Manifest, selection map[string]string) domain.Report {
	report := VerifyRequest(domain.SolveRequest{Manifest: manifest}, selection)
	return report
}

func checkGoals(req domain.SolveRequest, selection map[string]string, versions map[string]map[string]domain.Version) []domain.GoalStatus {
	goals := normalizeGoals(req)
	seen := map[string]bool{}
	out := []domain.GoalStatus{}
	for _, goal := range goals {
		key := goal.Component + "@" + goal.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		item := domain.GoalStatus{Component: goal.Component, Required: goal.Version, Selected: selection[goal.Component]}
		chosen, included := selection[goal.Component]
		switch {
		case included && chosen == goal.Version:
			item.Status = "satisfied"
		case included && chosen != goal.Version:
			item.Status = "unmet"
			item.Reason = fmt.Sprintf("migration goal requires %s %s, but %s %s was selected", goal.Component, goal.Version, goal.Component, chosen)
		case len(versions[goal.Component]) == 0:
			item.Status = "unmet"
			item.Reason = fmt.Sprintf("migration goal component %s is not present in the manifest", goal.Component)
		case versions[goal.Component][goal.Version].Version == "":
			item.Status = "unmet"
			item.Reason = fmt.Sprintf("migration goal version %s %s is not present in the manifest", goal.Component, goal.Version)
		default:
			item.Status = "pending"
			item.Reason = fmt.Sprintf("migration goal %s %s is not covered by the selection", goal.Component, goal.Version)
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Component == out[j].Component {
			return out[i].Required < out[j].Required
		}
		return out[i].Component < out[j].Component
	})
	return out
}

func normalizeGoals(req domain.SolveRequest) []domain.MigrationGoal {
	out := append([]domain.MigrationGoal{}, req.Goals...)
	if req.Target != "" && req.DesiredVersion != "" {
		found := false
		for _, goal := range out {
			if goal.Component == req.Target && goal.Version == req.DesiredVersion {
				found = true
			}
		}
		if !found {
			out = append(out, domain.MigrationGoal{Component: req.Target, Version: req.DesiredVersion})
		}
	}
	return out
}

func findCycles(manifest domain.Manifest, selection map[string]string) [][]domain.ChainLink {
	versionByComponent := map[string]map[string]domain.Version{}
	for _, component := range manifest.Components {
		versionByComponent[component.Name] = map[string]domain.Version{}
		for _, item := range component.Versions {
			versionByComponent[component.Name][item.Version] = item
		}
	}
	color := map[string]int{}
	var stack []domain.ChainLink
	var cycles [][]domain.ChainLink
	var visit func(string)
	visit = func(name string) {
		color[name] = 1
		item := versionByComponent[name][selection[name]]
		for _, req := range item.Requirements {
			if _, ok := selection[req.Component]; !ok {
				continue
			}
			link := domain.ChainLink{From: name, FromVersion: selection[name], To: req.Component, Constraint: req.Constraint, Optional: req.Optional}
			if color[req.Component] == 0 {
				stack = append(stack, link)
				visit(req.Component)
				stack = stack[:len(stack)-1]
			} else if color[req.Component] == 1 {
				cycle := []domain.ChainLink{}
				for i := range stack {
					if stack[i].From == req.Component {
						cycle = append(cycle, stack[i:]...)
						break
					}
				}
				cycles = append(cycles, append(cycle, link))
			}
		}
		color[name] = 2
	}
	names := make([]string, 0, len(selection))
	for name := range selection {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if color[name] == 0 {
			visit(name)
		}
	}
	return cycles
}

func clone(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
