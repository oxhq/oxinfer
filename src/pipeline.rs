use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;
use std::time::Instant;

use anyhow::{Context, Result};
use rayon::prelude::*;

use crate::discovery::discover_php_files;
use crate::manifest::Manifest;
use crate::matchers::analyze_file;
use crate::model::{AnalyzedFile, Delta};
use crate::output::build_delta;
use crate::parser::parse_file;
use crate::routes::{RouteBinding, extract_route_bindings};

pub struct PipelineResult {
    pub files: Vec<AnalyzedFile>,
    pub route_bindings: Vec<RouteBinding>,
    pub partial: bool,
    pub duration_ms: u128,
}

pub fn run_pipeline(manifest: &Manifest) -> Result<Delta> {
    Ok(build_delta(analyze_project(manifest)?))
}

pub fn analyze_project(manifest: &Manifest) -> Result<PipelineResult> {
    let started = Instant::now();
    let discovered = discover_php_files(manifest)?;
    let worker_count = manifest
        .limits
        .max_workers
        .filter(|value| *value > 0)
        .unwrap_or_else(default_worker_count);

    let pool = rayon::ThreadPoolBuilder::new()
        .num_threads(worker_count)
        .build()
        .context("failed to build rayon worker pool")?;

    let root = manifest.project.root.clone();
    let files = pool.install(|| analyze_files(&root, discovered, manifest))?;
    let route_bindings = collect_route_bindings(&root, &files)?;
    Ok(PipelineResult {
        files,
        route_bindings,
        partial: false,
        duration_ms: started.elapsed().as_millis(),
    })
}

fn analyze_files(
    root: &PathBuf,
    discovered: Vec<PathBuf>,
    manifest: &Manifest,
) -> Result<Vec<AnalyzedFile>> {
    let mut files = discovered
        .par_iter()
        .map(|path| analyze_single_file(root, path, manifest))
        .collect::<Result<Vec<_>>>()?;

    filter_controller_scopes(&mut files);
    files.sort_by(|a, b| a.relative_path.cmp(&b.relative_path));
    Ok(files)
}

fn analyze_single_file(
    root: &PathBuf,
    path: &PathBuf,
    manifest: &Manifest,
) -> Result<AnalyzedFile> {
    let relative_path = path
        .strip_prefix(root)
        .unwrap_or(path)
        .to_string_lossy()
        .replace('\\', "/");

    let parsed = parse_file(path)?;
    let source_text = String::from_utf8_lossy(&parsed.source).into_owned();
    let facts = analyze_file(&parsed, &relative_path, &manifest.features)
        .with_context(|| format!("failed to analyze {}", path.display()))?;

    Ok(AnalyzedFile {
        path: path.clone(),
        relative_path,
        source_text,
        facts,
    })
}

fn default_worker_count() -> usize {
    std::thread::available_parallelism()
        .map(|count| count.get())
        .unwrap_or(4)
        .max(4)
}

fn collect_route_bindings(root: &PathBuf, files: &[AnalyzedFile]) -> Result<Vec<RouteBinding>> {
    let mut route_bindings = Vec::new();

    for file in files {
        if !file.relative_path.starts_with("routes/") {
            continue;
        }

        let source = fs::read_to_string(root.join(&file.relative_path))
            .with_context(|| format!("failed to read route file {}", file.relative_path))?;
        route_bindings.extend(extract_route_bindings(&source));
    }

    route_bindings.sort_by(|a, b| {
        (&a.controller_fqcn, &a.method_name, &a.http_methods).cmp(&(
            &b.controller_fqcn,
            &b.method_name,
            &b.http_methods,
        ))
    });
    route_bindings.dedup_by(|a, b| {
        a.controller_fqcn == b.controller_fqcn
            && a.method_name == b.method_name
            && a.http_methods == b.http_methods
    });

    Ok(route_bindings)
}

fn filter_controller_scopes(files: &mut [AnalyzedFile]) {
    let known_scopes = files
        .iter()
        .flat_map(|file| file.facts.models.iter())
        .flat_map(|model| model.scopes.iter().cloned())
        .collect::<BTreeSet<_>>();

    if known_scopes.is_empty() {
        for file in files {
            for controller in &mut file.facts.controllers {
                controller.scopes_used.clear();
            }
        }
        return;
    }

    for file in files {
        for controller in &mut file.facts.controllers {
            controller
                .scopes_used
                .retain(|scope| known_scopes.contains(&scope.name));
        }
    }
}
