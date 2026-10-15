// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/qix-dev/pubgrub"
)

func TestStateCloneAndManualBacktrack(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"a", sets.Full()}})
	p.AddDependencies("a", 1, nil)
	p.AddDependencies("a", 2, nil)
	state := pubgrub.NewState(p, "root", 1)
	if _, err := state.UnitPropagation(state.RootPackage); err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.AddPackageVersionDependencies(state.RootPackage, 1, sets.Singleton(1), constraints{{"a", sets.Full()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.UnitPropagation(state.RootPackage); err != nil {
		t.Fatal(err)
	}
	a := state.InternPackage("a")
	unchanging, ok := state.UnchangingTerm(a)
	if !ok || !unchanging.Positive || !unchanging.Set.IsFull() {
		t.Fatal(unchanging)
	}
	cloned := state.Clone()
	if err := state.AddDecision(a, 2); err != nil {
		t.Fatal(err)
	}
	if state.DecisionLevel() != 2 || cloned.DecisionLevel() != 1 {
		t.Fatal("clone shares assignments")
	}
	cloned.AddUnavailable(a, sets.Singleton(2), "disabled")
	if _, err := cloned.UnitPropagation(a); err != nil {
		t.Fatal(err)
	}
	if err := cloned.AddDecision(a, 1); err != nil {
		t.Fatal(err)
	}
	if got := state.ExtractSolution().Map(); !reflect.DeepEqual(got, map[string]int{"root": 1, "a": 2}) {
		t.Fatal(got)
	}
	if got := cloned.ExtractSolution().Map(); !reflect.DeepEqual(got, map[string]int{"root": 1, "a": 1}) {
		t.Fatal(got)
	}
	if count, ok := state.BacktrackPackage(a); !ok || count != 1 {
		t.Fatalf("%d %v", count, ok)
	}
	if _, ok := state.BacktrackPackage(a); ok {
		t.Fatal("undecided package was backtracked")
	}
	if !state.HasBacktracked() {
		t.Fatal("backtrack generation missing")
	}
	if err := state.AddDecision(a, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := cloned.ExtractSolution().Get("a"); got != 1 {
		t.Fatal(got)
	}
}
func TestStateIntrospectionThroughBacktrack(t *testing.T) {
	p := provider()
	rootDeps := constraints{{"a", sets.Between(1, 4)}, {"b", sets.Between(1, 3)}, {"c", sets.Singleton(1)}}
	p.AddDependencies("root", 1, rootDeps)
	for _, name := range []string{"a", "b", "c"} {
		for v := 1; v <= 3; v++ {
			p.AddDependencies(name, v, nil)
		}
	}
	cDeps := constraints{{"a", sets.Between(2, 4)}}
	p.AddDependencies("c", 1, cDeps)
	state := pubgrub.NewState(p, "root", 1)
	a, b, c := state.InternPackage("a"), state.InternPackage("b"), state.InternPackage("c")
	forbidden := state.InternPackage("forbidden")
	unknown := state.InternPackage("unknown")
	assertTerm := func(id pubgrub.PackageID, want pubgrub.Term[int, intSet]) {
		t.Helper()
		got, ok := state.TermIntersection(id)
		if !ok || !got.Equal(want) {
			t.Fatalf("TermIntersection(%d) = %v, %v; want %v", id, got, ok, want)
		}
	}
	assertPrioritized := func(want map[pubgrub.PackageID]intSet) {
		t.Helper()
		got := state.PrioritizedPackages()
		if len(got) != len(want) {
			t.Fatalf("PrioritizedPackages() = %v; want %v", got, want)
		}
		seen := make(map[pubgrub.PackageID]bool)
		for _, entry := range got {
			versions, ok := want[entry.Package]
			if !ok || seen[entry.Package] || !entry.Versions.Equal(versions) {
				t.Fatalf("unexpected prioritized package: %v; want %v", entry, want)
			}
			seen[entry.Package] = true
		}
	}
	assertQueued := func(want map[pubgrub.PackageID]pubgrub.OfflinePriority) {
		t.Helper()
		got := state.UndecidedPackages()
		if len(got) != len(want) {
			t.Fatalf("UndecidedPackages() = %v; want %v", got, want)
		}
		seen := make(map[pubgrub.PackageID]bool)
		for _, entry := range got {
			priority, ok := want[entry.Package]
			if !ok || seen[entry.Package] || entry.Priority != priority {
				t.Fatalf("unexpected queued package: %v; want %v", entry, want)
			}
			seen[entry.Package] = true
		}
	}
	propagate := func(id pubgrub.PackageID) {
		t.Helper()
		if conflicts, err := state.UnitPropagation(id); err != nil || len(conflicts) != 0 {
			t.Fatalf("UnitPropagation(%d): conflicts=%v, err=%v", id, conflicts, err)
		}
	}
	pick := func(wantID pubgrub.PackageID, wantVersions intSet) {
		t.Helper()
		id, versions, ok := state.PickHighestPriorityPackage(func(id pubgrub.PackageID, versions intSet) pubgrub.OfflinePriority {
			name, ok := state.Package(id)
			if !ok {
				t.Fatalf("unknown package ID %d", id)
			}
			return p.Prioritize(name, versions, pubgrub.PackageResolutionStatistics{})
		})
		if !ok || id != wantID || !versions.Equal(wantVersions) {
			t.Fatalf("picked %d %v, %v; want %d %v", id, versions, ok, wantID, wantVersions)
		}
	}

	if _, ok := state.TermIntersection(state.RootPackage); ok {
		t.Fatal("root has a term before propagation")
	}
	assertPrioritized(nil)
	assertQueued(nil)
	propagate(state.RootPackage)
	assertTerm(state.RootPackage, pubgrub.Positive[int](sets.Singleton(1)))
	assertPrioritized(map[pubgrub.PackageID]intSet{state.RootPackage: sets.Singleton(1)})
	pick(state.RootPackage, sets.Singleton(1))
	if _, rejected, err := state.AddPackageVersionDependencies(state.RootPackage, 1, sets.Singleton(1), rootDeps); err != nil || rejected {
		t.Fatalf("root dependencies: rejected=%v, err=%v", rejected, err)
	}
	propagate(state.RootPackage)
	state.AddUnavailable(forbidden, sets.Singleton(9), "disabled")
	propagate(forbidden)
	assertTerm(a, pubgrub.Positive[int](sets.Between(1, 4)))
	assertTerm(forbidden, pubgrub.Negative[int](sets.Singleton(9)))
	if _, ok := state.TermIntersection(unknown); ok {
		t.Fatal("unassigned package has a term")
	}
	initial := map[pubgrub.PackageID]intSet{a: sets.Between(1, 4), b: sets.Between(1, 3), c: sets.Singleton(1)}
	assertPrioritized(initial)
	// Priorities are populated lazily, and picking removes the chosen package.
	assertQueued(nil)
	pick(c, sets.Singleton(1))
	queued := map[pubgrub.PackageID]pubgrub.OfflinePriority{a: {VersionCount: 3}, b: {VersionCount: 2}}
	assertQueued(queued)
	snapshot := state.UndecidedPackages()
	snapshot[0].Package = unknown
	snapshot[0].Priority.VersionCount = 99
	assertQueued(queued)
	oldTerm, _ := state.TermIntersection(a)

	if _, rejected, err := state.AddPackageVersionDependencies(c, 1, sets.Singleton(1), cDeps); err != nil || rejected {
		t.Fatalf("c dependencies: rejected=%v, err=%v", rejected, err)
	}
	propagate(c)
	assertTerm(a, pubgrub.Positive[int](sets.Between(2, 4)))
	if !oldTerm.Equal(pubgrub.Positive[int](sets.Between(1, 4))) {
		t.Fatalf("previous term changed after propagation: %v", oldTerm)
	}
	// Only a changed at this decision level; b is still pending at the prior level.
	assertPrioritized(map[pubgrub.PackageID]intSet{a: sets.Between(2, 4)})
	pick(a, sets.Between(2, 4))
	assertQueued(map[pubgrub.PackageID]pubgrub.OfflinePriority{b: {VersionCount: 2}})
	if err := state.AddDecision(a, 3); err != nil {
		t.Fatal(err)
	}
	assertTerm(a, pubgrub.Positive[int](sets.Singleton(3)))
	assertPrioritized(nil)

	if count, ok := state.BacktrackPackage(c); !ok || count != 2 || state.DecisionLevel() != 1 {
		t.Fatalf("backtrack c: count=%d, ok=%v, level=%d", count, ok, state.DecisionLevel())
	}
	assertTerm(a, pubgrub.Positive[int](sets.Between(1, 4)))
	assertTerm(c, pubgrub.Positive[int](sets.Singleton(1)))
	assertPrioritized(initial)
	pick(c, sets.Singleton(1))
	assertQueued(queued)
}
func TestMergedRecurringDependencies(t *testing.T) {
	state := pubgrub.NewState(provider(), "root", 0)
	p, dependency := state.InternPackage("package"), state.InternPackage("dependency")
	state.AddDependency(p, sets.Singleton(0), dependency, sets.Singleton(0))
	state.AddDependency(p, sets.Singleton(1), dependency, sets.Singleton(2))
	state.AddDependency(p, sets.Singleton(2), dependency, sets.Singleton(0))
	deps := state.Dependencies(p)
	if len(deps) != 2 {
		t.Fatal(deps)
	}
	if !deps[1].DependentVersions.Equal(sets.Singleton(0).Union(sets.Singleton(2))) {
		t.Fatal(deps)
	}
	if len(state.Dependencies(dependency)) != 2 {
		t.Fatal("reverse dependency view inconsistent")
	}
	state.AddNoVersions(dependency, sets.Singleton(9))
	if len(state.Dependencies(dependency)) != 2 {
		t.Fatal("unavailable constraint leaked into dependency view")
	}
}
func TestDependencyViewForSelfAndEmpty(t *testing.T) {
	state := pubgrub.NewState(provider(), "root", 0)
	p := state.InternPackage("p")
	q := state.InternPackage("q")
	state.AddDependency(p, sets.Between(1, 4), p, sets.Between(2, 4))
	state.AddDependency(p, sets.Singleton(5), q, sets.Empty())
	deps := state.Dependencies(p)
	if len(deps) != 2 || deps[0].HasDependencyVersions || deps[1].HasDependencyVersions {
		t.Fatal(deps)
	}
	if !deps[0].DependentVersions.Equal(sets.Between(1, 2)) {
		t.Fatal(deps[0])
	}
}
func TestManualLoopWithWidenedDependencies(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"a", sets.Full()}})
	p.AddDependencies("a", 1, nil)
	p.AddDependencies("a", 2, constraints{{"missing", sets.Full()}})
	state := pubgrub.NewState(p, "root", 1)
	if _, err := state.UnitPropagation(state.RootPackage); err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.AddPackageVersionDependencies(state.RootPackage, 1, sets.Singleton(1), constraints{{"a", sets.Full()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.UnitPropagation(state.RootPackage); err != nil {
		t.Fatal(err)
	}
	a := state.InternPackage("a")
	if _, _, err := state.AddPackageVersionDependencies(a, 2, sets.Singleton(2).WidenVersions([]int{1, 2}), constraints{{"missing", sets.Full()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.UnitPropagation(a); err != nil {
		t.Fatal(err)
	}
	missing := state.InternPackage("missing")
	state.AddNoVersions(missing, sets.Full())
	conflicts, err := state.UnitPropagation(missing)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) == 0 || !state.HasBacktracked() {
		t.Fatal("expected learned conflict")
	}
	if _, _, err := state.AddPackageVersionDependencies(a, 1, sets.Singleton(1).WidenVersions([]int{1, 2}), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := state.UnitPropagation(a); err != nil {
		t.Fatal(err)
	}
	if got := state.ExtractSolution().Map(); !reflect.DeepEqual(got, map[string]int{"root": 1, "a": 1}) {
		t.Fatal(got)
	}
}
func TestProviderRoundTripAndReplace(t *testing.T) {
	p := provider()
	p.AddDependencies("root", 1, constraints{{"a", sets.Full()}})
	p.AddDependencies("root", 1, constraints{{"a", sets.HigherThan(1)}, {"a", sets.LowerThan(1)}})
	p.AddDependencies("a", 1, nil)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	q := provider()
	if err := json.Unmarshal(data, q); err != nil {
		t.Fatal(err)
	}
	assertSolution(t, q, "root", 1, map[string]int{"root": 1, "a": 1})
	deps, err := q.GetDependencies("root", 1)
	if err != nil || len(deps.Constraints) != 2 {
		t.Fatalf("%v %v", deps, err)
	}
	copy := p.Clone()
	copy.AddDependencies("root", 1, nil)
	assertSolution(t, p, "root", 1, map[string]int{"root": 1, "a": 1})
	assertSolution(t, copy, "root", 1, map[string]int{"root": 1})
}
func TestProviderCountsCandidates(t *testing.T) {
	p := provider()
	p.AddDependencies("p", 1, nil)
	p.AddDependencies("p", 3, nil)
	p.AddDependencies("p", 5, nil)
	p.AddDependencies("p", 7, nil)
	got := p.Prioritize("p", sets.Between(2, 7), pubgrub.PackageResolutionStatistics{DependenciesAffected: 3})
	if got != (pubgrub.OfflinePriority{Conflicts: 3, VersionCount: 2}) {
		t.Fatal(got)
	}
	if got := p.Prioritize("p", sets.HigherThan(9), pubgrub.PackageResolutionStatistics{}); got != (pubgrub.OfflinePriority{Conflicts: math.MaxUint32}) {
		t.Fatal(got)
	}
}
func TestSemanticVersion(t *testing.T) {
	v, err := pubgrub.ParseSemanticVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if v != pubgrub.NewSemanticVersion(1, 2, 3) {
		t.Fatal(v)
	}
	next, err := v.BumpMinor()
	if err != nil || next.String() != "1.3.0" {
		t.Fatalf("%s %v", next, err)
	}
	if cmp.Compare(pubgrub.CompareSemanticVersions(v, next), 0) >= 0 {
		t.Fatal("ordering")
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"1.2.3"` {
		t.Fatal(string(data))
	}
	var restored pubgrub.SemanticVersion
	if err := json.Unmarshal(data, &restored); err != nil || restored != v {
		t.Fatalf("%v %v", restored, err)
	}
}
func TestSemanticVersionParseFailure(t *testing.T) {
	_, err := pubgrub.ParseSemanticVersion("1.2")
	var failure *pubgrub.VersionParseError
	if !errors.As(err, &failure) || failure.Kind != pubgrub.NotThreeParts {
		t.Fatal(err)
	}
}
func TestSemanticVersionOverflow(t *testing.T) {
	_, err := pubgrub.ParseSemanticVersion("1.2.9876543210")
	if err == nil {
		t.Fatal("overflow accepted")
	}
	if _, err := pubgrub.NewSemanticVersion(1, 2, math.MaxUint32).BumpPatch(); err == nil {
		t.Fatal("overflow bump")
	}
}
func TestSemanticVersionBumps(t *testing.T) {
	version := pubgrub.NewSemanticVersion
	cases := []struct {
		name        string
		bump        func(pubgrub.SemanticVersion) (pubgrub.SemanticVersion, error)
		input, want pubgrub.SemanticVersion
		wantError   string
	}{
		{"patch", pubgrub.SemanticVersion.BumpPatch, version(1, 2, 3), version(1, 2, 4), ""},
		{"patch maximum", pubgrub.SemanticVersion.BumpPatch, version(1, 2, math.MaxUint32-1), version(1, 2, math.MaxUint32), ""},
		{"patch overflow", pubgrub.SemanticVersion.BumpPatch, version(1, 2, math.MaxUint32), version(1, 2, math.MaxUint32), "patch version overflow"},
		{"minor resets patch", pubgrub.SemanticVersion.BumpMinor, version(1, 2, math.MaxUint32), version(1, 3, 0), ""},
		{"minor maximum", pubgrub.SemanticVersion.BumpMinor, version(1, math.MaxUint32-1, 3), version(1, math.MaxUint32, 0), ""},
		{"minor overflow", pubgrub.SemanticVersion.BumpMinor, version(1, math.MaxUint32, 3), version(1, math.MaxUint32, 3), "minor version overflow"},
		{"major resets minor and patch", pubgrub.SemanticVersion.BumpMajor, version(1, math.MaxUint32, math.MaxUint32), version(2, 0, 0), ""},
		{"major maximum", pubgrub.SemanticVersion.BumpMajor, version(math.MaxUint32-1, 2, 3), version(math.MaxUint32, 0, 0), ""},
		{"major overflow", pubgrub.SemanticVersion.BumpMajor, version(math.MaxUint32, 2, 3), version(math.MaxUint32, 2, 3), "major version overflow"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.bump(c.input)
			if c.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != c.wantError {
				t.Fatalf("error = %v; want %q", err, c.wantError)
			}
			if got != c.want {
				t.Fatalf("bump(%s) = %s; want %s", c.input, got, c.want)
			}
		})
	}
}
func TestSemanticVersionText(t *testing.T) {
	cases := []struct {
		version pubgrub.SemanticVersion
		text    string
	}{
		{pubgrub.NewSemanticVersion(0, 0, 0), "0.0.0"},
		{pubgrub.NewSemanticVersion(1, 2, 3), "1.2.3"},
		{pubgrub.NewSemanticVersion(math.MaxUint32, math.MaxUint32, math.MaxUint32), "4294967295.4294967295.4294967295"},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			data, err := c.version.MarshalText()
			if err != nil || string(data) != c.text {
				t.Fatalf("MarshalText() = %q, %v; want %q", data, err, c.text)
			}
			restored := pubgrub.NewSemanticVersion(9, 8, 7)
			if err := restored.UnmarshalText(data); err != nil || restored != c.version {
				t.Fatalf("UnmarshalText(%q) = %v, %v; want %v", data, restored, err, c.version)
			}
		})
	}
	for _, text := range []string{"1.2", "1.bad.3", "4294967296.2.3"} {
		t.Run("invalid/"+text, func(t *testing.T) {
			before := pubgrub.NewSemanticVersion(9, 8, 7)
			got := before
			if err := got.UnmarshalText([]byte(text)); err == nil {
				t.Fatal("invalid version accepted")
			}
			if got != before {
				t.Fatalf("failed decode changed receiver: got %v; want %v", got, before)
			}
		})
	}
}
