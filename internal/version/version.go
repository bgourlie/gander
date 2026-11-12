// Package version parses and orders Goose's release tags.
//
// Tags look like v2026.4, v1.2.3 or v2.0-rc1. Numeric parts compare as numbers, and a
// release with a suffix sorts below the same release without one.
package version

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// Version is one parsed release tag. Versions compare equal with == exactly when their
// tags are the same.
type Version struct {
	tag string // canonical, always starting with "v"
}

// Parse reads a tag, with or without its leading "v". It accepts exactly
// v?N(.N)*(-[0-9A-Za-z.]+)?, which also rules out "/" and "..", so a parsed
// version is always safe to use as a file name.
func Parse(s string) (Version, error) {
	rest := strings.TrimPrefix(s, "v")
	if _, _, ok := split(rest); !ok {
		return Version{}, invalid(s)
	}
	return Version{tag: "v" + rest}, nil
}

// split breaks a tag without its "v" into its numeric parts and suffix.
func split(rest string) (nums []uint64, suffix string, ok bool) {
	numPart, suffix, hasSuffix := strings.Cut(rest, "-")
	if (hasSuffix && !validSuffix(suffix)) || numPart == "" {
		return nil, "", false
	}
	for p := range strings.SplitSeq(numPart, ".") {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return nil, "", false
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, "", false
		}
		nums = append(nums, n)
	}
	return nums, suffix, true
}

func (v Version) parts() ([]uint64, string) {
	nums, suffix, _ := split(strings.TrimPrefix(v.tag, "v"))
	return nums, suffix
}

// MustParse is Parse for tags known to be valid, such as test fixtures.
func MustParse(s string) Version {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func validSuffix(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.':
		default:
			return false
		}
	}
	return true
}

func invalid(s string) error {
	return fmt.Errorf("%q is not a Goose version; versions look like v2026.4", s)
}

// String returns the tag, always with its "v".
func (v Version) String() string { return v.tag }

// IsZero reports whether v is the zero Version.
func (v Version) IsZero() bool { return v.tag == "" }

// Suffix returns what follows the "-" in the tag, or "".
func (v Version) Suffix() string {
	_, suffix := v.parts()
	return suffix
}

// Compare orders a and b, returning -1, 0 or +1. Numeric parts compare as numbers,
// missing parts count as zero, a suffix sorts below its release, and suffixes compare
// naturally (rc9 before rc10). Tags that are still equal, such as v1.2 and v1.2.0,
// fall back to comparing the strings, so only identical tags compare equal.
func Compare(a, b Version) int {
	an, as := a.parts()
	bn, bs := b.parts()
	for i := range max(len(an), len(bn)) {
		if c := cmp.Compare(part(an, i), part(bn, i)); c != 0 {
			return c
		}
	}
	switch {
	case as == "" && bs != "":
		return 1
	case as != "" && bs == "":
		return -1
	}
	if c := naturalCompare(as, bs); c != 0 {
		return c
	}
	return strings.Compare(a.tag, b.tag)
}

func part(nums []uint64, i int) uint64 {
	if i < len(nums) {
		return nums[i]
	}
	return 0
}

// naturalCompare compares runs of digits as numbers and everything else byte by byte.
func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		ad, bd := isDigit(a[0]), isDigit(b[0])
		if ad && bd {
			ra, rb := digitRun(a), digitRun(b)
			na, nb := strings.TrimLeft(ra, "0"), strings.TrimLeft(rb, "0")
			if c := cmp.Compare(len(na), len(nb)); c != 0 {
				return c
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			a, b = a[len(ra):], b[len(rb):]
			continue
		}
		if c := cmp.Compare(a[0], b[0]); c != 0 {
			return c
		}
		a, b = a[1:], b[1:]
	}
	return cmp.Compare(len(a), len(b))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digitRun(s string) string {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i]
}
