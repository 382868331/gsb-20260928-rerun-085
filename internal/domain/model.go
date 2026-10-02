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

// RouteStep is one executable stage of a migration route. Applying Changes to
// the previous state yields State, which must pass independent verification
// on its own before the next step may start.
type RouteStep struct {
	Index        int               `json:"index"`
	Changes      []Change          `json:"changes"`
	State        map[string]string `json:"state"`
	Verification Report            `json:"verification"`
	Transitional bool              `json:"transitional,omitempty"`
	Together     bool              `json:"together,omitempty"`
	Reason       string            `json:"reason,omitempty"`
}

// RollbackPoint describes a confirmed intermediate state the route can fall
// back to, and whether reverting that step would break later confirmed steps.
type RollbackPoint struct {
	Step       int               `json:"step"`
	State      map[string]string `json:"state"`
	Revertible bool              `json:"revertible"`
	Reason     string            `json:"reason,omitempty"`
}

// RouteRevision records a goal change made after some steps were confirmed,
// so a reopened route can explain why remaining steps were replanned.
type RouteRevision struct {
	ConfirmedSteps int               `json:"confirmedSteps"`
	Goals          []MigrationGoal   `json:"goals"`
	Locks          map[string]string `json:"locks,omitempty"`
	Note           string            `json:"note"`
	At             string            `json:"at"`
}

// Route is a persisted staged migration plan. Confirmed steps are frozen
// history: replanning after a goal change only ever recomputes the steps
// after Confirmed, starting from the confirmed state.
type Route struct {
	ID              string            `json:"id"`
	Request         SolveRequest      `json:"request"`
	Steps           []RouteStep       `json:"steps"`
	Confirmed       int               `json:"confirmed"`
	Status          string            `json:"status"`
	FinalSelection  map[string]string `json:"finalSelection,omitempty"`
	ConflictMessage string            `json:"conflictMessage,omitempty"`
	ConflictChain   []ChainLink       `json:"conflictChain,omitempty"`
	Rollback        []RollbackPoint   `json:"rollback,omitempty"`
	Revisions       []RouteRevision   `json:"revisions,omitempty"`
	CreatedAt       string            `json:"createdAt"`
	UpdatedAt       string            `json:"updatedAt"`
}
