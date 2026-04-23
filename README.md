# Oxinfer

`oxinfer` is a Rust CLI for static Laravel analysis. It parses PHP with tree-sitter, resolves routes and controllers in the same pipeline, and emits deterministic JSON either as a project delta or as the `oxcribe.oxinfer.v2` machine contract used by `oxcribe`.

`v0.1.4` is the current public preview release.

## Build

```bash
cargo build --locked --release
```

The release binary is `target/release/oxinfer`.

## Repo Tasks

The canonical repo task surface lives in [.cargo/config.toml](.cargo/config.toml) as Cargo aliases. Use these first on any platform:

```bash
cargo ox-build
cargo ox-test
cargo ox-fmt
cargo ox-vet
cargo ox-clean
cargo ox-run --manifest fixtures/minimal.manifest.json
```

The wrappers below are thin convenience shims over the same aliases. They do not define separate build logic.

Unix shells:

```bash
make help
make build
make test
make fmt
make vet
make clean
make run MANIFEST=fixtures/minimal.manifest.json
```

Windows PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 help
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 build
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 test
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 fmt
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 vet
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 clean
powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 run -Manifest fixtures/minimal.manifest.json
```

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
- `--cache-dir <path>` overrides the on-disk cache directory used when manifest caching is enabled.
- `--log-level <error|warn|info|debug>` controls CLI warnings on stderr.
- `--quiet` suppresses warnings by forcing `--log-level error`.
- `--no-color` is accepted for compatibility; the Rust CLI currently emits plain JSON/plain stderr text.
- `--print-hash` prints a canonical SHA256 to stderr.
- `--stamp` adds `meta.generatedAt` to manifest-mode output.
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

- Laravel fixture smoke tests in [tests/fixture_smoke.rs](tests/fixture_smoke.rs)
- route extraction checks in [tests/routes_smoke.rs](tests/routes_smoke.rs)
- request-mode golden parity in [tests/request_mode_smoke.rs](tests/request_mode_smoke.rs)
- CLI/cache smoke coverage in [tests/cli_smoke.rs](tests/cli_smoke.rs), including full warm-cache reuse across `minimal`, `api`, and `complex`

Fixture projects and request payloads live under [test/fixtures](test/fixtures). Golden contract outputs live under [test/golden/request-mode](test/golden/request-mode).

For a quick cold-vs-warm cache report against the shipped fixture manifests, run [scripts/cache-bench.ps1](scripts/cache-bench.ps1):

```powershell
./scripts/cache-bench.ps1
```

The script creates temporary cache-enabled copies of the fixture manifests, runs a cold pass and a warm pass for each fixture, and fails if the warm pass does not reuse every parsed file.

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
  "cache": {
    "enabled": true,
    "kind": "sha256+mtime"
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

When `cache.enabled` is `true`, the Rust pipeline stores per-file analyzed results under `<project>/.oxinfer/cache/v3` by default and only reparses files whose fingerprint changed. Cache validation supports `kind: "mtime"` and `kind: "sha256+mtime"`. `--cache-dir` and `OXINFER_CACHE_DIR` both override the default location.

The bundled [Makefile](Makefile) is a Unix convenience wrapper. [scripts/dev.ps1](scripts/dev.ps1) provides the same task names for PowerShell. Both delegate to the Cargo aliases above so the documented task surface stays unified.

Benchmark/reporting is available separately through [scripts/cache-bench.ps1](scripts/cache-bench.ps1), which reports cold and warm timings plus cache hit/miss counts for the fixture manifests.
