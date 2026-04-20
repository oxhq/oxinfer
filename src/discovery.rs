use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use walkdir::WalkDir;

use crate::manifest::Manifest;

pub fn discover_php_files(manifest: &Manifest) -> Result<Vec<PathBuf>> {
    let max_depth = manifest.limits.max_depth.unwrap_or(usize::MAX);
    let max_files = manifest.limits.max_files.unwrap_or(usize::MAX);
    let mut files = Vec::new();

    for target in &manifest.scan.targets {
        let base = manifest.project.root.join(target);
        if !base.exists() {
            continue;
        }

        let walker = WalkDir::new(&base)
            .max_depth(max_depth)
            .follow_links(false)
            .into_iter();

        for entry in walker {
            let entry =
                entry.with_context(|| format!("failed while scanning {}", base.display()))?;
            if !entry.file_type().is_file() {
                continue;
            }

            let path = entry.into_path();
            if !is_php_file(&path) {
                continue;
            }

            if should_skip_vendor(
                &path,
                &manifest.project.root,
                &manifest.scan.vendor_whitelist,
            ) {
                continue;
            }

            files.push(path);
            if files.len() >= max_files {
                break;
            }
        }

        if files.len() >= max_files {
            break;
        }
    }

    files.sort();
    Ok(files)
}

fn is_php_file(path: &Path) -> bool {
    path.extension()
        .and_then(|ext| ext.to_str())
        .map(|ext| ext.eq_ignore_ascii_case("php"))
        .unwrap_or(false)
}

fn should_skip_vendor(path: &Path, root: &Path, whitelist: &[String]) -> bool {
    let relative = match path.strip_prefix(root) {
        Ok(path) => path,
        Err(_) => return false,
    };

    let relative_text = relative.to_string_lossy().replace('\\', "/");
    if !relative_text.contains("/vendor/") && !relative_text.starts_with("vendor/") {
        return false;
    }

    whitelist
        .iter()
        .all(|allowed| !relative_text.starts_with(allowed))
}
