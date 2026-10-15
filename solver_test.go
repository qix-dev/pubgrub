// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	"cmp"
	"context"
	"errors"
	"math/rand"
	"reflect"
	"testing"

	"github.com/qix-dev/pubgrub"
	"github.com/qix-dev/pubgrub/ranges"
)

type intSet = ranges.Ranges[int]
type constraints = pubgrub.DependencyConstraints[string, intSet]

var sets = ranges.Ordered[int]()

func provider() *pubgrub.OfflineDependencyProvider[string, int, intSet] {
	return pubgrub.NewOfflineDependencyProvider[string](sets, cmp.Compare[int])
}
func assertSolution(t *testing.T, p *pubgrub.OfflineDependencyProvider[string, int, intSet], root string, version int, want map[string]int) {
	t.Helper()
	got, err := pubgrub.Resolve(p, root, version)
	if err != nil {
		var failure *pubgrub.ResolveError[string, int, intSet, string]
		if errors.As(err, &failure) && failure.Tree != nil {
			t.Fatalf("%v\n%s", err, pubgrub.Report(failure.Tree))
		}
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Map(), want) {
		t.Fatalf("got %v, want %v", got.Map(), want)
	}
}
func requireNoSolution(t *testing.T, p *pubgrub.OfflineDependencyProvider[string, int, intSet], root string, version int) *pubgrub.ResolveError[string, int, intSet, string] {
	t.Helper()
	_, err := pubgrub.Resolve(p, root, version)
	var failure *pubgrub.ResolveError[string, int, intSet, string]
	if !errors.As(err, &failure) || failure.Kind != pubgrub.NoSolution {
		t.Fatalf("expected no solution, got %v", err)
	}
	return failure
}
func TestNoConflict(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"foo", sets.Between(1, 2)}})
	p.AddDependencies("foo", 1, constraints{{"bar", sets.Between(1, 2)}})
	p.AddDependencies("bar", 1, nil)
	p.AddDependencies("bar", 2, nil)
	assertSolution(t, p, "root", 1, map[string]int{"root": 1, "foo": 1, "bar": 1})
}
func TestAvoidingConflictDuringDecisionMaking(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 100, constraints{{"foo", sets.Between(100, 200)}, {"bar", sets.Between(100, 200)}})
	p.AddDependencies("foo", 110, constraints{{"bar", sets.Between(200, 300)}})
	p.AddDependencies("foo", 100, nil)
	p.AddDependencies("bar", 100, nil)
	p.AddDependencies("bar", 110, nil)
	p.AddDependencies("bar", 200, nil)
	assertSolution(t, p, "root", 100, map[string]int{"root": 100, "foo": 100, "bar": 110})
}
func TestConflictResolution(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"foo", sets.HigherThan(1)}})
	p.AddDependencies("foo", 2, constraints{{"bar", sets.Between(1, 2)}})
	p.AddDependencies("foo", 1, nil)
	p.AddDependencies("bar", 1, constraints{{"foo", sets.Between(1, 2)}})
	assertSolution(t, p, "root", 1, map[string]int{"root": 1, "foo": 1})
}

// The satisfier contributes only part of shared's constraint; resolving the
// conflict must retain the remaining term when learning the prior cause.
func TestConflictWithPartialSatisfier(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 100, constraints{{"foo", sets.Between(100, 200)}, {"target", sets.Between(200, 300)}})
	p.AddDependencies("foo", 110, constraints{{"left", sets.Between(100, 200)}, {"right", sets.Between(100, 200)}})
	p.AddDependencies("foo", 100, nil)
	p.AddDependencies("left", 100, constraints{{"shared", sets.HigherThan(100)}})
	p.AddDependencies("right", 100, constraints{{"shared", sets.StrictlyLowerThan(200)}})
	p.AddDependencies("shared", 200, nil)
	p.AddDependencies("shared", 100, constraints{{"target", sets.Between(100, 200)}})
	p.AddDependencies("target", 200, nil)
	p.AddDependencies("target", 100, nil)
	assertSolution(t, p, "root", 100, map[string]int{"root": 100, "foo": 100, "target": 200})
}
func TestDoubleChoices(t *testing.T) {
	p := provider()
	p.AddDependencies("a", 0, constraints{{"b", sets.Full()}, {"c", sets.Full()}})
	p.AddDependencies("b", 0, constraints{{"d", sets.Singleton(0)}})
	p.AddDependencies("b", 1, constraints{{"d", sets.Singleton(1)}})
	p.AddDependencies("c", 0, nil)
	p.AddDependencies("c", 1, constraints{{"d", sets.Singleton(2)}})
	p.AddDependencies("d", 0, nil)
	assertSolution(t, p, "a", 0, map[string]int{"a": 0, "b": 0, "c": 0, "d": 0})
}
func TestEmptyDependency(t *testing.T) {
	p := provider()
	p.AddDependencies("a", 0, constraints{{"b", sets.Empty()}})
	requireNoSolution(t, p, "a", 0)
}
func TestTransitiveEmptyDependency(t *testing.T) {
	p := provider()
	p.AddDependencies("a", 0, constraints{{"b", sets.Empty()}})
	p.AddDependencies("c", 0, constraints{{"a", sets.Full()}})
	requireNoSolution(t, p, "c", 0)
}
func TestSelfDependency(t *testing.T) {
	p := provider()
	p.AddDependencies("a", 0, constraints{{"a", sets.Full()}, {"a", sets.Singleton(0)}})
	assertSolution(t, p, "a", 0, map[string]int{"a": 0})
}
func TestIncompatibleSelfDependency(t *testing.T) {
	p := provider()
	p.AddDependencies("a", 66, constraints{{"a", sets.Singleton(111)}})
	requireNoSolution(t, p, "a", 66)
}
func TestSelfDependencyAfterBacktracking(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"a", sets.Full()}})
	p.AddDependencies("a", 2, constraints{{"b", sets.Singleton(1)}})
	p.AddDependencies("a", 1, constraints{{"a", sets.HigherThan(2)}})
	requireNoSolution(t, p, "root", 1)
}
func TestDuplicateDependenciesIntersect(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"a", sets.HigherThan(2)}, {"a", sets.LowerThan(2)}})
	p.AddDependencies("a", 1, nil)
	p.AddDependencies("a", 2, nil)
	p.AddDependencies("a", 3, nil)
	assertSolution(t, p, "root", 1, map[string]int{"root": 1, "a": 2})
}
func TestCyclicDependencies(t *testing.T) {
	p := provider()
	p.AddDependencies("a", 1, constraints{{"b", sets.Full()}})
	p.AddDependencies("b", 1, constraints{{"a", sets.Full()}})
	assertSolution(t, p, "a", 1, map[string]int{"a": 1, "b": 1})
}
func TestReportWithLotsOfHoles(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"foo", sets.Full()}, {"baz", sets.Full()}})
	p.AddDependencies("foo", 1, constraints{{"bar", sets.Full()}})
	p.AddDependencies("foo", 2, constraints{{"bar", sets.Full()}})
	p.AddDependencies("foo", 3, constraints{{"bar", sets.Full()}})
	p.AddDependencies("foo", 4, constraints{{"bar", sets.Full()}})
	p.AddDependencies("foo", 5, constraints{{"bar", sets.Full()}})
	p.AddDependencies("baz", 1, nil)
	failure := requireNoSolution(t, p, "root", 1)
	want := "Because there is no available version for bar and foo 1 | 2 | 3 | 4 | 5 depends on bar, foo 1 | 2 | 3 | 4 | 5 is forbidden.\nAnd because there is no version of foo in <1 | >1, <2 | >2, <3 | >3, <4 | >4, <5 | >5 and root 1 depends on foo, root 1 is forbidden."
	if got := pubgrub.Report(failure.Tree); got != want {
		t.Fatalf("report:\n%s\nwant:\n%s", got, want)
	}
	original := failure.Tree.Clone()
	failure.Tree.CollapseNoVersions()
	if got := pubgrub.Report(failure.Tree); got != "Because foo depends on bar and root 1 depends on foo, root 1 is forbidden." {
		t.Fatal(got)
	}
	if got := pubgrub.Report(original); got != want {
		t.Fatal("collapse changed original proof")
	}
	if !reflect.DeepEqual(failure.Tree.Packages(), map[string]struct{}{"root": {}, "foo": {}, "bar": {}}) {
		t.Fatal(failure.Tree.Packages())
	}
}
func TestContextCancellation(t *testing.T) {
	p := provider()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pubgrub.ResolveContext(ctx, p, "root", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

type instrumentedProvider struct {
	*pubgrub.OfflineDependencyProvider[string, int, intSet]
	chooseError, dependenciesError, cancelError error
	unavailable                                 bool
	invalidVersion                              bool
	constantPriority                            bool
}

func (p *instrumentedProvider) ShouldCancel() error { return p.cancelError }
func (p *instrumentedProvider) ChooseVersion(name string, set intSet) (int, bool, error) {
	if p.chooseError != nil {
		return 0, false, p.chooseError
	}
	if p.invalidVersion {
		return -123, true, nil
	}
	return p.OfflineDependencyProvider.ChooseVersion(name, set)
}
func (p *instrumentedProvider) GetDependencies(name string, v int) (pubgrub.Dependencies[string, intSet, string], error) {
	if p.dependenciesError != nil {
		return pubgrub.Dependencies[string, intSet, string]{}, p.dependenciesError
	}
	if p.unavailable {
		return pubgrub.Unavailable[string, intSet]("offline"), nil
	}
	return p.OfflineDependencyProvider.GetDependencies(name, v)
}
func (p *instrumentedProvider) Prioritize(name string, set intSet, s pubgrub.PackageResolutionStatistics) pubgrub.OfflinePriority {
	if p.constantPriority {
		return pubgrub.OfflinePriority{}
	}
	return p.OfflineDependencyProvider.Prioritize(name, set, s)
}
func TestProviderChooseError(t *testing.T) {
	sentinel := errors.New("choose failed")
	p := &instrumentedProvider{OfflineDependencyProvider: provider(), chooseError: sentinel}
	_, err := pubgrub.Resolve(p, "root", 1)
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}
func TestProviderDependenciesError(t *testing.T) {
	sentinel := errors.New("metadata failed")
	p := &instrumentedProvider{OfflineDependencyProvider: provider(), dependenciesError: sentinel}
	p.AddDependencies("root", 1, nil)
	_, err := pubgrub.Resolve(p, "root", 1)
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}
func TestProviderCancellation(t *testing.T) {
	sentinel := errors.New("cancelled")
	p := &instrumentedProvider{OfflineDependencyProvider: provider(), cancelError: sentinel}
	_, err := pubgrub.Resolve(p, "root", 1)
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}
func TestUnavailableMetadata(t *testing.T) {
	p := &instrumentedProvider{OfflineDependencyProvider: provider(), unavailable: true}
	p.AddDependencies("root", 1, nil)
	_, err := pubgrub.Resolve(p, "root", 1)
	var failure *pubgrub.ResolveError[string, int, intSet, string]
	if !errors.As(err, &failure) || failure.Kind != pubgrub.NoSolution {
		t.Fatal(err)
	}
	if failure.Tree.External == nil || failure.Tree.External.Kind != pubgrub.ExternalCustom || failure.Tree.External.Reason != "offline" {
		t.Fatalf("%#v", failure.Tree)
	}
}
func TestInvalidChosenVersion(t *testing.T) {
	p := &instrumentedProvider{OfflineDependencyProvider: provider(), invalidVersion: true}
	_, err := pubgrub.Resolve(p, "root", 1)
	var failure *pubgrub.ResolveError[string, int, intSet, string]
	if !errors.As(err, &failure) || failure.Kind != pubgrub.InvalidProvider {
		t.Fatal(err)
	}
}
func TestPriorityTieUsesDiscoveryOrder(t *testing.T) {
	p := &instrumentedProvider{OfflineDependencyProvider: provider(), constantPriority: true}
	p.AddDependencies("root", 1, constraints{{"b", sets.Full()}, {"a", sets.Full()}})
	p.AddDependencies("b", 2, constraints{{"a", sets.Singleton(1)}})
	p.AddDependencies("b", 1, nil)
	p.AddDependencies("a", 2, constraints{{"b", sets.Singleton(1)}})
	p.AddDependencies("a", 1, nil)
	got, err := pubgrub.Resolve(p, "root", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Map(), map[string]int{"root": 1, "b": 2, "a": 1}) {
		t.Fatal(got.Map())
	}
}

// Brute-force enumeration is independent of propagation, clause learning and
// backtracking. Absence is an explicit choice, so optional packages and cycles
// are checked as well as ordinary version conflicts.
func checkAgainstBruteForce(t *testing.T, data []byte) {
	t.Helper()
	if len(data) == 0 {
		return
	}
	position := 0
	next := func() byte { v := data[position%len(data)]; position++; return v }
	const count = 4
	p := pubgrub.NewOfflineDependencyProvider[int](sets, cmp.Compare[int])
	metadata := make(map[[2]int]pubgrub.DependencyConstraints[int, intSet])
	for name := 0; name < count; name++ {
		versionCount := int(next() % 4)
		if name == 0 {
			versionCount = 1
		}
		for version := 1; version <= versionCount; version++ {
			deps := make(pubgrub.DependencyConstraints[int, intSet], 0)
			n := int(next() % 5)
			for j := 0; j < n; j++ {
				dep := int(next() % count)
				mask := next() % 16
				set := sets.Empty()
				for v := 0; v < 4; v++ {
					if mask&(1<<v) != 0 {
						set = set.Union(sets.Singleton(v))
					}
				}
				deps = append(deps, pubgrub.DependencyConstraint[int, intSet]{Package: dep, Versions: set})
			}
			p.AddDependencies(name, version, deps)
			metadata[[2]int{name, version}] = deps
		}
	}
	valid := func(values []int) bool {
		if values[0] != 1 {
			return false
		}
		for name, version := range values {
			if version == 0 {
				continue
			}
			deps, ok := metadata[[2]int{name, version}]
			if !ok {
				return false
			}
			for _, dep := range deps {
				if values[dep.Package] == 0 || !dep.Versions.Contains(values[dep.Package]) {
					return false
				}
			}
		}
		return true
	}
	satisfiable := false
	for encoding := 0; encoding < 1<<(2*count); encoding++ {
		values := make([]int, count)
		for name := range values {
			values[name] = (encoding >> (2 * name)) & 3
		}
		if valid(values) {
			satisfiable = true
			break
		}
	}
	result, err := pubgrub.Resolve(p, 0, 1)
	if err != nil {
		var failure *pubgrub.ResolveError[int, int, intSet, string]
		if !errors.As(err, &failure) || failure.Kind != pubgrub.NoSolution {
			t.Fatalf("data %x: %v", data, err)
		}
		if satisfiable {
			t.Fatalf("false unsat for %x: %s", data, pubgrub.Report(failure.Tree))
		}
		return
	}
	if !satisfiable {
		t.Fatalf("false sat for %x: %v", data, result.Map())
	}
	values := make([]int, count)
	for name, version := range result.Map() {
		values[name] = version
	}
	if !valid(values) {
		t.Fatalf("invalid solution for %x: %v", data, result.Map())
	}
}
func TestRandomGraphsAgainstBruteForce(t *testing.T) {
	random := rand.New(rand.NewSource(72639))
	for sample := 0; sample < 3000; sample++ {
		data := make([]byte, 128)
		if _, err := random.Read(data); err != nil {
			t.Fatal(err)
		}
		checkAgainstBruteForce(t, data)
	}
}
func FuzzResolution(f *testing.F) {
	f.Add([]byte{1, 2, 3, 0, 7, 5, 10, 4, 1})
	f.Add([]byte{3, 2, 7, 1, 9, 12, 0, 6})
	f.Fuzz(checkAgainstBruteForce)
}
