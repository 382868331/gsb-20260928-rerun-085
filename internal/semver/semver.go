package semver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Version struct{ Major, Minor, Patch int }

type Comparator struct {
	Op      string
	Version Version
}

var tokenRE = regexp.MustCompile(`(>=|<=|>|<|=|\^)?\s*(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)`)

func Parse(value string) (Version, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q must be major.minor.patch", value)
	}
	nums := [3]int{}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("invalid version %q", value)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}

func ParseConstraint(value string) ([]Comparator, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "*" {
		return nil, nil
	}
	matches := tokenRE.FindAllStringSubmatch(value, -1)
	var out []Comparator
	for _, match := range matches {
		v, err := Parse(match[2] + "." + match[3] + "." + match[4])
		if err != nil {
			return nil, err
		}
		op := match[1]
		if op == "" {
			op = "="
		}
		out = append(out, Comparator{Op: op, Version: v})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("invalid constraint %q", value)
	}
	return out, nil
}

func Satisfies(version Version, constraint string) bool {
	comparators, err := ParseConstraint(constraint)
	if err != nil {
		return false
	}
	for _, c := range comparators {
		cmp := Compare(version, c.Version)
		ok := false
		switch c.Op {
		case "=":
			ok = cmp == 0
		case ">":
			ok = cmp > 0
		case ">=":
			ok = cmp >= 0
		case "<":
			ok = cmp < 0
		case "<=":
			ok = cmp <= 0
		case "^":
			if c.Version.Major > 0 {
				ok = cmp >= 0 && version.Major == c.Version.Major
			} else if c.Version.Minor > 0 {
				ok = cmp >= 0 && version.Major == 0 && version.Minor == c.Version.Minor
			} else {
				ok = cmp >= 0 && version.Major == 0 && version.Minor == 0 && version.Patch == c.Version.Patch
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func Compare(a, b Version) int {
	for _, pair := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}
