//go:build goexperiment.jsonv2

package integration

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/infer"
	"github.com/oxhq/oxinfer/internal/matchers"
	"github.com/oxhq/oxinfer/internal/pipeline"
)

// TestRequestShapeEndToEndIntegration tests request shape recursion from matchers to final delta.json output.
func TestRequestShapeEndToEndIntegration(t *testing.T) {
	// Create test data that simulates real Laravel request patterns
	testCases := []struct {
		name            string
		requestPatterns []matchers.RequestUsageMatch
		expectedBody    map[string]interface{}
	}{
		{
			name: "Laravel_only_with_nested_paths",
			requestPatterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"only"},
					Body: map[string]interface{}{
						"users.*.email":     "test@example.com",
						"users.*.profile":   "profile data",
						"filters.date.from": "2024-01-01",
						"filters.date.to":   "2024-12-31",
						"settings.theme":    "dark",
					},
				},
			},
			expectedBody: map[string]interface{}{
				"users": map[string]interface{}{
					"*": map[string]interface{}{
						"email":   map[string]interface{}{},
						"profile": map[string]interface{}{},
					},
				},
				"filters": map[string]interface{}{
					"date": map[string]interface{}{
						"from": map[string]interface{}{},
						"to":   map[string]interface{}{},
					},
				},
				"settings": map[string]interface{}{
					"theme": map[string]interface{}{},
				},
			},
		},
		{
			name: "Complex_nested_with_multiple_wildcards",
			requestPatterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"validate"},
					Body: map[string]interface{}{
						"products.*.variants.*.price": "100.00",
						"products.*.variants.*.stock": "50",
						"products.*.name":             "Product Name",
						"metadata.created_by":         "admin",
					},
				},
			},
			expectedBody: map[string]interface{}{
				"products": map[string]interface{}{
					"*": map[string]interface{}{
						"variants": map[string]interface{}{
							"*": map[string]interface{}{
								"price": map[string]interface{}{},
								"stock": map[string]interface{}{},
							},
						},
						"name": map[string]interface{}{},
					},
				},
				"metadata": map[string]interface{}{
					"created_by": map[string]interface{}{},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Step 1: Create pipeline results with our test patterns
			controllerFQCN := "App\\Http\\Controllers\\TestController"
			methodKey := controllerFQCN + "::store"

			httpStatusMatches := []*matchers.HTTPStatusMatch{
				{
					Status:   201,
					Explicit: true,
					Method:   methodKey,
				},
			}

			requestUsageMatches := make([]*matchers.RequestUsageMatch, len(tc.requestPatterns))
			for i := range tc.requestPatterns {
				copyPattern := tc.requestPatterns[i]
				requestUsageMatches[i] = &copyPattern
			}

			inferencer := infer.NewShapeInferencer(nil, nil, nil, nil)
			requestShape, _ := inferencer.InferRequestShape(tc.requestPatterns)

			pipelineResults := &pipeline.PipelineResults{
				ParseResults: &pipeline.ParseResults{
					Controllers: map[string][]string{
						controllerFQCN: {"store"},
					},
				},
				MatchResults: &pipeline.MatchResults{
					HTTPStatusMatches:   httpStatusMatches,
					RequestUsageMatches: requestUsageMatches,
				},
				InferenceResults: &pipeline.InferenceResults{
					RequestShapes: map[string]*infer.RequestInfo{
						methodKey: requestShape,
					},
				},
			}

			// Step 2: Assemble the delta using the real assembler
			assembler := pipeline.NewDeltaAssembler()
			delta, err := assembler.AssembleDelta(context.Background(), pipelineResults)
			if err != nil {
				t.Fatalf("Failed to assemble delta: %v", err)
			}

			// Step 3: Verify the controller has the expected nested request structure
			if len(delta.Controllers) != 1 {
				t.Fatalf("Expected 1 controller, got %d", len(delta.Controllers))
			}

			controller := delta.Controllers[0]
			if controller.FQCN != controllerFQCN {
				t.Errorf("Expected controller FQCN %s, got %s", controllerFQCN, controller.FQCN)
			}
			if controller.Method != "store" {
				t.Errorf("Expected method 'store', got %s", controller.Method)
			}

			// Verify request body structure matches expected
			if controller.Request == nil {
				t.Fatal("Request is nil in controller")
			}

			// Compare the body structure
			if !compareStructures(t, controller.Request.Body, tc.expectedBody) {
				// Marshal both for better error reporting
				actualJSON, _ := json.Marshal(controller.Request.Body, json.Deterministic(true))
				expectedJSON, _ := json.Marshal(tc.expectedBody, json.Deterministic(true))

				t.Errorf("Body structure mismatch:\nExpected:\n%s\n\nActual:\n%s",
					string(expectedJSON), string(actualJSON))
			}
		})
	}
}

// compareStructures recursively compares nested map structures.
func compareStructures(t *testing.T, actual emitter.OrderedObject, expected map[string]interface{}) bool {
	if len(actual) != len(expected) {
		t.Logf("Key count mismatch: actual=%d, expected=%d", len(actual), len(expected))
		return false
	}

	for key, expectedValue := range expected {
		actualValue, exists := actual[key]
		if !exists {
			t.Logf("Missing key: %s", key)
			return false
		}

		// Recursively compare nested objects
		if expectedObj, isObj := expectedValue.(map[string]interface{}); isObj {
			if !compareStructures(t, actualValue, expectedObj) {
				return false
			}
		}
	}

	return true
}
