package solver

import (
	"fmt"
	"sort"
	"strings"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/semver"
	"dependency-workbench/internal/verifier"
)

type edge struct {
	link domain.ChainLink
}

type state struct {
	current             map[string]string
	locks               map[string]string
	versions            map[string][]domain.Version
	assignment          map[string]string
	edges               map[string][]edge
	required            map[string]bool
	optionalOmitted     map[string]bool
	conflictChain       []domain.ChainLink
	conflictMessage     string
	propagationConflict *edge
	bestCount           int
	bestMovement        int
}

func Solve(req domain.SolveRequest) domain.Report {
	if err := validate(req); err != nil {
		return fail(err.Error(), nil)
	}
	s := newState(req)
	if !s.versionExists(req.Target, req.DesiredVersion) {
		return fail(fmt.Sprintf("target version %s %s does not exist in the manifest", req.Target, req.DesiredVersion), nil)
	}
	for name, version := range req.Locks {
		if !s.versionExists(name, version) {
			return fail(fmt.Sprintf("locked version %s %s does not exist in the manifest", name, version), nil)
		}
	}
	s.required[req.Target] = true
	s.assign(req.Target, req.DesiredVersion)
	s.edges[req.Target] = []edge{{link: domain.ChainLink{From: "upgrade request", To: req.Target, Constraint: "=" + req.DesiredVersion}}}
	for name, version := range req.Locks {
		s.required[name] = true
		s.assign(name, version)
		s.edges[name] = append(s.edges[name], edge{link: domain.ChainLink{From: "manual lock", To: name, Constraint: "=" + version}})
	}
	s.bestCount = len(req.Current) + 1
	s.bestMovement = int(^uint(0) >> 1)
	best, changes, ok := s.search()
	if !ok {
		return fail(s.conflictMessage, s.conflictChain)
	}
	report := buildReport(req, best, changes, s)
	roots := []domain.Requirement{{Component: req.Target, Constraint: "=" + req.DesiredVersion}}
	for name, version := range req.Locks {
		roots = append(roots, domain.Requirement{Component: name, Constraint: "=" + version})
	}
	verified := verifier.Verify(req.Manifest, report.Selection, roots...)
	report.Checks = verified.Checks
	if verified.Status == "conflict" {
		report.Status = "conflict"
		for _, check := range verified.Checks {
			if check.Status == "error" {
				report.ConflictMessage = check.Message
				report.ConflictChain = check.Chain
				break
			}
		}
	}
	return report
}

func (s *state) versionExists(name, version string) bool {
	for _, item := range s.versions[name] {
		if item.Version == version {
			return true
		}
	}
	return false
}

func validate(req domain.SolveRequest) error {
	if len(req.Manifest.Components) == 0 {
		return fmt.Errorf("manifest is empty")
	}
	if req.Target == "" {
		return fmt.Errorf("target component is required")
	}
	if _, err := semver.Parse(req.DesiredVersion); err != nil {
		return err
	}
	for name, version := range req.Current {
		if _, err := semver.Parse(version); err != nil {
			return fmt.Errorf("current version for %s: %w", name, err)
		}
	}
	for name, version := range req.Locks {
		if _, err := semver.Parse(version); err != nil {
			return fmt.Errorf("locked version for %s: %w", name, err)
		}
	}
	return nil
}

func newState(req domain.SolveRequest) *state {
	s := &state{
		current:         req.Current,
		locks:           req.Locks,
		versions:        map[string][]domain.Version{},
		assignment:      map[string]string{},
		edges:           map[string][]edge{},
		required:        map[string]bool{},
		optionalOmitted: map[string]bool{},
	}
	for _, component := range req.Manifest.Components {
		sorted := append([]domain.Version(nil), component.Versions...)
		sort.SliceStable(sorted, func(i, j int) bool {
			a, _ := semver.Parse(sorted[i].Version)
			b, _ := semver.Parse(sorted[j].Version)
			return semver.Compare(a, b) < 0
		})
		s.versions[component.Name] = sorted
	}
	return s
}

func (s *state) search() (map[string]string, []domain.Change, bool) {
	if s.propagationConflict != nil {
		bad := *s.propagationConflict
		s.conflictChain = s.pathFor(bad.link.To)
		if len(s.conflictChain) == 0 {
			s.conflictChain = []domain.ChainLink{bad.link}
		}
		s.conflictMessage = fmt.Sprintf("selected %s %s does not satisfy %s from %s %s", bad.link.To, s.assignment[bad.link.To], bad.link.Constraint, bad.link.From, bad.link.FromVersion)
		return nil, nil, false
	}
	changed, movement := 0, 0
	for name := range s.required {
		if cur := s.current[name]; cur != "" && s.assignment[name] != cur {
			changed++
			movement += rank(s.assignment[name])
		}
	}
	if changed > s.bestCount {
		return nil, nil, false
	}
	domains := map[string][]domain.Version{}
	for name := range s.required {
		if _, done := s.assignment[name]; done {
			continue
		}
		domainValues, chain, message := s.domain(name)
		if len(domainValues) == 0 {
			s.conflictChain, s.conflictMessage = chain, message
			return nil, nil, false
		}
		domains[name] = domainValues
	}
	if len(domains) == 0 {
		if changed == s.bestCount && movement >= s.bestMovement {
			return nil, nil, false
		}
		s.bestCount, s.bestMovement = changed, movement
		return clone(s.assignment), s.changes(), true
	}
	names := make([]string, 0, len(domains))
	for name := range domains {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(domains[names[i]]) != len(domains[names[j]]) {
			return len(domains[names[i]]) < len(domains[names[j]])
		}
		return names[i] < names[j]
	})
	name := names[0]
	candidates := s.order(name, domains[name])
	var best map[string]string
	var bestChanges []domain.Change
	for _, candidate := range candidates {
		s.assign(name, candidate.Version)
		got, gotChanges, ok := s.search()
		s.undo(name)
		if ok {
			best, bestChanges = got, gotChanges
			if s.bestCount == 0 {
				break
			}
		}
	}
	return best, bestChanges, best != nil
}

func (s *state) domain(name string) ([]domain.Version, []domain.ChainLink, string) {
	all, exists := s.versions[name]
	if !exists {
		return nil, chains(s.edges[name]), fmt.Sprintf("required component %q is not present in the manifest", name)
	}
	out := append([]domain.Version(nil), all...)
	for _, item := range s.edges[name] {
		constraint := strings.TrimPrefix(item.link.Constraint, "=")
		filtered := out[:0]
		for _, version := range out {
			v, _ := semver.Parse(version.Version)
			if semver.Satisfies(v, constraint) {
				filtered = append(filtered, version)
			}
		}
		out = append([]domain.Version(nil), filtered...)
		if len(out) == 0 {
			return nil, chains(s.edges[name]), fmt.Sprintf("no version of %q satisfies %s from %s %s", name, item.link.Constraint, item.link.From, item.link.FromVersion)
		}
	}
	return out, chains(s.edges[name]), ""
}

func (s *state) order(name string, candidates []domain.Version) []domain.Version {
	locked := s.locks[name]
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i].Version, candidates[j].Version
		if locked != "" {
			if a == locked {
				return true
			}
			if b == locked {
				return false
			}
		}
		av, _ := semver.Parse(a)
		bv, _ := semver.Parse(b)
		return semver.Compare(av, bv) < 0
	})
	return candidates
}

func rank(version string) int {
	v, _ := semver.Parse(version)
	return v.Major*1_000_000 + v.Minor*1_000 + v.Patch
}

func (s *state) assign(name, version string) {
	s.assignment[name] = version
	for _, componentVersion := range s.versions[name] {
		if componentVersion.Version != version {
			continue
		}
		for _, req := range componentVersion.Requirements {
			if req.Optional {
				s.optionalOmitted[name+" -> "+req.Component] = true
				continue
			}
			if selected, done := s.assignment[req.Component]; done {
				v, _ := semver.Parse(selected)
				if !semver.Satisfies(v, req.Constraint) {
					bad := edge{link: domain.ChainLink{From: name, FromVersion: version, To: req.Component, Constraint: req.Constraint, Optional: req.Optional}}
					s.propagationConflict = &bad
				}
			}
			link := domain.ChainLink{From: name, FromVersion: version, To: req.Component, Constraint: req.Constraint, Optional: req.Optional}
			s.edges[req.Component] = append(s.edges[req.Component], edge{link: link})
			s.required[req.Component] = true
		}
	}
}

func (s *state) undo(name string) {
	version := s.assignment[name]
	s.propagationConflict = nil
	delete(s.assignment, name)
	for _, componentVersion := range s.versions[name] {
		if componentVersion.Version != version {
			continue
		}
		for _, req := range componentVersion.Requirements {
			delete(s.optionalOmitted, name+" -> "+req.Component)
			edges := s.edges[req.Component]
			for i := len(edges) - 1; i >= 0; i-- {
				if edges[i].link.From == name && edges[i].link.FromVersion == version {
					s.edges[req.Component] = append(edges[:i], edges[i+1:]...)
					break
				}
			}
			if len(s.edges[req.Component]) == 0 {
				delete(s.edges, req.Component)
				delete(s.required, req.Component)
			}
		}
	}
}

func (s *state) changes() []domain.Change {
	out := []domain.Change{}
	for name := range s.required {
		cur := s.current[name]
		if cur == "" || cur == s.assignment[name] {
			continue
		}
		reason := "required dependency adjustment"
		if len(s.edges[name]) > 0 {
			reason = "required by " + s.edges[name][0].link.From
		}
		out = append(out, domain.Change{Component: name, From: cur, To: s.assignment[name], Reason: reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out
}

func (s *state) pathFor(destination string) []domain.ChainLink {
	outgoing := map[string][]domain.ChainLink{}
	for _, items := range s.edges {
		for _, item := range items {
			if item.link.From == "upgrade request" || item.link.From == "manual lock" {
				continue
			}
			outgoing[item.link.From] = append(outgoing[item.link.From], item.link)
		}
	}
	roots := make([]string, 0, len(s.edges))
	for name, items := range s.edges {
		for _, item := range items {
			if item.link.From == "upgrade request" || item.link.From == "manual lock" {
				roots = append(roots, name)
				break
			}
		}
	}
	sort.Strings(roots)
	type node struct {
		name string
		path []domain.ChainLink
	}
	seen := map[string]bool{}
	queue := []node{}
	for _, root := range roots {
		queue = append(queue, node{root, nil})
		seen[root] = true
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, link := range outgoing[current.name] {
			nextPath := append(append([]domain.ChainLink{}, current.path...), link)
			if link.To == destination {
				return nextPath
			}
			if !seen[link.To] {
				seen[link.To] = true
				queue = append(queue, node{link.To, nextPath})
			}
		}
	}
	return nil
}

func buildReport(req domain.SolveRequest, selection map[string]string, changes []domain.Change, s *state) domain.Report {
	for i := range changes {
		switch {
		case changes[i].Component == req.Target:
			changes[i].Reason = "requested upgrade"
		case s.locks[changes[i].Component] != "":
			changes[i].Reason = "manual lock"
		case len(s.edges[changes[i].Component]) > 0:
			item := s.edges[changes[i].Component][0]
			changes[i].Reason = "required by " + item.link.From + " " + item.link.FromVersion + " (" + item.link.Constraint + ")"
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Component == req.Target {
			return true
		}
		if changes[j].Component == req.Target {
			return false
		}
		return changes[i].Component < changes[j].Component
	})
	return domain.Report{Status: "feasible", Selection: selection, Changes: changes, ChangedCount: len(changes), OptionalOmitted: keys(s.optionalOmitted)}
}

func clone(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func keys(in map[string]bool) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func chains(in []edge) []domain.ChainLink {
	out := []domain.ChainLink{}
	for _, e := range in {
		out = append(out, e.link)
	}
	return out
}

func fail(message string, chain []domain.ChainLink) domain.Report {
	return domain.Report{Status: "conflict", ConflictMessage: message, ConflictChain: chain, Changes: []domain.Change{}, Selection: map[string]string{}, Checks: []domain.Check{}}
}
