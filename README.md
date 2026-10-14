# pubgrub-go

Pure Go dependency resolution ported from `astral-pubgrub` and
`astral-version-ranges`. Requires Go 1.24 or newer. The Go library has no external
dependencies and does not call Rust, Python or C.

The reference source is the adjacent `../pubgrub` checkout at commit
`e4ae64d371a25df953d42d9f0e8e0d43d8e79e6e`, declaring
`astral-pubgrub` 0.6.1 and `astral-version-ranges` 0.2.2.
The commit identifies the behavior more precisely than the crate version.

## Use

```go
package main

import (
    "cmp"
    "fmt"

    pubgrub "pubgrub-go"
    "pubgrub-go/ranges"
)

func main() {
    versions := ranges.Ordered[int]()
    provider := pubgrub.NewOfflineDependencyProvider[string](versions, cmp.Compare[int])
    provider.AddDependencies("root", 1, pubgrub.DependencyConstraints[string, ranges.Ranges[int]]{
        {Package: "web", Versions: versions.HigherThan(1)},
    })
    provider.AddDependencies("web", 1, nil)
    provider.AddDependencies("web", 2, nil)

    solution, err := pubgrub.Resolve(provider, "root", 1)
    if err != nil {
        fmt.Println(err)
        return
    }
    version, _ := solution.Get("web")
    fmt.Println(version) // 2
}
```

This example also runs as `ExampleResolve`. A consuming module can use a local
`replace pubgrub-go => /absolute/path/to/pubgrub-go`; change the module path to
your repository path when publishing.

Use `ResolveContext` for cancellation through a Go `context.Context`. Provider
errors are available through `errors.Is` and `errors.As`. To explain an
unsatisfiable input:

```go
var failure *pubgrub.ResolveError[string, int, ranges.Ranges[int], string]
if errors.As(err, &failure) && failure.Kind == pubgrub.NoSolution {
    fmt.Println(pubgrub.Report(failure.Tree))
    failure.Tree.CollapseNoVersions()
    fmt.Println(pubgrub.Report(failure.Tree))
}
```

`CollapseNoVersions` is appropriate when the provider has access to all existing
versions; missing offline metadata may otherwise be hidden by that simplification.

## Packages and source mapping

| Go implementation | Rust source | Behavior |
| --- | --- | --- |
| `solver.go` | `src/solver.rs` | Resolve loop, cancellation, conflict statistics, dependency deduplication after the first backtrack |
| `state.go` | `src/internal/core.rs` | Unit propagation, conflict learning, nonchronological backtracking, dependency merging, proof construction |
| `partial_solution.go` | `src/internal/partial_solution.rs` | Ordered assignments, dated derivations, satisfier search, priority updates, contradiction-cache generations |
| `incompatibility.go` | `src/internal/incompatibility.rs` | Root, unavailable, no-version, dependency and derived incompatibilities; self-dependencies and rule of resolution |
| `term.go` | `src/term.rs` | Positive/negative terms and their set algebra, including package absence |
| `types.go` | `version_set.rs`, `error.rs`, `package.rs`, part of `solver.rs` | Generic contracts, dependency/result types and structured errors |
| `provider.go` | `src/provider.rs` | Offline metadata, replacement semantics, newest-version selection and conflict/candidate-count priority |
| `report.go` | `src/report.rs` | Shared proof DAGs, package collection, no-version collapse, line references and custom report formatters |
| `version.go` | `src/version.rs` | Three-component `uint32` semantic versions, parsing, comparison, bumps and JSON |
| `ranges/` | `version-ranges/src/lib.rs` | Canonical intervals, constructors, ordering, union/intersection/complement/difference, relation, batch membership, widen/narrow/simplify and JSON |
| `semver/` | `version-ranges/src/semver.rs` | Optional SemVer requirement conversion, with full SemVer values and total ordering |

Internal arenas use stable integer IDs into slices. Ordered assignment storage,
an indexed binary heap, contradiction generations and dependency merging are
retained. Rust-specific inline small containers and custom hashers are represented
with Go slices and maps; their allocation layout is not reproduced.

## Custom providers and Python integration

`DependencyProvider[P, V, S, M, R]` separates metadata from the algorithm:

- `P` is a comparable package key; structured package identities are supported.
- `V` can be any immutable type, including a version containing slices.
- `S` implements `VersionSet[V, S]`; `ranges.Ranges[V]` is the bundled implementation.
- `M` carries unavailable-metadata reasons.
- `R` is a caller-defined priority, ordered by `ComparePriorities`.
- `EmptySet` supplies an initialized set factory.
- `ChooseVersion` returns `(version, found, error)`.
- `GetDependencies` returns either complete ordered constraints or an unavailable reason.
- `ShouldCancel` may interrupt the resolution.

Dependency order is significant when priorities tie. Duplicate dependency entries
are retained and all their constraints apply. Repeated `AddDependencies` calls for
the same package/version replace the previous metadata.

Version comparison must define a consistent total order. Set operations must
return canonical immutable values. Package, version, metadata and priority values
are caller-owned immutable values; mutable data referenced by those values is not
deep-copied. `ranges.Ranges` protects its own interval slices.

`NewState` exposes the incremental operations required by a caller driving its own
resolution loop:

1. `UnitPropagation`.
2. `PickHighestPriorityPackage`.
3. Fetch a version and its metadata.
4. `AddNoVersions`, `AddUnavailable`, or `AddPackageVersionDependencies`.
5. Repeat until no undecided positive package remains, then `ExtractSolution`.

`Clone` isolates all solver-owned state for environment forks.
`BacktrackPackage`, `TermIntersection`, `UnchangingTerm`, `Dependencies`,
`PrioritizedPackages`, `UndecidedPackages` and `ConflictPackages` expose the
corresponding reference operations. IDs belong to the state that allocated them
and its clones. State mutation is single-threaded; independent states can run
concurrently with a concurrency-safe provider.

When widening a dependent version range, all *existing* versions in that range
must have the supplied dependencies, and the set of existing versions must not
grow during a resolution.

This package is the generic PubGrub engine. Python PEP 440/508, package-index I/O,
wheel compatibility and uv's environment-fork policy belong in a provider or the
surrounding resolver.

## Compatibility details

- There is no dependency on a particular version syntax in the solver.
  `SemanticVersion` reproduces the reference's simple three-number type;
  `semver.Version` is the separate optional adapter's prerelease/build-aware type.
- `semver.FromRequirement` intentionally follows the Rust adapter's lossy
  conversion: numeric release bounds are constructed without prerelease
  eligibility filtering. It is not a PEP 440 implementation.
- Default reports follow the reference wording, chaining and line references.
  Collections of three or more incompatible terms can have a different display
  order because Rust hash-table iteration is not a portable ordering contract.
  The Go implementation retains deterministic construction order.
- `ranges` JSON supports Rust's tagged-bound representation and the older
  discrete interval representation. Initialize the comparator before decoding.
  Decoding validates and normalizes intervals.
- Offline provider JSON uses ordered Go records to preserve arbitrary key types
  and duplicate constraints; it is not the Rust serde map format.
- Invalid provider choices and numeric bump overflows return errors instead of
  relying on Rust panics. Custom formatters produce Go strings.
- No claim of equal allocation behavior or performance is implied by behavioral
  compatibility. In particular, equal dependency ranges are located within
  package-pair buckets using equality, without a Rust-specific version-set hash.

## Validation

From this directory:

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -fuzz '^FuzzResolution$' -fuzztime=45s -parallel=4 .
```

The suite includes upstream resolution examples and regressions, self and cyclic
dependencies, partial satisfiers, duplicate constraints, empty ranges, error
propagation, cancellation, priority ties, branch cloning, manual backtracking,
widening, report snapshots and shared references, JSON and custom version domains.
It also checks 3,000 seeded small graphs against an independent exhaustive
solver and 2,000 interval pairs against membership and algebraic invariants.

Rust differential tests are opt-in so ordinary Go tests need no Rust toolchain.
With the adjacent reference checkout present:

```sh
CARGO_TARGET_DIR=/tmp/pubgrub-go-reference-target \
  cargo +1.92.0 build --locked --manifest-path testdata/rust-reference/Cargo.toml

PUBGRUB_RUST_REFERENCE=/tmp/pubgrub-go-reference-target/debug/pubgrub-go-reference \
  go test -run '^TestRustDifferential' -v .
```

The reference executable is test infrastructure only. Differential tests compare:

- 2,000 seeded graphs: exact selected versions for satisfiable inputs; every
  proof term, ordered cause and shared-node marker for unsatisfiable inputs,
  both before and after no-version collapse. Only map iteration order is
  canonicalized.
- 3,000 interval pairs: canonical results for every set operation, ordering,
  membership, display and the three simplification operations.
- SemVer requirements and prerelease/build-aware version comparisons.

The adjacent Rust checkout remains unchanged. Its commit is recorded above so
future changes can be assessed against a precise baseline.

## License

MPL-2.0, matching the ported PubGrub sources. See [LICENSE](LICENSE) and
[NOTICE](NOTICE).
