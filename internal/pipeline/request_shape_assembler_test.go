package pipeline

import (
	"testing"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/infer"
	"github.com/garaekz/oxinfer/internal/matchers"
)

// TestRequestShapeRecursion tests the implementation for nested request structures.
func TestRequestShapeRecursion(t *testing.T) {
	tests := []struct {
		name     string
		input    *infer.RequestInfo
		expected map[string]interface{}
	}{
		{
			name: "nested_object_users_wildcard_email",
			input: createRequestInfoWithPath(t, "users.*.email"),
			expected: map[string]interface{}{
				"users": map[string]interface{}{
					"*": map[string]interface{}{
						"email": map[string]interface{}{},
					},
				},
			},
		},
		{
			name: "nested_filters_date_from",
			input: createRequestInfoWithPath(t, "filters.date.from"),
			expected: map[string]interface{}{
				"filters": map[string]interface{}{
					"date": map[string]interface{}{
						"from": map[string]interface{}{},
					},
				},
			},
		},
		{
			name: "simple_flat_property",
			input: createRequestInfoWithSimpleProperty("name"),
			expected: map[string]interface{}{
				"name": map[string]interface{}{},
			},
		},
		{
			name: "complex_nested_structure",
			input: createComplexNestedRequestInfo(),
			expected: map[string]interface{}{
				"users": map[string]interface{}{
					"*": map[string]interface{}{
						"profile": map[string]interface{}{
							"bio": map[string]interface{}{},
						},
						"email": map[string]interface{}{},
					},
				},
				"settings": map[string]interface{}{
					"theme": map[string]interface{}{},
				},
			},
		},
	}

	assembler := NewDeltaAssembler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert the RequestInfo body to emitter format
			result := assembler.convertOrderedObjectToEmitter(&tt.input.Body)

			// Verify the structure matches expected
			if !compareEmitterObjects(t, result, tt.expected) {
				t.Errorf("Test '%s' failed: structure mismatch", tt.name)
				t.Logf("Expected: %+v", tt.expected)
				t.Logf("Got: %+v", result)
			}
		})
	}
}

// TestLaravelPatternIntegration tests request shape recursion with real Laravel patterns.
func TestLaravelPatternIntegration(t *testing.T) {
	assembler := NewDeltaAssembler()
	
	// Create shape inferencer
	inferencer := infer.NewShapeInferencer(nil, nil, nil, nil)

	// Test Laravel only() patterns
	patterns := []matchers.RequestUsageMatch{
		{
			Methods: []string{"only"},
			Body: map[string]interface{}{
				"users.*.email":    "test@example.com",
				"users.*.name":     "John",
				"filters.date.from": "2024-01-01",
				"filters.date.to":   "2024-12-31",
			},
		},
	}

	// Infer the shape
	requestInfo, err := inferencer.InferRequestShape(patterns)
	if err != nil {
		t.Fatalf("Failed to infer shape: %v", err)
	}

	// Convert to emitter format
	result := assembler.convertOrderedObjectToEmitter(&requestInfo.Body)

	// Verify nested structures are created
	if users, hasUsers := result["users"]; hasUsers {
		if wildcard, hasWildcard := users["*"]; hasWildcard {
			if _, hasEmail := wildcard["email"]; !hasEmail {
				t.Error("Missing nested email property in users.*")
			}
			if _, hasName := wildcard["name"]; !hasName {
				t.Error("Missing nested name property in users.*")
			}
		} else {
			t.Error("Missing wildcard (*) in users structure")
		}
	} else {
		t.Error("Missing users object in result")
	}

	if filters, hasFilters := result["filters"]; hasFilters {
		if date, hasDate := filters["date"]; hasDate {
			if _, hasFrom := date["from"]; !hasFrom {
				t.Error("Missing nested from property in filters.date")
			}
			if _, hasTo := date["to"]; !hasTo {
				t.Error("Missing nested to property in filters.date")
			}
		} else {
			t.Error("Missing date object in filters")
		}
	} else {
		t.Error("Missing filters object in result")
	}
}

// Helper functions for test data creation

func createRequestInfoWithPath(t *testing.T, path string) *infer.RequestInfo {
	parser := infer.NewKeyPathParser(nil)
	obj, err := infer.PathSegmentsToNestedObject(parser, path)
	if err != nil {
		t.Fatalf("Failed to create nested object from path: %v", err)
	}

	return &infer.RequestInfo{
		ContentTypes: []string{"application/json"},
		Body:         *obj,
	}
}

func createRequestInfoWithSimpleProperty(key string) *infer.RequestInfo {
	obj := infer.CreateEmptyOrderedObject()
	obj.AddProperty(key, infer.CreateStringProperty("", ""))

	return &infer.RequestInfo{
		ContentTypes: []string{"application/json"},
		Body:         *obj,
	}
}

func createComplexNestedRequestInfo() *infer.RequestInfo {
	parser := infer.NewKeyPathParser(nil)
	
	// Merge multiple paths
	paths := []string{
		"users.*.profile.bio",
		"users.*.email",
		"settings.theme",
	}

	merged, _ := infer.MergePaths(parser, paths)

	return &infer.RequestInfo{
		ContentTypes: []string{"application/json"},
		Body:         *merged,
	}
}

func compareEmitterObjects(t *testing.T, actual emitter.OrderedObject, expected map[string]interface{}) bool {
	// Compare keys
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
			// actualValue is already of type OrderedObject
			if !compareEmitterObjects(t, actualValue, expectedObj) {
				return false
			}
		}
		// If it's not a nested object, we just check existence (already done above)
	}

	return true
}
