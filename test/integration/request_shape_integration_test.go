//go:build goexperiment.jsonv2

package integration

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/infer"
	"github.com/garaekz/oxinfer/internal/matchers"
	"github.com/garaekz/oxinfer/internal/parser"
	"github.com/garaekz/oxinfer/internal/pipeline"
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
						"products.*.name":              "Product Name",
						"metadata.created_by":          "admin",
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
			pipelineResults := &pipeline.PipelineResults{
				ParseResults: &parser.ParseResults{
					Controllers: []parser.ControllerInfo{
						{
							FilePath:  "app/Http/Controllers/TestController.php",
							Namespace: "App\\Http\\Controllers",
							ClassName: "TestController",
							FQCN:      "App\\Http\\Controllers\\TestController",
							Methods: []parser.MethodInfo{
								{
									Name:       "store",
									ReturnType: "Response",
									Visibility: "public",
								},
							},
						},
					},
				},
				MatchResults: &matchers.MatchResults{
					HTTPStatus: []matchers.HTTPStatusMatch{
						{
							FilePath:       "app/Http/Controllers/TestController.php",
							ControllerName: "TestController",
							MethodName:     "store",
							StatusCode:     201,
							Confidence:     0.95,
						},
					},
					RequestUsage: tc.requestPatterns,
				},
				InferenceResults: &infer.InferenceResults{
					Controllers: map[string]*infer.ControllerInference{
						"App\\Http\\Controllers\\TestController::store": {
							Request: func() *infer.RequestInfo {
								// Use the shape inferencer to process patterns
								inferencer := infer.NewShapeInferencer(nil, nil, nil, nil)
								result, _ := inferencer.InferRequestShape(tc.requestPatterns)
								return result
							}(),
						},
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
			if controller.Class != "App\\Http\\Controllers\\TestController" {
				t.Errorf("Expected controller class App\\Http\\Controllers\\TestController, got %s", controller.Class)
			}

			// Find the store method
			var storeMethod *emitter.ControllerMethod
			for i := range controller.Methods {
				if controller.Methods[i].Name == "store" {
					storeMethod = &controller.Methods[i]
					break
				}
			}

			if storeMethod == nil {
				t.Fatal("Store method not found in controller")
			}

			// Verify request body structure matches expected
			if storeMethod.Request == nil {
				t.Fatal("Request is nil in store method")
			}

			// Compare the body structure
			if !compareStructures(t, storeMethod.Request.Body, tc.expectedBody) {
				// Marshal both for better error reporting
				actualJSON, _ := json.MarshalIndent(storeMethod.Request.Body, "", "  ")
				expectedJSON, _ := json.MarshalIndent(tc.expectedBody, "", "  ")
				
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
