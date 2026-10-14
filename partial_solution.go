// SPDX-License-Identifier: MPL-2.0
package pubgrub

import (
	"fmt"
	"slices"
	"sort"
)

type datedDerivation[V any, S VersionSet[V, S]] struct {
	index       uint64
	level       int
	cause       ConflictID
	accumulated Term[V, S]
}
type assignment[V any, S VersionSet[V, S]] struct {
	term                  Term[V, S]
	decided               bool
	version               V
	decisionIndex         uint64
	firstLevel, lastLevel int
	derivations           []datedDerivation[V, S]
}
type priorityEntry[R any] struct {
	packageID PackageID
	priority  R
}

// An indexed heap preserves O(log n) priority updates and package removal.
type priorityQueue[R any] struct {
	entries   []priorityEntry[R]
	positions map[PackageID]int
	compare   func(R, R) int
}

func newQueue[R any](compare func(R, R) int) priorityQueue[R] {
	return priorityQueue[R]{positions: make(map[PackageID]int), compare: compare}
}
func (q *priorityQueue[R]) better(a, b int) bool {
	left, right := q.entries[a], q.entries[b]
	c := q.compare(left.priority, right.priority)
	return c > 0 || c == 0 && left.packageID < right.packageID
}
func (q *priorityQueue[R]) swap(a, b int) {
	q.entries[a], q.entries[b] = q.entries[b], q.entries[a]
	q.positions[q.entries[a].packageID] = a
	q.positions[q.entries[b].packageID] = b
}
func (q *priorityQueue[R]) up(index int) int {
	for index > 0 {
		parent := (index - 1) / 2
		if !q.better(index, parent) {
			break
		}
		q.swap(index, parent)
		index = parent
	}
	return index
}
func (q *priorityQueue[R]) down(index int) {
	for {
		left := 2*index + 1
		if left >= len(q.entries) {
			return
		}
		best := left
		if left+1 < len(q.entries) && q.better(left+1, left) {
			best = left + 1
		}
		if !q.better(best, index) {
			return
		}
		q.swap(index, best)
		index = best
	}
}
func (q *priorityQueue[R]) push(p PackageID, priority R) {
	if index, ok := q.positions[p]; ok {
		q.entries[index].priority = priority
		q.down(q.up(index))
		return
	}
	q.positions[p] = len(q.entries)
	q.entries = append(q.entries, priorityEntry[R]{p, priority})
	q.up(len(q.entries) - 1)
}
func (q *priorityQueue[R]) remove(p PackageID) {
	index, ok := q.positions[p]
	if !ok {
		return
	}
	last := len(q.entries) - 1
	q.swap(index, last)
	q.entries = q.entries[:last]
	delete(q.positions, p)
	if index < last {
		q.down(q.up(index))
	}
}
func (q *priorityQueue[R]) pop() (PackageID, bool) {
	if len(q.entries) == 0 {
		return 0, false
	}
	p := q.entries[0].packageID
	q.remove(p)
	return p, true
}

type partialSolution[V any, S VersionSet[V, S], R any] struct {
	nextIndex       uint64
	level           int
	order           []PackageID
	assignments     map[PackageID]*assignment[V, S]
	priorities      priorityQueue[R]
	dirty           []PackageID
	dirtySet        map[PackageID]bool
	lastValidLevels []int
}

func newPartial[V any, S VersionSet[V, S], R any](compare func(R, R) int) partialSolution[V, S, R] {
	return partialSolution[V, S, R]{assignments: make(map[PackageID]*assignment[V, S]), priorities: newQueue(compare), dirtySet: make(map[PackageID]bool), lastValidLevels: []int{0}}
}
func (p *partialSolution[V, S, R]) clone() partialSolution[V, S, R] {
	out := *p
	out.order = slices.Clone(p.order)
	out.dirty = slices.Clone(p.dirty)
	out.lastValidLevels = slices.Clone(p.lastValidLevels)
	out.assignments = make(map[PackageID]*assignment[V, S], len(p.assignments))
	out.dirtySet = make(map[PackageID]bool, len(p.dirtySet))
	for key, value := range p.dirtySet {
		out.dirtySet[key] = value
	}
	for key, value := range p.assignments {
		a := *value
		a.term = a.term.Clone()
		a.derivations = slices.Clone(a.derivations)
		for index := range a.derivations {
			a.derivations[index].accumulated = a.derivations[index].accumulated.Clone()
		}
		out.assignments[key] = &a
	}
	out.priorities = newQueue(p.priorities.compare)
	out.priorities.entries = slices.Clone(p.priorities.entries)
	for key, index := range p.priorities.positions {
		out.priorities.positions[key] = index
	}
	return out
}
func (p *partialSolution[V, S, R]) markDirty(id PackageID) {
	if !p.dirtySet[id] {
		p.dirty = append(p.dirty, id)
		p.dirtySet[id] = true
	}
}
func (p *partialSolution[V, S, R]) lookup(id PackageID) (Term[V, S], bool) {
	if a, ok := p.assignments[id]; ok {
		return a.term, true
	}
	return Term[V, S]{}, false
}
func (p *partialSolution[V, S, R]) addDerivation(id PackageID, cause ConflictID, negated Term[V, S]) error {
	a, ok := p.assignments[id]
	if ok && a.decided {
		return fmt.Errorf("derivation after decision for package %d", id)
	}
	if !ok {
		a = &assignment[V, S]{firstLevel: p.level, term: negated}
		p.assignments[id] = a
		p.order = append(p.order, id)
	} else {
		a.term = a.term.Intersection(negated)
	}
	a.lastLevel = p.level
	a.derivations = append(a.derivations, datedDerivation[V, S]{p.nextIndex, p.level, cause, a.term.Clone()})
	p.nextIndex++
	if a.term.Positive {
		p.markDirty(id)
	}
	return nil
}
func (p *partialSolution[V, S, R]) addDecision(id PackageID, version V) error {
	a, ok := p.assignments[id]
	if !ok || a.decided {
		return fmt.Errorf("decision requires undecided derivations for package %d", id)
	}
	if !a.term.Positive || !a.term.Contains(version) {
		return fmt.Errorf("decision for package %d is outside its positive constraint", id)
	}
	index := slices.Index(p.order, id)
	p.order[index], p.order[p.level] = p.order[p.level], p.order[index]
	p.level++
	a.lastLevel = p.level
	a.decided = true
	a.version = version
	a.decisionIndex = p.nextIndex
	a.term = Positive[V](a.term.Set.Singleton(version))
	p.nextIndex++
	return nil
}
func (p *partialSolution[V, S, R]) pick(prioritize func(PackageID, S) R) (PackageID, S, bool) {
	for len(p.dirty) > 0 {
		id := p.dirty[len(p.dirty)-1]
		p.dirty = p.dirty[:len(p.dirty)-1]
		delete(p.dirtySet, id)
		a, ok := p.assignments[id]
		if !ok || a.decided || !a.term.Positive {
			continue
		}
		p.priorities.push(id, prioritize(id, a.term.Set))
	}
	for {
		id, ok := p.priorities.pop()
		if !ok {
			var zero S
			return 0, zero, false
		}
		if a, ok := p.assignments[id]; ok && !a.decided && a.term.Positive {
			return id, a.term.Set, true
		}
	}
}
func (p *partialSolution[V, S, R]) backtrack(level int) {
	p.level = level
	order := p.order[:0]
	for _, id := range p.order {
		a := p.assignments[id]
		if a.firstLevel > level {
			delete(p.assignments, id)
			continue
		}
		if a.lastLevel <= level {
			if !a.decided && a.term.Positive {
				if _, ok := p.priorities.positions[id]; !ok {
					p.markDirty(id)
				}
			}
		} else {
			for len(a.derivations) > 0 && a.derivations[len(a.derivations)-1].level > level {
				a.derivations = a.derivations[:len(a.derivations)-1]
			}
			last := a.derivations[len(a.derivations)-1]
			a.lastLevel = last.level
			a.decided = false
			a.term = last.accumulated.Clone()
			var zero V
			a.version = zero
			p.priorities.remove(id)
			if a.term.Positive {
				p.markDirty(id)
			}
		}
		order = append(order, id)
	}
	p.order = order
	p.lastValidLevels = append(p.lastValidLevels, int(^uint(0)>>1))
	index := sort.Search(len(p.lastValidLevels), func(i int) bool { return p.lastValidLevels[i] > level })
	for i := index; i < len(p.lastValidLevels); i++ {
		p.lastValidLevels[i] = level
	}
}

type satisfier struct {
	cause    ConflictID
	hasCause bool
	index    uint64
	level    int
}

func (a *assignment[V, S]) satisfier(start Term[V, S]) (satisfier, error) {
	index := sort.Search(len(a.derivations), func(i int) bool { return a.derivations[i].accumulated.IsDisjoint(start) })
	if index < len(a.derivations) {
		d := a.derivations[index]
		return satisfier{d.cause, true, d.index, d.level}, nil
	}
	if a.decided {
		return satisfier{index: a.decisionIndex, level: a.lastLevel}, nil
	}
	return satisfier{}, fmt.Errorf("satisfier missing: accumulated %s, starting %s", a.term, start)
}
