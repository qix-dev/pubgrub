// SPDX-License-Identifier: MPL-2.0
package pubgrub

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

type OfflinePriority struct {
	Conflicts    uint32
	VersionCount int
}
type offlineVersion[P comparable, V any, S any] struct {
	version      V
	dependencies DependencyConstraints[P, S]
}

// OfflineDependencyProvider stores complete metadata in memory. Newer versions
// are preferred; packages with more conflicts and then fewer candidates come first.
type OfflineDependencyProvider[P comparable, V any, S VersionSet[V, S]] struct {
	empty    S
	compare  func(V, V) int
	packages []P
	versions map[P][]offlineVersion[P, V, S]
}

func NewOfflineDependencyProvider[P comparable, V any, S VersionSet[V, S]](empty S, compare func(V, V) int) *OfflineDependencyProvider[P, V, S] {
	return &OfflineDependencyProvider[P, V, S]{empty: empty.Empty(), compare: compare, versions: make(map[P][]offlineVersion[P, V, S])}
}
func (p *OfflineDependencyProvider[P, V, S]) EmptySet() S                { return p.empty.Empty() }
func (p *OfflineDependencyProvider[P, V, S]) CompareVersions(a, b V) int { return p.compare(a, b) }
func (p *OfflineDependencyProvider[P, V, S]) ComparePriorities(a, b OfflinePriority) int {
	if c := cmp.Compare(a.Conflicts, b.Conflicts); c != 0 {
		return c
	}
	return cmp.Compare(b.VersionCount, a.VersionCount)
}
func (p *OfflineDependencyProvider[P, V, S]) ShouldCancel() error { return nil }
func cloneConstraints[P comparable, V any, S VersionSet[V, S]](deps DependencyConstraints[P, S]) DependencyConstraints[P, S] {
	out := make(DependencyConstraints[P, S], len(deps))
	for i, d := range deps {
		out[i] = DependencyConstraint[P, S]{d.Package, d.Versions.Clone()}
	}
	return out
}

// AddDependencies replaces, rather than appends to, the metadata for this version.
func (p *OfflineDependencyProvider[P, V, S]) AddDependencies(name P, version V, dependencies DependencyConstraints[P, S]) {
	versions, exists := p.versions[name]
	if !exists {
		p.packages = append(p.packages, name)
	}
	i := sort.Search(len(versions), func(i int) bool { return p.compare(versions[i].version, version) >= 0 })
	entry := offlineVersion[P, V, S]{version, cloneConstraints[P, V](dependencies)}
	if i < len(versions) && p.compare(versions[i].version, version) == 0 {
		versions[i] = entry
	} else {
		versions = slices.Insert(versions, i, entry)
	}
	p.versions[name] = versions
}
func (p *OfflineDependencyProvider[P, V, S]) Packages() []P { return slices.Clone(p.packages) }
func (p *OfflineDependencyProvider[P, V, S]) Versions(name P) ([]V, bool) {
	versions, ok := p.versions[name]
	if !ok {
		return nil, false
	}
	out := make([]V, len(versions))
	for i, v := range versions {
		out[i] = v.version
	}
	return out, true
}
func (p *OfflineDependencyProvider[P, V, S]) ChooseVersion(name P, set S) (V, bool, error) {
	versions := p.versions[name]
	for i := len(versions) - 1; i >= 0; i-- {
		if set.Contains(versions[i].version) {
			return versions[i].version, true, nil
		}
	}
	var zero V
	return zero, false, nil
}
func (p *OfflineDependencyProvider[P, V, S]) Prioritize(name P, set S, statistics PackageResolutionStatistics) OfflinePriority {
	versions, _ := p.Versions(name)
	count := 0
	for _, inside := range set.ContainsMany(versions) {
		if inside {
			count++
		}
	}
	if count == 0 {
		return OfflinePriority{^uint32(0), 0}
	}
	return OfflinePriority{statistics.ConflictCount(), count}
}
func (p *OfflineDependencyProvider[P, V, S]) GetDependencies(name P, version V) (Dependencies[P, S, string], error) {
	versions := p.versions[name]
	i := sort.Search(len(versions), func(i int) bool { return p.compare(versions[i].version, version) >= 0 })
	if i == len(versions) || p.compare(versions[i].version, version) != 0 {
		return Unavailable[P, S]("its dependencies could not be determined"), nil
	}
	return Available[P, S, string](cloneConstraints[P, V](versions[i].dependencies)), nil
}
func (p *OfflineDependencyProvider[P, V, S]) Clone() *OfflineDependencyProvider[P, V, S] {
	out := NewOfflineDependencyProvider[P](p.empty, p.compare)
	for _, name := range p.packages {
		for _, v := range p.versions[name] {
			out.AddDependencies(name, v.version, v.dependencies)
		}
	}
	return out
}

// MarshalJSON uses ordered records so non-string package/version types and
// duplicate constraints survive round trips. This is a Go schema, not Rust's
// serde map schema (which cannot retain duplicate constraints).
func (p *OfflineDependencyProvider[P, V, S]) MarshalJSON() ([]byte, error) {
	type record struct {
		Package      P                           `json:"package"`
		Version      V                           `json:"version"`
		Dependencies DependencyConstraints[P, S] `json:"dependencies"`
	}
	records := make([]record, 0)
	for _, name := range p.packages {
		for _, v := range p.versions[name] {
			records = append(records, record{name, v.version, v.dependencies})
		}
	}
	return json.Marshal(records)
}

// UnmarshalJSON requires a provider initialized with its comparator and set factory.
func (p *OfflineDependencyProvider[P, V, S]) UnmarshalJSON(data []byte) error {
	if p.compare == nil {
		return fmt.Errorf("provider: initialize before decoding")
	}
	type dependency struct {
		Package  P
		Versions json.RawMessage
	}
	type record struct {
		Package      P            `json:"package"`
		Version      V            `json:"version"`
		Dependencies []dependency `json:"dependencies"`
	}
	var records []record
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	out := NewOfflineDependencyProvider[P](p.empty, p.compare)
	for _, r := range records {
		deps := make(DependencyConstraints[P, S], 0, len(r.Dependencies))
		for _, d := range r.Dependencies {
			set := p.empty.Empty()
			if err := json.Unmarshal(d.Versions, &set); err != nil {
				return err
			}
			deps = append(deps, DependencyConstraint[P, S]{d.Package, set})
		}
		out.AddDependencies(r.Package, r.Version, deps)
	}
	*p = *out
	return nil
}
