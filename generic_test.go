// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	pubgrub "pubgrub-go"
	"pubgrub-go/ranges"
)

// The solver must not require Go-comparable version values: real Python
// versions may contain arbitrary-length release tuples.
type tupleVersion struct{ release []int }

func (v tupleVersion) String() string { return fmt.Sprint(v.release) }

func TestNonComparableVersions(t *testing.T) {
	compare := func(a, b tupleVersion) int { return slices.Compare(a.release, b.release) }
	sets := ranges.New(compare)
	p := pubgrub.NewOfflineDependencyProvider[string](sets, compare)
	root := tupleVersion{[]int{0}}
	first, second := tupleVersion{[]int{1, 0}}, tupleVersion{[]int{2, 0}}
	p.AddDependencies("root", root, pubgrub.DependencyConstraints[string, ranges.Ranges[tupleVersion]]{
		{Package: "lib", Versions: sets.Full()},
	})
	p.AddDependencies("lib", first, nil)
	p.AddDependencies("lib", second, nil)
	result, err := pubgrub.Resolve(p, "root", root)
	if err != nil {
		t.Fatal(err)
	}
	chosen, ok := result.Get("lib")
	if !ok || compare(chosen, second) != 0 {
		t.Fatal(chosen)
	}
}

// A finite version domain exercises the VersionSet extension point without
// using the bundled interval representation or its operations.
type bitSet uint8

func (s bitSet) Empty() bitSet                    { return 0 }
func (s bitSet) Full() bitSet                     { return 255 }
func (s bitSet) Singleton(v int) bitSet           { return 1 << v }
func (s bitSet) Clone() bitSet                    { return s }
func (s bitSet) Complement() bitSet               { return ^s }
func (s bitSet) Intersection(other bitSet) bitSet { return s & other }
func (s bitSet) Union(other bitSet) bitSet        { return s | other }
func (s bitSet) Difference(other bitSet) bitSet   { return s &^ other }
func (s bitSet) Contains(v int) bool              { return s&(1<<v) != 0 }
func (s bitSet) Equal(other bitSet) bool          { return s == other }
func (s bitSet) IsEmpty() bool                    { return s == 0 }
func (s bitSet) IsDisjoint(other bitSet) bool     { return s&other == 0 }
func (s bitSet) SubsetOf(other bitSet) bool       { return s&^other == 0 }
func (s bitSet) String() string                   { return fmt.Sprintf("%08b", s) }
func (s bitSet) ContainsMany(versions []int) []bool {
	contained := make([]bool, len(versions))
	for i, version := range versions {
		contained[i] = s.Contains(version)
	}
	return contained
}
func (s bitSet) Relation(other bitSet) ranges.Relation {
	if s.SubsetOf(other) {
		return ranges.Subset
	}
	if s.IsDisjoint(other) {
		return ranges.Disjoint
	}
	return ranges.Overlapping
}

func TestCustomVersionSet(t *testing.T) {
	p := pubgrub.NewOfflineDependencyProvider[string](bitSet(0), cmp.Compare[int])
	p.AddDependencies("root", 0, pubgrub.DependencyConstraints[string, bitSet]{
		{Package: "lib", Versions: bitSet(0).Full()},
	})
	p.AddDependencies("lib", 2, pubgrub.DependencyConstraints[string, bitSet]{
		{Package: "missing", Versions: bitSet(0).Singleton(1)},
	})
	p.AddDependencies("lib", 1, nil)
	result, err := pubgrub.Resolve(p, "root", 0)
	if err != nil {
		t.Fatal(err)
	}
	if chosen, ok := result.Get("lib"); !ok || chosen != 1 {
		t.Fatal(chosen)
	}
}
