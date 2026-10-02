// Package route plans staged migration routes: an ordered list of steps where
// every intermediate state is independently verifiable against the manifest.
// A compatible start and end state does not imply any order works, so the
// planner searches for an executable order, allowing transitional versions
// and grouping components that must switch together.
package route

import (
	"fmt"
	"sort"
	"strings"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/semver"
	"dependency-workbench/internal/solver"
	"dependency-workbench/internal/verifier"
)

// Result is the outcome of planning a route from a given state.
type Result struct {
	Steps          []domain.RouteStep
	FinalSelection map[string]string
	Conflict       *domain.Report
}

// Plan solves the final selection for req starting from the running state
// from, then searches for an executable order of steps. Every returned step
// carries an independent verification of the state it lands in.
func Plan(req domain.SolveRequest, from map[string]string) Result {
	solveReq := req
	solveReq.Current = clone(from)
	solved := solver.Solve(solveReq)
	if solved.Status != "feasible" {
		return Result{Conflict: &solved}
	}
	start := clone(from)
	if report := verifier.CheckState(req.Manifest, start); HasErrors(report) {
		broken := report
		broken.ConflictMessage = "current state does not satisfy its own constraints: " + firstError(report)
		return Result{Conflict: &broken}
	}
	steps, ok := findRoute(req.Manifest, start, solved.Selection, solved.Changes)
	if !ok {
		report := domain.Report{
			Status:          "conflict",
			ConflictMessage: "no executable step order reaches the solved selection; every remaining order violates an intermediate constraint",
			Selection:       clone(solved.Selection),
			Checks:          []domain.Check{},
			Changes:         []domain.Change{},
		}
		return Result{Conflict: &report}
	}
	return Result{Steps: steps, FinalSelection: clone(solved.Selection)}
}

// HasErrors reports whether a verification report contains any error check.
func HasErrors(report domain.Report) bool {
	for _, check := range report.Checks {
		if check.Status == "error" {
			return true
		}
	}
	return false
}

func firstError(report domain.Report) string {
	for _, check := range report.Checks {
		if check.Status == "error" {
			return check.Message
		}
	}
	return ""
}

type move struct {
	assign map[string]string
}

type stepRec struct {
	move  move
	state map[string]string
}

func findRoute(manifest domain.Manifest, start, goal map[string]string, solvedChanges []domain.Change) ([]domain.RouteStep, bool) {
	versions := map[string][]domain.Version{}
	for _, component := range manifest.Components {
		versions[component.Name] = component.Versions
	}
	diff := diffComponents(start, goal)
	if len(diff) == 0 {
		return []domain.RouteStep{}, true
	}
	// First try single-component moves only; only if no executable order
	// exists, allow pairs of components to switch in one atomic step.
	recs, ok := bfs(manifest, start, goal, diff, versions, false)
	if !ok {
		recs, ok = bfs(manifest, start, goal, diff, versions, true)
	}
	if !ok {
		return nil, false
	}
	reasons := map[string]string{}
	for _, change := range solvedChanges {
		reasons[change.Component] = change.Reason
	}
	steps := make([]domain.RouteStep, 0, len(recs))
	previous := start
	for i, rec := range recs {
		step := domain.RouteStep{Index: i + 1, State: clone(rec.state)}
		names := make([]string, 0, len(rec.move.assign))
		for name := range rec.move.assign {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			from := previous[name]
			reason := reasons[name]
			if reason == "" {
				reason = "required dependency adjustment"
			}
			step.Changes = append(step.Changes, domain.Change{Component: name, From: from, To: rec.move.assign[name], Reason: reason})
		}
		step.Together = len(step.Changes) > 1
		for _, change := range step.Changes {
			if goal[change.Component] != change.To {
				step.Transitional = true
			}
		}
		step.Reason = explain(manifest, previous, step, goal)
		step.Verification = verifier.CheckState(manifest, rec.state)
		steps = append(steps, step)
		previous = rec.state
	}
	return steps, true
}

func diffComponents(start, goal map[string]string) []string {
	out := []string{}
	for name, version := range goal {
		if start[name] != version {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func bfs(manifest domain.Manifest, start, goal map[string]string, diff []string, versions map[string][]domain.Version, allowPairs bool) ([]stepRec, bool) {
	type node struct {
		state map[string]string
		steps []stepRec
	}
	depthCap := 2*len(diff) + 2
	queue := []node{{state: clone(start)}}
	visited := map[string]bool{stateKey(start): true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.steps) >= depthCap {
			continue
		}
		for _, mv := range moves(current.state, goal, diff, versions, allowPairs) {
			next := apply(current.state, mv)
			key := stateKey(next)
			if visited[key] {
				continue
			}
			visited[key] = true
			if HasErrors(verifier.CheckState(manifest, next)) {
				continue
			}
			steps := append(append([]stepRec{}, current.steps...), stepRec{move: mv, state: next})
			if reaches(next, goal) {
				return steps, true
			}
			queue = append(queue, node{state: next, steps: steps})
		}
	}
	return nil, false
}

// moves enumerates candidate next moves, goal versions first so the shortest
// and most direct route wins; transitional versions remain as fallbacks.
func moves(state, goal map[string]string, diff []string, versions map[string][]domain.Version, allowPairs bool) []move {
	out := []move{}
	for _, name := range diff {
		if state[name] == goal[name] {
			continue
		}
		ordered := orderVersions(versions[name], goal[name], state[name])
		for _, version := range ordered {
			out = append(out, move{assign: map[string]string{name: version}})
		}
	}
	if allowPairs {
		pending := []string{}
		for _, name := range diff {
			if state[name] != goal[name] {
				pending = append(pending, name)
			}
		}
		for i := 0; i < len(pending); i++ {
			for j := i + 1; j < len(pending); j++ {
				out = append(out, move{assign: map[string]string{pending[i]: goal[pending[i]], pending[j]: goal[pending[j]]}})
			}
		}
	}
	return out
}

func orderVersions(all []domain.Version, goal, current string) []string {
	out := []string{}
	for _, item := range all {
		if item.Version != current {
			out = append(out, item.Version)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i] == goal {
			return true
		}
		if out[j] == goal {
			return false
		}
		return distance(out[i], goal) < distance(out[j], goal)
	})
	return out
}

func distance(version, goal string) int {
	a, errA := semver.Parse(version)
	b, errB := semver.Parse(goal)
	if errA != nil || errB != nil {
		return 0
	}
	d := (a.Major - b.Major) * 1_000_000
	d += (a.Minor - b.Minor) * 1_000
	d += a.Patch - b.Patch
	if d < 0 {
		return -d
	}
	return d
}

func reaches(state, goal map[string]string) bool {
	for name, version := range goal {
		if state[name] != version {
			return false
		}
	}
	return true
}

func apply(state map[string]string, mv move) map[string]string {
	next := clone(state)
	for name, version := range mv.assign {
		next[name] = version
	}
	return next
}

func stateKey(state map[string]string) string {
	names := make([]string, 0, len(state))
	for name := range state {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"@"+state[name])
	}
	return strings.Join(parts, "|")
}

// explain derives a human readable reason for a step: why a transitional
// version is needed, or why components must switch together.
func explain(manifest domain.Manifest, previous map[string]string, step domain.RouteStep, goal map[string]string) string {
	if step.Together {
		parts := []string{}
		names := []string{}
		for _, change := range step.Changes {
			names = append(names, change.Component)
			solo := apply(previous, move{assign: map[string]string{change.Component: change.To}})
			if report := verifier.CheckState(manifest, solo); HasErrors(report) {
				parts = append(parts, fmt.Sprintf("upgrading %s alone breaks: %s", change.Component, firstError(report)))
			}
		}
		if len(parts) == 0 {
			return strings.Join(names, " and ") + " must switch in one atomic step"
		}
		return strings.Join(names, " and ") + " must switch together: " + strings.Join(parts, "; ")
	}
	if step.Transitional && len(step.Changes) == 1 {
		change := step.Changes[0]
		direct := apply(previous, move{assign: map[string]string{change.Component: goal[change.Component]}})
		if report := verifier.CheckState(manifest, direct); HasErrors(report) {
			return fmt.Sprintf("%s cannot go directly to %s (%s); staging via transitional %s", change.Component, goal[change.Component], firstError(report), change.To)
		}
		return fmt.Sprintf("%s stages via transitional %s before reaching %s", change.Component, change.To, goal[change.Component])
	}
	return ""
}

// AnalyzeRollback lists the confirmed intermediate states a blocked route can
// fall back to. A step is not directly revertible when later confirmed steps
// depend on its changes, so undoing it would strand those later states.
func AnalyzeRollback(manifest domain.Manifest, start map[string]string, steps []domain.RouteStep, confirmed int) []domain.RollbackPoint {
	if confirmed > len(steps) {
		confirmed = len(steps)
	}
	points := []domain.RollbackPoint{}
	for k := 0; k < confirmed; k++ {
		point := domain.RollbackPoint{Step: steps[k].Index, State: clone(steps[k].State), Revertible: true}
		// Rebuild the state as if step k had never happened but every later
		// confirmed step still applied its changes.
		base := clone(start)
		if k > 0 {
			base = clone(steps[k-1].State)
		}
		for later := k + 1; later < confirmed; later++ {
			for _, change := range steps[later].Changes {
				base[change.Component] = change.To
			}
		}
		if report := verifier.CheckState(manifest, base); HasErrors(report) {
			point.Revertible = false
			point.Reason = fmt.Sprintf("later confirmed steps depend on this change; undoing it breaks: %s", firstError(report))
		}
		points = append(points, point)
	}
	return points
}

func clone(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
