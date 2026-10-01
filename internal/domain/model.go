package domain

import "sort"

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

type SolveRequest struct {
	Manifest       Manifest          `json:"manifest"`
	Current        map[string]string `json:"current"`
	Target         string            `json:"target"`
	DesiredVersion string            `json:"desiredVersion"`
	Locks          map[string]string `json:"locks,omitempty"`
	// Adopted carries component versions the user has already confirmed from
	// a previous plan. They behave like locks, but are reported as a separate
	// goal source so the page can distinguish confirmed from still-open goals.
	Adopted map[string]string `json:"adopted,omitempty"`
}

// Goal is a single migration objective derived from the request: the upgrade
// target, each manual lock and each adopted (confirmed) component.
type Goal struct {
	Component string `json:"component"`
	Desired   string `json:"desired"`
	Source    string `json:"source"` // target | lock | adopted
}

// GoalStatus reports how one goal ended up after solving or verification:
// satisfied, pending or unsatisfiable.
type GoalStatus struct {
	Goal   Goal   `json:"goal"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
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
	Changes         []Change          `json:"changes"`
	ChangedCount    int               `json:"changedCount"`
	ConflictChain   []ChainLink       `json:"conflictChain,omitempty"`
	ConflictMessage string            `json:"conflictMessage,omitempty"`
	Checks          []Check           `json:"checks"`
	OptionalOmitted []string          `json:"optionalOmitted,omitempty"`
	Goals           []GoalStatus      `json:"goals,omitempty"`
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
	// ManifestHash binds the stored conclusions to the exact manifest
	// snapshot they were computed from, so a conclusion can never be
	// borrowed by a different revision of the input data.
	ManifestHash string `json:"manifestHash"`
	CreatedAt    string `json:"createdAt"`
}

// Goals extracts the migration objectives from a solve request.
func (req SolveRequest) Goals() []Goal {
	goals := []Goal{}
	if req.Target != "" {
		goals = append(goals, Goal{Component: req.Target, Desired: req.DesiredVersion, Source: "target"})
	}
	for _, source := range []struct {
		name string
		in   map[string]string
	}{{"lock", req.Locks}, {"adopted", req.Adopted}} {
		names := make([]string, 0, len(source.in))
		for name := range source.in {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			goals = append(goals, Goal{Component: name, Desired: source.in[name], Source: source.name})
		}
	}
	return goals
}
