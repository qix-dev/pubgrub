// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	pubgrub "pubgrub-go"
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
