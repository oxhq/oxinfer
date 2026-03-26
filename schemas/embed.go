package schemas

import _ "embed"

//go:embed manifest.schema.json
var ManifestSchema []byte

//go:embed delta.schema.json
var DeltaSchema []byte

//go:embed analysis-request-v2.schema.json
var AnalysisRequestV2Schema []byte

//go:embed analysis-response-v2.schema.json
var AnalysisResponseV2Schema []byte
