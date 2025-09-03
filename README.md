# Oxinfer

A Go CLI tool that uses tree-sitter PHP to analyze Laravel/PHP repositories without executing PHP. It scans `app/` and `routes/` directories, detects key Laravel patterns, and emits deterministic JSON for OpenAPI generation.

## Features

- **Static Analysis**: Analyzes PHP code without execution using tree-sitter
- **Laravel Pattern Detection**: Detects 8 key Laravel patterns (HTTP status, resources, pivots, attributes, scopes, polymorphic, broadcast)
- **Deterministic Output**: Same repository always produces identical JSON (byte-for-byte)
- **Concurrent Processing**: Configurable worker pools with memory-efficient caching
- **Contract-First**: Input/output validated against JSON schemas

## Installation

```bash
go build -o oxinfer cmd/oxinfer
```

## Usage

### Basic Usage

```bash
# Analyze with manifest file
oxinfer --manifest manifest.json

# Read manifest from stdin
cat manifest.json | oxinfer

# Output to specific file
oxinfer --manifest manifest.json --out delta.json

# Print help
oxinfer --help
```

### Manifest File Format

```json
{
  "project": {
    "root": "/path/to/laravel/project",
    "composer": "composer.json"
  },
  "scan": {
    "targets": ["app", "routes"],
    "globs": ["**/*.php"],
    "vendor_whitelist": []
  },
  "limits": {
    "max_workers": 8,
    "max_files": 500,
    "max_depth": 2
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

## CLI Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--manifest <file>` | Path to manifest file (or `-` for stdin) | - |
| `--out <file>` | Output file path (or `-` for stdout) | stdout |
| `--cache-dir <dir>` | Override cache directory | `<project>/.oxinfer/cache/v1` |
| `--log-level <level>` | Log level: error\|warn\|info\|debug | warn |
| `--no-color` | Disable colored output | false |
| `--quiet` | Quiet mode (error logs only) | false |
| `--print-hash` | Print canonical SHA256 to stderr | false |
| `--stamp` | Include timestamp in output | false |
| `--version` | Show version information | false |

## Limits & Performance

### Resource Limits

- **max_workers**: Concurrent processing workers (1-32)
- **max_files**: Maximum files to process before setting `partial: true`
- **max_depth**: Maximum relationship traversal depth

### Performance Goals

- **Medium projects** (200-600 files): <10s cold, <2s with cache
- **Memory usage**: Stable, no catastrophic spikes
- **Deterministic**: Multiple runs produce identical output

### Cache Behavior

- **Location**: `<project>/.oxinfer/cache/v1/` (configurable)
- **Validation**: `sha256+mtime` (secure) or `mtime` (fast)
- **Invalidation**: Automatic on file changes

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | Input validation error |
| 2 | Internal processing error |
| 3 | Schema load/validation failure |
| 4 | Hard limit exceeded |
| 5 | Ownership violation (reserved) |

## Output Format

Oxinfer produces structured JSON with detected Laravel patterns:

```json
{
  "meta": {
    "partial": false,
    "stats": {
      "filesParsed": 42,
      "skipped": 0,
      "durationMs": 150
    },
    "version": "0.1.0"
  },
  "controllers": [...],
  "models": [...],
  "polymorphic": [...],
  "broadcast": [...]
}
```

## Pattern Detection

### Supported Patterns

1. **HTTP Status**: `response(..., 201)`, `->setStatusCode(404)`, `->noContent()`
2. **Request Usage**: `input()`, `get()`, `only(['name'])`, `file('upload')`
3. **Resource Usage**: `new UserResource($user)`, `UserResource::collection($users)`
4. **Pivot**: `->withPivot('role', 'created_at')`, `->withTimestamps()`
5. **Attributes**: `Attribute::make(fn() => ...)`
6. **Scopes**: `$query->scopeActive()`, `Model::query()->scopePublished()`
7. **Polymorphic**: `morphTo()`, `morphMany()`, `Relation::morphMap()`
8. **Broadcast**: `Broadcast::channel('users.{id}', fn...)`

### File Scanning

- **Default targets**: `app/**/*.php`, `routes/**/*.php`
- **Vendor scanning**: Only whitelisted paths (security)
- **Deterministic ordering**: Files processed in sorted order

## Troubleshooting

### Common Issues

**Cache corruption**: Use `rm -rf <project>/.oxinfer/cache` to reset

**Memory issues**: Reduce `max_workers` or `max_files` in manifest

**Slow performance**: Enable caching and use `kind: "mtime"` for faster validation

**Non-deterministic output**: Check for concurrent processing race conditions (report as bug)

### Debug Mode

```bash
oxinfer --manifest manifest.json --log-level debug
```

### Validation

```bash
# Verify output schema
oxinfer --manifest manifest.json | jq . >/dev/null && echo "Valid JSON"

# Check determinism
oxinfer --manifest manifest.json --print-hash
```

## Makefile Commands

The repository includes a Makefile with helpful shortcuts:

```bash
# List available commands
make help

# Build the CLI
make build

# Run the full test suite
make test

# Format and vet
make fmt
make vet

# Run the CLI (provide a manifest path; optionally set OUT)
make run MANIFEST=path/to/manifest.json [OUT=delta.json]

# Generate a performance validation report (writes to .oxinfer/performance_reports)
make perf-validate     # alias: make perf-report

# Clean generated performance reports and cache (keeps .oxinfer/README.md)
make perf-clean
make cache-clean

# Remove build artifacts and generated files (safe)
make clean
```

Generated artifacts live under the hidden `.oxinfer/` directory:
- `.oxinfer/performance_reports/`: performance validation JSON reports (auto-pruned to the latest)
- `.oxinfer/cache/v1/`: on-disk indexer cache (per project key)

To override the cache location:
- CLI flag: `--cache-dir /abs/path` (highest precedence)
- Env var: `OXINFER_CACHE_DIR=/abs/path`

## License

MIT License - see LICENSE file for details.
