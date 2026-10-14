// SPDX-License-Identifier: MPL-2.0

// Package ranges implements canonical unions of intervals over a totally ordered
// version domain. Values are immutable; operations never mutate their operands.
package ranges

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

type Relation uint8

const (
	Subset Relation = iota
	Disjoint
	Overlapping
)

type BoundKind uint8

const (
	Unbounded BoundKind = iota
	Included
	Excluded
)

type Bound[V any] struct {
	Kind  BoundKind
	Value V
}
type Interval[V any] struct{ Start, End Bound[V] }

func Include[V any](value V) Bound[V] { return Bound[V]{Included, value} }
func Exclude[V any](value V) Bound[V] { return Bound[V]{Excluded, value} }
func Unbound[V any]() Bound[V]        { return Bound[V]{} }

// Ranges is an ordered list of nonempty, non-touching intervals. Initialize it
// with New or Ordered; its comparator must define the same order for all operands.
// Version values themselves must be immutable (including anything they point to).
type Ranges[V any] struct {
	segments []Interval[V]
	compare  func(V, V) int
}

func New[V any](compare func(V, V) int) Ranges[V]    { return Ranges[V]{compare: compare} }
func Ordered[V cmp.Ordered]() Ranges[V]              { return New(cmp.Compare[V]) }
func (r Ranges[V]) Empty() Ranges[V]                 { return New(r.compare) }
func (r Ranges[V]) Full() Ranges[V]                  { return r.FromBounds(Unbound[V](), Unbound[V]()) }
func (r Ranges[V]) Singleton(v V) Ranges[V]          { return r.FromBounds(Include(v), Include(v)) }
func (r Ranges[V]) HigherThan(v V) Ranges[V]         { return r.FromBounds(Include(v), Unbound[V]()) }
func (r Ranges[V]) StrictlyHigherThan(v V) Ranges[V] { return r.FromBounds(Exclude(v), Unbound[V]()) }
func (r Ranges[V]) LowerThan(v V) Ranges[V]          { return r.FromBounds(Unbound[V](), Include(v)) }
func (r Ranges[V]) StrictlyLowerThan(v V) Ranges[V]  { return r.FromBounds(Unbound[V](), Exclude(v)) }
func (r Ranges[V]) Between(a, b V) Ranges[V]         { return r.FromBounds(Include(a), Exclude(b)) }
func (r Ranges[V]) Clone() Ranges[V]                 { return Ranges[V]{slices.Clone(r.segments), r.compare} }
func (r Ranges[V]) IsEmpty() bool                    { return len(r.segments) == 0 }
func (r Ranges[V]) IsFull() bool {
	return len(r.segments) == 1 && r.segments[0].Start.Kind == Unbounded && r.segments[0].End.Kind == Unbounded
}
func (r Ranges[V]) Intervals() []Interval[V] { return slices.Clone(r.segments) }
func (r Ranges[V]) FromBounds(start, end Bound[V]) Ranges[V] {
	out := r.Empty()
	if r.valid(start, end) {
		out.segments = []Interval[V]{{start, end}}
	}
	return out
}
func (r Ranges[V]) valid(start, end Bound[V]) bool {
	if start.Kind > Excluded || end.Kind > Excluded {
		return false
	}
	if start.Kind == Unbounded || end.Kind == Unbounded {
		return true
	}
	c := r.compare(start.Value, end.Value)
	return c < 0 || c == 0 && start.Kind == Included && end.Kind == Included
}
func (r Ranges[V]) cmpStart(a, b Bound[V]) int {
	if a.Kind == Unbounded {
		if b.Kind == Unbounded {
			return 0
		}
		return -1
	}
	if b.Kind == Unbounded {
		return 1
	}
	if c := r.compare(a.Value, b.Value); c != 0 {
		return c
	}
	return cmp.Compare(a.Kind, b.Kind)
}
func (r Ranges[V]) cmpEnd(a, b Bound[V]) int {
	if a.Kind == Unbounded {
		if b.Kind == Unbounded {
			return 0
		}
		return 1
	}
	if b.Kind == Unbounded {
		return -1
	}
	if c := r.compare(a.Value, b.Value); c != 0 {
		return c
	}
	return cmp.Compare(b.Kind, a.Kind)
}
func (r Ranges[V]) gap(end, start Bound[V]) bool {
	if end.Kind == Unbounded || start.Kind == Unbounded {
		return false
	}
	c := r.compare(end.Value, start.Value)
	return c < 0 || c == 0 && end.Kind == Excluded && start.Kind == Excluded
}
func (r Ranges[V]) FromIntervals(intervals ...Interval[V]) Ranges[V] {
	out := r.Empty()
	for _, i := range intervals {
		if r.valid(i.Start, i.End) {
			out.segments = append(out.segments, i)
		}
	}
	slices.SortFunc(out.segments, func(a, b Interval[V]) int { return r.cmpStart(a.Start, b.Start) })
	compact := out.segments[:0]
	for _, i := range out.segments {
		if len(compact) > 0 && !r.gap(compact[len(compact)-1].End, i.Start) {
			if r.cmpEnd(compact[len(compact)-1].End, i.End) < 0 {
				compact[len(compact)-1].End = i.End
			}
		} else {
			compact = append(compact, i)
		}
	}
	out.segments = compact
	return out
}
func (r Ranges[V]) within(v V, i Interval[V]) int {
	if i.Start.Kind != Unbounded {
		c := r.compare(v, i.Start.Value)
		if c < 0 || c == 0 && i.Start.Kind == Excluded {
			return -1
		}
	}
	if i.End.Kind != Unbounded {
		c := r.compare(v, i.End.Value)
		if c > 0 || c == 0 && i.End.Kind == Excluded {
			return 1
		}
	}
	return 0
}
func (r Ranges[V]) Contains(v V) bool {
	i := sort.Search(len(r.segments), func(i int) bool { return r.within(v, r.segments[i]) <= 0 })
	return i < len(r.segments) && r.within(v, r.segments[i]) == 0
}

// ContainsMany requires versions in nondecreasing order and runs in O(n+m).
func (r Ranges[V]) ContainsMany(versions []V) []bool {
	out := make([]bool, len(versions))
	i := 0
	for j, v := range versions {
		for i < len(r.segments) && r.within(v, r.segments[i]) > 0 {
			i++
		}
		out[j] = i < len(r.segments) && r.within(v, r.segments[i]) == 0
	}
	return out
}
func (r Ranges[V]) AsSingleton() (V, bool) {
	if len(r.segments) == 1 {
		i := r.segments[0]
		if i.Start.Kind == Included && i.End.Kind == Included && r.compare(i.Start.Value, i.End.Value) == 0 {
			return i.Start.Value, true
		}
	}
	var zero V
	return zero, false
}
func (r Ranges[V]) BoundingRange() (Interval[V], bool) {
	if r.IsEmpty() {
		return Interval[V]{}, false
	}
	return Interval[V]{r.segments[0].Start, r.segments[len(r.segments)-1].End}, true
}
func flip[V any](b Bound[V]) Bound[V] {
	if b.Kind == Included {
		b.Kind = Excluded
	} else if b.Kind == Excluded {
		b.Kind = Included
	}
	return b
}
func (r Ranges[V]) Complement() Ranges[V] {
	out := r.Empty()
	start := Unbound[V]()
	for _, i := range r.segments {
		if i.Start.Kind != Unbounded {
			out.segments = append(out.segments, Interval[V]{start, flip(i.Start)})
		}
		if i.End.Kind == Unbounded {
			return out
		}
		start = flip(i.End)
	}
	out.segments = append(out.segments, Interval[V]{start, Unbound[V]()})
	return out
}
func (r Ranges[V]) Union(other Ranges[V]) Ranges[V] {
	out := r.Empty()
	i, j := 0, 0
	for i < len(r.segments) || j < len(other.segments) {
		var next Interval[V]
		if j == len(other.segments) || i < len(r.segments) && r.cmpStart(r.segments[i].Start, other.segments[j].Start) <= 0 {
			next = r.segments[i]
			i++
		} else {
			next = other.segments[j]
			j++
		}
		n := len(out.segments)
		if n > 0 && !r.gap(out.segments[n-1].End, next.Start) {
			if r.cmpEnd(out.segments[n-1].End, next.End) < 0 {
				out.segments[n-1].End = next.End
			}
		} else {
			out.segments = append(out.segments, next)
		}
	}
	return out
}
func (r Ranges[V]) Intersection(other Ranges[V]) Ranges[V] {
	out := r.Empty()
	i, j := 0, 0
	for i < len(r.segments) && j < len(other.segments) {
		a, b := r.segments[i], other.segments[j]
		start, end := a.Start, a.End
		if r.cmpStart(start, b.Start) < 0 {
			start = b.Start
		}
		if r.cmpEnd(end, b.End) > 0 {
			end = b.End
		}
		if r.valid(start, end) {
			out.segments = append(out.segments, Interval[V]{start, end})
		}
		c := r.cmpEnd(a.End, b.End)
		if c <= 0 {
			i++
		}
		if c >= 0 {
			j++
		}
	}
	return out
}
func (r Ranges[V]) Difference(other Ranges[V]) Ranges[V] { return r.Intersection(other.Complement()) }
func (r Ranges[V]) Equal(other Ranges[V]) bool           { return r.Compare(other) == 0 }
func (r Ranges[V]) Compare(other Ranges[V]) int {
	for i := 0; i < min(len(r.segments), len(other.segments)); i++ {
		if c := r.cmpStart(r.segments[i].Start, other.segments[i].Start); c != 0 {
			return c
		}
		if c := r.cmpEnd(r.segments[i].End, other.segments[i].End); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(r.segments), len(other.segments))
}
func (r Ranges[V]) IsDisjoint(other Ranges[V]) bool { return r.Intersection(other).IsEmpty() }
func (r Ranges[V]) SubsetOf(other Ranges[V]) bool   { return r.Equal(r.Intersection(other)) }
func (r Ranges[V]) Relation(other Ranges[V]) Relation {
	if r.SubsetOf(other) {
		return Subset
	}
	if r.IsDisjoint(other) {
		return Disjoint
	}
	return Overlapping
}

// WidenVersions preserves membership of sorted known versions and extends each
// segment to its nearest excluded neighbors. The result is a superset of r.
func (r Ranges[V]) WidenVersions(versions []V) Ranges[V] {
	intervals := make([]Interval[V], 0, len(r.segments))
	for _, segment := range r.segments {
		below := sort.Search(len(versions), func(i int) bool { return r.within(versions[i], segment) >= 0 })
		above := sort.Search(len(versions), func(i int) bool { return r.within(versions[i], segment) > 0 })
		start, end := Unbound[V](), Unbound[V]()
		if below > 0 {
			start = Exclude(versions[below-1])
		}
		if above < len(versions) {
			end = Exclude(versions[above])
		}
		intervals = append(intervals, Interval[V]{start, end})
	}
	return r.FromIntervals(intervals...)
}

// NarrowVersions shrinks bounded ends to the outermost sorted known versions.
// Segments containing no known versions and unbounded ends are retained.
func (r Ranges[V]) NarrowVersions(versions []V) Ranges[V] {
	out := r.Clone()
	for index, segment := range r.segments {
		first := sort.Search(len(versions), func(i int) bool { return r.within(versions[i], segment) >= 0 })
		last := sort.Search(len(versions), func(i int) bool { return r.within(versions[i], segment) > 0 })
		if first == last {
			continue
		}
		if segment.Start.Kind != Unbounded {
			out.segments[index].Start = Include(versions[first])
		}
		if segment.End.Kind != Unbounded {
			out.segments[index].End = Include(versions[last-1])
		}
	}
	return out
}

// Simplify preserves membership of sorted known versions, while removing holes
// with no known versions. Singletons and sets containing none are unchanged.
func (r Ranges[V]) Simplify(versions []V) Ranges[V] {
	if _, ok := r.AsSingleton(); ok {
		return r.Clone()
	}
	out := r.Empty()
	start, last := -1, -1
	for index, v := range versions {
		location := sort.Search(len(r.segments), func(i int) bool { return r.within(v, r.segments[i]) <= 0 })
		inside := location < len(r.segments) && r.within(v, r.segments[location]) == 0
		if inside {
			if start == -1 {
				start = location
				if index == 0 {
					start = -2
				}
			}
			last = location
		} else if start != -1 {
			lower := Unbound[V]()
			if start >= 0 {
				lower = r.segments[start].Start
			}
			out.segments = append(out.segments, Interval[V]{lower, r.segments[last].End})
			start, last = -1, -1
		}
	}
	if start != -1 {
		lower := Unbound[V]()
		if start >= 0 {
			lower = r.segments[start].Start
		}
		out.segments = append(out.segments, Interval[V]{lower, Unbound[V]()})
	}
	if out.IsEmpty() {
		return r.Clone()
	}
	return out
}
func (r Ranges[V]) String() string {
	if r.IsEmpty() {
		return "∅"
	}
	parts := make([]string, 0, len(r.segments))
	for _, i := range r.segments {
		if i.Start.Kind == Included && i.End.Kind == Included && r.compare(i.Start.Value, i.End.Value) == 0 {
			parts = append(parts, fmt.Sprint(i.Start.Value))
			continue
		}
		var bounds []string
		if i.Start.Kind == Included {
			bounds = append(bounds, fmt.Sprint(">=", i.Start.Value))
		} else if i.Start.Kind == Excluded {
			bounds = append(bounds, fmt.Sprint(">", i.Start.Value))
		}
		if i.End.Kind == Included {
			bounds = append(bounds, fmt.Sprint("<=", i.End.Value))
		} else if i.End.Kind == Excluded {
			bounds = append(bounds, fmt.Sprint("<", i.End.Value))
		}
		if len(bounds) == 0 {
			parts = append(parts, "*")
		} else {
			parts = append(parts, strings.Join(bounds, ", "))
		}
	}
	return strings.Join(parts, " | ")
}
func (b Bound[V]) MarshalJSON() ([]byte, error) {
	switch b.Kind {
	case Unbounded:
		return []byte(`"Unbounded"`), nil
	case Included:
		return json.Marshal(map[string]V{"Included": b.Value})
	case Excluded:
		return json.Marshal(map[string]V{"Excluded": b.Value})
	}
	return nil, fmt.Errorf("invalid bound kind %d", b.Kind)
}
func (b *Bound[V]) UnmarshalJSON(data []byte) error {
	if string(data) == `"Unbounded"` {
		*b = Unbound[V]()
		return nil
	}
	var value map[string]V
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if len(value) == 1 {
		if v, ok := value["Included"]; ok {
			*b = Include(v)
			return nil
		}
		if v, ok := value["Excluded"]; ok {
			*b = Exclude(v)
			return nil
		}
	}
	return fmt.Errorf("invalid bound %s", data)
}
func (r Ranges[V]) MarshalJSON() ([]byte, error) {
	values := make([][2]Bound[V], len(r.segments))
	for i, v := range r.segments {
		values[i] = [2]Bound[V]{v.Start, v.End}
	}
	return json.Marshal(values)
}

// UnmarshalJSON also accepts the older discrete [start,end-or-null] format.
// The receiver must be initialized with a comparator before decoding.
func (r *Ranges[V]) UnmarshalJSON(data []byte) error {
	if r.compare == nil {
		return fmt.Errorf("ranges: initialize a comparator before decoding")
	}
	var values [][]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	intervals := make([]Interval[V], 0, len(values))
	for _, v := range values {
		if len(v) != 2 {
			return fmt.Errorf("ranges: interval requires two bounds")
		}
		var start, end Bound[V]
		if err := json.Unmarshal(v[0], &start); err == nil {
			if err := json.Unmarshal(v[1], &end); err != nil {
				return err
			}
		} else {
			var lower, upper V
			if err := json.Unmarshal(v[0], &lower); err != nil {
				return err
			}
			start = Include(lower)
			if string(v[1]) != "null" {
				if err := json.Unmarshal(v[1], &upper); err != nil {
					return err
				}
				end = Exclude(upper)
			}
		}
		if !r.valid(start, end) {
			return fmt.Errorf("ranges: empty or reversed interval")
		}
		intervals = append(intervals, Interval[V]{start, end})
	}
	*r = r.FromIntervals(intervals...)
	return nil
}
