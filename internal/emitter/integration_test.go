// Package emitter provides integration tests for polymorphic pattern emission.
package emitter

import (
	"encoding/json"
	"testing"
)

func TestPolymorphicIntegrationEndToEnd(t *testing.T) {
	// Test complete polymorphic integration with all components
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 3,
				Skipped:     0,
				DurationMs:  250,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\CommentController",
				Method: "store",
				HTTP: &HTTPInfo{
					Status:   intPtr(201),
					Explicit: boolPtr(true),
				},
				Resources: []Resource{
					{Class: "CommentResource", Collection: false},
				},
			},
		},
		Models: []Model{
			{
				FQCN: "App\\Models\\Post",
			},
		},
		Polymorphic: []Polymorphic{
			{
				Parent: "App\\Models\\Comment",
				Morph: MorphInfo{
					Key:        "commentable",
					TypeColumn: "commentable_type",
					IdColumn:   "commentable_id",
				},
				Discriminator: Discriminator{
					PropertyName: "commentable_type",
					Mapping: map[string]string{
						"post":  "App\\Models\\Post",
						"video": "App\\Models\\Video",
					},
				},
			},
		},
		Broadcast: []Broadcast{},
	}

	emitter := NewJSONEmitter()

	// Test MarshalDeterministic produces valid JSON
	jsonBytes, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		t.Fatalf("MarshalDeterministic failed: %v", err)
	}

	// Verify JSON is valid and can be unmarshaled
	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Generated JSON is not valid: %v", err)
	}

	// Verify structure includes polymorphic data at top level
	polymorphics, ok := unmarshaled["polymorphic"].([]interface{})
	if !ok || len(polymorphics) == 0 {
		t.Fatal("Polymorphic relations not found at top level")
	}

	// Verify polymorphic structure matches plan.md schema
	polyRelation := polymorphics[0].(map[string]interface{})
	if polyRelation["parent"] == nil {
		t.Error("Expected 'parent' field in polymorphic relation")
	}
	
	// Verify morph structure
	morph, ok := polyRelation["morph"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'morph' field in polymorphic relation")
	}
	if morph["key"] == nil {
		t.Error("Expected 'key' field in morph")
	}

	// Verify discriminator structure
	discriminator, ok := polyRelation["discriminator"].(map[string]interface{})
	if !ok {
		t.Fatal("Discriminator not found in polymorphic relation")
	}
	if discriminator["propertyName"] != "commentable_type" {
		t.Errorf("Expected propertyName 'commentable_type', got %v", discriminator["propertyName"])
	}

	// Verify models section (models no longer have polymorphic fields directly)
	models, ok := unmarshaled["models"].([]interface{})
	if !ok || len(models) == 0 {
		t.Fatal("Models not found in JSON output")
	}

	model := models[0].(map[string]interface{})
	if model["fqcn"] == nil {
		t.Error("Expected 'fqcn' field in model")
	}

	// Verify global polymorphic section
	globalPolymorphics, ok := unmarshaled["polymorphic"].([]interface{})
	if !ok || len(globalPolymorphics) == 0 {
		t.Fatal("Global polymorphic configurations not found")
	}

	globalPoly := globalPolymorphics[0].(map[string]interface{})
	if globalPoly["parent"] != "App\\Models\\Comment" {
		t.Errorf("Expected parent 'App\\Models\\Comment', got %v", globalPoly["parent"])
	}

	// Test deterministic output - multiple runs should be identical
	for i := 0; i < 5; i++ {
		jsonBytes2, err := emitter.MarshalDeterministic(delta)
		if err != nil {
			t.Fatalf("MarshalDeterministic run %d failed: %v", i, err)
		}
		if string(jsonBytes) != string(jsonBytes2) {
			t.Errorf("Output not deterministic on run %d", i)
		}
	}

	t.Logf("Integration test passed - JSON output length: %d bytes", len(jsonBytes))
}

func TestPolymorphicSchemaValidation(t *testing.T) {
	// Test that polymorphic structures match expected schema
	relation := PolymorphicRelation{
		Relation:  "morphable",
		Type:      "morphTo",
		MorphType: "morphable_type",
		MorphId:   "morphable_id",
		Model:     stringPtr("App\\Models\\Target"),
		Discriminator: &PolymorphicDiscriminator{
			PropertyName: "morphable_type",
			Mapping:      map[string]string{"key": "value"},
			Source:       "explicit",
			IsExplicit:   true,
			DefaultType:  stringPtr("default"),
		},
		RelatedModels:    []string{"Model1", "Model2"},
		DepthTruncated:   boolPtr(true),
		MaxDepth:         intPtr(3),
	}

	// Marshal to JSON and verify structure
	jsonBytes, err := json.Marshal(relation)
	if err != nil {
		t.Fatalf("Failed to marshal PolymorphicRelation: %v", err)
	}

	// Unmarshal and verify all fields are preserved
	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Check required fields
	requiredFields := []string{"relation", "type"}
	for _, field := range requiredFields {
		if _, exists := unmarshaled[field]; !exists {
			t.Errorf("Required field '%s' missing from JSON", field)
		}
	}

	// Check optional fields are present when set
	optionalFields := []string{"morphType", "morphId", "model", "discriminator", "relatedModels", "depthTruncated", "maxDepth"}
	for _, field := range optionalFields {
		if _, exists := unmarshaled[field]; !exists {
			t.Errorf("Expected optional field '%s' missing from JSON", field)
		}
	}

	// Verify discriminator structure
	discriminator, ok := unmarshaled["discriminator"].(map[string]interface{})
	if !ok {
		t.Fatal("Discriminator is not a proper object")
	}

	discriminatorFields := []string{"propertyName", "mapping", "source", "isExplicit", "defaultType"}
	for _, field := range discriminatorFields {
		if _, exists := discriminator[field]; !exists {
			t.Errorf("Discriminator field '%s' missing", field)
		}
	}
}

// Helper functions are defined in polymorphic_test.go