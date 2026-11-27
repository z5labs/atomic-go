package remote

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrVersion reports text that does not parse as a [Version].
var ErrVersion = errors.New("invalid version")

// Version is a major.minor.patch version, as the Rust client compares
// versions in needs_upgrade.
type Version struct {
	Major, Minor, Patch uint32
}

// ParseVersion parses s with the rules of the Rust client's needs_upgrade.
//
// Surrounding whitespace is trimmed and the rest is split on '.' into at
// most four parts. The first three are read as unsigned 32-bit decimal
// integers; a missing minor or patch part counts as 0, and a fourth part,
// which holds everything after the third dot, is ignored. A part present
// among the first three that does not parse, including an empty part, a "v"
// prefix or a pre-release suffix such as "3-rc1", makes the whole version
// fail to parse, and the error wraps [ErrVersion].
//
// As Rust's integer parsing does, a part may carry one leading '+'.
func ParseVersion(s string) (Version, error) {
	parts := strings.SplitN(strings.TrimSpace(s), ".", 4)
	if len(parts) > 3 {
		parts = parts[:3]
	}

	var nums [3]uint32
	for i, p := range parts {
		n, ok := parseVersionPart(p)
		if !ok {
			return Version{}, fmt.Errorf("remote: parse version %q: part %d %q: %w", s, i+1, p, ErrVersion)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}

// parseVersionPart reads p as Rust's u32::from_str does: decimal digits, with
// at most one leading '+', and nothing else.
func parseVersionPart(p string) (uint32, bool) {
	digits := strings.TrimPrefix(p, "+")
	if digits == "" || digits[0] < '0' || digits[0] > '9' {
		// strconv.ParseUint refuses these too; checking here keeps "++1"
		// and "+-1" from depending on its sign handling.
		return 0, false
	}
	n, err := strconv.ParseUint(digits, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// String returns v as "major.minor.patch".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns -1 if v is older than w, 0 if they are the same version and
// +1 if v is newer. Major is compared first, then minor, then patch.
func (v Version) Compare(w Version) int {
	if c := cmp.Compare(v.Major, w.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Minor, w.Minor); c != 0 {
		return c
	}
	return cmp.Compare(v.Patch, w.Patch)
}

// NeedsUpgrade reports whether current is older than required. It is false
// when either version does not parse with [ParseVersion], so an absent or
// unreadable [MinVersionHeader] never asks for an upgrade.
func NeedsUpgrade(current, required string) bool {
	cur, err := ParseVersion(current)
	if err != nil {
		return false
	}
	req, err := ParseVersion(required)
	if err != nil {
		return false
	}
	return cur.Compare(req) < 0
}
