// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	"strings"
	"testing"

	pubgrub "pubgrub-go"
)

type proof = pubgrub.DerivationTree[string, int, intSet, string]
type derived = pubgrub.Derived[string, int, intSet, string]
type external = pubgrub.External[string, int, intSet, string]
type proofTerms = []pubgrub.TermEntry[string, int, intSet]

func TestReportSharedProofReferences(t *testing.T) {
	a := &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "a", Versions: sets.Full()}}
	b := &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "b", Versions: sets.Full()}}
	shared := &proof{Derived: &derived{Terms: proofTerms{{Package: "a", Term: pubgrub.Positive[int](sets.Singleton(1))}}, SharedID: 7, HasSharedID: true, Cause1: a, Cause2: b}}
	left := &proof{Derived: &derived{Terms: proofTerms{{Package: "left", Term: pubgrub.Positive[int](sets.Singleton(1))}}, SharedID: 8, HasSharedID: true, Cause1: shared, Cause2: shared}}
	root := &proof{Derived: &derived{Cause1: left, Cause2: shared}}
	want := "Because there is no available version for a and there is no available version for b, a 1 is forbidden. (1)\n\nBecause a 1 is forbidden (1) and a 1 is forbidden (1), left 1 is forbidden. (2)\n\nBecause left 1 is forbidden (2) and a 1 is forbidden (1), version solving failed."
	if got := pubgrub.Report(root); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := len(root.Packages()); got != 3 {
		t.Fatal(got)
	}
}
func TestReportDerivedAndExternalReference(t *testing.T) {
	a := &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "a", Versions: sets.Full()}}
	b := &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "b", Versions: sets.Full()}}
	shared := &proof{Derived: &derived{Terms: proofTerms{{Package: "a", Term: pubgrub.Positive[int](sets.Singleton(1))}}, SharedID: 1, HasSharedID: true, Cause1: a, Cause2: b}}
	branch := &proof{Derived: &derived{Terms: proofTerms{{Package: "b", Term: pubgrub.Positive[int](sets.Singleton(1))}}, Cause1: shared, Cause2: b}}
	root := &proof{Derived: &derived{Cause1: shared, Cause2: branch}}
	want := "Because there is no available version for a and there is no available version for b, a 1 is forbidden. (1)\n\nBecause a 1 is forbidden (1) and there is no available version for b, b 1 is forbidden.\nAnd because a 1 is forbidden (1), version solving failed."
	if got := pubgrub.Report(root); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
func TestReportUnsharedBranches(t *testing.T) {
	a := &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "a", Versions: sets.Full()}}
	b := &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "b", Versions: sets.Full()}}
	left := &proof{Derived: &derived{Terms: proofTerms{{Package: "a", Term: pubgrub.Positive[int](sets.Singleton(1))}}, Cause1: a, Cause2: b}}
	right := &proof{Derived: &derived{Terms: proofTerms{{Package: "b", Term: pubgrub.Positive[int](sets.Singleton(1))}}, Cause1: b, Cause2: a}}
	root := &proof{Derived: &derived{Cause1: left, Cause2: right}}
	want := "Because there is no available version for a and there is no available version for b, a 1 is forbidden. (1)\n\nBecause there is no available version for b and there is no available version for a, b 1 is forbidden.\nAnd because a 1 is forbidden (1), version solving failed."
	if got := pubgrub.Report(root); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReportExternalAfterDerivedBranches(t *testing.T) {
	left := &proof{Derived: &derived{
		Terms:  proofTerms{{Package: "a", Term: pubgrub.Positive[int](sets.Singleton(1))}},
		Cause1: &proof{External: &external{Kind: pubgrub.ExternalDependency, Package: "a", Versions: sets.Singleton(1), Dependency: "b", DependencyVersions: sets.Singleton(1)}},
		Cause2: &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "b", Versions: sets.Singleton(1)}},
	}}
	right := &proof{Derived: &derived{
		Terms:  proofTerms{{Package: "a", Term: pubgrub.Positive[int](sets.Singleton(2))}},
		Cause1: &proof{External: &external{Kind: pubgrub.ExternalDependency, Package: "a", Versions: sets.Singleton(2), Dependency: "c", DependencyVersions: sets.Singleton(1)}},
		Cause2: &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "c", Versions: sets.Singleton(1)}},
	}}
	versions := sets.Singleton(1).Union(sets.Singleton(2))
	branch := &proof{Derived: &derived{
		Terms:  proofTerms{{Package: "a", Term: pubgrub.Positive[int](versions)}},
		Cause1: left, Cause2: right,
	}}
	requirement := &proof{External: &external{
		Kind: pubgrub.ExternalDependency, Package: "root", Versions: sets.Singleton(1),
		Dependency: "a", DependencyVersions: versions,
	}}
	prefix := "Because a 1 depends on b 1 and there is no version of b in 1, a 1 is forbidden. (1)\n\n" +
		"Because a 2 depends on c 1 and there is no version of c in 1, a 2 is forbidden.\n" +
		"And because a 1 is forbidden (1), a 1 | 2 is forbidden.\n"
	continuation := "And because root 1 depends on a 1 | 2, root 1 is forbidden."
	for _, swapped := range []bool{false, true} {
		name := "derived then external"
		first, second := branch, requirement
		if swapped {
			name = "external then derived"
			first, second = second, first
		}
		t.Run(name, func(t *testing.T) {
			tree := &proof{Derived: &derived{
				Terms:  proofTerms{{Package: "root", Term: pubgrub.Positive[int](sets.Singleton(1))}},
				Cause1: first, Cause2: second,
			}}
			if got, want := pubgrub.Report(tree), prefix+continuation; got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
			if got, want := pubgrub.ReportWithFormatter(tree, continuationFormatter{}), prefix+strings.ToUpper(continuation); got != want {
				t.Fatalf("custom formatter got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

type continuationFormatter struct {
	pubgrub.DefaultStringReportFormatter[string, int, intSet, string]
}

func (f continuationFormatter) AndExplainExternal(e *external, terms proofTerms) string {
	return strings.ToUpper(f.DefaultStringReportFormatter.AndExplainExternal(e, terms))
}

// The formatter is a user extension point; verify it controls the completed
// explanation rather than only the external leaves.
type customFormatter struct {
	pubgrub.DefaultStringReportFormatter[string, int, intSet, string]
}

func (f customFormatter) ExplainBothExternal(a, b *external, terms proofTerms) string {
	return strings.ToUpper(f.DefaultStringReportFormatter.ExplainBothExternal(a, b, terms))
}
func TestCustomReportFormatter(t *testing.T) {
	tree := &proof{Derived: &derived{Cause1: &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "a", Versions: sets.Full()}}, Cause2: &proof{External: &external{Kind: pubgrub.ExternalNoVersions, Package: "b", Versions: sets.Full()}}}}
	if got := pubgrub.ReportWithFormatter(tree, customFormatter{}); got != "BECAUSE THERE IS NO AVAILABLE VERSION FOR A AND THERE IS NO AVAILABLE VERSION FOR B, VERSION SOLVING FAILED." {
		t.Fatal(got)
	}
}
func TestReportTerms(t *testing.T) {
	f := pubgrub.DefaultStringReportFormatter[string, int, intSet, string]{}
	if got := f.FormatTerms(proofTerms{{Package: "a", Term: pubgrub.Negative[int](sets.Singleton(1))}}); got != "a 1 is mandatory" {
		t.Fatal(got)
	}
	if got := f.FormatTerms(proofTerms{{Package: "a", Term: pubgrub.Negative[int](sets.Singleton(1))}, {Package: "b", Term: pubgrub.Positive[int](sets.Full())}}); got != "b depends on a 1" {
		t.Fatal(got)
	}
	if got := f.FormatTerms(proofTerms{{Package: "a", Term: pubgrub.Positive[int](sets.Singleton(1))}, {Package: "b", Term: pubgrub.Positive[int](sets.Singleton(2))}}); got != "a 1, b 2 are incompatible" {
		t.Fatal(got)
	}
}
