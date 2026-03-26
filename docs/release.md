# Release

`oxinfer` is frozen for `v0.1.0` with these public surfaces treated as stable:

- CLI `oxinfer --manifest ...`
- CLI `oxinfer --request ...`
- machine contract `oxcribe.oxinfer.v2`
- active schema documents in `schemas/analysis-request-v2.schema.json` and `schemas/analysis-response-v2.schema.json`

Pre-release checklist:

1. `GOEXPERIMENT=jsonv2 go test ./...`
2. `GOEXPERIMENT=jsonv2 go build -o oxinfer ./cmd/oxinfer`
3. Confirm `README.md`, `docs/analysis-contract-v2.md`, and `CHANGELOG.md` match the current public surface
4. Tag `v0.1.0`
5. Publish GitHub release from that tag

Post-freeze policy:

- `v0.1.x` should only carry maintenance changes and contract-safe fixes
- contract changes require a new versioned contract document and matching schemas
