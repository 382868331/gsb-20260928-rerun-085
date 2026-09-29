package semver

import (
	"fmt"
	"strconv"
	"strings"
)

type Version struct {
	Major int
	Minor int
	Patch int
}

func Parse(value string) (Version, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q must use major.minor.patch", value)
	}
	nums := make([]int, 3)
	for index, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("invalid version %q", value)
		}
		nums[index] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}

func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return cmp(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return cmp(v.Minor, other.Minor)
	}
	return cmp(v.Patch, other.Patch)
}

func cmp(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func Distance(a, b Version) int {
	d := a.Major - b.Major
	if d < 0 {
		d = -d
	}
	d *= 1000000
	d += abs(a.Minor-b.Minor) * 1000
	d += abs(a.Patch - b.Patch)
	return d
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func Satisfies(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}
	v, err := Parse(version)
	if err != nil {
		return false
	}
	for _, term := range strings.Split(constraint, " ") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if !satisfiesOne(v, term) {
			return false
		}
	}
	return true
}

func satisfiesOne(v Version, raw string) bool {
	op := "="
	value := raw
	for _, candidate := range []string{">=", "<=", "^", "~", ">", "<", "="} {
		if strings.HasPrefix(raw, candidate) {
			op = candidate
			value = strings.TrimPrefix(raw, candidate)
			break
		}
	}
	base, err := Parse(value)
	if err != nil {
		return false
	}
	switch op {
	case "=":
		return v.Compare(base) == 0
	case ">":
		return v.Compare(base) > 0
	case "<":
		return v.Compare(base) < 0
	case ">=":
		return v.Compare(base) >= 0
	case "<=":
		return v.Compare(base) <= 0
	case "^":
		upper := Version{Major: base.Major + 1}
		return v.Compare(base) >= 0 && v.Compare(upper) < 0
	case "~":
		upper := Version{Major: base.Major, Minor: base.Minor + 1}
		return v.Compare(base) >= 0 && v.Compare(upper) < 0
	default:
		return false
	}
}
