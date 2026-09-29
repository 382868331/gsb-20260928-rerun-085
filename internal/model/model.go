package model

type Catalog struct {
	Components []Component `json:"components"`
}

type Component struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Versions    []Version `json:"versions"`
}

type Version struct {
	Version      string       `json:"version"`
	Released     string       `json:"released,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

type Dependency struct {
	Component  string `json:"component"`
	Constraint string `json:"constraint"`
	Optional   bool   `json:"optional,omitempty"`
}

type Edge struct {
	From        string `json:"from"`
	FromVersion string `json:"fromVersion"`
	To          string `json:"to"`
	Constraint  string `json:"constraint"`
	Optional    bool   `json:"optional,omitempty"`
}

type SolveRequest struct {
	TargetComponent string            `json:"targetComponent"`
	TargetVersion   string            `json:"targetVersion"`
	Current         map[string]string `json:"current"`
	Locks           map[string]string `json:"locks"`
}

type Plan struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Current map[string]string `json:"current"`
	Choices map[string]string `json:"choices"`
	Target  string            `json:"target"`
}

type Change struct {
	Component  string `json:"component"`
	From       string `json:"from"`
	To         string `json:"to"`
	Reason     string `json:"reason"`
	IsTarget   bool   `json:"isTarget"`
	IsOptional bool   `json:"isOptional,omitempty"`
}

type Report struct {
	Satisfied bool        `json:"satisfied"`
	Changes   []Change    `json:"changes"`
	Choices   StringPairs `json:"choices"`
	Messages  []string    `json:"messages"`
	Conflicts []Conflict  `json:"conflicts,omitempty"`
	Cycles    [][]Edge    `json:"cycles,omitempty"`
}

type StringPairs map[string]string

type Conflict struct {
	Kind       string `json:"kind"`
	Component  string `json:"component,omitempty"`
	Version    string `json:"version,omitempty"`
	Constraint string `json:"constraint,omitempty"`
	Message    string `json:"message"`
	Chain      []Edge `json:"chain"`
}
