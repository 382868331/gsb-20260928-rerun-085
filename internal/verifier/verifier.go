package verifier

import (
	"fmt"
	"sort"

	"dependency-workbench/internal/model"
	"dependency-workbench/internal/semver"
)

type CatalogIndex map[string]model.Component

func Index(catalog model.Catalog) CatalogIndex {
	index := CatalogIndex{}
	for _, component := range catalog.Components {
		index[component.Name] = component
	}
	return index
}

func (index CatalogIndex) Verify(choices, current map[string]string, target string) model.Report {
	if current == nil {
		current = map[string]string{}
	}
	report := model.Report{Choices: map[string]string{}, Satisfied: true}
	for component, version := range choices {
		report.Choices[component] = version
	}
	names := make([]string, 0, len(choices))
	for component := range choices {
		names = append(names, component)
	}
	sort.Strings(names)
	for _, component := range names {
		version := choices[component]
		componentData, ok := index[component]
		if !ok {
			addConflict(&report, model.Conflict{Kind: "missing-component", Component: component, Message: fmt.Sprintf("组件 %s 不在版本资料中", component), Chain: index.path(choices, component)})
			continue
		}
		versionData, found := findVersion(componentData, version)
		if !found {
			addConflict(&report, model.Conflict{Kind: "missing-version", Component: component, Version: version, Message: fmt.Sprintf("组件 %s 没有版本 %s", component, version), Chain: index.path(choices, component)})
			continue
		}
		for _, dependency := range versionData.Dependencies {
			if dependency.Optional {
				if _, selected := choices[dependency.Component]; !selected {
					continue
				}
			}
			edge := model.Edge{From: component, FromVersion: version, To: dependency.Component, Constraint: dependency.Constraint, Optional: dependency.Optional}
			if _, selected := choices[dependency.Component]; !selected {
				addConflict(&report, model.Conflict{Kind: "missing-selection", Component: dependency.Component, Constraint: dependency.Constraint, Message: fmt.Sprintf("缺少%s依赖 %s", optionalText(dependency.Optional), dependency.Component), Chain: append(index.path(choices, component), edge)})
				continue
			}
			if _, exists := index[dependency.Component]; !exists {
				addConflict(&report, model.Conflict{Kind: "missing-component", Component: dependency.Component, Constraint: dependency.Constraint, Message: fmt.Sprintf("依赖 %s 不在版本资料中", dependency.Component), Chain: append(index.path(choices, component), edge)})
				continue
			}
			if !semver.Satisfies(choices[dependency.Component], dependency.Constraint) {
				addConflict(&report, model.Conflict{Kind: "constraint", Component: dependency.Component, Version: choices[dependency.Component], Constraint: dependency.Constraint, Message: fmt.Sprintf("%s@%s 需要 %s %s，但选择了 %s", component, version, dependency.Component, dependency.Constraint, choices[dependency.Component]), Chain: append(index.path(choices, component), edge)})
			}
		}
	}
	cycles := index.cycles(choices)
	for _, cycle := range cycles {
		addConflict(&report, model.Conflict{Kind: "cycle", Message: "选择结果包含循环依赖", Chain: cycle})
	}
	if target != "" {
		if choices[target] == "" {
			addConflict(&report, model.Conflict{Kind: "target-missing", Component: target, Message: fmt.Sprintf("目标组件 %s 不在方案中", target)})
		}
	}
	report.Changes = changes(choices, current, target)
	if report.Satisfied {
		report.Messages = []string{fmt.Sprintf("核验通过：共选择 %d 个组件，调整 %d 个组件。", len(choices), len(report.Changes))}
	} else {
		report.Messages = []string{fmt.Sprintf("核验失败：发现 %d 个冲突或循环依赖。", len(report.Conflicts))}
	}
	return report
}

func addConflict(report *model.Report, conflict model.Conflict) {
	report.Satisfied = false
	report.Conflicts = append(report.Conflicts, conflict)
}

func findVersion(component model.Component, version string) (model.Version, bool) {
	for _, item := range component.Versions {
		if item.Version == version {
			return item, true
		}
	}
	return model.Version{}, false
}

func optionalText(optional bool) string {
	if optional {
		return "可选"
	}
	return "必需"
}

func changes(choices, current map[string]string, target string) []model.Change {
	result := []model.Change{}
	for component, version := range choices {
		from := current[component]
		if from == version {
			continue
		}
		result = append(result, model.Change{Component: component, From: from, To: version, IsTarget: component == target, Reason: reasonFor(choices, current, component, target)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsTarget != result[j].IsTarget {
			return result[i].IsTarget
		}
		return result[i].Component < result[j].Component
	})
	return result
}

func reasonFor(choices, current map[string]string, component, target string) string {
	if component == target {
		return "用户指定升级"
	}
	if _, exists := current[component]; !exists {
		return "新增的传递依赖"
	}
	return "满足共同依赖约束"
}

func (index CatalogIndex) path(choices map[string]string, target string) []model.Edge {
	parent := map[string]model.Edge{}
	queue := []string{}
	visited := map[string]bool{}
	for component := range choices {
		if index.isRoot(choices, component) {
			queue = append(queue, component)
			visited[component] = true
		}
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		component := queue[0]
		queue = queue[1:]
		if component == target {
			break
		}
		for _, edge := range index.outEdges(choices, component) {
			if !visited[edge.To] {
				visited[edge.To] = true
				parent[edge.To] = edge
				queue = append(queue, edge.To)
			}
		}
	}
	var path []model.Edge
	component := target
	for edge, ok := parent[component]; ok; edge, ok = parent[component] {
		path = append([]model.Edge{edge}, path...)
		component = edge.From
	}
	return path
}

func (index CatalogIndex) isRoot(choices map[string]string, component string) bool {
	for _, incoming := range index.allEdges(choices) {
		if incoming.To == component {
			return false
		}
	}
	return true
}

func (index CatalogIndex) allEdges(choices map[string]string) []model.Edge {
	result := []model.Edge{}
	for component, version := range choices {
		componentData, ok := index[component]
		if !ok {
			continue
		}
		versionData, found := findVersion(componentData, version)
		if !found {
			continue
		}
		for _, dependency := range versionData.Dependencies {
			if dependency.Optional {
				if _, selected := choices[dependency.Component]; !selected {
					continue
				}
			}
			result = append(result, model.Edge{From: component, FromVersion: version, To: dependency.Component, Constraint: dependency.Constraint, Optional: dependency.Optional})
		}
	}
	return result
}

func (index CatalogIndex) outEdges(choices map[string]string, component string) []model.Edge {
	result := []model.Edge{}
	version := choices[component]
	componentData, ok := index[component]
	if !ok {
		return result
	}
	versionData, found := findVersion(componentData, version)
	if !found {
		return result
	}
	for _, dependency := range versionData.Dependencies {
		if dependency.Optional {
			if _, selected := choices[dependency.Component]; !selected {
				continue
			}
		}
		result = append(result, model.Edge{From: component, FromVersion: version, To: dependency.Component, Constraint: dependency.Constraint, Optional: dependency.Optional})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].To < result[j].To })
	return result
}

func (index CatalogIndex) cycles(choices map[string]string) [][]model.Edge {
	color := map[string]int{}
	var stack []model.Edge
	var cycles [][]model.Edge
	var visit func(string)
	visit = func(component string) {
		color[component] = 1
		for _, edge := range index.outEdges(choices, component) {
			if _, selected := choices[edge.To]; !selected {
				continue
			}
			if color[edge.To] == 1 {
				cycle := []model.Edge{edge}
				for i := len(stack) - 1; i >= 0; i-- {
					cycle = append([]model.Edge{stack[i]}, cycle...)
					if stack[i].From == edge.To {
						break
					}
				}
				cycles = append(cycles, cycle)
			} else if color[edge.To] == 0 {
				stack = append(stack, edge)
				visit(edge.To)
				stack = stack[:len(stack)-1]
			}
		}
		color[component] = 2
	}
	names := make([]string, 0, len(choices))
	for component := range choices {
		names = append(names, component)
	}
	sort.Strings(names)
	for _, component := range names {
		if color[component] == 0 {
			visit(component)
		}
	}
	return cycles
}
