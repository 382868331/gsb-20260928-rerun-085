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

// Plan computes a staged route from the current running set to the migration
// goals. Confirmed steps are replayed verbatim and never recomputed; only the
// remaining changes are planned, starting from the state they leave behind.
func Plan(req domain.RouteRequest) domain.RouteReport {
	if len(req.Manifest.Components) == 0 {
		return failRoute(req, "manifest is empty", nil)
	}
	goals := normalizedGoals(req)
	if len(goals) == 0 {
		return failRoute(req, "缺少迁移目标：请提供升级组件与目标版本", nil)
	}

	state := cloneMap(req.Current)
	confirmed := make([]domain.RouteStep, 0, len(req.Confirmed))
	for i, step := range req.Confirmed {
		if len(step.Checks) > 0 && !step.Executed {
			// A planned-but-not-executed step proves nothing; stop replaying
			// and keep planning from the last genuinely completed state.
			break
		}
		failed, insufficient := checkOutcome(step)
		if failed != "" && !step.AcceptedFailure {
			state = applyChanges(state, step.Changes)
			step.Index = i + 1
			step.State = cloneMap(state)
			step.Confirmed = true
			step.Verification = verifyState(req.Manifest, state, stepGoals(goals, state, step.Changes))
			confirmed = append(confirmed, step)
			return blockedReport(req, confirmed, step, failed)
		}
		if insufficient != "" {
			// Not enough evidence: the step stays pending and the remaining
			// route is planned from the state before it.
			pending := step
			pending.Index = i + 1
			report := Plan(domain.RouteRequest{
				Manifest:       req.Manifest,
				Current:        cloneMap(state),
				Target:         req.Target,
				DesiredVersion: req.DesiredVersion,
				Goals:          req.Goals,
				Locks:          req.Locks,
			})
			report.Status = "pending"
			report.ConfirmedSteps = confirmed
			report.PendingStep = &pending
			report.ConflictMessage = fmt.Sprintf("第 %d 步的核对 %q 资料不足：该步保持待确认，不能当作已生效", i+1, insufficient)
			for j := range report.Steps {
				report.Steps[j].Index = len(confirmed) + 1 + j + 1
			}
			return report
		}
		previous := cloneMap(state)
		state = applyChanges(state, step.Changes)
		if provided := step.State; len(provided) > 0 && !sameState(state, provided) {
			return failRoute(req, fmt.Sprintf("已确认的第 %d 步与当前状态不一致：历史步骤不能被重算成另一条路线", i+1), nil)
		}
		step.Index = i + 1
		step.State = cloneMap(state)
		step.Confirmed = true
		step.Verification = verifyState(req.Manifest, state, stepGoals(goals, state, step.Changes))
		if step.Verification.Status != "feasible" {
			report := failRoute(req, fmt.Sprintf("已确认的第 %d 步留下的中间状态无法通过独立核验", i+1), step.Verification.Checks)
			report.ConfirmedSteps = confirmed
			report.ResumedFrom = cloneMap(previous)
			return report
		}
		confirmed = append(confirmed, step)
	}

	solveReport := solver.Solve(domain.SolveRequest{
		Manifest:       req.Manifest,
		Current:        cloneMap(state),
		Target:         req.Target,
		DesiredVersion: req.DesiredVersion,
		Goals:          req.Goals,
		Locks:          req.Locks,
	})
	if solveReport.Status != "feasible" {
		report := failRoute(req, "从当前状态找不到通往目标的路线："+solveReport.ConflictMessage, errorChecks(solveReport.Checks))
		report.ConfirmedSteps = confirmed
		report.ResumedFrom = state
		return report
	}
	final := solveReport.Selection

	steps := []domain.RouteStep{}
	index := len(confirmed)
	for {
		pending := pendingChanges(state, final)
		if len(pending) == 0 {
			break
		}
		combo, ok := findCombo(req.Manifest, state, final, pending, goals)
		if !ok {
			report := domain.RouteReport{
				Status:             "conflict",
				ResumedFrom:        cloneMap(state),
				ConfirmedSteps:     confirmed,
				Steps:              steps,
				FinalSelection:     cloneMap(final),
				ConflictMessage:    "剩余改动无法排出可执行的下一步：任意组合都会破坏当前运行组件的约束",
				RemainingConflicts: remainingConflicts(req.Manifest, state, pending),
			}
			return report
		}
		index++
		step := buildStep(req.Manifest, index, state, combo, final, goals)
		steps = append(steps, step)
		state = step.State
	}
	return domain.RouteReport{
		Status:         "feasible",
		ResumedFrom:    cloneMap(state),
		ConfirmedSteps: confirmed,
		Steps:          steps,
		FinalSelection: cloneMap(final),
	}
}

func normalizedGoals(req domain.RouteRequest) []domain.MigrationGoal {
	goals := append([]domain.MigrationGoal{}, req.Goals...)
	if req.Target != "" && req.DesiredVersion != "" {
		found := false
		for _, goal := range goals {
			if goal.Component == req.Target && goal.Version == req.DesiredVersion {
				found = true
			}
		}
		if !found {
			goals = append(goals, domain.MigrationGoal{Component: req.Target, Version: req.DesiredVersion})
		}
	}
	return goals
}

// pendingChanges lists upgrades, additions and exits still needed to reach the
// final selection from the current state.
func pendingChanges(state, final map[string]string) []domain.Change {
	out := []domain.Change{}
	names := make([]string, 0, len(final))
	for name := range final {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if state[name] != final[name] {
			out = append(out, domain.Change{Component: name, From: state[name], To: final[name]})
		}
	}
	removed := make([]string, 0)
	for name := range state {
		if _, ok := final[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	for _, name := range removed {
		out = append(out, domain.Change{Component: name, From: state[name], To: ""})
	}
	return out
}

// findCombo searches the smallest set of pending changes that can be applied
// together while keeping every running component's constraints satisfied.
// Final versions are tried first; only when no combination works does it fall
// back to transition versions from the manifest.
func findCombo(manifest domain.Manifest, state, final map[string]string, pending []domain.Change, goals []domain.MigrationGoal) ([]domain.Change, bool) {
	finalPending := make([]domain.Change, 0, len(pending))
	for _, change := range pending {
		if allowedJump(change.From, change.To) {
			finalPending = append(finalPending, change)
		}
	}
	for size := 1; size <= 3 && size <= len(pending); size++ {
		if combo, ok := searchCombos(manifest, state, goals, finalPending, size, nil); ok {
			return combo, true
		}
	}
	versions := map[string][]domain.Version{}
	for _, component := range manifest.Components {
		versions[component.Name] = component.Versions
	}
	for size := 1; size <= 2 && size <= len(pending); size++ {
		var best []domain.Change
		bestMovement := -1
		var try func(start int, picked []domain.Change)
		try = func(start int, picked []domain.Change) {
			if len(picked) == size {
				expanded := [][]domain.Change{{}}
				for _, change := range picked {
					if change.To == "" {
						for i := range expanded {
							expanded[i] = append(expanded[i], change)
						}
						continue
					}
					next := [][]domain.Change{}
					for _, item := range versions[change.Component] {
						if item.Version == state[change.Component] {
							continue
						}
						if !allowedJump(state[change.Component], item.Version) {
							continue
						}
						for _, base := range expanded {
							candidate := append(append([]domain.Change{}, base...), domain.Change{Component: change.Component, From: change.From, To: item.Version})
							next = append(next, candidate)
						}
					}
					expanded = next
				}
				for _, candidate := range expanded {
					candidateState := applyChanges(state, candidate)
					if verifyState(manifest, candidateState, stepGoals(goals, candidateState, candidate)).Status != "feasible" {
						continue
					}
					movement := 0
					for _, change := range candidate {
						if change.To != "" {
							movement += rankOf(change.To)
						}
					}
					if bestMovement == -1 || movement < bestMovement {
						bestMovement = movement
						best = candidate
					}
				}
				return
			}
			for i := start; i < len(pending); i++ {
				try(i+1, append(picked, pending[i]))
			}
		}
		try(0, nil)
		if best != nil {
			return best, true
		}
	}
	return nil, false
}

// allowedJump keeps a single step executable in practice: one step may cross
// at most one major version per component, so big upgrades must pass through
// transition versions instead of skipping them.
func allowedJump(from, to string) bool {
	if from == "" || to == "" {
		return true
	}
	fromV, errFrom := semver.Parse(from)
	toV, errTo := semver.Parse(to)
	if errFrom != nil || errTo != nil {
		return false
	}
	diff := toV.Major - fromV.Major
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1
}

func searchCombos(manifest domain.Manifest, state map[string]string, goals []domain.MigrationGoal, pending []domain.Change, size int, _ any) ([]domain.Change, bool) {
	var found []domain.Change
	var try func(start int, picked []domain.Change)
	try = func(start int, picked []domain.Change) {
		if found != nil {
			return
		}
		if len(picked) == size {
			candidateState := applyChanges(state, picked)
			if verifyState(manifest, candidateState, stepGoals(goals, candidateState, picked)).Status == "feasible" {
				found = append([]domain.Change{}, picked...)
			}
			return
		}
		for i := start; i < len(pending); i++ {
			try(i+1, append(picked, pending[i]))
		}
	}
	try(0, nil)
	return found, found != nil
}

func buildStep(manifest domain.Manifest, index int, state map[string]string, combo []domain.Change, final map[string]string, goals []domain.MigrationGoal) domain.RouteStep {
	next := applyChanges(state, combo)
	step := domain.RouteStep{
		Index:   index,
		Changes: combo,
		State:   cloneMap(next),
		Rollback: domain.RollbackInfo{
			State:        cloneMap(state),
			Irreversible: irreversible(combo),
		},
	}
	notes := []string{}
	if len(combo) > 1 {
		names := []string{}
		for _, change := range combo {
			names = append(names, change.Component)
		}
		step.Together = names
		notes = append(notes, strings.Join(names, "、")+" 必须一起切换：单独切换任一方都会破坏相互约束")
	}
	transition := []string{}
	for _, change := range combo {
		if change.To == "" {
			notes = append(notes, fmt.Sprintf("%s 退出运行状态：后续组件不再依赖它", change.Component))
			continue
		}
		if final[change.Component] != change.To {
			transition = append(transition, change.Component)
			notes = append(notes, fmt.Sprintf("%s 需经过渡版本 %s：单步不允许跨多个大版本，且直接升到 %s 会破坏当前运行组件的约束", change.Component, change.To, final[change.Component]))
		}
	}
	step.Transition = transition
	step.Note = strings.Join(notes, "；")
	step.Checks = plannedChecks(combo)
	step.Verification = verifyState(manifest, next, stepGoals(goals, next, combo))
	return step
}

// plannedChecks attaches the verifications an operator must record after
// executing a step: startup health for every changed component, plus data
// migration or re-attachment checks for irreversible conversions.
func plannedChecks(combo []domain.Change) []domain.StepCheck {
	checks := []domain.StepCheck{}
	for _, change := range combo {
		if change.To == "" {
			checks = append(checks, domain.StepCheck{
				Name:   change.Component + " 退出核对",
				Expect: change.Component + " 的数据与配置已备份，或已确认可由后续组件重新接入",
			})
			continue
		}
		checks = append(checks, domain.StepCheck{
			Name:   change.Component + " 启动核对",
			Expect: change.Component + "@" + change.To + " 启动并通过健康检查",
		})
		if change.From != "" {
			from, errFrom := semver.Parse(change.From)
			to, errTo := semver.Parse(change.To)
			if errFrom == nil && errTo == nil && to.Major > from.Major {
				checks = append(checks, domain.StepCheck{
					Name:   change.Component + " 数据迁移核对",
					Expect: fmt.Sprintf("%s %s → %s 的一次性数据转换已完成且旧数据可读取；该转换不可直接撤销", change.Component, change.From, change.To),
				})
			}
		}
	}
	return checks
}

// stepGoals verifies an intermediate state against the final goals it already
// satisfies; before any final goal is reached, the step's own changes act as
// the coverage target so an empty proof is never accepted.
func stepGoals(goals []domain.MigrationGoal, state map[string]string, changes []domain.Change) []domain.MigrationGoal {
	covered := []domain.MigrationGoal{}
	for _, goal := range goals {
		if state[goal.Component] == goal.Version {
			covered = append(covered, goal)
		}
	}
	if len(covered) > 0 {
		return covered
	}
	for _, change := range changes {
		if change.To != "" {
			covered = append(covered, domain.MigrationGoal{Component: change.Component, Version: change.To})
		}
	}
	return covered
}

func verifyState(manifest domain.Manifest, state map[string]string, goals []domain.MigrationGoal) domain.Report {
	return verifier.VerifyRequest(domain.SolveRequest{Manifest: manifest, Goals: goals}, state)
}

// checkOutcome inspects the operator records of an executed step and returns
// the first failed check name and the first check name whose evidence is
// missing or marked insufficient-data.
func checkOutcome(step domain.RouteStep) (failed string, insufficient string) {
	results := map[string]string{}
	for _, record := range step.Records {
		results[record.Name] = record.Result
	}
	for _, check := range step.Checks {
		switch results[check.Name] {
		case "failure":
			if failed == "" {
				failed = check.Name
			}
		case "success":
		default:
			if insufficient == "" {
				insufficient = check.Name
			}
		}
	}
	return failed, insufficient
}

// blockedReport explains the choices once an executed step's check failed:
// keep the current state and fix the remaining route, fall back to the last
// compatible state, or stop — each with the impacts it does not resolve.
func blockedReport(req domain.RouteRequest, confirmed []domain.RouteStep, step domain.RouteStep, failed string) domain.RouteReport {
	irreversible := append([]string{}, step.Rollback.Irreversible...)
	unresolvedContinue := append([]string{fmt.Sprintf("核对 %q 记录为失败，问题本身仍未解决", failed)}, irreversible...)
	unresolvedStop := append([]string{}, unresolvedContinue...)
	unresolvedRollback := append([]string{}, irreversible...)
	if len(unresolvedRollback) == 0 {
		unresolvedRollback = append(unresolvedRollback, "回退只恢复版本选择，执行期间产生的其他副作用需人工确认")
	}
	return domain.RouteReport{
		Status:         "blocked",
		ResumedFrom:    cloneMap(step.State),
		ConfirmedSteps: confirmed,
		Steps:          []domain.RouteStep{},
		ConflictMessage: fmt.Sprintf("第 %d 步已执行且包含不可直接撤回的转换，核对 %q 失败：版本回退不会撤销已发生的数据转换",
			step.Index, failed),
		Options: []domain.RouteOption{
			{
				Kind:       "continue",
				Title:      "保留当前状态，修正后续路线",
				State:      cloneMap(step.State),
				Unresolved: unresolvedContinue,
			},
			{
				Kind:       "rollback",
				Title:      "回到仍兼容的旧状态（仅恢复版本选择）",
				State:      cloneMap(step.Rollback.State),
				Unresolved: unresolvedRollback,
			},
			{
				Kind:       "stop",
				Title:      "停止处理，保留现场等待人工决策",
				State:      cloneMap(step.State),
				Unresolved: unresolvedStop,
			},
		},
	}
}

func remainingConflicts(manifest domain.Manifest, state map[string]string, pending []domain.Change) []domain.Check {
	out := []domain.Check{}
	for _, change := range pending {
		candidate := applyChanges(state, []domain.Change{change})
		report := verifyState(manifest, candidate, []domain.MigrationGoal{{Component: change.Component, Version: change.To}})
		for _, check := range report.Checks {
			if check.Status == "error" {
				out = append(out, check)
			}
		}
	}
	return out
}

func irreversible(changes []domain.Change) []string {
	out := []string{}
	for _, change := range changes {
		if change.To == "" {
			out = append(out, fmt.Sprintf("%s 已退出运行状态：回退只恢复版本选择，其数据与配置需手动重新接入", change.Component))
			continue
		}
		if change.From == "" {
			continue
		}
		from, errFrom := semver.Parse(change.From)
		to, errTo := semver.Parse(change.To)
		if errFrom == nil && errTo == nil && to.Major > from.Major {
			out = append(out, fmt.Sprintf("%s 跨大版本升级（%s → %s）：可能已执行数据迁移，回退版本不会自动撤销数据变更", change.Component, change.From, change.To))
		}
	}
	return out
}

func applyChanges(state map[string]string, changes []domain.Change) map[string]string {
	out := cloneMap(state)
	for _, change := range changes {
		if change.To == "" {
			delete(out, change.Component)
		} else {
			out[change.Component] = change.To
		}
	}
	return out
}

func sameState(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, version := range a {
		if b[name] != version {
			return false
		}
	}
	return true
}

func rankOf(version string) int {
	v, err := semver.Parse(version)
	if err != nil {
		return 0
	}
	return v.Major*1_000_000 + v.Minor*1_000 + v.Patch
}

func errorChecks(checks []domain.Check) []domain.Check {
	out := []domain.Check{}
	for _, check := range checks {
		if check.Status == "error" {
			out = append(out, check)
		}
	}
	return out
}

func failRoute(req domain.RouteRequest, message string, checks []domain.Check) domain.RouteReport {
	return domain.RouteReport{
		Status:             "conflict",
		ResumedFrom:        cloneMap(req.Current),
		Steps:              []domain.RouteStep{},
		ConflictMessage:    message,
		RemainingConflicts: checks,
	}
}

func cloneMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}
