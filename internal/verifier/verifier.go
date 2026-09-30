package verifier

import (
	"fmt"
	"sort"
	"strings"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/semver"
)

func Verify(manifest domain.Manifest, selection map[string]string, roots ...domain.Requirement) domain.Report {
	versions := map[string]map[string]domain.Version{}
	for _, component := range manifest.Components {
		versions[component.Name] = map[string]domain.Version{}
		for _, item := range component.Versions {
			versions[component.Name][item.Version] = item
		}
	}
	checks := []domain.Check{}
	selectedComponents := make([]string, 0, len(selection))
	for name := range selection {
		selectedComponents = append(selectedComponents, name)
	}
	sort.Strings(selectedComponents)

	// Root requirements (the upgrade request and manual locks) anchor the
	// selection: an empty or truncated selection must not verify as feasible.
	for _, root := range roots {
		chosen, included := selection[root.Component]
		if !included {
			if _, known := versions[root.Component]; !known {
				checks = append(checks, domain.Check{Kind: "selection", Status: "error", Message: fmt.Sprintf("required component %q is not present in the manifest", root.Component), Chain: []domain.ChainLink{{From: "upgrade request", To: root.Component, Constraint: root.Constraint}}})
				continue
			}
			checks = append(checks, domain.Check{Kind: "selection", Status: "error", Message: fmt.Sprintf("required component %s is missing from the selection", root.Component), Chain: []domain.ChainLink{{From: "upgrade request", To: root.Component, Constraint: root.Constraint}}})
			continue
		}
		v, _ := semver.Parse(chosen)
		if !semver.Satisfies(v, strings.TrimPrefix(root.Constraint, "=")) {
			checks = append(checks, domain.Check{Kind: "selection", Status: "error", Message: fmt.Sprintf("%s %s does not satisfy %s from upgrade request", root.Component, chosen, root.Constraint), Chain: []domain.ChainLink{{From: "upgrade request", To: root.Component, Constraint: root.Constraint}}})
		}
	}

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
	return domain.Report{Status: status, Selection: clone(selection), Changes: []domain.Change{}, Checks: checks}
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
