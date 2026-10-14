// SPDX-License-Identifier: MPL-2.0
package pubgrub

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// SemanticVersion is the original pubgrub three-component u32 version type.
// The semver subpackage separately supports prerelease/build identifiers.
type SemanticVersion struct{ Major, Minor, Patch uint32 }

func NewSemanticVersion(major, minor, patch uint32) SemanticVersion {
	return SemanticVersion{major, minor, patch}
}
func ZeroVersion() SemanticVersion { return SemanticVersion{} }
func OneVersion() SemanticVersion  { return SemanticVersion{Major: 1} }
func TwoVersion() SemanticVersion  { return SemanticVersion{Major: 2} }
func CompareSemanticVersions(a, b SemanticVersion) int {
	if c := cmp.Compare(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Minor, b.Minor); c != 0 {
		return c
	}
	return cmp.Compare(a.Patch, b.Patch)
}
func (v SemanticVersion) String() string   { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }
func (v SemanticVersion) Tuple() [3]uint32 { return [3]uint32{v.Major, v.Minor, v.Patch} }
func (v SemanticVersion) BumpPatch() (SemanticVersion, error) {
	if v.Patch == math.MaxUint32 {
		return v, fmt.Errorf("patch version overflow")
	}
	return SemanticVersion{v.Major, v.Minor, v.Patch + 1}, nil
}
func (v SemanticVersion) BumpMinor() (SemanticVersion, error) {
	if v.Minor == math.MaxUint32 {
		return v, fmt.Errorf("minor version overflow")
	}
	return SemanticVersion{v.Major, v.Minor + 1, 0}, nil
}
func (v SemanticVersion) BumpMajor() (SemanticVersion, error) {
	if v.Major == math.MaxUint32 {
		return v, fmt.Errorf("major version overflow")
	}
	return SemanticVersion{v.Major + 1, 0, 0}, nil
}

type VersionParseErrorKind uint8

const (
	NotThreeParts VersionParseErrorKind = iota
	InvalidVersionPart
)

type VersionParseError struct {
	Kind                     VersionParseErrorKind
	FullVersion, VersionPart string
	Source                   error
}

func (e *VersionParseError) Error() string {
	if e.Kind == NotThreeParts {
		return fmt.Sprintf("version %s must contain 3 numbers separated by dot", e.FullVersion)
	}
	return fmt.Sprintf("cannot parse '%s' in '%s' as u32: %v", e.VersionPart, e.FullVersion, e.Source)
}
func (e *VersionParseError) Unwrap() error { return e.Source }
func ParseSemanticVersion(text string) (SemanticVersion, error) {
	parts := strings.Split(text, ".")
	if len(parts) != 3 {
		return SemanticVersion{}, &VersionParseError{Kind: NotThreeParts, FullVersion: text}
	}
	var numbers [3]uint32
	for i, part := range parts {
		number, err := strconv.ParseUint(strings.TrimPrefix(part, "+"), 10, 32)
		if err != nil {
			return SemanticVersion{}, &VersionParseError{Kind: InvalidVersionPart, FullVersion: text, VersionPart: part, Source: err}
		}
		numbers[i] = uint32(number)
	}
	return SemanticVersion{numbers[0], numbers[1], numbers[2]}, nil
}
func (v SemanticVersion) MarshalText() ([]byte, error) { return []byte(v.String()), nil }
func (v *SemanticVersion) UnmarshalText(text []byte) error {
	parsed, err := ParseSemanticVersion(string(text))
	if err == nil {
		*v = parsed
	}
	return err
}
func (v SemanticVersion) MarshalJSON() ([]byte, error) { return json.Marshal(v.String()) }
func (v *SemanticVersion) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	return v.UnmarshalText([]byte(text))
}
