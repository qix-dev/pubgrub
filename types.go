// SPDX-License-Identifier: MPL-2.0

// Package pubgrub implements conflict-driven dependency resolution, ported from
// astral-pubgrub. Version ordering and package metadata are supplied by callers.
package pubgrub

import (
	"fmt"
	"github.com/qix-dev/pubgrub/ranges"
)

// VersionSet is the algebra required by PubGrub. Empty and Singleton use the
// receiver as a factory. All operations must be immutable, equality canonical,
// and Clone must isolate mutable storage. Version values must be immutable.
type VersionSet[V any, S any] interface {
	Empty() S
	Singleton(V) S
	Full() S
	Clone() S
	Complement() S
	Intersection(S) S
	Union(S) S
	Difference(S) S
	Contains(V) bool
	ContainsMany([]V) []bool
	Equal(S) bool
	IsEmpty() bool
	IsDisjoint(S) bool
	SubsetOf(S) bool
	Relation(S) ranges.Relation
	String() string
}

// DependencyConstraints preserves input order and duplicate dependencies.
type DependencyConstraints[P comparable, S any] []DependencyConstraint[P, S]
type DependencyConstraint[P comparable, S any] struct {
	Package  P
	Versions S
}
type Dependencies[P comparable, S any, M any] struct {
	Available   bool
	Constraints DependencyConstraints[P, S]
	Reason      M
}

func Available[P comparable, S any, M any](constraints DependencyConstraints[P, S]) Dependencies[P, S, M] {
	return Dependencies[P, S, M]{Available: true, Constraints: constraints}
}
func Unavailable[P comparable, S any, M any](reason M) Dependencies[P, S, M] {
	return Dependencies[P, S, M]{Reason: reason}
}

// DependencyProvider must return stable, complete metadata during a resolution.
// Equal priorities are broken by package discovery order. A higher comparator
// result means higher priority. CompareVersions must be a total order.
// EmptySet returns an initialized version-set factory, not a Go zero value.
// Package, version, metadata and priority values must be immutable. In
// particular, the solver does not copy storage referenced by caller values.
// Returned version sets must obey the immutable VersionSet contract.
type DependencyProvider[P comparable, V any, S VersionSet[V, S], M any, R any] interface {
	EmptySet() S
	CompareVersions(V, V) int
	ComparePriorities(R, R) int
	Prioritize(P, S, PackageResolutionStatistics) R
	ChooseVersion(P, S) (V, bool, error)
	GetDependencies(P, V) (Dependencies[P, S, M], error)
	ShouldCancel() error
}

type PackageResolutionStatistics struct {
	UnitPropagationAffected uint32
	UnitPropagationCulprit  uint32
	DependenciesAffected    uint32
	DependenciesCulprit     uint32
}

func (s PackageResolutionStatistics) ConflictCount() uint32 {
	return s.UnitPropagationAffected + s.UnitPropagationCulprit + s.DependenciesAffected + s.DependenciesCulprit
}

type PackageID int
type ConflictID int

type Selection[P comparable, V any] struct {
	Package P
	Version V
}
type SelectedDependencies[P comparable, V any] struct {
	entries []Selection[P, V]
	values  map[P]V
}

func newSelected[P comparable, V any](entries []Selection[P, V]) SelectedDependencies[P, V] {
	values := make(map[P]V, len(entries))
	for _, entry := range entries {
		values[entry.Package] = entry.Version
	}
	return SelectedDependencies[P, V]{entries, values}
}
func (s SelectedDependencies[P, V]) Get(p P) (V, bool) { v, ok := s.values[p]; return v, ok }
func (s SelectedDependencies[P, V]) Len() int          { return len(s.entries) }
func (s SelectedDependencies[P, V]) Entries() []Selection[P, V] {
	return append([]Selection[P, V](nil), s.entries...)
}
func (s SelectedDependencies[P, V]) Map() map[P]V {
	m := make(map[P]V, len(s.values))
	for p, v := range s.values {
		m[p] = v
	}
	return m
}

type ErrorKind uint8

const (
	NoSolution ErrorKind = iota
	ErrorRetrievingDependencies
	ErrorChoosingVersion
	ErrorInShouldCancel
	InvalidProvider
	InternalError
)

// ResolveError carries a derivation tree for NoSolution and preserves provider
// errors through Unwrap so errors.Is/errors.As continue to work.
type ResolveError[P comparable, V any, S VersionSet[V, S], M any] struct {
	Kind    ErrorKind
	Package P
	Version V
	Source  error
	Tree    *DerivationTree[P, V, S, M]
}

func (e *ResolveError[P, V, S, M]) Error() string {
	switch e.Kind {
	case NoSolution:
		return "There is no solution"
	case ErrorRetrievingDependencies:
		return fmt.Sprintf("Retrieving dependencies of %v %v failed", e.Package, e.Version)
	case ErrorChoosingVersion:
		return fmt.Sprintf("Choosing a version for %v failed", e.Package)
	case ErrorInShouldCancel:
		return "The solver was cancelled"
	case InvalidProvider:
		return fmt.Sprintf("Invalid dependency provider: %v", e.Source)
	case InternalError:
		return fmt.Sprintf("PubGrub invariant violated: %v", e.Source)
	}
	return "Unknown resolver error"
}
func (e *ResolveError[P, V, S, M]) Unwrap() error { return e.Source }
