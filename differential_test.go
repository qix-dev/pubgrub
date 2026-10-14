// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	pubgrub "pubgrub-go"
	"pubgrub-go/ranges"
	"pubgrub-go/semver"
)

type rustReference struct {
	input  *json.Encoder
	output *json.Decoder
}

func startRustReference(t *testing.T) *rustReference {
	t.Helper()
	path := os.Getenv("PUBGRUB_RUST_REFERENCE")
	if path == "" {
		t.Skip("set PUBGRUB_RUST_REFERENCE to the testdata/rust-reference executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	command := exec.CommandContext(ctx, path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	input, err := command.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := input.Close(); err != nil {
			t.Error(err)
		}
		if err := command.Wait(); err != nil {
			t.Errorf("reference: %v\n%s", err, stderr.String())
		}
		cancel()
	})
	return &rustReference{json.NewEncoder(input), json.NewDecoder(output)}
}
func (r *rustReference) call(t *testing.T, input any, output any) {
	t.Helper()
	if err := r.input.Encode(input); err != nil {
		t.Fatal(err)
	}
	if err := r.output.Decode(output); err != nil {
		t.Fatal(err)
	}
}

type referenceDependency struct {
	Package  string `json:"package"`
	Versions intSet `json:"versions"`
}
type referenceRecord struct {
	Package      string                `json:"package"`
	Version      int                   `json:"version"`
	Dependencies []referenceDependency `json:"dependencies"`
}
type referenceSolveResult struct {
	Solution       map[string]int `json:"solution"`
	NoSolution     bool           `json:"no_solution"`
	Report         string         `json:"report"`
	Collapsed      string         `json:"collapsed"`
	Error          string         `json:"error"`
	Proof          any            `json:"proof"`
	CollapsedProof any            `json:"collapsed_proof"`
}

// Canonicalize only map iteration order; retain every term, cause, shared-node
// marker and the order of the two causes in the proof.
func canonicalProof(tree *proof) any {
	if tree.External != nil {
		return map[string]any{"external": tree.External.String()}
	}
	terms := make(map[string]any)
	for _, term := range tree.Derived.Terms {
		terms[term.Package] = term.Term.String()
	}
	return map[string]any{"terms": terms, "shared": tree.Derived.HasSharedID,
		"cause1": canonicalProof(tree.Derived.Cause1), "cause2": canonicalProof(tree.Derived.Cause2)}
}

func TestRustDifferentialResolution(t *testing.T) {
	reference := startRustReference(t)
	random := rand.New(rand.NewSource(427017))
	solved, unsolved := 0, 0
	for sample := 0; sample < 2000; sample++ {
		p := provider()
		records := make([]referenceRecord, 0)
		for name := 0; name < 7; name++ {
			versions := random.Intn(7)
			if name == 0 {
				versions = 1
			}
			for version := 1; version <= versions; version++ {
				count := random.Intn(5)
				if name == 0 {
					count = 1 + random.Intn(4)
				}
				deps := make(constraints, 0, count)
				wire := make([]referenceDependency, 0, count)
				for d := 0; d < count; d++ {
					dep := fmt.Sprint(random.Intn(8))
					set := sets.Empty()
					switch random.Intn(4) {
					case 0:
						set = sets.Full()
					case 1:
						set = sets.Between(random.Intn(5), 2+random.Intn(6))
					default:
						for v := 1; v < 8; v++ {
							if random.Intn(2) == 0 {
								set = set.Union(sets.Singleton(v))
							}
						}
					}
					deps = append(deps, pubgrub.DependencyConstraint[string, intSet]{Package: dep, Versions: set})
					wire = append(wire, referenceDependency{dep, set})
				}
				p.AddDependencies(fmt.Sprint(name), version, deps)
				records = append(records, referenceRecord{fmt.Sprint(name), version, wire})
			}
		}
		request := map[string]any{"op": "solve", "root": "0", "version": 1, "records": records}
		var expected referenceSolveResult
		reference.call(t, request, &expected)
		if expected.Error != "" {
			t.Fatalf("reference error: %s", expected.Error)
		}
		result, err := pubgrub.Resolve(p, "0", 1)
		if err != nil {
			var failure *pubgrub.ResolveError[string, int, intSet, string]
			if !errors.As(err, &failure) || failure.Kind != pubgrub.NoSolution || !expected.NoSolution {
				input, _ := json.Marshal(request)
				t.Fatalf("sample %d input %s: Rust %+v, Go %v", sample, input, expected, err)
			}
			if got := canonicalProof(failure.Tree); !reflect.DeepEqual(got, expected.Proof) {
				t.Fatalf("sample %d proof:\nRust %v\nGo   %v", sample, expected.Proof, got)
			}
			failure.Tree.CollapseNoVersions()
			if got := canonicalProof(failure.Tree); !reflect.DeepEqual(got, expected.CollapsedProof) {
				t.Fatalf("sample %d collapsed proof:\nRust %v\nGo   %v", sample, expected.CollapsedProof, got)
			}
			unsolved++
		} else {
			if expected.NoSolution || !reflect.DeepEqual(result.Map(), expected.Solution) {
				input, _ := json.Marshal(request)
				t.Fatalf("sample %d input %s: Rust %+v Go %v", sample, input, expected, result.Map())
			}
			solved++
		}
	}
	t.Logf("matched %d exact solutions and %d unsatisfiable inputs", solved, unsolved)
}
func TestRustDifferentialRanges(t *testing.T) {
	reference := startRustReference(t)
	random := rand.New(rand.NewSource(65829))
	makeSet := func() intSet {
		intervals := make([]ranges.Interval[int], random.Intn(5))
		for i := range intervals {
			intervals[i] = ranges.Interval[int]{Start: ranges.Bound[int]{Kind: ranges.BoundKind(random.Intn(3)), Value: random.Intn(15)}, End: ranges.Bound[int]{Kind: ranges.BoundKind(random.Intn(3)), Value: random.Intn(15)}}
		}
		return sets.FromIntervals(intervals...)
	}
	for sample := 0; sample < 3000; sample++ {
		a, b := makeSet(), makeSet()
		versions := []int{0, 2, 5, 7, 11, 15}
		request := map[string]any{"op": "ranges", "a": a, "b": b, "versions": versions}
		var expected map[string]json.RawMessage
		reference.call(t, request, &expected)
		if message, ok := expected["error"]; ok {
			t.Fatal(string(message))
		}
		for name, result := range map[string]intSet{"union": a.Union(b), "intersection": a.Intersection(b), "difference": a.Difference(b), "complement": a.Complement(), "widen": a.WidenVersions(versions), "narrow": a.NarrowVersions(versions), "simplify": a.Simplify(versions)} {
			want := sets.Empty()
			if err := json.Unmarshal(expected[name], &want); err != nil {
				t.Fatal(err)
			}
			if !result.Equal(want) {
				t.Fatalf("sample %d %s a=%s b=%s: Rust %s Go %s", sample, name, a, b, want, result)
			}
		}
		for name, result := range map[string]any{"subset": a.SubsetOf(b), "disjoint": a.IsDisjoint(b), "compare": a.Compare(b), "contains": a.ContainsMany(versions), "display": a.String()} {
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var actualValue, expectedValue any
			if err := json.Unmarshal(data, &actualValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(expected[name], &expectedValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actualValue, expectedValue) {
				t.Fatalf("sample %d %s a=%s b=%s: Rust %s Go %s", sample, name, a, b, expected[name], data)
			}
		}
	}
}
func TestRustDifferentialSemver(t *testing.T) {
	reference := startRustReference(t)
	requirements := []string{"*", "x", "1", "1.2", "1.2.3", "=1", "=1.2", "=1.2.3", ">1", ">1.2", ">1.2.3", ">=1", ">=1.2.3", "<1", "<1.2.3", "<=1", "<=1.2", "<=1.2.3", "~1", "~1.2", "~1.2.3", "^0", "^0.0", "^0.0.3", "^0.2", "^0.2.3", "^1.2", "^1.2.3", "1.*", "1.2.*", "=1.*", "^1.*", ">1.*", "<=1.*", ">=1, <2", "=1.2.3-rc.1+build.2", "1.2.3-beta", "", "1.*.3", "01.2.3", "1.2.3 || 2", "*.*", "=1.2.3, =1.2.4"}
	for _, requirement := range requirements {
		var expected struct {
			Ranges  string `json:"ranges"`
			Invalid bool   `json:"invalid"`
			Error   string `json:"error"`
		}
		reference.call(t, map[string]any{"op": "semver", "requirement": requirement}, &expected)
		got, err := semver.ParseRanges(requirement)
		if expected.Error != "" {
			t.Fatal(expected.Error)
		}
		if expected.Invalid != (err != nil) {
			t.Fatalf("%q invalid: Rust %v Go %v", requirement, expected.Invalid, err)
		}
		if err == nil && got.String() != expected.Ranges {
			t.Fatalf("%q: Rust %s Go %s", requirement, expected.Ranges, got)
		}
	}
	names := []string{"1.0.0", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-beta.11", "1.0.0+0", "1.0.0+00", "1.0.0+1", "1.0.0+01", "1.0.0+123", "1.0.0+a", "1.0.0+a.0", "1.0.0+a.1", "1.0.0+99999999999999999999999999", "1.0.0+100000000000000000000000000"}
	for _, a := range names {
		for _, b := range names {
			var expected struct {
				Compare int `json:"compare"`
			}
			reference.call(t, map[string]any{"op": "version_compare", "a": a, "b": b}, &expected)
			left, err := semver.Parse(a)
			if err != nil {
				t.Fatal(err)
			}
			right, err := semver.Parse(b)
			if err != nil {
				t.Fatal(err)
			}
			if got := semver.Compare(left, right); got != expected.Compare {
				t.Fatalf("%s vs %s: Rust %d Go %d", a, b, expected.Compare, got)
			}
		}
	}
}
