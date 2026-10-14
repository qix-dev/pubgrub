// SPDX-License-Identifier: MPL-2.0
use std::collections::BTreeMap;
use std::error::Error;
use std::io::{self, BufRead, Write};

use pubgrub::{
    DefaultStringReporter, DerivationTree, OfflineDependencyProvider, PubGrubError, Reporter,
};
use serde::Deserialize;
use serde_json::{Value, json};
use version_ranges::Ranges;

#[derive(Deserialize)]
struct Dependency {
    package: String,
    versions: Ranges<u32>,
}
#[derive(Deserialize)]
struct Record {
    package: String,
    version: u32,
    dependencies: Vec<Dependency>,
}
#[derive(Deserialize)]
struct Solve {
    root: String,
    version: u32,
    records: Vec<Record>,
}
#[derive(Deserialize)]
struct RangeInput {
    a: Ranges<u32>,
    b: Ranges<u32>,
    versions: Vec<u32>,
}
fn proof(tree: &DerivationTree<String, Ranges<u32>, String>) -> Value {
    match tree {
        DerivationTree::External(external) => json!({"external": external.to_string()}),
        DerivationTree::Derived(derived) => {
            let terms: BTreeMap<_, _> = derived
                .terms
                .iter()
                .map(|(package, term)| (package.clone(), term.to_string()))
                .collect();
            json!({"terms": terms, "shared": derived.shared_id.is_some(),
                "cause1": proof(&derived.cause1), "cause2": proof(&derived.cause2)})
        }
    }
}
fn respond(value: Value) -> Result<Value, Box<dyn Error>> {
    match value["op"].as_str() {
        Some("solve") => {
            let input: Solve = serde_json::from_value(value)?;
            let mut provider = OfflineDependencyProvider::<String, Ranges<u32>>::new();
            for record in input.records {
                provider.add_dependencies(
                    record.package,
                    record.version,
                    record
                        .dependencies
                        .into_iter()
                        .map(|d| (d.package, d.versions)),
                );
            }
            match pubgrub::resolve(&provider, input.root, input.version) {
                Ok(solution) => {
                    Ok(json!({"solution": solution.into_iter().collect::<BTreeMap<_, _>>() }))
                }
                Err(PubGrubError::NoSolution(mut tree)) => {
                    let report = DefaultStringReporter::report(&tree);
                    let original_proof = proof(&tree);
                    tree.collapse_no_versions();
                    Ok(json!({"no_solution": true, "report": report,
                        "proof": original_proof, "collapsed_proof": proof(&tree),
                        "collapsed": DefaultStringReporter::report(&tree)}))
                }
                Err(error) => Err(Box::new(error)),
            }
        }
        Some("ranges") => {
            let input: RangeInput = serde_json::from_value(value)?;
            Ok(json!({
                "union": input.a.union(&input.b),
                "intersection": input.a.intersection(&input.b),
                "difference": input.a.difference(&input.b),
                "complement": input.a.complement(),
                "subset": input.a.subset_of(&input.b),
                "disjoint": input.a.is_disjoint(&input.b),
                "compare": input.a.cmp(&input.b) as i8,
                "contains": input.a.contains_many(input.versions.iter()).collect::<Vec<_>>(),
                "widen": input.a.widen_versions(&input.versions),
                "narrow": input.a.narrow_versions(&input.versions),
                "simplify": input.a.simplify(input.versions.iter()),
                "display": input.a.to_string(),
            }))
        }
        Some("semver") => {
            let text: String = serde_json::from_value(value["requirement"].clone())?;
            match text.parse::<semver::VersionReq>() {
                Ok(requirement) => Ok(json!({"ranges": Ranges::from_req(requirement).to_string()})),
                Err(_) => Ok(json!({"invalid": true})),
            }
        }
        Some("version_compare") => {
            let a: semver::Version = serde_json::from_value(value["a"].clone())?;
            let b: semver::Version = serde_json::from_value(value["b"].clone())?;
            Ok(json!({"compare": a.cmp(&b) as i8}))
        }
        _ => Err("unknown operation".into()),
    }
}
fn main() -> Result<(), Box<dyn Error>> {
    let stdin = io::stdin();
    let mut stdout = io::BufWriter::new(io::stdout().lock());
    for line in stdin.lock().lines() {
        let input: Value = serde_json::from_str(&line?)?;
        let output = match respond(input) {
            Ok(output) => output,
            Err(error) => json!({"error": error.to_string()}),
        };
        serde_json::to_writer(&mut stdout, &output)?;
        writeln!(stdout)?;
        stdout.flush()?;
    }
    Ok(())
}
