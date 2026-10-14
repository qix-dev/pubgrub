// SPDX-License-Identifier: MPL-2.0
package pubgrub

import (
	"fmt"
	"slices"
	"strings"
)

type ExternalKind uint8

const (
	ExternalNotRoot ExternalKind = iota
	ExternalNoVersions
	ExternalDependency
	ExternalCustom
)

type External[P comparable, V any, S VersionSet[V, S], M any] struct {
	Kind               ExternalKind
	Package            P
	Version            V
	Versions           S
	Dependency         P
	DependencyVersions S
	Reason             M
}
type TermEntry[P comparable, V any, S VersionSet[V, S]] struct {
	Package P
	Term    Term[V, S]
}
type Derived[P comparable, V any, S VersionSet[V, S], M any] struct {
	Terms          []TermEntry[P, V, S]
	SharedID       int
	HasSharedID    bool
	Cause1, Cause2 *DerivationTree[P, V, S, M]
}

// DerivationTree is a proof DAG: exactly one of External or Derived is non-nil.
// Shared causes are retained so reports can cite an explanation once by line ID.
type DerivationTree[P comparable, V any, S VersionSet[V, S], M any] struct {
	External *External[P, V, S, M]
	Derived  *Derived[P, V, S, M]
}

func (s *State[P, V, S, M, R]) derivationTree(root ConflictID) *DerivationTree[P, V, S, M] {
	seen, shared := make(map[ConflictID]bool), make(map[ConflictID]bool)
	stack := []ConflictID{root}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		i := s.store[id]
		if seen[id] {
			if i.kind == derivedFrom {
				shared[id] = true
			}
			continue
		}
		seen[id] = true
		if i.kind == derivedFrom {
			stack = append(stack, i.cause1, i.cause2)
		}
	}
	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, int(id))
	}
	slices.Sort(ids)
	computed := make(map[ConflictID]*DerivationTree[P, V, S, M], len(ids))
	for _, index := range ids {
		id := ConflictID(index)
		i := s.store[id]
		tree := &DerivationTree[P, V, S, M]{}
		if i.kind == derivedFrom {
			terms := make([]TermEntry[P, V, S], len(i.terms))
			for j, term := range i.terms {
				terms[j] = TermEntry[P, V, S]{s.packages[term.packageID], term.term.Clone()}
			}
			tree.Derived = &Derived[P, V, S, M]{terms, index, shared[id], computed[i.cause1], computed[i.cause2]}
		} else {
			external := &External[P, V, S, M]{Package: s.packages[i.packageID]}
			switch i.kind {
			case notRoot:
				external.Kind = ExternalNotRoot
				external.Version = i.version
			case noVersions:
				external.Kind = ExternalNoVersions
				external.Versions = i.versions.Clone()
			case custom:
				external.Kind = ExternalCustom
				external.Versions = i.versions.Clone()
				external.Reason = i.reason
			case fromDependency:
				external.Kind = ExternalDependency
				external.Dependency = s.packages[i.dependencyID]
				external.Versions = i.terms[0].term.Set.Clone()
				external.DependencyVersions = s.provider.EmptySet()
				if len(i.terms) > 1 {
					external.DependencyVersions = i.terms[1].term.Set.Clone()
				}
			case derivedFrom:
			}
			tree.External = external
		}
		computed[id] = tree
	}
	return computed[root]
}
func (t *DerivationTree[P, V, S, M]) Packages() map[P]struct{} {
	out := make(map[P]struct{})
	seen := make(map[*DerivationTree[P, V, S, M]]bool)
	work := []*DerivationTree[P, V, S, M]{t}
	for len(work) > 0 {
		node := work[len(work)-1]
		work = work[:len(work)-1]
		if node == nil || seen[node] {
			continue
		}
		seen[node] = true
		if e := node.External; e != nil {
			out[e.Package] = struct{}{}
			if e.Kind == ExternalDependency {
				out[e.Dependency] = struct{}{}
			}
		} else if d := node.Derived; d != nil {
			for _, term := range d.Terms {
				out[term.Package] = struct{}{}
			}
			work = append(work, d.Cause1, d.Cause2)
		}
	}
	return out
}
func (t *DerivationTree[P, V, S, M]) Clone() *DerivationTree[P, V, S, M] {
	if t == nil {
		return nil
	}
	copies := make(map[*DerivationTree[P, V, S, M]]*DerivationTree[P, V, S, M])
	work := []*DerivationTree[P, V, S, M]{t}
	for len(work) > 0 {
		node := work[len(work)-1]
		work = work[:len(work)-1]
		if copies[node] != nil {
			continue
		}
		out := &DerivationTree[P, V, S, M]{}
		copies[node] = out
		if node.External != nil {
			external := *node.External
			if external.Kind != ExternalNotRoot {
				external.Versions = external.Versions.Clone()
			}
			if external.Kind == ExternalDependency {
				external.DependencyVersions = external.DependencyVersions.Clone()
			}
			out.External = &external
		} else if node.Derived != nil {
			derived := *node.Derived
			derived.Terms = slices.Clone(derived.Terms)
			for i := range derived.Terms {
				derived.Terms[i].Term = derived.Terms[i].Term.Clone()
			}
			out.Derived = &derived
			work = append(work, derived.Cause1, derived.Cause2)
		}
	}
	for node, out := range copies {
		if node.Derived != nil {
			out.Derived.Cause1 = copies[node.Derived.Cause1]
			out.Derived.Cause2 = copies[node.Derived.Cause2]
		}
	}
	return copies[t]
}

// CollapseNoVersions simplifies missing-version causes. It isolates the tree
// before modification so a shared proof held by another caller is unchanged.
func (t *DerivationTree[P, V, S, M]) CollapseNoVersions() {
	if t == nil {
		return
	}
	copy := t.Clone()
	copy.collapse()
	*t = *copy
}
func (t *DerivationTree[P, V, S, M]) cloneNode() *DerivationTree[P, V, S, M] {
	out := *t
	if t.External != nil {
		external := *t.External
		if external.Kind != ExternalNotRoot {
			external.Versions = external.Versions.Clone()
		}
		if external.Kind == ExternalDependency {
			external.DependencyVersions = external.DependencyVersions.Clone()
		}
		out.External = &external
	}
	if t.Derived != nil {
		derived := *t.Derived
		derived.Terms = slices.Clone(derived.Terms)
		for i := range derived.Terms {
			derived.Terms[i].Term = derived.Terms[i].Term.Clone()
		}
		out.Derived = &derived
	}
	return &out
}
func (t *DerivationTree[P, V, S, M]) collapse() {
	d := t.Derived
	if d == nil {
		return
	}
	// Give each incoming edge its own transformed cause, matching
	// Arc::make_mut. Simplifying a shared node on one path must not
	// rewrite an occurrence already processed on another path.
	d.Cause1, d.Cause2 = d.Cause1.cloneNode(), d.Cause2.cloneNode()
	first, second := d.Cause1, d.Cause2
	if first.External != nil && first.External.Kind == ExternalNoVersions {
		second.collapse()
		if merged, ok := second.mergeNoVersions(first.External.Package, first.External.Versions); ok {
			*t = *merged
		}
		return
	}
	if second.External != nil && second.External.Kind == ExternalNoVersions {
		first.collapse()
		if merged, ok := first.mergeNoVersions(second.External.Package, second.External.Versions); ok {
			*t = *merged
		}
		return
	}
	first.collapse()
	second.collapse()
}
func (t *DerivationTree[P, V, S, M]) mergeNoVersions(p P, set S) (*DerivationTree[P, V, S, M], bool) {
	if t.Derived != nil {
		return t.cloneNode(), true
	}
	if t.External == nil || t.External.Kind != ExternalDependency {
		return t, false
	}
	out := t.cloneNode()
	if out.External.Package == p {
		out.External.Versions = out.External.Versions.Union(set)
	} else {
		out.External.DependencyVersions = out.External.DependencyVersions.Union(set)
	}
	return out, true
}
func (e External[P, V, S, M]) String() string {
	switch e.Kind {
	case ExternalNotRoot:
		return fmt.Sprintf("we are solving dependencies of %v %v", e.Package, e.Version)
	case ExternalNoVersions:
		if e.Versions.Equal(e.Versions.Full()) {
			return fmt.Sprintf("there is no available version for %v", e.Package)
		}
		return fmt.Sprintf("there is no version of %v in %s", e.Package, e.Versions)
	case ExternalCustom:
		if e.Versions.Equal(e.Versions.Full()) {
			return fmt.Sprintf("dependencies of %v are unavailable %v", e.Package, e.Reason)
		}
		return fmt.Sprintf("dependencies of %v at version %s are unavailable %v", e.Package, e.Versions, e.Reason)
	case ExternalDependency:
		p, dep := fmt.Sprint(e.Package), fmt.Sprint(e.Dependency)
		if !e.Versions.Equal(e.Versions.Full()) {
			p += " " + e.Versions.String()
		}
		if !e.DependencyVersions.Equal(e.DependencyVersions.Full()) {
			dep += " " + e.DependencyVersions.String()
		}
		return p + " depends on " + dep
	}
	return "invalid external incompatibility"
}

type ReportFormatter[P comparable, V any, S VersionSet[V, S], M any] interface {
	FormatExternal(*External[P, V, S, M]) string
	FormatTerms([]TermEntry[P, V, S]) string
	ExplainBothExternal(*External[P, V, S, M], *External[P, V, S, M], []TermEntry[P, V, S]) string
	ExplainBothRef(int, *Derived[P, V, S, M], int, *Derived[P, V, S, M], []TermEntry[P, V, S]) string
	ExplainRefAndExternal(int, *Derived[P, V, S, M], *External[P, V, S, M], []TermEntry[P, V, S]) string
	AndExplainExternal(*External[P, V, S, M], []TermEntry[P, V, S]) string
	AndExplainRef(int, *Derived[P, V, S, M], []TermEntry[P, V, S]) string
	AndExplainPriorAndExternal(*External[P, V, S, M], *External[P, V, S, M], []TermEntry[P, V, S]) string
}
type DefaultStringReportFormatter[P comparable, V any, S VersionSet[V, S], M any] struct{}

func (f DefaultStringReportFormatter[P, V, S, M]) FormatExternal(e *External[P, V, S, M]) string {
	return e.String()
}
func (f DefaultStringReportFormatter[P, V, S, M]) FormatTerms(terms []TermEntry[P, V, S]) string {
	if len(terms) == 0 {
		return "version solving failed"
	}
	if len(terms) == 1 {
		t := terms[0]
		if t.Term.Positive {
			return fmt.Sprintf("%v %s is forbidden", t.Package, t.Term.Set)
		}
		return fmt.Sprintf("%v %s is mandatory", t.Package, t.Term.Set)
	}
	if len(terms) == 2 && terms[0].Term.Positive != terms[1].Term.Positive {
		positive, negative := terms[0], terms[1]
		if !positive.Term.Positive {
			positive, negative = negative, positive
		}
		return f.FormatExternal(&External[P, V, S, M]{Kind: ExternalDependency, Package: positive.Package, Versions: positive.Term.Set, Dependency: negative.Package, DependencyVersions: negative.Term.Set})
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = fmt.Sprintf("%v %s", t.Package, t.Term)
	}
	return strings.Join(parts, ", ") + " are incompatible"
}
func (f DefaultStringReportFormatter[P, V, S, M]) ExplainBothExternal(a, b *External[P, V, S, M], terms []TermEntry[P, V, S]) string {
	return fmt.Sprintf("Because %s and %s, %s.", f.FormatExternal(a), f.FormatExternal(b), f.FormatTerms(terms))
}
func (f DefaultStringReportFormatter[P, V, S, M]) ExplainBothRef(a int, ad *Derived[P, V, S, M], b int, bd *Derived[P, V, S, M], terms []TermEntry[P, V, S]) string {
	return fmt.Sprintf("Because %s (%d) and %s (%d), %s.", f.FormatTerms(ad.Terms), a, f.FormatTerms(bd.Terms), b, f.FormatTerms(terms))
}
func (f DefaultStringReportFormatter[P, V, S, M]) ExplainRefAndExternal(id int, d *Derived[P, V, S, M], e *External[P, V, S, M], terms []TermEntry[P, V, S]) string {
	return fmt.Sprintf("Because %s (%d) and %s, %s.", f.FormatTerms(d.Terms), id, f.FormatExternal(e), f.FormatTerms(terms))
}
func (f DefaultStringReportFormatter[P, V, S, M]) AndExplainExternal(e *External[P, V, S, M], terms []TermEntry[P, V, S]) string {
	return fmt.Sprintf("And because %s, %s.", f.FormatExternal(e), f.FormatTerms(terms))
}
func (f DefaultStringReportFormatter[P, V, S, M]) AndExplainRef(id int, d *Derived[P, V, S, M], terms []TermEntry[P, V, S]) string {
	return fmt.Sprintf("And because %s (%d), %s.", f.FormatTerms(d.Terms), id, f.FormatTerms(terms))
}
func (f DefaultStringReportFormatter[P, V, S, M]) AndExplainPriorAndExternal(a, b *External[P, V, S, M], terms []TermEntry[P, V, S]) string {
	return fmt.Sprintf("And because %s and %s, %s.", f.FormatExternal(a), f.FormatExternal(b), f.FormatTerms(terms))
}

func Report[P comparable, V any, S VersionSet[V, S], M any](tree *DerivationTree[P, V, S, M]) string {
	return ReportWithFormatter(tree, DefaultStringReportFormatter[P, V, S, M]{})
}
func ReportWithFormatter[P comparable, V any, S VersionSet[V, S], M any](tree *DerivationTree[P, V, S, M], formatter ReportFormatter[P, V, S, M]) string {
	if tree == nil {
		return ""
	}
	if tree.External != nil {
		return formatter.FormatExternal(tree.External)
	}
	reporter := stringReporter[P, V, S, M]{formatter: formatter, refs: make(map[int]int)}
	reporter.build(tree.Derived)
	return strings.Join(reporter.lines, "\n")
}

type stringReporter[P comparable, V any, S VersionSet[V, S], M any] struct {
	count     int
	refs      map[int]int
	lines     []string
	formatter ReportFormatter[P, V, S, M]
}

func (r *stringReporter[P, V, S, M]) ref(d *Derived[P, V, S, M]) (int, bool) {
	if !d.HasSharedID {
		return 0, false
	}
	id, ok := r.refs[d.SharedID]
	return id, ok
}
func (r *stringReporter[P, V, S, M]) addRef() {
	r.count++
	if len(r.lines) > 0 {
		r.lines[len(r.lines)-1] += fmt.Sprintf(" (%d)", r.count)
	}
}
func (r *stringReporter[P, V, S, M]) build(d *Derived[P, V, S, M]) {
	r.helper(d)
	if d.HasSharedID {
		if _, ok := r.refs[d.SharedID]; !ok {
			r.addRef()
			r.refs[d.SharedID] = r.count
		}
	}
}
func (r *stringReporter[P, V, S, M]) helper(d *Derived[P, V, S, M]) {
	a, b := d.Cause1, d.Cause2
	f := r.formatter
	switch {
	case a.External != nil && b.External != nil:
		r.lines = append(r.lines, f.ExplainBothExternal(a.External, b.External, d.Terms))
	case a.Derived != nil && b.External != nil:
		r.oneEach(a.Derived, b.External, d.Terms)
	case a.External != nil && b.Derived != nil:
		r.oneEach(b.Derived, a.External, d.Terms)
	case a.Derived != nil && b.Derived != nil:
		ref1, ok1 := r.ref(a.Derived)
		ref2, ok2 := r.ref(b.Derived)
		switch {
		case ok1 && ok2:
			r.lines = append(r.lines, f.ExplainBothRef(ref1, a.Derived, ref2, b.Derived, d.Terms))
		case ok1:
			r.build(b.Derived)
			r.lines = append(r.lines, f.AndExplainRef(ref1, a.Derived, d.Terms))
		case ok2:
			r.build(a.Derived)
			r.lines = append(r.lines, f.AndExplainRef(ref2, b.Derived, d.Terms))
		default:
			r.build(a.Derived)
			if a.Derived.HasSharedID {
				r.lines = append(r.lines, "")
				r.build(d)
			} else {
				r.addRef()
				ref1 = r.count
				r.lines = append(r.lines, "")
				r.build(b.Derived)
				r.lines = append(r.lines, f.AndExplainRef(ref1, a.Derived, d.Terms))
			}
		}
	}
}
func (r *stringReporter[P, V, S, M]) oneEach(d *Derived[P, V, S, M], e *External[P, V, S, M], terms []TermEntry[P, V, S]) {
	if id, ok := r.ref(d); ok {
		r.lines = append(r.lines, r.formatter.ExplainRefAndExternal(id, d, e, terms))
		return
	}
	a, b := d.Cause1, d.Cause2
	if a.Derived != nil && b.External != nil {
		r.build(a.Derived)
		r.lines = append(r.lines, r.formatter.AndExplainPriorAndExternal(b.External, e, terms))
	} else if a.External != nil && b.Derived != nil {
		r.build(b.Derived)
		r.lines = append(r.lines, r.formatter.AndExplainPriorAndExternal(a.External, e, terms))
	} else {
		r.build(d)
		r.lines = append(r.lines, r.formatter.AndExplainExternal(e, terms))
	}
}
