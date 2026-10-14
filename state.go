// SPDX-License-Identifier: MPL-2.0
package pubgrub

import (
	"fmt"
	"slices"
)

type dependencyKey struct{ dependent, dependency PackageID }
type PropagationConflict struct {
	Package  PackageID
	Conflict ConflictID
}
type DependencyView[S any] struct {
	Dependent, Dependency                 PackageID
	DependentVersions, DependencyVersions S
	HasDependencyVersions                 bool
}
type PackageRange[S any] struct {
	Package  PackageID
	Versions S
}
type PackagePriority[R any] struct {
	Package  PackageID
	Priority R
}

// State exposes the incremental solver primitives needed by callers that drive
// their own loop (such as a resolver splitting environments). The provider is
// shared by Clone; all solver-owned mutable state is copied. Instances are not
// safe for concurrent mutation.
type State[P comparable, V any, S VersionSet[V, S], M any, R any] struct {
	provider    DependencyProvider[P, V, S, M, R]
	RootPackage PackageID
	rootVersion V
	packages    []P
	packageIDs  map[P]PackageID
	byPackage   map[PackageID][]ConflictID
	merged      map[dependencyKey][]ConflictID
	store       []incompatibility[V, S, M]
	partial     partialSolution[V, S, R]
}

func NewState[P comparable, V any, S VersionSet[V, S], M any, R any](provider DependencyProvider[P, V, S, M, R], root P, version V) *State[P, V, S, M, R] {
	s := &State[P, V, S, M, R]{provider: provider, rootVersion: version, packageIDs: make(map[P]PackageID), byPackage: make(map[PackageID][]ConflictID), merged: make(map[dependencyKey][]ConflictID), partial: newPartial[V, S](provider.ComparePriorities)}
	s.RootPackage = s.InternPackage(root)
	s.add(incompatibility[V, S, M]{terms: []packageTerm[V, S]{{s.RootPackage, Negative[V](provider.EmptySet().Singleton(version))}}, kind: notRoot, packageID: s.RootPackage, version: version})
	return s
}
func (s *State[P, V, S, M, R]) Clone() *State[P, V, S, M, R] {
	out := *s
	out.packages = slices.Clone(s.packages)
	out.packageIDs = make(map[P]PackageID, len(s.packageIDs))
	for p, id := range s.packageIDs {
		out.packageIDs[p] = id
	}
	out.byPackage = make(map[PackageID][]ConflictID, len(s.byPackage))
	for p, ids := range s.byPackage {
		out.byPackage[p] = slices.Clone(ids)
	}
	out.merged = make(map[dependencyKey][]ConflictID, len(s.merged))
	for key, ids := range s.merged {
		out.merged[key] = slices.Clone(ids)
	}
	out.store = make([]incompatibility[V, S, M], len(s.store))
	for index, i := range s.store {
		out.store[index] = i.clone()
	}
	out.partial = s.partial.clone()
	return &out
}
func (s *State[P, V, S, M, R]) InternPackage(p P) PackageID {
	if id, ok := s.packageIDs[p]; ok {
		return id
	}
	id := PackageID(len(s.packages))
	s.packages = append(s.packages, p)
	s.packageIDs[p] = id
	return id
}
func (s *State[P, V, S, M, R]) Package(id PackageID) (P, bool) {
	if id < 0 || int(id) >= len(s.packages) {
		var zero P
		return zero, false
	}
	return s.packages[id], true
}
func (s *State[P, V, S, M, R]) Packages() []P             { return slices.Clone(s.packages) }
func (s *State[P, V, S, M, R]) DecisionLevel() int        { return s.partial.level }
func (s *State[P, V, S, M, R]) HasBacktracked() bool      { return len(s.partial.lastValidLevels) > 1 }
func (s *State[P, V, S, M, R]) IncompatibilityCount() int { return len(s.store) }
func (s *State[P, V, S, M, R]) alloc(i incompatibility[V, S, M]) ConflictID {
	id := ConflictID(len(s.store))
	s.store = append(s.store, i)
	return id
}
func (s *State[P, V, S, M, R]) add(i incompatibility[V, S, M]) ConflictID {
	id := s.alloc(i)
	s.merge(id)
	return id
}
func (s *State[P, V, S, M, R]) merge(id ConflictID) {
	current := s.store[id]
	if current.kind == fromDependency && current.packageID != current.dependencyID {
		key := dependencyKey{current.packageID, current.dependencyID}
		bucket := s.merged[key]
		found := false
		dep, hasDep := current.get(current.dependencyID)
		for index, pastID := range bucket {
			past := s.store[pastID]
			pastDep, pastHasDep := past.get(current.dependencyID)
			if hasDep != pastHasDep || hasDep && !dep.Equal(pastDep) {
				continue
			}
			versions, _ := current.get(current.packageID)
			pastVersions, _ := past.get(current.packageID)
			depVersions := s.provider.EmptySet()
			if hasDep {
				depVersions = dep.Set.Clone()
			}
			merged := dependencyIncompatibility[V, S, M](current.packageID, versions.Set.Union(pastVersions.Set), current.dependencyID, depVersions)
			id = s.alloc(merged)
			for _, term := range merged.terms {
				s.byPackage[term.packageID] = slices.DeleteFunc(s.byPackage[term.packageID], func(i ConflictID) bool { return i == pastID })
			}
			bucket[index] = id
			found = true
			break
		}
		if !found {
			bucket = append(bucket, id)
		}
		s.merged[key] = bucket
	}
	for _, entry := range s.store[id].terms {
		s.byPackage[entry.packageID] = append(s.byPackage[entry.packageID], id)
	}
}
func (s *State[P, V, S, M, R]) AddNoVersions(p PackageID, versions S) {
	s.add(incompatibility[V, S, M]{terms: []packageTerm[V, S]{{p, Positive[V](versions.Clone())}}, kind: noVersions, packageID: p, versions: versions.Clone()})
}
func (s *State[P, V, S, M, R]) AddUnavailable(p PackageID, versions S, reason M) {
	s.add(incompatibility[V, S, M]{terms: []packageTerm[V, S]{{p, Positive[V](versions.Clone())}}, kind: custom, packageID: p, versions: versions.Clone(), reason: reason})
}
func (s *State[P, V, S, M, R]) AddDependency(p PackageID, versions S, dependency PackageID, dependencyVersions S) {
	s.add(dependencyIncompatibility[V, S, M](p, versions.Clone(), dependency, dependencyVersions.Clone()))
}
func (s *State[P, V, S, M, R]) AddDecision(p PackageID, version V) error {
	return s.partial.addDecision(p, version)
}

// AddPackageVersionDependencies records metadata, merging equivalent dependency
// constraints. versions must contain version and every existing version it
// contains must have identical dependencies. Returns a rejected decision's
// conflict, or commits the decision when no dependency conflict is present.
func (s *State[P, V, S, M, R]) AddPackageVersionDependencies(p PackageID, version V, versions S, dependencies DependencyConstraints[P, S]) (ConflictID, bool, error) {
	if !versions.Contains(version) {
		return 0, false, fmt.Errorf("dependent version set does not contain chosen version")
	}
	first := len(s.store)
	for _, dep := range dependencies {
		s.alloc(dependencyIncompatibility[V, S, M](p, versions.Clone(), s.InternPackage(dep.Package), dep.Versions.Clone()))
	}
	end := len(s.store)
	for i := first; i < end; i++ {
		s.merge(ConflictID(i))
	}
	if s.HasBacktracked() {
		exact := Positive[V](versions.Singleton(version))
		for i := first; i < end; i++ {
			relation, _ := s.store[i].relation(func(id PackageID) (Term[V, S], bool) {
				if id == p {
					return exact, true
				}
				return s.partial.lookup(id)
			})
			if relation == incompatSatisfied {
				return ConflictID(i), true, nil
			}
		}
	}
	return 0, false, s.partial.addDecision(p, version)
}
func (s *State[P, V, S, M, R]) TermIntersection(p PackageID) (Term[V, S], bool) {
	term, ok := s.partial.lookup(p)
	if ok {
		return term.Clone(), true
	}
	return term, false
}
func (s *State[P, V, S, M, R]) UnchangingTerm(p PackageID) (Term[V, S], bool) {
	if a, ok := s.partial.assignments[p]; ok {
		for i := len(a.derivations) - 1; i >= 0; i-- {
			if a.derivations[i].level <= 1 {
				return a.derivations[i].accumulated.Clone(), true
			}
		}
	}
	return Term[V, S]{}, false
}
func (s *State[P, V, S, M, R]) PickHighestPriorityPackage(prioritize func(PackageID, S) R) (PackageID, S, bool) {
	return s.partial.pick(prioritize)
}
func (s *State[P, V, S, M, R]) PrioritizedPackages() []PackageRange[S] {
	var out []PackageRange[S]
	for _, id := range s.partial.order[s.partial.level:] {
		a := s.partial.assignments[id]
		if a.lastLevel == s.partial.level && !a.decided && a.term.Positive {
			out = append(out, PackageRange[S]{id, a.term.Set.Clone()})
		}
	}
	return out
}
func (s *State[P, V, S, M, R]) UndecidedPackages() []PackagePriority[R] {
	out := make([]PackagePriority[R], 0, len(s.partial.priorities.entries))
	for _, entry := range s.partial.priorities.entries {
		out = append(out, PackagePriority[R]{entry.packageID, entry.priority})
	}
	return out
}
func (s *State[P, V, S, M, R]) ExtractSolution() SelectedDependencies[P, V] {
	out := make([]Selection[P, V], 0, s.partial.level)
	for _, id := range s.partial.order[:s.partial.level] {
		out = append(out, Selection[P, V]{s.packages[id], s.partial.assignments[id].version})
	}
	return newSelected(out)
}
func (s *State[P, V, S, M, R]) BacktrackPackage(p PackageID) (int, bool) {
	index := slices.Index(s.partial.order, p)
	if index < 0 || index >= s.partial.level {
		return 0, false
	}
	count := s.partial.level - index
	s.partial.backtrack(index)
	return count, true
}
func (s *State[P, V, S, M, R]) Dependencies(p PackageID) []DependencyView[S] {
	var out []DependencyView[S]
	for _, id := range s.byPackage[p] {
		i := s.store[id]
		if i.kind != fromDependency {
			continue
		}
		versions, _ := i.get(i.packageID)
		dep, ok := i.get(i.dependencyID)
		ok = ok && len(i.terms) > 1
		depVersions := s.provider.EmptySet()
		if ok {
			depVersions = dep.Set.Clone()
		}
		out = append(out, DependencyView[S]{i.packageID, i.dependencyID, versions.Set.Clone(), depVersions, ok})
	}
	return out
}
func (s *State[P, V, S, M, R]) ConflictPackages(id ConflictID) []PackageID {
	var out []PackageID
	if id < 0 || int(id) >= len(s.store) {
		return out
	}
	for _, entry := range s.store[id].terms {
		out = append(out, entry.packageID)
	}
	return out
}
func (s *State[P, V, S, M, R]) contradicted(id ConflictID) bool {
	i := s.store[id]
	if !i.contradicted {
		return false
	}
	return i.contradictionGeneration >= len(s.partial.lastValidLevels) || i.contradictionLevel <= s.partial.lastValidLevels[i.contradictionGeneration]
}
func (s *State[P, V, S, M, R]) markContradicted(id ConflictID) {
	i := &s.store[id]
	i.contradicted = true
	i.contradictionLevel = s.partial.level
	i.contradictionGeneration = len(s.partial.lastValidLevels)
}
func (s *State[P, V, S, M, R]) derive(p PackageID, id ConflictID) error {
	term, ok := s.store[id].get(p)
	if !ok {
		return fmt.Errorf("derived package %d is absent from conflict %d", p, id)
	}
	return s.partial.addDerivation(p, id, term.Negate())
}

// UnitPropagation derives forced constraints, resolves conflicts, learns clauses
// and backjumps until no constraint changes. NoSolution includes the proof tree.
func (s *State[P, V, S, M, R]) UnitPropagation(p PackageID) ([]PropagationConflict, error) {
	work := []PackageID{p}
	var causes []PropagationConflict
	for len(work) > 0 {
		current := work[len(work)-1]
		work = work[:len(work)-1]
		ids := s.byPackage[current]
		conflict := ConflictID(-1)
		for index := len(ids) - 1; index >= 0; index-- {
			id := ids[index]
			if s.contradicted(id) {
				continue
			}
			relation, almost := s.store[id].relation(s.partial.lookup)
			switch relation {
			case incompatSatisfied:
				conflict = id
			case incompatAlmostSatisfied:
				if !slices.Contains(work, almost) {
					work = append(work, almost)
				}
				if err := s.derive(almost, id); err != nil {
					return causes, err
				}
				s.markContradicted(id)
			case incompatContradicted:
				s.markContradicted(id)
			case incompatInconclusive:
			}
			if conflict >= 0 {
				break
			}
		}
		if conflict >= 0 {
			almost, rootCause, err := s.resolveConflict(conflict, &causes)
			if err != nil {
				return causes, err
			}
			work = append(work[:0], almost)
			if err := s.derive(almost, rootCause); err != nil {
				return causes, err
			}
			s.markContradicted(rootCause)
		}
	}
	return causes, nil
}
func (s *State[P, V, S, M, R]) resolveConflict(id ConflictID, causes *[]PropagationConflict) (PackageID, ConflictID, error) {
	changed := false
	for {
		current := s.store[id]
		if current.terminal(s.RootPackage, s.rootVersion) {
			return 0, 0, &ResolveError[P, V, S, M]{Kind: NoSolution, Tree: s.derivationTree(id)}
		}
		p, previous, cause, same, err := s.satisfierSearch(current)
		if err != nil {
			return 0, 0, err
		}
		if !same {
			s.partial.backtrack(previous)
			if changed {
				s.merge(id)
			}
			*causes = append(*causes, PropagationConflict{p, id})
			return p, id, nil
		}
		id = s.alloc(priorCause(id, cause, p, s.store))
		*causes = append(*causes, PropagationConflict{p, id})
		changed = true
	}
}
func (s *State[P, V, S, M, R]) satisfierSearch(i incompatibility[V, S, M]) (PackageID, int, ConflictID, bool, error) {
	satisfied := make([]satisfier, len(i.terms))
	latest := -1
	for index, entry := range i.terms {
		a, ok := s.partial.assignments[entry.packageID]
		if !ok {
			return 0, 0, 0, false, fmt.Errorf("missing assignment for conflict package %d", entry.packageID)
		}
		sat, err := a.satisfier(entry.term.Negate())
		if err != nil {
			return 0, 0, 0, false, err
		}
		satisfied[index] = sat
		if latest < 0 || sat.index >= satisfied[latest].index {
			latest = index
		}
	}
	entry := i.terms[latest]
	sat := satisfied[latest]
	a := s.partial.assignments[entry.packageID]
	accumulated := a.term
	if sat.hasCause {
		term, ok := s.store[sat.cause].get(entry.packageID)
		if !ok {
			return 0, 0, 0, false, fmt.Errorf("satisfier cause missing package")
		}
		accumulated = term.Negate()
	}
	previous, err := a.satisfier(accumulated.Intersection(entry.term.Negate()))
	if err != nil {
		return 0, 0, 0, false, err
	}
	satisfied[latest] = previous
	latestPrevious := 0
	for index, one := range satisfied {
		if one.index >= satisfied[latestPrevious].index {
			latestPrevious = index
		}
	}
	level := max(1, satisfied[latestPrevious].level)
	same := level >= sat.level
	if same && !sat.hasCause {
		return 0, 0, 0, false, fmt.Errorf("decision cannot have a derivation cause")
	}
	return entry.packageID, level, sat.cause, same, nil
}
