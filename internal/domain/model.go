package domain

type Requirement struct {
	Component  string `json:"component"`
	Constraint string `json:"constraint"`
	Optional   bool   `json:"optional,omitempty"`
}

type Version struct {
	Version      string        `json:"version"`
	Requirements []Requirement `json:"requirements,omitempty"`
}

type Component struct {
	Name     string    `json:"name"`
	Versions []Version `json:"versions"`
}

type Manifest struct {
	Components []Component `json:"components"`
}

type MigrationGoal struct {
	Component string `json:"component"`
	Version   string `json:"version"`
}

type GoalStatus struct {
	Component string `json:"component"`
	Required  string `json:"required"`
	Selected  string `json:"selected,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
}

type SolveRequest struct {
	Manifest       Manifest          `json:"manifest"`
	Current        map[string]string `json:"current"`
	Target         string            `json:"target"`
	DesiredVersion string            `json:"desiredVersion"`
	Locks          map[string]string `json:"locks,omitempty"`
	Goals          []MigrationGoal   `json:"goals,omitempty"`
	Confirmed      map[string]string `json:"confirmed,omitempty"`
}

type Change struct {
	Component string `json:"component"`
	From      string `json:"from"`
	To        string `json:"to"`
	Reason    string `json:"reason"`
}

type ChainLink struct {
	From        string `json:"from"`
	FromVersion string `json:"fromVersion"`
	To          string `json:"to"`
	Constraint  string `json:"constraint"`
	Optional    bool   `json:"optional"`
}

type Check struct {
	Kind    string      `json:"kind"`
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Chain   []ChainLink `json:"chain,omitempty"`
}

type Report struct {
	Status          string            `json:"status"`
	Selection       map[string]string `json:"selection"`
	GoalStatuses    []GoalStatus      `json:"goalStatuses,omitempty"`
	Changes         []Change          `json:"changes"`
	ChangedCount    int               `json:"changedCount"`
	ConflictChain   []ChainLink       `json:"conflictChain,omitempty"`
	ConflictMessage string            `json:"conflictMessage,omitempty"`
	Checks          []Check           `json:"checks"`
	OptionalOmitted []string          `json:"optionalOmitted,omitempty"`
}

type Plan struct {
	ID      string       `json:"id"`
	Request SolveRequest `json:"request"`
	Report  Report       `json:"report"`
	// Verification is produced independently from the solver report, against
	// the manifest snapshot stored in Request. It must never be copied from
	// Report even when the solver marked the selection feasible.
	Verification Report `json:"verification"`
	Consistent   bool   `json:"consistent"`
	CreatedAt    string `json:"createdAt"`
}

// RouteStep is one executable stage of a migration route. State is the full
// running set after the step is applied; every state must verify on its own.
type RouteStep struct {
	Index        int               `json:"index"`
	Changes      []Change          `json:"changes"`
	State        map[string]string `json:"state"`
	Together     []string          `json:"together,omitempty"`
	Transition   []string          `json:"transition,omitempty"`
	Note         string            `json:"note,omitempty"`
	Checks       []StepCheck       `json:"checks,omitempty"`
	Verification Report            `json:"verification"`
	Rollback     RollbackInfo      `json:"rollback"`
	Confirmed    bool              `json:"confirmed,omitempty"`
	// Effective is true only when the step was confirmed AND every attached
	// check was recorded as a success by the user. A plan claiming success
	// is never enough on its own.
	Effective bool `json:"effective,omitempty"`
}

// StepCheck is a verification the user must perform after executing a step.
// Status is one of pending, success, failed or insufficient (资料不足).
// Recorded outcomes are append-only: a new route must never overwrite them.
type StepCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

// RouteOption is one way forward when a route stalls or a check fails.
// Kind is continue (keep current state and fix the remaining route),
// rollback (return to a still-compatible old state) or stop (halt here).
// Impacts lists what each choice still leaves unresolved.
type RouteOption struct {
	Kind    string            `json:"kind"`
	Summary string            `json:"summary"`
	State   map[string]string `json:"state"`
	Impacts []string          `json:"impacts"`
}

// RollbackInfo describes the compatible state a step can fall back to and
// the impacts that rolling back versions does not undo by itself.
type RollbackInfo struct {
	State        map[string]string `json:"state"`
	Irreversible []string          `json:"irreversible,omitempty"`
}

// RouteRequest asks for a staged route. Confirmed steps are kept verbatim:
// planning always resumes from the state they leave behind and never
// recomputes them into a different route.
type RouteRequest struct {
	Manifest       Manifest          `json:"manifest"`
	Current        map[string]string `json:"current"`
	Target         string            `json:"target,omitempty"`
	DesiredVersion string            `json:"desiredVersion,omitempty"`
	Goals          []MigrationGoal   `json:"goals,omitempty"`
	Locks          map[string]string `json:"locks,omitempty"`
	Confirmed      []RouteStep       `json:"confirmed,omitempty"`
}

type RouteReport struct {
	Status             string            `json:"status"`
	ResumedFrom        map[string]string `json:"resumedFrom"`
	ConfirmedSteps     []RouteStep       `json:"confirmedSteps,omitempty"`
	// HeldSteps are confirmed steps whose checks are not all successful;
	// they are preserved verbatim but planning does not treat them as done.
	HeldSteps          []RouteStep       `json:"heldSteps,omitempty"`
	Steps              []RouteStep       `json:"steps"`
	FinalSelection     map[string]string `json:"finalSelection,omitempty"`
	ConflictMessage    string            `json:"conflictMessage,omitempty"`
	RemainingConflicts []Check           `json:"remainingConflicts,omitempty"`
	Options            []RouteOption     `json:"options,omitempty"`
}

type SavedRoute struct {
	ID          string       `json:"id"`
	Fingerprint string       `json:"fingerprint,omitempty"`
	Request     RouteRequest `json:"request"`
	Report      RouteReport  `json:"report"`
	CreatedAt   string       `json:"createdAt"`
}

// ConfirmRequest records the user's check outcomes for one step of a saved
// route. Re-sending the identical record is a no-op (save interruptions must
// not re-execute one-time transformations); a different record for the same
// step is rejected because executed history cannot be overwritten.
type ConfirmRequest struct {
	RouteID   string      `json:"routeId"`
	StepIndex int         `json:"stepIndex"`
	Checks    []StepCheck `json:"checks"`
}
