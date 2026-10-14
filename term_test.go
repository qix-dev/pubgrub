// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	pubgrub "pubgrub-go"
	"testing"
)

func TestNegativeTermAllowsAbsentPackage(t *testing.T) {
	negative := pubgrub.Negative[int](sets.Full())
	positive := pubgrub.Positive[int](sets.Empty())
	if negative.Equal(positive) {
		t.Fatal("absence semantics collapsed")
	}
	if negative.IsDisjoint(pubgrub.Negative[int](sets.Empty())) {
		t.Fatal("negative terms share absence")
	}
	if negative.RelationWith(positive) != pubgrub.Satisfied {
		t.Fatal("empty positive must satisfy negative")
	}
}
func TestTermAlgebra(t *testing.T) {
	terms := []pubgrub.Term[int, intSet]{pubgrub.Positive[int](sets.Empty()), pubgrub.Negative[int](sets.Empty()), pubgrub.Positive[int](sets.Full()), pubgrub.Negative[int](sets.Full()), pubgrub.Positive[int](sets.Between(1, 4)), pubgrub.Negative[int](sets.Between(2, 5)), pubgrub.Positive[int](sets.Singleton(2)), pubgrub.Negative[int](sets.Singleton(3))}
	for _, a := range terms {
		for _, b := range terms {
			intersection := a.Intersection(b)
			union := a.Union(b)
			if !union.Equal(a.Negate().Intersection(b.Negate()).Negate()) {
				t.Fatal("De Morgan")
			}
			subset, disjoint := true, true
			for value := -1; value < 7; value++ {
				av, bv := a.Contains(value), b.Contains(value)
				if intersection.Contains(value) != (av && bv) || union.Contains(value) != (av || bv) {
					t.Fatalf("%v %v", a, b)
				}
				subset = subset && (!av || bv)
				disjoint = disjoint && !(av && bv)
			}
			av, bv := !a.Positive, !b.Positive
			subset = subset && (!av || bv)
			disjoint = disjoint && !(av && bv)
			if a.SubsetOf(b) != subset || a.IsDisjoint(b) != disjoint {
				t.Fatalf("relation %v %v", a, b)
			}
			want := pubgrub.Inconclusive
			if subset {
				want = pubgrub.Satisfied
			} else if disjoint {
				want = pubgrub.Contradicted
			}
			if b.RelationWith(a) != want {
				t.Fatalf("%v vs %v", a, b)
			}
		}
	}
}
