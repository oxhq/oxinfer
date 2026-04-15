# Release

`oxinfer` is released at `v0.1.1` with these public surfaces treated as stable:

- CLI `oxinfer --manifest ...`
- CLI `oxinfer --request ...`
- machine contract `oxcribe.oxinfer.v2`
- active schema documents in `schemas/analysis-request-v2.schema.json` and `schemas/analysis-response-v2.schema.json`

Pre-release checklist:

1. `GOEXPERIMENT=jsonv2 go test ./...`
2. `GOEXPERIMENT=jsonv2 go build -o oxinfer ./cmd/oxinfer`
3. Confirm `README.md`, `docs/analysis-contract-v2.md`, and `CHANGELOG.md` match the current public surface
4. Confirm the release workflow will publish all six platform binaries plus `checksums.txt`
5. Tag `v0.1.1`
6. Publish GitHub release from that tag

Release asset contract:

- binaries are published as `oxinfer_<tag>_<os>_<arch>[.exe]`
- `checksums.txt` is published in the same release

`oxcribe:install-binary` relies on that contract for verified binary installs. If a release is missing assets, the supported temporary fallback is to build from source with `OXINFER_SOURCE_ROOT`.

Post-freeze policy:

- `v0.1.x` should only carry maintenance changes and contract-safe fixes
- contract changes require a new versioned contract document and matching schemas
