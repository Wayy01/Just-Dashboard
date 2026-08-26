package selfupdate

import (
	"strconv"
	"strings"
)

// Version comparison, for a version scheme that is deliberately not semver.
//
// The product version has two components most of the time and three when a
// release fixes something without adding to it — 0.5, 0.5.1, 0.6. Comparing
// those as strings gets "0.10" < "0.5" and would offer an operator on 0.10 a
// downgrade to 0.5 as an update; comparing them with a semver library means a
// dependency and a scheme that insists on three components, which would turn
// every version in this repo into a lie about being a semver contract. So:
// numeric components, missing ones read as zero, and a leading "v" tolerated
// because that is how the same number is written on a git tag.

// ValidVersion reports whether s is a version this scheme can compare: one to
// four numeric components, optionally prefixed with "v".
//
// Refusing the ones it cannot compare is the whole job. A version string that
// silently sorts as zero is how an install ends up believing it is newer than
// every release and never offering an update again.
func ValidVersion(s string) bool {
	parts, ok := split(s)
	return ok && len(parts) > 0
}

func split(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return nil, false
	}
	fields := strings.Split(s, ".")
	if len(fields) > 4 {
		return nil, false
	}
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			return nil, false
		}
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// Compare returns -1, 0 or 1 as a is older than, the same as, or newer than b.
//
// A version that cannot be parsed compares as older than one that can, and two
// unparseable versions compare equal. That direction is chosen so the failure
// mode is an install that offers an update it cannot describe, rather than one
// that silently decides it is already ahead of everything.
func Compare(a, b string) int {
	av, aok := split(a)
	bv, bok := split(b)
	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return -1
	case !bok:
		return 1
	}
	for i := 0; i < len(av) || i < len(bv); i++ {
		x, y := at(av, i), at(bv, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func at(v []int, i int) int {
	if i < len(v) {
		return v[i]
	}
	// 0.5 and 0.5.0 are the same release. Treating the missing component as
	// zero is what makes that true rather than making 0.5.0 an update.
	return 0
}

// Newer reports whether candidate is a version worth offering to someone
// running current.
func Newer(current, candidate string) bool {
	return ValidVersion(candidate) && Compare(candidate, current) > 0
}

// Normalise strips a leading "v" so a tag and a constant compare and display
// as the same thing.
func Normalise(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "v")
}
