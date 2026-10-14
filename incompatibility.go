// SPDX-License-Identifier: MPL-2.0
package pubgrub

type incompatibilityKind uint8

const (
	notRoot incompatibilityKind = iota
	noVersions
	fromDependency
	derivedFrom
	custom
)

type packageTerm[V any, S VersionSet[V, S]] struct {
	packageID PackageID
	term      Term[V, S]
}
type incompatibility[V any, S VersionSet[V, S], M any] struct {
	terms                                       []packageTerm[V, S]
	kind                                        incompatibilityKind
	packageID, dependencyID                     PackageID
	version                                     V
	versions                                    S
	reason                                      M
	cause1, cause2                              ConflictID
	contradicted                                bool
	contradictionLevel, contradictionGeneration int
}

func (i incompatibility[V, S, M]) get(p PackageID) (Term[V, S], bool) {
	for _, t := range i.terms {
		if t.packageID == p {
			return t.term, true
		}
	}
	return Term[V, S]{}, false
}
func (i incompatibility[V, S, M]) clone() incompatibility[V, S, M] {
	out := i
	out.terms = make([]packageTerm[V, S], len(i.terms))
	for index, t := range i.terms {
		out.terms[index] = packageTerm[V, S]{t.packageID, t.term.Clone()}
	}
	if i.kind == noVersions || i.kind == custom {
		out.versions = i.versions.Clone()
	}
	return out
}
func dependencyIncompatibility[V any, S VersionSet[V, S], M any](p PackageID, versions S, dep PackageID, depVersions S) incompatibility[V, S, M] {
	terms := []packageTerm[V, S]{{p, Positive[V](versions)}}
	if depVersions.IsEmpty() {
	} else if p == dep {
		terms[0].term = Positive[V](versions.Difference(depVersions))
	} else {
		terms = append(terms, packageTerm[V, S]{dep, Negative[V](depVersions)})
	}
	return incompatibility[V, S, M]{terms: terms, kind: fromDependency, packageID: p, dependencyID: dep}
}

type incompatibilityRelation uint8

const (
	incompatSatisfied incompatibilityRelation = iota
	incompatContradicted
	incompatAlmostSatisfied
	incompatInconclusive
)

func (i incompatibility[V, S, M]) relation(lookup func(PackageID) (Term[V, S], bool)) (incompatibilityRelation, PackageID) {
	relation := incompatSatisfied
	var almost PackageID
	for _, entry := range i.terms {
		assignment, ok := lookup(entry.packageID)
		value := Inconclusive
		if ok {
			value = entry.term.RelationWith(assignment)
		}
		switch value {
		case Satisfied:
		case Contradicted:
			return incompatContradicted, entry.packageID
		case Inconclusive:
			if relation == incompatSatisfied {
				relation = incompatAlmostSatisfied
				almost = entry.packageID
			} else {
				return incompatInconclusive, 0
			}
		}
	}
	return relation, almost
}
func (i incompatibility[V, S, M]) terminal(root PackageID, version V) bool {
	return len(i.terms) == 0 || len(i.terms) == 1 && i.terms[0].packageID == root && i.terms[0].term.Contains(version)
}
func priorCause[V any, S VersionSet[V, S], M any](first, second ConflictID, p PackageID, store []incompatibility[V, S, M]) incompatibility[V, S, M] {
	a, b := store[first], store[second]
	out := incompatibility[V, S, M]{kind: derivedFrom, cause1: first, cause2: second}
	for _, entry := range a.terms {
		if entry.packageID != p {
			out.terms = append(out.terms, packageTerm[V, S]{entry.packageID, entry.term.Clone()})
		}
	}
	for _, entry := range b.terms {
		if entry.packageID == p {
			continue
		}
		found := false
		for index, existing := range out.terms {
			if existing.packageID == entry.packageID {
				out.terms[index].term = existing.term.Intersection(entry.term)
				found = true
				break
			}
		}
		if !found {
			out.terms = append(out.terms, packageTerm[V, S]{entry.packageID, entry.term.Clone()})
		}
	}
	left, _ := a.get(p)
	right, _ := b.get(p)
	unified := left.Union(right)
	if !unified.IsAny() {
		out.terms = append(out.terms, packageTerm[V, S]{p, unified})
	}
	return out
}
