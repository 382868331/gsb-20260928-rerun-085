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

type SolveRequest struct {
	Manifest       Manifest          `json:"manifest"`
	Current        map[string]string `json:"current"`
	Target         string            `json:"target"`
	DesiredVersion string            `json:"desiredVersion"`
	Locks          map[string]string `json:"locks,omitempty"`
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
}

type Plan struct {
	ID        string       `json:"id"`
	Request   SolveRequest `json:"request"`
	Report    Report       `json:"report"`
	CreatedAt string       `json:"createdAt"`
}
