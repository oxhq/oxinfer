//go:build goexperiment.jsonv2

package contracts

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"path/filepath"

	"github.com/oxhq/oxinfer/internal/cli"
	"github.com/oxhq/oxinfer/internal/manifest"
	oxschemas "github.com/oxhq/oxinfer/schemas"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	manifestSchemaID         = "https://oxcribe.dev/schema/manifest.schema.json"
	deltaSchemaID            = "https://oxcribe.dev/schema/delta.schema.json"
	analysisRequestSchemaID  = "https://oxcribe.dev/schema/analysis-request-v2.schema.json"
	analysisResponseSchemaID = "https://oxcribe.dev/schema/analysis-response-v2.schema.json"
)

func LoadAnalysisRequestFromReader(r io.Reader) (*AnalysisRequest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, cli.WrapInputError("failed to read analysis request data", err)
	}
	return LoadAnalysisRequest(data)
}

func LoadAnalysisRequest(data []byte) (*AnalysisRequest, error) {
	if err := validateJSONDocument(data, analysisRequestSchemaID, "analysis request"); err != nil {
		return nil, err
	}

	var request AnalysisRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, cli.WrapInputError("invalid JSON in analysis request", err)
	}

	manifest.ApplyDefaults(&request.Manifest)
	if err := manifest.NewValidator().ValidatePaths(&request.Manifest); err != nil {
		return nil, err
	}

	request.Normalize()
	if err := validateRequestBusinessRules(&request); err != nil {
		return nil, err
	}

	return &request, nil
}

func ValidateAnalysisResponse(response *AnalysisResponse) error {
	data, err := json.Marshal(response, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("failed to marshal analysis response for validation: %w", err)
	}
	return validateJSONDocument(data, analysisResponseSchemaID, "analysis response")
}

func MarshalAnalysisResponse(response *AnalysisResponse) ([]byte, error) {
	return json.Marshal(response, json.Deterministic(true))
}

func validateRequestBusinessRules(request *AnalysisRequest) error {
	if !filepath.IsAbs(request.Runtime.App.BasePath) {
		return cli.NewInputError("runtime.app.basePath must be an absolute path")
	}

	if filepath.Clean(request.Runtime.App.BasePath) != filepath.Clean(request.Manifest.Project.Root) {
		return cli.NewInputError("runtime.app.basePath must match manifest.project.root after normalization")
	}

	seenRouteIDs := make(map[string]struct{}, len(request.Runtime.Routes))
	for _, route := range request.Runtime.Routes {
		if _, exists := seenRouteIDs[route.RouteID]; exists {
			return cli.NewInputError(fmt.Sprintf("runtime.routes contains duplicate routeId %q", route.RouteID))
		}
		seenRouteIDs[route.RouteID] = struct{}{}
	}

	return nil
}

func validateJSONDocument(data []byte, schemaID string, documentName string) error {
	schema, err := compileSchema(schemaID)
	if err != nil {
		return err
	}

	var jsonData any
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return cli.WrapInputError(fmt.Sprintf("invalid JSON structure in %s", documentName), err)
	}

	if err := schema.Validate(jsonData); err != nil {
		return cli.WrapInputError(fmt.Sprintf("%s validation failed: %v", documentName, err), err)
	}

	return nil
}

func compileSchema(schemaID string) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020

	for id, resource := range map[string][]byte{
		manifestSchemaID:         oxschemas.ManifestSchema,
		deltaSchemaID:            oxschemas.DeltaSchema,
		analysisRequestSchemaID:  oxschemas.AnalysisRequestV2Schema,
		analysisResponseSchemaID: oxschemas.AnalysisResponseV2Schema,
	} {
		if err := compiler.AddResource(id, bytes.NewReader(resource)); err != nil {
			return nil, cli.WrapSchemaError(fmt.Sprintf("failed to load schema resource %s", id), err)
		}
	}

	schema, err := compiler.Compile(schemaID)
	if err != nil {
		return nil, cli.WrapSchemaError(fmt.Sprintf("failed to compile schema %s", schemaID), err)
	}
	return schema, nil
}
