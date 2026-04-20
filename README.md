# Oxinfer

`oxinfer` is a Rust CLI for static Laravel analysis. It parses PHP with tree-sitter, resolves routes and controllers in the same pipeline, and emits deterministic JSON either as a project delta or as the `oxcribe.oxinfer.v2` machine contract used by `oxcribe`.

## Build

```bash
cargo build --locked --release
```

The release binary is `target/release/oxinfer`. `make build` wraps the same command and copies the binary to `./oxinfer`.

## Usage

```bash
# Manifest mode
cargo run -- --manifest fixtures/minimal.manifest.json

# Manifest from stdin
cat fixtures/minimal.manifest.json | cargo run -- --manifest -

# Contract mode
cat test/fixtures/contracts/request-mode/matched-invokable.json | cargo run -- --request -

# Write output to a file
cargo run -- --manifest fixtures/api.manifest.json --out delta.json
```

CLI flags:

- `--manifest <path>` reads a manifest JSON document or `-` from stdin.
- `--request <path>` reads an `AnalysisRequest` JSON document or `-` from stdin.
- `--out <path>` writes JSON to a file or `-` for stdout.
- `--help` and `--version` expose the standard clap metadata.

## Output Modes

Manifest mode emits grouped static facts:

- controllers grouped by FQCN and file, with per-method HTTP methods, status codes, request usage, resource usage, and scopes
- models with relationships and pivot metadata
- polymorphic relationship groups
- broadcast channels

Request mode wraps the same analysis in the published contract:

- `contractVersion: "oxcribe.oxinfer.v2"`
- `status`, `meta`, `delta`, `routeMatches`, and `diagnostics`

The active contract reference lives in [docs/analysis-contract-v2.md](docs/analysis-contract-v2.md). Release policy lives in [docs/release.md](docs/release.md).

## Tests

```bash
cargo test --locked
```

The Rust suite covers:

- Laravel fixture smoke tests in [tests/fixture_smoke.rs](/Users/garaekz/Documents/projects/go/oxinfer/tests/fixture_smoke.rs)
- route extraction checks in [tests/routes_smoke.rs](/Users/garaekz/Documents/projects/go/oxinfer/tests/routes_smoke.rs)
- request-mode golden parity in [tests/request_mode_smoke.rs](/Users/garaekz/Documents/projects/go/oxinfer/tests/request_mode_smoke.rs)

Fixture projects and request payloads live under [test/fixtures](/Users/garaekz/Documents/projects/go/oxinfer/test/fixtures). Golden contract outputs live under [test/golden/request-mode](/Users/garaekz/Documents/projects/go/oxinfer/test/golden/request-mode).

## Manifest Shape

```json
{
  "project": {
    "root": "/path/to/laravel/project",
    "composer": "composer.json"
  },
  "scan": {
    "targets": ["app", "routes"],
    "globs": ["**/*.php"]
  },
  "limits": {
    "max_workers": 8,
    "max_files": 1000,
    "max_depth": 6
  },
  "features": {
    "http_status": true,
    "request_usage": true,
    "resource_usage": true,
    "with_pivot": true,
    "attribute_make": true,
    "scopes_used": true,
    "polymorphic": true,
    "broadcast_channels": true
  }
}
```

## Repo Shortcuts

```bash
make help
make build
make test
make fmt
make vet
make run MANIFEST=fixtures/minimal.manifest.json
```
