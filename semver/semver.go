// SPDX-License-Identifier: MPL-2.0

// Package semver provides the optional version-ranges SemVer adapter. Its range
// conversion follows astral-version-ranges, including the intentionally lossy
// treatment of prereleases: it models intervals, not VersionReq.matches rules.
package semver

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"pubgrub-go/ranges"
)

type Version struct {
	Major, Minor, Patch uint64
	Pre, Build          string
}

func New(major, minor, patch uint64) Version {
	return Version{Major: major, Minor: minor, Patch: patch}
}
func (v Version) String() string {
	value := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		value += "-" + v.Pre
	}
	if v.Build != "" {
		value += "+" + v.Build
	}
	return value
}
func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func identifiers(text string, prerelease bool) error {
	for _, id := range strings.Split(text, ".") {
		if id == "" {
			return fmt.Errorf("empty semver identifier")
		}
		for _, c := range id {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
				return fmt.Errorf("invalid semver identifier %q", id)
			}
		}
		if prerelease && numeric(id) && len(id) > 1 && id[0] == '0' {
			return fmt.Errorf("numeric prerelease identifier has leading zero")
		}
	}
	return nil
}
func parseNumber(s string) (uint64, error) {
	if !numeric(s) || len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("invalid semver number %q", s)
	}
	return strconv.ParseUint(s, 10, 64)
}
func Parse(text string) (Version, error) {
	var v Version
	core := text
	if index := strings.IndexByte(core, '+'); index >= 0 {
		v.Build = core[index+1:]
		if err := identifiers(v.Build, false); err != nil {
			return Version{}, err
		}
		core = core[:index]
	}
	if index := strings.IndexByte(core, '-'); index >= 0 {
		v.Pre = core[index+1:]
		if err := identifiers(v.Pre, true); err != nil {
			return Version{}, err
		}
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("semver requires major.minor.patch")
	}
	numbers := []*uint64{&v.Major, &v.Minor, &v.Patch}
	for i, part := range parts {
		number, err := parseNumber(part)
		if err != nil {
			return Version{}, err
		}
		*numbers[i] = number
	}
	return v, nil
}
func compareNumeric(a, b string) int {
	if c := cmp.Compare(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}
func compareIdentifiers(a, b string, build bool) int {
	if a == b {
		return 0
	}
	if !build {
		if a == "" {
			return 1
		}
		if b == "" {
			return -1
		}
	}
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < min(len(left), len(right)); i++ {
		l, r := left[i], right[i]
		ln, rn := numeric(l) || build && l == "", numeric(r) || build && r == ""
		var c int
		if ln && rn {
			if build {
				trimL, trimR := strings.TrimLeft(l, "0"), strings.TrimLeft(r, "0")
				c = compareNumeric(trimL, trimR)
				if c == 0 {
					c = cmp.Compare(len(l), len(r))
				}
			} else {
				c = compareNumeric(l, r)
			}
		} else if ln {
			c = -1
		} else if rn {
			c = 1
		} else {
			c = strings.Compare(l, r)
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(len(left), len(right))
}
func ComparePrecedence(a, b Version) int {
	if c := cmp.Compare(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Patch, b.Patch); c != 0 {
		return c
	}
	return compareIdentifiers(a.Pre, b.Pre, false)
}

// Compare is a total order, including build metadata, like Rust semver::Version.
// ComparePrecedence ignores build metadata as prescribed by the SemVer spec.
func Compare(a, b Version) int {
	if c := ComparePrecedence(a, b); c != 0 {
		return c
	}
	return compareIdentifiers(a.Build, b.Build, true)
}
func (v Version) MarshalText() ([]byte, error) { return []byte(v.String()), nil }
func (v *Version) UnmarshalText(text []byte) error {
	value, err := Parse(string(text))
	if err == nil {
		*v = value
	}
	return err
}
func (v Version) MarshalJSON() ([]byte, error) { return json.Marshal(v.String()) }
func (v *Version) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	return v.UnmarshalText([]byte(text))
}

type Op uint8

const (
	Exact Op = iota
	Greater
	GreaterEq
	Less
	LessEq
	Tilde
	Caret
	Wildcard
)

type Comparator struct {
	Op           Op
	Major        uint64
	Minor, Patch *uint64
	Pre          string
}
type Requirement struct{ Comparators []Comparator }

func Number(value uint64) *uint64 { return &value }
func ParseRequirement(text string) (Requirement, error) {
	text = strings.TrimSpace(text)
	if text == "*" || text == "x" || text == "X" {
		return Requirement{}, nil
	}
	if text == "" {
		return Requirement{}, fmt.Errorf("empty semver requirement")
	}
	var result Requirement
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		c := Comparator{Op: Caret}
		explicit := false
		for _, operator := range []struct {
			prefix string
			op     Op
		}{{">=", GreaterEq}, {"<=", LessEq}, {"=", Exact}, {">", Greater}, {"<", Less}, {"~", Tilde}, {"^", Caret}} {
			if strings.HasPrefix(part, operator.prefix) {
				c.Op = operator.op
				part = strings.TrimSpace(strings.TrimPrefix(part, operator.prefix))
				explicit = true
				break
			}
		}
		if index := strings.IndexByte(part, '+'); index >= 0 {
			if err := identifiers(part[index+1:], false); err != nil {
				return Requirement{}, err
			}
			part = part[:index]
		}
		if index := strings.IndexByte(part, '-'); index >= 0 {
			c.Pre = part[index+1:]
			if err := identifiers(c.Pre, true); err != nil {
				return Requirement{}, err
			}
			part = part[:index]
		}
		parts := strings.Split(part, ".")
		if len(parts) > 3 {
			return Requirement{}, fmt.Errorf("too many version components")
		}
		wildcard := false
		for i, value := range parts {
			if value == "*" || value == "x" || value == "X" {
				if i == 0 {
					return Requirement{}, fmt.Errorf("standalone wildcard cannot have comparators")
				}
				wildcard = true
				continue
			}
			if wildcard {
				return Requirement{}, fmt.Errorf("numeric component after wildcard")
			}
			number, err := parseNumber(value)
			if err != nil {
				return Requirement{}, err
			}
			switch i {
			case 0:
				c.Major = number
			case 1:
				c.Minor = Number(number)
			case 2:
				c.Patch = Number(number)
			}
		}
		if wildcard && !explicit {
			c.Op = Wildcard
		}
		if c.Pre != "" && c.Patch == nil {
			return Requirement{}, fmt.Errorf("prerelease requires a patch component")
		}
		result.Comparators = append(result.Comparators, c)
	}
	return result, nil
}
func value(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}

// FromRequirement follows the Rust adapter, which constructs release bounds
// from numeric components and does not apply prerelease eligibility filtering.
func FromRequirement(req Requirement) (ranges.Ranges[Version], error) {
	factory := ranges.New(Compare)
	out := factory.Full()
	for _, c := range req.Comparators {
		if c.Patch != nil && c.Minor == nil {
			return factory, fmt.Errorf("patch requires minor")
		}
		if c.Major == math.MaxUint64 || c.Minor != nil && *c.Minor == math.MaxUint64 || c.Patch != nil && *c.Patch == math.MaxUint64 {
			return factory, fmt.Errorf("requirement component overflow")
		}
		major, minor, patch := c.Major, value(c.Minor), value(c.Patch)
		base := New(major, minor, patch)
		exact := func() ranges.Ranges[Version] {
			if c.Minor == nil {
				return factory.Between(New(major, 0, 0), New(major+1, 0, 0))
			}
			if c.Patch == nil {
				return factory.Between(New(major, minor, 0), New(major, minor+1, 0))
			}
			return factory.Singleton(base)
		}
		var set ranges.Ranges[Version]
		switch c.Op {
		case Exact:
			set = exact()
		case Greater:
			if c.Minor == nil {
				set = factory.HigherThan(New(major+1, 0, 0))
			} else if c.Patch == nil {
				set = factory.HigherThan(New(major, minor+1, 0))
			} else {
				set = factory.StrictlyHigherThan(base)
			}
		case GreaterEq:
			set = factory.HigherThan(base)
		case Less:
			set = factory.StrictlyLowerThan(base)
		case LessEq:
			if c.Minor == nil {
				set = factory.StrictlyLowerThan(New(major+1, 0, 0))
			} else if c.Patch == nil {
				set = factory.StrictlyLowerThan(New(major, minor+1, 0))
			} else {
				set = factory.LowerThan(base)
			}
		case Tilde:
			if c.Patch == nil {
				set = exact()
			} else {
				set = factory.Between(base, New(major, minor+1, 0))
			}
		case Caret:
			switch {
			case c.Minor == nil:
				set = exact()
			case major > 0:
				set = factory.Between(base, New(major+1, 0, 0))
			case minor > 0:
				set = factory.Between(base, New(0, minor+1, 0))
			default:
				set = exact()
			}
		case Wildcard:
			if c.Minor == nil {
				set = factory.Between(New(major, 0, 0), New(major+1, 0, 0))
			} else {
				set = factory.Between(New(major, minor, 0), New(major, minor+1, 0))
			}
		default:
			return factory, fmt.Errorf("unknown semver operator %d", c.Op)
		}
		out = out.Intersection(set)
	}
	return out, nil
}
func ParseRanges(text string) (ranges.Ranges[Version], error) {
	req, err := ParseRequirement(text)
	if err != nil {
		return ranges.New(Compare), err
	}
	return FromRequirement(req)
}
