use std::fs;
use std::io::{self, IsTerminal, Read};
use std::path::{Path, PathBuf};
use std::process::ExitCode;

use clap::Parser;
use oxinfer::OXINFER_VERSION;
use oxinfer::contracts::{build_analysis_response, load_analysis_request_from_slice};
use oxinfer::manifest::Manifest;
use oxinfer::pipeline::{analyze_project, run_pipeline};

#[derive(Debug, Parser)]
#[command(name = "oxinfer")]
#[command(version = OXINFER_VERSION)]
#[command(about = "Static Laravel analyzer")]
struct Args {
    #[arg(long, conflicts_with = "request")]
    manifest: Option<PathBuf>,

    #[arg(long, conflicts_with = "manifest")]
    request: Option<PathBuf>,

    #[arg(long, default_value = "-")]
    out: String,
}

#[derive(Debug, serde::Serialize)]
struct CliErrorPayload {
    #[serde(rename = "type")]
    error_type: String,
    message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    details: Option<std::collections::BTreeMap<String, String>>,
    #[serde(rename = "exit_code")]
    exit_code: u8,
}

#[derive(Debug)]
struct CliError {
    payload: CliErrorPayload,
}

impl CliError {
    fn validation(message: impl Into<String>) -> Self {
        Self {
            payload: CliErrorPayload {
                error_type: "validation".to_string(),
                message: message.into(),
                details: None,
                exit_code: 1,
            },
        }
    }

    fn validation_with_cause(message: impl Into<String>, cause: impl Into<String>) -> Self {
        let mut details = std::collections::BTreeMap::new();
        details.insert("underlying_error".to_string(), cause.into());
        Self {
            payload: CliErrorPayload {
                error_type: "validation".to_string(),
                message: message.into(),
                details: Some(details),
                exit_code: 1,
            },
        }
    }

    fn internal(message: impl Into<String>, cause: impl Into<String>) -> Self {
        let mut details = std::collections::BTreeMap::new();
        details.insert("underlying_error".to_string(), cause.into());
        Self {
            payload: CliErrorPayload {
                error_type: "internal".to_string(),
                message: message.into(),
                details: Some(details),
                exit_code: 2,
            },
        }
    }

    fn exit_code(&self) -> ExitCode {
        ExitCode::from(self.payload.exit_code)
    }
}

fn main() -> ExitCode {
    match try_main() {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!(
                "{}",
                serde_json::to_string(&err.payload).unwrap_or_else(|_| {
                    format!(
                        "{{\"type\":\"internal\",\"message\":\"{}\",\"exit_code\":2}}",
                        err.payload.message.replace('"', "'")
                    )
                })
            );
            err.exit_code()
        }
    }
}

fn try_main() -> Result<(), CliError> {
    let mut args = Args::parse();
    if args.manifest.is_none() && args.request.is_none() && !io::stdin().is_terminal() {
        args.manifest = Some(PathBuf::from("-"));
    }

    if let Some(request_path) = &args.request {
        return execute_request_mode(request_path, &args.out);
    }

    let manifest_path = args.manifest.as_ref().ok_or_else(|| {
        CliError::validation("no manifest provided: set --manifest <path> or --manifest -")
    })?;
    execute_manifest_mode(manifest_path, &args.out)
}

fn execute_manifest_mode(manifest_path: &Path, out: &str) -> Result<(), CliError> {
    let input = read_input_file(manifest_path, "manifest")?;
    let mut manifest: Manifest = serde_json::from_slice(&input.bytes).map_err(|err| {
        CliError::validation_with_cause("failed to decode manifest JSON", err.to_string())
    })?;
    if let Some(source_path) = input.source_path.as_deref() {
        manifest.resolve_paths(source_path);
    }
    validate_manifest(&manifest)?;

    let delta = run_pipeline(&manifest)
        .map_err(|err| CliError::internal("failed to run rust pipeline", err.to_string()))?;
    let payload = serde_json::to_string_pretty(&delta)
        .map_err(|err| CliError::internal("failed to encode delta JSON", err.to_string()))?;
    emit_output(out, &payload)
}

fn execute_request_mode(request_path: &Path, out: &str) -> Result<(), CliError> {
    let input = read_input_file(request_path, "analysis request")?;
    let request = load_analysis_request_from_slice(&input.bytes, input.source_path.as_deref())
        .map_err(|err| {
            CliError::validation_with_cause("analysis request validation failed", err.to_string())
        })?;
    validate_manifest(&request.manifest)?;

    let result = analyze_project(&request.manifest)
        .map_err(|err| CliError::internal("failed to run rust pipeline", err.to_string()))?;
    let response = build_analysis_response(&request, &result);
    let payload = serde_json::to_string(&response)
        .map_err(|err| CliError::internal("failed to encode analysis response", err.to_string()))?;
    emit_output(out, &payload)
}

struct InputFile {
    bytes: Vec<u8>,
    source_path: Option<PathBuf>,
}

fn read_input_file(path: &Path, kind: &str) -> Result<InputFile, CliError> {
    if path == Path::new("-") {
        let mut bytes = Vec::new();
        io::stdin().read_to_end(&mut bytes).map_err(|err| {
            CliError::validation_with_cause(format!("failed to read {kind} data"), err.to_string())
        })?;
        return Ok(InputFile {
            bytes,
            source_path: None,
        });
    }

    let bytes = fs::read(path).map_err(|err| {
        CliError::validation_with_cause(
            format!("failed to open {kind} file \"{}\"", path.display()),
            err.to_string(),
        )
    })?;
    Ok(InputFile {
        bytes,
        source_path: Some(path.to_path_buf()),
    })
}

fn validate_manifest(manifest: &Manifest) -> Result<(), CliError> {
    let root = manifest.project.root.as_path();
    if root.as_os_str().is_empty() {
        return Err(CliError::validation("project.root cannot be empty"));
    }
    if !root.exists() {
        return Err(CliError::validation(format!(
            "project root path does not exist: \"{}\"",
            root.display()
        )));
    }
    if !root.is_dir() {
        return Err(CliError::validation(format!(
            "project root must be a directory, got file: \"{}\"",
            root.display()
        )));
    }
    Ok(())
}

fn emit_output(out: &str, payload: &str) -> Result<(), CliError> {
    if out == "-" {
        println!("{payload}");
        return Ok(());
    }

    let out_path = Path::new(out);
    if let Some(parent) = out_path.parent() {
        fs::create_dir_all(parent).map_err(|err| {
            CliError::internal("failed to create output directory", err.to_string())
        })?;
    }
    fs::write(out_path, payload)
        .map_err(|err| CliError::internal(format!("failed to write output {out}"), err.to_string()))
}
