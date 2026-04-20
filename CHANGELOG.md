# Changelog

## v0.1.1 - 2026-04-14

Maintenance release.

- publish a first-class GitHub release workflow that emits all platform binaries plus `checksums.txt`
- document the release asset contract expected by `oxcribe:install-binary`

## v0.1.0 - 2026-03-25

Initial public freeze of `oxinfer`.

- Stable CLI for manifest-driven static Laravel analysis
- Stable machine contract `oxcribe.oxinfer.v2` via `--request`
- Deterministic JSON v2 output with route matches and diagnostics
- Laravel-aware inference for request shapes, responses, resources, auth hints, and supported Spatie integrations
- Repository build/test flow moved to Cargo
