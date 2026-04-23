# Changelog

## v0.1.2 - 2026-04-23

Preview release focused on request-mode fidelity, cache-backed CLI hardening, and release automation.

- split request-mode contract assembly into focused authorization, inertia, error-response, and query-builder modules
- improve route and model matching for standard Laravel controllers and custom base-model subclasses
- infer richer framework responses, resource envelopes, URI formats, and Spatie-heavy request and response metadata
- add cache-aware CLI smoke coverage plus cross-platform CI and release smoke for shipped binaries
- document the current Rust-first build, task, and release flow
- keep the published machine contract at `oxcribe.oxinfer.v2`

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
