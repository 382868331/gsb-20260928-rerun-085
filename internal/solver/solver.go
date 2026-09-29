package solver

import (
	"fmt"
	"sort"

	"dependency-workbench/internal/model"
	"dependency-workbench/internal/semver"
)

type Engine struct {
	components map[string]model.Component
}

type selected struct {
	version string
}

type state struct {
	engine       *Engine
	request      model.SolveRequest
	active       map[string]bool
	choices      map[string]string
	reasons      map[string]string
	optional     map[string]bool
	edges        map[string][]model.Edge
	best         map[string]string
	bestCost     int
	bestReasons  map[string]string
	bestOptional map[string]bool
}

func New(catalog model.Catalog) (*Engine, error) {
	e := &Engine{components: map[string]model.Component{}}
	for _, component := range catalog.Components {
		if _, exists := e.components[component.Name]; exists {
			return nil, fmt.Errorf("duplicate component %q", component.Name)
		}
		e.components[component.Name] = component
		for _, version := range component.Versions {
			if _, err := semver.Parse(version.Version); err != nil {
				return nil, fmt.Errorf("component %q: %w", component.Name, err)
			}
			seen := map[string]bool{}
			for _, dependency := range version.Dependencies {
				key := dependency.Component + "\x00" + dependency.Constraint
				if seen[key] {
					return nil, fmt.Errorf("component %q %s has duplicate dependency %s %s", component.Name, version.Version, dependency.Component, dependency.Constraint)
				}
				seen[key] = true
			}
		}
	}
	return e, nil
}

func (e *Engine) Has(component string) bool {
	_, ok := e.components[component]
	return ok
}

func (e *Engine) Solve(request model.SolveRequest) (map[string]string, map[string]string, map[string]bool, []model.Conflict) {
	s := &state{
		engine:       e,
		request:      request,
		active:       map[string]bool{},
		choices:      map[string]string{},
		reasons:      map[string]string{},
		optional:     map[string]bool{},
		edges:        map[string][]model.Edge{},
		bestCost:     1<<31 - 1,
		bestReasons:  map[string]string{},
		bestOptional: map[string]bool{},
	}
	if request.Current == nil {
		request.Current = map[string]string{}
	}
	if request.Locks == nil {
		request.Locks = map[string]string{}
	}
	s.request = request
	for name := range request.Current {
		s.active[name] = true
	}
	s.active[request.TargetComponent] = true
	for name := range request.Locks {
		s.active[name] = true
	}
	conflicts := s.search()
	if s.best == nil {
		return nil, nil, nil, conflicts
	}
	return s.best, s.bestReasons, s.bestOptional, nil
}

func (s *state) search() []model.Conflict {
	component := s.nextComponent()
	if component == "" {
		cycles := findCycles(s.choices, s.edges)
		if len(cycles) > 0 {
			return []model.Conflict{{Kind: "cycle", Message: "selected versions form a dependency cycle", Chain: cycles[0]}}
		}
		cost := s.cost()
		if cost < s.bestCost {
			s.bestCost = cost
			s.best = copyMap(s.choices)
			s.bestReasons = s.buildReasons()
			s.bestOptional = copyBoolMap(s.optional)
		}
		return nil
	}
	candidates := s.candidates(component)
	var failures []model.Conflict
	for _, candidate := range candidates {
		addedActive := s.choose(component, candidate.version)
		failure := s.search()
		s.undo(component, addedActive)
		if failure != nil && s.best == nil {
			failures = append(failures, failure...)
		}
		if s.currentCostLowerBound() >= s.bestCost {
			break
		}
	}
	if len(failures) == 0 {
		failures = []model.Conflict{{Kind: "no-version", Component: component, Message: fmt.Sprintf("no usable version for %s", component), Chain: s.edges[component]}}
	}
	return failures
}

func (s *state) nextComponent() string {
	candidates := []string{}
	for component := range s.active {
		if _, chosen := s.choices[component]; !chosen {
			candidates = append(candidates, component)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		iForced := s.forcedVersion(candidates[i]) != ""
		jForced := s.forcedVersion(candidates[j]) != ""
		if iForced != jForced {
			return iForced
		}
		if candidates[i] == s.request.TargetComponent {
			return true
		}
		if candidates[j] == s.request.TargetComponent {
			return false
		}
		return candidates[i] < candidates[j]
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func (s *state) forcedVersion(component string) string {
	if component == s.request.TargetComponent {
		return s.request.TargetVersion
	}
	return s.request.Locks[component]
}

type candidate struct {
	version string
	score   int
}

func (s *state) candidates(component string) []candidate {
	forced := s.forcedVersion(component)
	if forced != "" {
		if s.compatible(component, forced) {
			return []candidate{{version: forced, score: 0}}
		}
		return nil
	}
	componentData, ok := s.engine.components[component]
	if !ok {
		return nil
	}
	current := s.request.Current[component]
	var result []candidate
	for _, item := range componentData.Versions {
		if !s.compatible(component, item.Version) {
			continue
		}
		score := 1000000000
		if current != "" {
			if parsedCurrent, err := semver.Parse(current); err == nil {
				score = semver.Distance(parsedCurrent, mustParse(item.Version))
			}
			if item.Version != current {
				score += 100000000
			}
		} else if _, reason := s.reasons[component]; reason {
			score = 10000000
		}
		result = append(result, candidate{version: item.Version, score: score})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].score < result[j].score })
	return result
}

func mustParse(value string) semver.Version {
	v, _ := semver.Parse(value)
	return v
}

func (s *state) compatible(component, version string) bool {
	for _, edge := range s.edges[component] {
		if !semver.Satisfies(version, edge.Constraint) {
			return false
		}
	}
	return true
}

func (s *state) choose(component, version string) []string {
	s.choices[component] = version
	current := s.request.Current[component]
	if component == s.request.TargetComponent {
		s.reasons[component] = "用户指定升级"
	} else if locked, ok := s.request.Locks[component]; ok && locked == version {
		s.reasons[component] = "方案锁定"
	} else if current != "" && current != version {
		s.reasons[component] = "满足共同依赖约束"
	} else if current == "" {
		s.reasons[component] = "新增的必需传递依赖"
	}
	data, ok := s.engine.components[component]
	if !ok {
		return nil
	}
	versionData := findVersion(data, version)
	added := []string{}
	for _, dependency := range versionData.Dependencies {
		edge := model.Edge{From: component, FromVersion: version, To: dependency.Component, Constraint: dependency.Constraint, Optional: dependency.Optional}
		existing := edgeExists(s.edges[dependency.Component], edge)
		if !existing {
			s.edges[dependency.Component] = append(s.edges[dependency.Component], edge)
		}
		if dependency.Optional && !s.active[dependency.Component] {
			continue
		}
		if !s.active[dependency.Component] {
			s.active[dependency.Component] = true
			added = append(added, dependency.Component)
		}
		if dependency.Optional {
			s.optional[dependency.Component] = true
			if _, ok := s.reasons[dependency.Component]; !ok {
				s.reasons[dependency.Component] = "可选依赖与已选组件共同核验"
			}
		}
	}
	return added
}

func edgeExists(edges []model.Edge, edge model.Edge) bool {
	for _, existing := range edges {
		if existing == edge {
			return true
		}
	}
	return false
}

func (s *state) undo(component string, added []string) {
	delete(s.choices, component)
	delete(s.reasons, component)
	delete(s.optional, component)
	for _, name := range added {
		delete(s.active, name)
	}
	for dependency := range s.edges {
		filtered := []model.Edge{}
		for _, edge := range s.edges[dependency] {
			if edge.From != component {
				filtered = append(filtered, edge)
			}
		}
		if len(filtered) == 0 {
			delete(s.edges, dependency)
		} else {
			s.edges[dependency] = filtered
		}
	}
	s.recomputeOptional()
}

func (s *state) recomputeOptional() {
	for component := range s.active {
		optional := false
		for _, edge := range s.edges[component] {
			optional = optional || edge.Optional
		}
		if optional {
			s.optional[component] = true
		} else {
			delete(s.optional, component)
		}
	}
}

func (s *state) buildReasons() map[string]string {
	reasons := map[string]string{}
	for component := range s.choices {
		switch {
		case component == s.request.TargetComponent:
			reasons[component] = "用户指定升级"
		case s.request.Locks[component] != "":
			reasons[component] = "方案锁定"
		case s.request.Current[component] == "":
			reasons[component] = "新增的传递依赖"
		case s.request.Current[component] != s.choices[component]:
			reasons[component] = "满足共同依赖约束"
		default:
			reasons[component] = "保持当前版本"
		}
	}
	return reasons
}

func findVersion(component model.Component, version string) model.Version {
	for _, item := range component.Versions {
		if item.Version == version {
			return item
		}
	}
	return model.Version{}
}

func (s *state) cost() int {
	cost := 0
	for component, version := range s.choices {
		current := s.request.Current[component]
		if current == version {
			continue
		}
		if component == s.request.TargetComponent {
			cost += 1
		} else if s.optional[component] {
			cost += 100000
		} else {
			cost += 1000
		}
	}
	return cost
}

func (s *state) currentCostLowerBound() int {
	cost := 0
	for component, version := range s.choices {
		if s.request.Current[component] != version && component != s.request.TargetComponent {
			cost += 1000
		}
	}
	return cost
}

func findCycles(choices map[string]string, edges map[string][]model.Edge) [][]model.Edge {
	color := map[string]int{}
	var stack []model.Edge
	var cycles [][]model.Edge
	var visit func(string)
	visit = func(component string) {
		color[component] = 1
		for _, edge := range edgesFrom(choices, edges, component) {
			if color[edge.To] == 1 {
				cycle := []model.Edge{edge}
				for index := len(stack) - 1; index >= 0; index-- {
					cycle = append([]model.Edge{stack[index]}, cycle...)
					if stack[index].From == edge.To {
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
	nodes := make([]string, 0, len(choices))
	for component := range choices {
		nodes = append(nodes, component)
	}
	sort.Strings(nodes)
	for _, component := range nodes {
		if color[component] == 0 {
			visit(component)
		}
	}
	return cycles
}

func edgesFrom(choices map[string]string, edges map[string][]model.Edge, component string) []model.Edge {
	result := []model.Edge{}
	for _, incoming := range edges {
		for _, edge := range incoming {
			if edge.From == component && choices[edge.From] == edge.FromVersion {
				if _, active := choices[edge.To]; active {
					result = append(result, edge)
				}
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].To < result[j].To })
	return result
}

func copyMap(input map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range input {
		result[key] = value
	}
	return result
}

func copyBoolMap(input map[string]bool) map[string]bool {
	result := map[string]bool{}
	for key, value := range input {
		result[key] = value
	}
	return result
}
