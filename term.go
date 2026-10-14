// SPDX-License-Identifier: MPL-2.0
package pubgrub

import "pubgrub-go/ranges"

// Term is positive (the package must be selected in Set) or negative (the
// package may be absent, or selected outside Set). These differ even when their
// allowed versions match: Negative(empty) permits an absent package.
type Term[V any, S VersionSet[V, S]] struct {
	Positive bool
	Set      S
}

func Positive[V any, S VersionSet[V, S]](set S) Term[V, S] { return Term[V, S]{true, set} }
func Negative[V any, S VersionSet[V, S]](set S) Term[V, S] { return Term[V, S]{false, set} }
func (t Term[V, S]) Clone() Term[V, S]                     { return Term[V, S]{t.Positive, t.Set.Clone()} }
func (t Term[V, S]) Negate() Term[V, S]                    { return Term[V, S]{!t.Positive, t.Set.Clone()} }
func (t Term[V, S]) Contains(version V) bool               { return t.Positive == t.Set.Contains(version) }
func (t Term[V, S]) Equal(other Term[V, S]) bool {
	return t.Positive == other.Positive && t.Set.Equal(other.Set)
}
func (t Term[V, S]) IsAny() bool { return !t.Positive && t.Set.IsEmpty() }
func (t Term[V, S]) Intersection(other Term[V, S]) Term[V, S] {
	switch {
	case t.Positive && other.Positive:
		return Positive[V](t.Set.Intersection(other.Set))
	case t.Positive:
		return Positive[V](t.Set.Difference(other.Set))
	case other.Positive:
		return Positive[V](other.Set.Difference(t.Set))
	default:
		return Negative[V](t.Set.Union(other.Set))
	}
}
func (t Term[V, S]) Union(other Term[V, S]) Term[V, S] {
	switch {
	case t.Positive && other.Positive:
		return Positive[V](t.Set.Union(other.Set))
	case t.Positive:
		return Negative[V](other.Set.Difference(t.Set))
	case other.Positive:
		return Negative[V](t.Set.Difference(other.Set))
	default:
		return Negative[V](t.Set.Intersection(other.Set))
	}
}
func (t Term[V, S]) IsDisjoint(other Term[V, S]) bool {
	switch {
	case t.Positive && other.Positive:
		return t.Set.IsDisjoint(other.Set)
	case !t.Positive && !other.Positive:
		return false
	case t.Positive:
		return t.Set.SubsetOf(other.Set)
	default:
		return other.Set.SubsetOf(t.Set)
	}
}
func (t Term[V, S]) SubsetOf(other Term[V, S]) bool {
	switch {
	case t.Positive && other.Positive:
		return t.Set.SubsetOf(other.Set)
	case t.Positive:
		return t.Set.IsDisjoint(other.Set)
	case other.Positive:
		return false
	default:
		return other.Set.SubsetOf(t.Set)
	}
}

type TermRelation uint8

const (
	Satisfied TermRelation = iota
	Contradicted
	Inconclusive
)

// RelationWith classifies the assignment against t, giving satisfaction
// precedence for an empty positive assignment as in the Rust implementation.
func (t Term[V, S]) RelationWith(assignment Term[V, S]) TermRelation {
	switch {
	case t.Positive && assignment.Positive:
		switch assignment.Set.Relation(t.Set) {
		case ranges.Subset:
			return Satisfied
		case ranges.Disjoint:
			return Contradicted
		case ranges.Overlapping:
			return Inconclusive
		}
	case t.Positive:
		if t.Set.SubsetOf(assignment.Set) {
			return Contradicted
		}
	case assignment.Positive:
		if assignment.Set.IsEmpty() {
			return Satisfied
		}
		switch assignment.Set.Relation(t.Set) {
		case ranges.Subset:
			return Contradicted
		case ranges.Disjoint:
			return Satisfied
		case ranges.Overlapping:
			return Inconclusive
		}
	default:
		if t.Set.SubsetOf(assignment.Set) {
			return Satisfied
		}
	}
	return Inconclusive
}
func (t Term[V, S]) String() string {
	if t.Positive {
		return t.Set.String()
	}
	return "Not ( " + t.Set.String() + " )"
}
