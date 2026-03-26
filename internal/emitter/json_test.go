//go:build goexperiment.jsonv2

package emitter

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
)

func TestJSONEmitter_EmitStub(t *testing.T) {
	emitter := NewJSONEmitter()

	delta, err := emitter.EmitStub()
	if err != nil {
		t.Fatalf("EmitStub() error = %v", err)
	}

	if delta == nil {
		t.Fatal("EmitStub() returned nil delta")
	}

	// Verify stub structure matches schema requirements
	if delta.Meta.Partial != false {
		t.Error("stub should have partial=false")
	}

	if delta.Meta.Stats.FilesParsed != 0 {
		t.Error("stub should have filesParsed=0")
	}

	if delta.Meta.Stats.Skipped != 0 {
		t.Error("stub should have skipped=0")
	}

	if delta.Meta.Stats.DurationMs != 0 {
		t.Error("stub should have durationMs=0")
	}

	// Verify all collections are empty but not nil
	if delta.Controllers == nil || len(delta.Controllers) != 0 {
		t.Error("stub should have empty controllers array")
	}

	if delta.Models == nil || len(delta.Models) != 0 {
		t.Error("stub should have empty models array")
	}

	if delta.Resources == nil || len(delta.Resources) != 0 {
		t.Error("stub should have empty resources array")
	}

	if delta.Polymorphic == nil || len(delta.Polymorphic) != 0 {
		t.Error("stub should have empty polymorphic array")
	}

	if delta.Broadcast == nil || len(delta.Broadcast) != 0 {
		t.Error("stub should have empty broadcast array")
	}
}

func TestJSONEmitter_MarshalDeterministic(t *testing.T) {
	emitter := NewJSONEmitter()

	tests := []struct {
		name     string
		delta    *Delta
		wantErr  bool
		validate func(*testing.T, []byte)
	}{
		{
			name:    "nil delta",
			delta:   nil,
			wantErr: true,
		},
		{
			name: "empty delta",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: false,
					Stats: MetaStats{
						FilesParsed: 0,
						Skipped:     0,
						DurationMs:  0,
					},
				},
				Controllers: []Controller{},
				Models:      []Model{},
				Resources:   []ResourceDef{},
				Polymorphic: []Polymorphic{},
				Broadcast:   []Broadcast{},
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Should be valid JSON
				var result map[string]any
				if err := json.Unmarshal(data, &result); err != nil {
					t.Errorf("invalid JSON: %v", err)
				}

				// Should contain meta
				if _, ok := result["meta"]; !ok {
					t.Error("missing meta field")
				}

				// Should contain arrays even if empty
				if _, ok := result["controllers"]; !ok {
					t.Error("missing controllers field")
				}
			},
		},
		{
			name: "complex delta with sorting",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: false,
					Stats: MetaStats{
						FilesParsed: 10,
						Skipped:     2,
						DurationMs:  500,
					},
				},
				Controllers: []Controller{
					// Note: intentionally unsorted to test sorting
					{
						FQCN:   "ZController", // Should be sorted last
						Method: "zzMethod",
						Request: &RequestInfo{
							ContentTypes: []string{"multipart/form-data", "application/json"}, // Test content type sorting
						},
						Responses: []Response{
							{
								Kind:        "json_object",
								Status:      responseIntPtr(202),
								ContentType: "application/json",
								BodySchema: &ResourceSchemaNode{
									Type: "object",
									Properties: map[string]ResourceSchemaNode{
										"zeta":  {Type: "string"},
										"alpha": {Type: "string"},
									},
									Required: []string{"zeta", "alpha"},
								},
								Source: "response()->json",
								Via:    "response()->json",
							},
							{
								Kind:        "no_content",
								Status:      responseIntPtr(204),
								Explicit:    responseBoolPtr(true),
								ContentType: "",
								Source:      "response()->noContent",
								Via:         "response()->noContent",
							},
						},
						Resources: []Resource{
							{Class: "ZResource", Collection: true},
							{Class: "AResource", Collection: false}, // Should be sorted first
						},
						ScopesUsed: []ScopeUsed{
							{On: "ZModel", Name: "zScope", Args: []string{"z", "a"}}, // Test arg sorting
							{On: "AModel", Name: "aScope"},                           // Should be sorted first
						},
					},
					{
						FQCN:   "AController", // Should be sorted first
						Method: "aMethod",
					},
				},
				Models: []Model{
					{
						FQCN: "ZModel", // Should be sorted last
						WithPivot: []PivotInfo{
							{Relation: "zRelation", Columns: []string{"z_col", "a_col"}}, // Test column sorting
							{Relation: "aRelation", Columns: []string{"col"}},            // Should be sorted first
						},
						Attributes: []Attribute{
							{Name: "z_attr", Via: "Attribute::make"},
							{Name: "a_attr", Via: "Attribute::make"}, // Should be sorted first
						},
					},
					{
						FQCN: "AModel", // Should be sorted first
					},
				},
				Resources: []ResourceDef{
					{
						FQCN:  "ZResource",
						Class: "ZResource",
						Schema: ResourceSchemaNode{
							Type: "object",
							Properties: map[string]ResourceSchemaNode{
								"zeta":  {Type: "string"},
								"alpha": {Type: "string"},
							},
							Required: []string{"zeta", "alpha"},
						},
					},
					{
						FQCN:  "AResource",
						Class: "AResource",
						Schema: ResourceSchemaNode{
							Type: "object",
							Properties: map[string]ResourceSchemaNode{
								"id": {Type: "integer"},
							},
							Required: []string{"id"},
						},
					},
				},
				Broadcast: []Broadcast{
					{
						Channel:    "z.channel",
						Params:     []string{"z", "a"}, // Test param sorting
						Visibility: "private",
					},
					{
						Channel:    "a.channel", // Should be sorted first
						Params:     []string{"param"},
						Visibility: "public",
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Parse JSON to verify structure
				var result map[string]any
				if err := json.Unmarshal(data, &result); err != nil {
					t.Errorf("invalid JSON: %v", err)
					return
				}

				// Verify controllers are sorted
				controllers, ok := result["controllers"].([]any)
				if !ok || len(controllers) != 2 {
					t.Error("expected 2 controllers")
					return
				}

				firstController := controllers[0].(map[string]any)
				if firstController["fqcn"] != "AController" {
					t.Errorf("first controller should be AController, got %v", firstController["fqcn"])
				}

				secondController := controllers[1].(map[string]any)
				if secondController["fqcn"] != "ZController" {
					t.Errorf("second controller should be ZController, got %v", secondController["fqcn"])
				}

				// Verify content types are sorted
				if request, ok := secondController["request"].(map[string]any); ok {
					if contentTypes, ok := request["contentTypes"].([]any); ok {
						if len(contentTypes) >= 2 {
							first := contentTypes[0].(string)
							second := contentTypes[1].(string)
							if first > second {
								t.Errorf("content types not sorted: %s > %s", first, second)
							}
						}
					}
				}

				// Verify models are sorted
				models, ok := result["models"].([]any)
				if !ok || len(models) != 2 {
					t.Error("expected 2 models")
					return
				}

				firstModel := models[0].(map[string]any)
				if firstModel["fqcn"] != "AModel" {
					t.Errorf("first model should be AModel, got %v", firstModel["fqcn"])
				}

				resourceDefs, ok := result["resources"].([]any)
				if !ok || len(resourceDefs) != 2 {
					t.Error("expected 2 resources")
					return
				}
				firstResource := resourceDefs[0].(map[string]any)
				if firstResource["fqcn"] != "AResource" {
					t.Errorf("first resource should be AResource, got %v", firstResource["fqcn"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := emitter.MarshalDeterministic(tt.delta)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("MarshalDeterministic() error = %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, data)
			}
		})
	}
}

func TestJSONEmitter_DeterministicOutput(t *testing.T) {
	emitter := NewJSONEmitter()

	// Create a complex delta with multiple unsorted elements
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 25,
				Skipped:     5,
				DurationMs:  1200,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "User\\ProfileController",
				Method: "update",
				Request: &RequestInfo{
					ContentTypes: []string{"multipart/form-data", "application/json"},
				},
				Responses: []Response{
					{
						Kind:        "json_array",
						Status:      responseIntPtr(202),
						ContentType: "application/json",
						BodySchema: &ResourceSchemaNode{
							Type: "array",
							Items: &ResourceSchemaNode{
								Type: "object",
								Properties: map[string]ResourceSchemaNode{
									"id": {Type: "integer"},
								},
								Required: []string{"id"},
							},
						},
						Source: "response()",
						Via:    "response()",
					},
				},
				Resources: []Resource{
					{Class: "UserResource", Collection: false},
					{Class: "ProfileResource", Collection: true},
				},
				ScopesUsed: []ScopeUsed{
					{On: "User", Name: "verified", Args: []string{"true", "active"}},
					{On: "Profile", Name: "visible"},
				},
			},
			{
				FQCN:   "Admin\\DashboardController",
				Method: "index",
			},
		},
		Models: []Model{
			{
				FQCN: "App\\Models\\User",
				WithPivot: []PivotInfo{
					{Relation: "roles", Columns: []string{"granted_at", "level"}},
				},
				Attributes: []Attribute{
					{Name: "full_name", Via: "Attribute::make"},
					{Name: "display_name", Via: "Attribute::make"},
				},
			},
		},
		Broadcast: []Broadcast{
			{
				Channel:    "user.{id}.notifications",
				Params:     []string{"id"},
				Visibility: "private",
			},
		},
	}

	// Marshal the same delta multiple times
	const iterations = 5
	results := make([][]byte, iterations)
	hashes := make([]string, iterations)

	for i := 0; i < iterations; i++ {
		data, err := emitter.MarshalDeterministic(delta)
		if err != nil {
			t.Fatalf("iteration %d: MarshalDeterministic() error = %v", i, err)
		}

		results[i] = data
		hash := sha256.Sum256(data)
		hashes[i] = fmt.Sprintf("%x", hash)
	}

	// All results should be identical
	for i := 1; i < iterations; i++ {
		if !bytes.Equal(results[0], results[i]) {
			t.Errorf("iteration %d produced different result than iteration 0", i)
			t.Logf("Iteration 0: %s", string(results[0]))
			t.Logf("Iteration %d: %s", i, string(results[i]))
		}

		if hashes[0] != hashes[i] {
			t.Errorf("iteration %d produced different hash: %s != %s", i, hashes[0], hashes[i])
		}
	}

	t.Logf("Deterministic test passed: %d iterations produced identical hash %s", iterations, hashes[0])
}

func TestJSONEmitter_WriteJSON(t *testing.T) {
	emitter := NewJSONEmitter()

	tests := []struct {
		name    string
		delta   *Delta
		wantErr bool
		verify  func(*testing.T, []byte)
	}{
		{
			name:    "nil delta",
			delta:   nil,
			wantErr: true,
		},
		{
			name: "stub delta",
			delta: func() *Delta {
				d, _ := emitter.EmitStub()
				return d
			}(),
			wantErr: false,
			verify: func(t *testing.T, data []byte) {
				var result map[string]any
				if err := json.Unmarshal(data, &result); err != nil {
					t.Errorf("invalid JSON: %v", err)
				}

				// Verify required fields from schema
				if meta, ok := result["meta"]; ok {
					if metaObj, ok := meta.(map[string]any); ok {
						if _, ok := metaObj["partial"]; !ok {
							t.Error("missing meta.partial field")
						}
						if _, ok := metaObj["stats"]; !ok {
							t.Error("missing meta.stats field")
						}
					}
				} else {
					t.Error("missing meta field")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := emitter.WriteJSON(&buf, tt.delta)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("WriteJSON() error = %v", err)
				return
			}

			data := buf.Bytes()
			if len(data) == 0 {
				t.Error("WriteJSON() produced empty output")
				return
			}

			if tt.verify != nil {
				tt.verify(t, data)
			}
		})
	}
}

func TestJSONEmitter_SchemaCompliance(t *testing.T) {
	emitter := NewJSONEmitter()

	// Test that stub delta produces schema-compliant JSON
	stub, err := emitter.EmitStub()
	if err != nil {
		t.Fatalf("EmitStub() error = %v", err)
	}

	data, err := emitter.MarshalDeterministic(stub)
	if err != nil {
		t.Fatalf("MarshalDeterministic() error = %v", err)
	}

	// Parse and verify structure matches expected schema
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check required top-level fields
	requiredFields := []string{"meta", "controllers", "models", "polymorphic", "broadcast"}
	for _, field := range requiredFields {
		if _, exists := result[field]; !exists {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Verify meta structure
	if meta, ok := result["meta"].(map[string]any); ok {
		if _, ok := meta["partial"]; !ok {
			t.Error("missing meta.partial")
		}
		if _, ok := meta["stats"]; !ok {
			t.Error("missing meta.stats")
		}

		if stats, ok := meta["stats"].(map[string]any); ok {
			statsFields := []string{"filesParsed", "skipped", "durationMs"}
			for _, field := range statsFields {
				if _, ok := stats[field]; !ok {
					t.Errorf("missing meta.stats.%s", field)
				}
			}
		}
	} else {
		t.Error("meta is not an object")
	}

	// Verify arrays are present (even if empty)
	arrayFields := []string{"controllers", "models", "polymorphic", "broadcast"}
	for _, field := range arrayFields {
		if arr, ok := result[field].([]any); !ok {
			t.Errorf("%s is not an array", field)
		} else if arr == nil {
			t.Errorf("%s array is nil", field)
		}
	}
}

func TestJSONEmitter_SortingBehavior(t *testing.T) {
	emitter := NewJSONEmitter()

	// Test data specifically designed to verify sorting
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats:   MetaStats{FilesParsed: 1, Skipped: 0, DurationMs: 100},
		},
		Controllers: []Controller{
			{
				FQCN:   "Z\\Controller", // Should be last
				Method: "zMethod",
				Request: &RequestInfo{
					ContentTypes: []string{"multipart/form-data", "application/json"}, // Should be sorted
				},
			},
			{
				FQCN:   "A\\Controller", // Should be first
				Method: "aMethod",
			},
		},
	}

	data, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		t.Fatalf("MarshalDeterministic() error = %v", err)
	}

	// Parse result and check sorting
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	controllers := result["controllers"].([]any)
	if len(controllers) != 2 {
		t.Fatal("expected 2 controllers")
	}

	// First should be A\Controller
	first := controllers[0].(map[string]any)
	if first["fqcn"] != "A\\Controller" {
		t.Errorf("first controller FQCN = %v, want A\\Controller", first["fqcn"])
	}

	// Second should be Z\Controller
	second := controllers[1].(map[string]any)
	if second["fqcn"] != "Z\\Controller" {
		t.Errorf("second controller FQCN = %v, want Z\\Controller", second["fqcn"])
	}

	// Check content types are sorted
	if request, ok := second["request"].(map[string]any); ok {
		if contentTypes, ok := request["contentTypes"].([]any); ok && len(contentTypes) >= 2 {
			first := contentTypes[0].(string)
			second := contentTypes[1].(string)
			if first != "application/json" || second != "multipart/form-data" {
				t.Errorf("content types not properly sorted: [%s, %s]", first, second)
			}
		}
	}

	if responses, ok := second["responses"].([]any); ok && len(responses) == 1 {
		resp := responses[0].(map[string]any)
		if resp["kind"] != "json_array" {
			t.Errorf("response kind = %v, want json_array", resp["kind"])
		}
		if resp["status"].(float64) != 202 {
			t.Errorf("response status = %v, want 202", resp["status"])
		}
	}
}

func TestJSONEmitter_EmptyCollections(t *testing.T) {
	emitter := NewJSONEmitter()

	// Test that empty collections are handled correctly
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats:   MetaStats{FilesParsed: 0, Skipped: 0, DurationMs: 0},
		},
		Controllers: []Controller{}, // Empty slice
		Models:      []Model{},      // Empty slice
		Polymorphic: nil,            // nil slice - should be handled
		Broadcast:   []Broadcast{},  // Empty slice
	}

	data, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		t.Fatalf("MarshalDeterministic() error = %v", err)
	}

	// Verify the JSON contains empty arrays, not null
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"controllers":[]`) {
		t.Error("controllers should be empty array, not null")
	}
	if !strings.Contains(jsonStr, `"models":[]`) {
		t.Error("models should be empty array, not null")
	}
	if !strings.Contains(jsonStr, `"broadcast":[]`) {
		t.Error("broadcast should be empty array, not null")
	}

	// Polymorphic might be null since it's nil - verify it doesn't break parsing
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("failed to parse JSON with nil slice: %v", err)
	}
}

func TestJSONEmitter_ResponseHeadersDeterministicOrder(t *testing.T) {
	emitter := NewJSONEmitter()

	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats:   MetaStats{FilesParsed: 1, Skipped: 0, DurationMs: 0},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\ReportController",
				Method: "download",
				Responses: []Response{
					{
						Kind:        "download",
						Status:      responseIntPtr(200),
						ContentType: "application/octet-stream",
						Headers: map[string]string{
							"Content-Disposition": `attachment; filename="z.zip"`,
						},
						Source: "response()->download",
						Via:    "response()->download",
					},
					{
						Kind:        "download",
						Status:      responseIntPtr(200),
						ContentType: "application/octet-stream",
						Headers: map[string]string{
							"X-Trace":            "1",
							"Content-Disposition": `attachment; filename="a.zip"`,
						},
						Source: "response()->download",
						Via:    "response()->download",
					},
				},
			},
		},
		Models:      []Model{},
		Resources:   []ResourceDef{},
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}

	data, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		t.Fatalf("MarshalDeterministic() error = %v", err)
	}

	jsonStr := string(data)
	first := strings.Index(jsonStr, `filename=\"a.zip\"`)
	second := strings.Index(jsonStr, `filename=\"z.zip\"`)
	if first == -1 || second == -1 {
		t.Fatalf("response headers missing from JSON: %s", jsonStr)
	}
	if first >= second {
		t.Fatalf("responses not ordered deterministically by headers: %s", jsonStr)
	}
}

func responseIntPtr(v int) *int {
	return &v
}

func responseBoolPtr(v bool) *bool {
	return &v
}
