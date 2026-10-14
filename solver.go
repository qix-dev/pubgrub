// SPDX-License-Identifier: MPL-2.0
package pubgrub

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Resolve finds a solution or returns a *ResolveError with a proof of failure.
func Resolve[P comparable, V any, S VersionSet[V, S], M any, R any](provider DependencyProvider[P, V, S, M, R], root P, version V) (SelectedDependencies[P, V], error) {
	return ResolveContext(context.Background(), provider, root, version)
}

// ResolveContext adds Go context cancellation to the provider cancellation hook.
func ResolveContext[P comparable, V any, S VersionSet[V, S], M any, R any](ctx context.Context, provider DependencyProvider[P, V, S, M, R], root P, version V) (SelectedDependencies[P, V], error) {
	state := NewState(provider, root, version)
	statistics := make(map[PackageID]PackageResolutionStatistics)
	type historyEntry struct {
		packageID PackageID
		version   V
	}
	var history []historyEntry
	var added map[PackageID][]V
	var empty SelectedDependencies[P, V]
	next := state.RootPackage
	track := func(affected PackageID, conflict ConflictID, propagation bool) {
		stat := statistics[affected]
		if propagation {
			stat.UnitPropagationAffected++
		} else {
			stat.DependenciesAffected++
		}
		statistics[affected] = stat
		for _, id := range state.ConflictPackages(conflict) {
			if id == affected {
				continue
			}
			stat := statistics[id]
			if propagation {
				stat.UnitPropagationCulprit++
			} else {
				stat.DependenciesCulprit++
			}
			statistics[id] = stat
		}
	}
	record := func(p PackageID, v V) bool {
		versions := added[p]
		index := sort.Search(len(versions), func(i int) bool { return provider.CompareVersions(versions[i], v) >= 0 })
		if index < len(versions) && provider.CompareVersions(versions[index], v) == 0 {
			return false
		}
		versions = append(versions, v)
		copy(versions[index+1:], versions[index:])
		versions[index] = v
		added[p] = versions
		return true
	}
	for {
		if err := ctx.Err(); err != nil {
			return empty, &ResolveError[P, V, S, M]{Kind: ErrorInShouldCancel, Source: err}
		}
		if err := provider.ShouldCancel(); err != nil {
			return empty, &ResolveError[P, V, S, M]{Kind: ErrorInShouldCancel, Source: err}
		}
		conflicts, err := state.UnitPropagation(next)
		if err != nil {
			var typed *ResolveError[P, V, S, M]
			if errors.As(err, &typed) {
				return empty, err
			}
			return empty, &ResolveError[P, V, S, M]{Kind: InternalError, Source: err}
		}
		for _, c := range conflicts {
			track(c.Package, c.Conflict, true)
		}
		chosen, set, ok := state.PickHighestPriorityPackage(func(id PackageID, set S) R { return provider.Prioritize(state.packages[id], set, statistics[id]) })
		if !ok {
			return state.ExtractSolution(), nil
		}
		next = chosen
		name := state.packages[chosen]
		version, ok, err := provider.ChooseVersion(name, set)
		if err != nil {
			return empty, &ResolveError[P, V, S, M]{Kind: ErrorChoosingVersion, Package: name, Source: err}
		}
		if !ok {
			state.AddNoVersions(chosen, set)
			continue
		}
		if !set.Contains(version) {
			return empty, &ResolveError[P, V, S, M]{Kind: InvalidProvider, Package: name, Version: version, Source: fmt.Errorf("chosen version %v is not in %s", version, set)}
		}
		isNew := true
		if !state.HasBacktracked() {
			history = append(history, historyEntry{chosen, version})
		} else {
			if added == nil {
				added = make(map[PackageID][]V)
				for _, entry := range history {
					record(entry.packageID, entry.version)
				}
				history = nil
			}
			isNew = record(chosen, version)
		}
		if !isNew {
			if err := state.AddDecision(chosen, version); err != nil {
				return empty, &ResolveError[P, V, S, M]{Kind: InternalError, Source: err}
			}
			continue
		}
		deps, err := provider.GetDependencies(name, version)
		if err != nil {
			return empty, &ResolveError[P, V, S, M]{Kind: ErrorRetrievingDependencies, Package: name, Version: version, Source: err}
		}
		if !deps.Available {
			state.AddUnavailable(chosen, set.Singleton(version), deps.Reason)
			continue
		}
		conflict, rejected, err := state.AddPackageVersionDependencies(chosen, version, set.Singleton(version), deps.Constraints)
		if err != nil {
			return empty, &ResolveError[P, V, S, M]{Kind: InternalError, Source: err}
		}
		if rejected {
			track(chosen, conflict, false)
		}
	}
}
