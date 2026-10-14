// SPDX-License-Identifier: MPL-2.0
package ranges_test

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"

	"pubgrub-go/ranges"
)

var ints = ranges.Ordered[int]()

func TestBoundsAndDisplay(t *testing.T) {
	set := ints.Between(1, 4).Union(ints.Singleton(6))
	if got := set.String(); got != ">=1, <4 | 6" {
		t.Fatal(got)
	}
	if got := set.Complement().String(); got != "<1 | >=4, <6 | >6" {
		t.Fatal(got)
	}
	if got := ints.FromBounds(ranges.Include(2), ranges.Exclude(2)); !got.IsEmpty() {
		t.Fatal(got)
	}
	if got := ints.FromIntervals(ranges.Interval[int]{Start: ranges.Include(1), End: ranges.Include(3)}, ranges.Interval[int]{Start: ranges.Exclude(3), End: ranges.Include(4)}); got.String() != ">=1, <=4" {
		t.Fatal(got)
	}
}
func TestBoundingRange(t *testing.T) {
	cases := []struct {
		name string
		set  ranges.Ranges[int]
		want ranges.Interval[int]
		ok   bool
	}{
		{"empty", ints.Empty(), ranges.Interval[int]{}, false},
		{"singleton", ints.Singleton(3), ranges.Interval[int]{Start: ranges.Include(3), End: ranges.Include(3)}, true},
		{"bounded", ints.Between(1, 4), ranges.Interval[int]{Start: ranges.Include(1), End: ranges.Exclude(4)}, true},
		{"disconnected", ints.Between(1, 4).Union(ints.Singleton(6)), ranges.Interval[int]{Start: ranges.Include(1), End: ranges.Include(6)}, true},
		{"open ends", ints.FromBounds(ranges.Exclude(1), ranges.Exclude(4)), ranges.Interval[int]{Start: ranges.Exclude(1), End: ranges.Exclude(4)}, true},
		{"unbounded below", ints.LowerThan(4), ranges.Interval[int]{Start: ranges.Unbound[int](), End: ranges.Include(4)}, true},
		{"unbounded above", ints.HigherThan(1), ranges.Interval[int]{Start: ranges.Include(1), End: ranges.Unbound[int]()}, true},
		{"full", ints.Full(), ranges.Interval[int]{Start: ranges.Unbound[int](), End: ranges.Unbound[int]()}, true},
		{"unbounded with gap", ints.StrictlyLowerThan(1).Union(ints.StrictlyHigherThan(4)), ranges.Interval[int]{Start: ranges.Unbound[int](), End: ranges.Unbound[int]()}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.set.BoundingRange()
			if ok != c.ok || got != c.want {
				t.Fatalf("BoundingRange(%s) = %+v, %v; want %+v, %v", c.set, got, ok, c.want, c.ok)
			}
		})
	}
}
func TestRangeRelation(t *testing.T) {
	cases := []struct {
		name    string
		a, b    ranges.Ranges[int]
		forward ranges.Relation
		reverse ranges.Relation
	}{
		// Empty sets are subsets even when they are also disjoint.
		{"both empty", ints.Empty(), ints.Empty(), ranges.Subset, ranges.Subset},
		{"empty and nonempty", ints.Empty(), ints.Singleton(1), ranges.Subset, ranges.Disjoint},
		{"equal", ints.Between(1, 4), ints.Between(1, 4), ranges.Subset, ranges.Subset},
		{"proper subset", ints.Between(2, 3), ints.Between(1, 4), ranges.Subset, ranges.Overlapping},
		{"full set", ints.Between(1, 4), ints.Full(), ranges.Subset, ranges.Overlapping},
		{"partial overlap", ints.Between(1, 4), ints.Between(3, 6), ranges.Overlapping, ranges.Overlapping},
		{"excluded boundary", ints.Between(1, 3), ints.Between(3, 5), ranges.Disjoint, ranges.Disjoint},
		{"included boundary", ints.LowerThan(3), ints.HigherThan(3), ranges.Overlapping, ranges.Overlapping},
		{"gap", ints.Singleton(1).Union(ints.Singleton(5)), ints.Between(2, 4), ranges.Disjoint, ranges.Disjoint},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.Relation(c.b); got != c.forward {
				t.Fatalf("(%s).Relation(%s) = %v; want %v", c.a, c.b, got, c.forward)
			}
			if got := c.b.Relation(c.a); got != c.reverse {
				t.Fatalf("(%s).Relation(%s) = %v; want %v", c.b, c.a, got, c.reverse)
			}
		})
	}
}
func TestWidenAndNarrow(t *testing.T) {
	versions := []int{1, 2, 3, 4}
	set := ints.Singleton(2).Union(ints.Singleton(3))
	wide := set.WidenVersions(versions)
	if !wide.Equal(ints.FromBounds(ranges.Exclude(1), ranges.Exclude(4))) {
		t.Fatal(wide)
	}
	if got := wide.NarrowVersions(versions); !got.Equal(ints.FromBounds(ranges.Include(2), ranges.Include(3))) {
		t.Fatal(got)
	}
	if got := ints.StrictlyHigherThan(1).NarrowVersions(versions); !got.Equal(ints.HigherThan(2)) {
		t.Fatal(got)
	}
	if got := ints.Between(8, 10).NarrowVersions(versions); !got.Equal(ints.Between(8, 10)) {
		t.Fatal(got)
	}
	if got := set.WidenVersions(nil); !got.IsFull() {
		t.Fatal(got)
	}
}
func TestSimplify(t *testing.T) {
	set := ints.Between(1, 3).Union(ints.Between(5, 7))
	if got := set.Simplify([]int{1, 2, 5, 6}); !got.IsFull() {
		t.Fatal(got)
	}
	if got := set.Simplify([]int{0, 1, 2, 5, 6, 8}); !got.Equal(ints.Between(1, 7)) {
		t.Fatal(got)
	}
	if got := set.Simplify([]int{0, 4, 8}); !got.Equal(set) {
		t.Fatal(got)
	}
	if got := ints.Singleton(2).Simplify([]int{2}); !got.Equal(ints.Singleton(2)) {
		t.Fatal(got)
	}
}
func TestSerdeCompatibility(t *testing.T) {
	set := ints.Between(1, 4).Union(ints.HigherThan(6))
	data, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[[{"Included":1},{"Excluded":4}],[{"Included":6},"Unbounded"]]` {
		t.Fatal(string(data))
	}
	restored := ints.Empty()
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !restored.Equal(set) {
		t.Fatal(restored)
	}
	legacy := ints.Empty()
	if err := json.Unmarshal([]byte(`[[1,4],[6,null]]`), &legacy); err != nil {
		t.Fatal(err)
	}
	if !legacy.Equal(set) {
		t.Fatal(legacy)
	}
}
func TestImmutableIntervals(t *testing.T) {
	set := ints.Between(1, 4)
	intervals := set.Intervals()
	intervals[0].Start = ranges.Include(3)
	if set.String() != ">=1, <4" {
		t.Fatal(set)
	}
}
func TestRangeAlgebraAgainstMembership(t *testing.T) {
	random := rand.New(rand.NewSource(95241))
	makeSet := func() ranges.Ranges[int] {
		intervals := make([]ranges.Interval[int], random.Intn(5))
		for i := range intervals {
			intervals[i] = ranges.Interval[int]{Start: ranges.Bound[int]{Kind: ranges.BoundKind(random.Intn(3)), Value: 2 * (random.Intn(9) - 4)}, End: ranges.Bound[int]{Kind: ranges.BoundKind(random.Intn(3)), Value: 2 * (random.Intn(9) - 4)}}
		}
		return ints.FromIntervals(intervals...)
	}
	for sample := 0; sample < 2000; sample++ {
		a, b := makeSet(), makeSet()
		union, intersection, difference := a.Union(b), a.Intersection(b), a.Difference(b)
		if !a.Complement().Complement().Equal(a) {
			t.Fatalf("double complement %s", a)
		}
		if !union.Equal(b.Union(a)) || !intersection.Equal(b.Intersection(a)) {
			t.Fatal("noncommutative")
		}
		subset, disjoint := true, true
		for v := -10; v <= 10; v++ {
			inA, inB := a.Contains(v), b.Contains(v)
			if union.Contains(v) != (inA || inB) || intersection.Contains(v) != (inA && inB) || difference.Contains(v) != (inA && !inB) || a.Complement().Contains(v) == inA {
				t.Fatalf("a=%s b=%s v=%d", a, b, v)
			}
			subset = subset && (!inA || inB)
			disjoint = disjoint && !(inA && inB)
		}
		if a.SubsetOf(b) != subset || a.IsDisjoint(b) != disjoint {
			t.Fatalf("relation: %s / %s", a, b)
		}
		versions := []int{-8, -5, -2, 0, 3, 5, 8}
		contains := a.ContainsMany(versions)
		want := make([]bool, len(versions))
		for i, v := range versions {
			want[i] = a.Contains(v)
		}
		if !reflect.DeepEqual(contains, want) {
			t.Fatal("batch contains")
		}
		wide, narrow, simple := a.WidenVersions(versions), a.NarrowVersions(versions), a.Simplify(versions)
		if !a.SubsetOf(wide) || !narrow.SubsetOf(a) {
			t.Fatal("widen/narrow inclusion")
		}
		if !reflect.DeepEqual(wide.ContainsMany(versions), want) || !reflect.DeepEqual(narrow.ContainsMany(versions), want) || !reflect.DeepEqual(simple.ContainsMany(versions), want) {
			t.Fatalf("simplification %s -> %s, %s, %s", a, wide, narrow, simple)
		}
	}
}
