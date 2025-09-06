// Package emitter provides polymorphic pattern emission tests.
//go:build goexperiment.jsonv2

package emitter

import (
	"encoding/json/v2"
	"testing"
)

func TestPolymorphicEmitterIntegration(t *testing.T) {
	tests := []struct {
		name     string
		delta    *Delta
		wantJSON string
	}{
		{
			name: "polymorphic_relation_in_controller",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: false,
					Stats: MetaStats{
						FilesParsed: 1,
						Skipped:     0,
						DurationMs:  100,
					},
				},
				Controllers: []Controller{
					{
						FQCN:   "App\\Http\\Controllers\\CommentController",
						Method: "store",
					},
				},
				Models:      []Model{},
				Polymorphic: []Polymorphic{},
				Broadcast:   []Broadcast{},
			},
			wantJSON: `{"meta":{"partial":false,"stats":{"filesParsed":1,"skipped":0,"durationMs":100}},"controllers":[{"fqcn":"App\\Http\\Controllers\\CommentController","method":"store"}],"models":[],"polymorphic":[],"broadcast":[]}`,
		},
		{
			name: "polymorphic_relation_in_model",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: false,
					Stats: MetaStats{
						FilesParsed: 1,
						Skipped:     0,
						DurationMs:  100,
					},
				},
				Controllers: []Controller{},
				Models: []Model{
					{
						FQCN: "App\\Models\\Post",
					},
				},
				Polymorphic: []Polymorphic{},
				Broadcast:   []Broadcast{},
			},
			wantJSON: `{"meta":{"partial":false,"stats":{"filesParsed":1,"skipped":0,"durationMs":100}},"controllers":[],"models":[{"fqcn":"App\\Models\\Post"}],"polymorphic":[],"broadcast":[]}`,
		},
		{
			name: "global_polymorphic_configuration",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: false,
					Stats: MetaStats{
						FilesParsed: 1,
						Skipped:     0,
						DurationMs:  100,
					},
				},
				Controllers: []Controller{},
				Models:      []Model{},
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
			},
			wantJSON: `{"meta":{"partial":false,"stats":{"filesParsed":1,"skipped":0,"durationMs":100}},"controllers":[],"models":[],"polymorphic":[{"parent":"App\\Models\\Comment","morph":{"key":"commentable","typeColumn":"commentable_type","idColumn":"commentable_id"},"discriminator":{"propertyName":"commentable_type","mapping":{"post":"App\\Models\\Post","video":"App\\Models\\Video"}}}],"broadcast":[]}`,
		},
		{
			name: "depth_truncated_polymorphic",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: false,
					Stats: MetaStats{
						FilesParsed: 1,
						Skipped:     0,
						DurationMs:  100,
					},
				},
				Controllers: []Controller{
					{
						FQCN:   "App\\Http\\Controllers\\TagController",
						Method: "index",
					},
				},
				Models:      []Model{},
				Polymorphic: []Polymorphic{},
				Broadcast:   []Broadcast{},
			},
			wantJSON: `{"meta":{"partial":false,"stats":{"filesParsed":1,"skipped":0,"durationMs":100}},"controllers":[{"fqcn":"App\\Http\\Controllers\\TagController","method":"index"}],"models":[],"polymorphic":[],"broadcast":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := NewJSONEmitter()

			// Test MarshalDeterministic
			gotJSON, err := emitter.MarshalDeterministic(tt.delta)
			if err != nil {
				t.Fatalf("MarshalDeterministic() error = %v", err)
			}

			if string(gotJSON) != tt.wantJSON {
				t.Errorf("MarshalDeterministic() JSON mismatch")
				t.Logf("Got:  %s", string(gotJSON))
				t.Logf("Want: %s", tt.wantJSON)

				// Parse both JSONs to compare structure
				var gotStruct, wantStruct any
				if err := json.Unmarshal(gotJSON, &gotStruct); err != nil {
					t.Fatalf("Failed to unmarshal got JSON: %v", err)
				}
				if err := json.Unmarshal([]byte(tt.wantJSON), &wantStruct); err != nil {
					t.Fatalf("Failed to unmarshal want JSON: %v", err)
				}

				// Pretty print for better comparison
				gotPretty, _ := json.Marshal(gotStruct, json.Deterministic(true), json.Indent("", "  "))
				wantPretty, _ := json.Marshal(wantStruct, json.Deterministic(true), json.Indent("", "  "))
				t.Logf("Got (pretty):\n%s", string(gotPretty))
				t.Logf("Want (pretty):\n%s", string(wantPretty))
			}

			// Test deterministic ordering by marshaling twice
			gotJSON2, err := emitter.MarshalDeterministic(tt.delta)
			if err != nil {
				t.Fatalf("Second MarshalDeterministic() error = %v", err)
			}

			if string(gotJSON) != string(gotJSON2) {
				t.Errorf("MarshalDeterministic() is not deterministic")
				t.Logf("First:  %s", string(gotJSON))
				t.Logf("Second: %s", string(gotJSON2))
			}
		})
	}
}

func TestPolymorphicNormalization(t *testing.T) {
	emitter := NewJSONEmitter()

	// Test polymorphic relation with unsorted data
	relation := &PolymorphicRelation{
		Relation:      "taggable",
		Type:          "morphTo",
		RelatedModels: []string{"Video", "Post", "Article"}, // Unsorted
		Discriminator: &PolymorphicDiscriminator{
			PropertyName: "taggable_type",
			Mapping: map[string]string{
				"video":   "App\\Models\\Video",
				"post":    "App\\Models\\Post",
				"article": "App\\Models\\Article",
			},
		},
	}

	// Normalize the relation
	emitter.normalizePolymorphicRelation(relation)

	// Verify sorting
	expectedModels := []string{"Article", "Post", "Video"}
	if len(relation.RelatedModels) != len(expectedModels) {
		t.Fatalf("RelatedModels length mismatch: got %d, want %d", len(relation.RelatedModels), len(expectedModels))
	}

	for i, expected := range expectedModels {
		if relation.RelatedModels[i] != expected {
			t.Errorf("RelatedModels[%d] = %s, want %s", i, relation.RelatedModels[i], expected)
		}
	}

	// Verify discriminator mapping is preserved (Go JSON marshaler will sort keys)
	if len(relation.Discriminator.Mapping) != 3 {
		t.Errorf("Discriminator mapping length = %d, want 3", len(relation.Discriminator.Mapping))
	}

	expectedMappings := map[string]string{
		"video":   "App\\Models\\Video",
		"post":    "App\\Models\\Post",
		"article": "App\\Models\\Article",
	}
	for key, expected := range expectedMappings {
		if actual, exists := relation.Discriminator.Mapping[key]; !exists || actual != expected {
			t.Errorf("Discriminator mapping[%s] = %s, want %s", key, actual, expected)
		}
	}
}

func TestDeterministicPolymorphicJSON(t *testing.T) {
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 1,
				Skipped:     0,
				DurationMs:  100,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\TestController",
				Method: "test",
			},
		},
		Models: []Model{
			{
				FQCN: "App\\Models\\TestModel",
			},
		},
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}

	emitter := NewJSONEmitter()

	// Marshal multiple times to verify deterministic ordering
	var results []string
	for i := 0; i < 5; i++ {
		jsonBytes, err := emitter.MarshalDeterministic(delta)
		if err != nil {
			t.Fatalf("MarshalDeterministic() error = %v", err)
		}
		results = append(results, string(jsonBytes))
	}

	// All results should be identical
	firstResult := results[0]
	for i, result := range results {
		if result != firstResult {
			t.Errorf("Result %d differs from first result", i)
			t.Logf("First:  %s", firstResult)
			t.Logf("Result: %s", result)
		}
	}

	// Verify polymorphic relations are sorted by relation name within controller
	var deltaStruct map[string]any
	if err := json.Unmarshal([]byte(firstResult), &deltaStruct); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	controllers := deltaStruct["controllers"].([]any)
	if len(controllers) > 0 {
		controller := controllers[0].(map[string]any)
		if polymorphics, exists := controller["polymorphic"]; exists {
			polyArray := polymorphics.([]any)
			if len(polyArray) >= 2 {
				// Should be sorted: alpha, zeta
				firstPoly := polyArray[0].(map[string]any)
				secondPoly := polyArray[1].(map[string]any)

				if firstPoly["relation"] != "alpha" {
					t.Errorf("First polymorphic relation should be 'alpha', got %v", firstPoly["relation"])
				}
				if secondPoly["relation"] != "zeta" {
					t.Errorf("Second polymorphic relation should be 'zeta', got %v", secondPoly["relation"])
				}
			}
		}
	}
}

// Helper functions for test data
func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}
