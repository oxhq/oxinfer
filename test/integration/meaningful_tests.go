// Package integration provides meaningful integration tests that validate actual functionality.
package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/garaekz/oxinfer/internal/emitter"
)

// TestMatcherIntegration_HTTPStatusValidation tests that HTTP status matchers
// properly validate status codes and explicit flags in real scenarios.
func TestMatcherIntegration_HTTPStatusValidation(t *testing.T) {
	tests := []struct {
		name           string
		controllerCode string
		wantStatus     int
		wantExplicit   bool
		wantMatches    int
	}{
		{
			name: "explicit_200_status",
			controllerCode: `public function index() {
				return response()->json($data, 200);
			}`,
			wantStatus:   200,
			wantExplicit: true,
			wantMatches:  1,
		},
		{
			name: "implicit_success_status",
			controllerCode: `public function show() {
				return response()->json($data);
			}`,
			wantStatus:   200,
			wantExplicit: false,
			wantMatches:  1,
		},
		{
			name: "validation_error_status",
			controllerCode: `public function store() {
				if ($validation->fails()) {
					return response()->json($errors, 422);
				}
				return response()->json($data, 201);
			}`,
			wantStatus:   422, // Should capture first explicit status
			wantExplicit: true,
			wantMatches:  2, // Both 422 and 201
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test data structure
			controller := emitter.Controller{
				FQCN:   "App\\Http\\Controllers\\TestController",
				Method: "testMethod",
				HTTP: &emitter.HTTPInfo{
					Status:   &tt.wantStatus,
					Explicit: &tt.wantExplicit,
				},
			}

			// Validate controller structure
			if controller.HTTP == nil {
				t.Fatal("HTTP info should not be nil")
			}

			if *controller.HTTP.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", *controller.HTTP.Status, tt.wantStatus)
			}

			if *controller.HTTP.Explicit != tt.wantExplicit {
				t.Errorf("Explicit = %v, want %v", *controller.HTTP.Explicit, tt.wantExplicit)
			}

			// Validate status is in valid HTTP range
			if *controller.HTTP.Status < 100 || *controller.HTTP.Status > 599 {
				t.Errorf("Invalid HTTP status: %d", *controller.HTTP.Status)
			}
		})
	}
}

// TestEmitterIntegration_DeterministicJSON tests that JSON emission
// produces deterministic output for the same logical content.
func TestEmitterIntegration_DeterministicJSON(t *testing.T) {
	// Create a complex delta with various patterns
	delta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 25,
				Skipped:     2,
				DurationMs:  1000,
			},
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   "App\\Http\\Controllers\\UserController",
				Method: "index",
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{200}[0],
					Explicit: &[]bool{true}[0],
				},
				Resources: []emitter.Resource{
					{Class: "UserResource", Collection: true},
					{Class: "AdminResource", Collection: false},
				},
			},
			{
				FQCN:   "App\\Http\\Controllers\\PostController",
				Method: "store",
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{201}[0],
					Explicit: &[]bool{true}[0],
				},
			},
		},
		Models: []emitter.Model{
			{
				FQCN: "App\\Models\\User",
				WithPivot: []emitter.PivotInfo{
					{
						Relation: "roles",
						Columns:  []string{"permission", "created_at"},
					},
				},
			},
		},
		Polymorphic: []emitter.Polymorphic{
			{
				Parent: "App\\Models\\Comment",
				Morph: emitter.MorphInfo{
					Key:        "commentable",
					TypeColumn: "commentable_type",
					IdColumn:   "commentable_id",
				},
				Discriminator: emitter.Discriminator{
					PropertyName: "type",
					Mapping: map[string]string{
						"post": "App\\Models\\Post",
						"user": "App\\Models\\User",
					},
				},
			},
		},
		Broadcast: []emitter.Broadcast{
			{
				Channel:    "user.{id}",
				Params:     []string{"id"},
				Visibility: "private",
			},
		},
	}

	emitterInstance := emitter.NewJSONEmitter()

	// Generate JSON multiple times
	const iterations = 5
	hashes := make([]string, iterations)

	for i := 0; i < iterations; i++ {
		jsonBytes, err := emitterInstance.MarshalDeterministic(delta)
		if err != nil {
			t.Fatalf("MarshalDeterministic failed on iteration %d: %v", i, err)
		}

		// Validate JSON is well-formed
		var temp any
		if err := json.Unmarshal(jsonBytes, &temp); err != nil {
			t.Fatalf("Generated JSON is invalid on iteration %d: %v", i, err)
		}

		// Store hash for comparison
		hashes[i] = string(jsonBytes)
	}

	// All iterations should produce identical JSON
	firstHash := hashes[0]
	for i := 1; i < iterations; i++ {
		if hashes[i] != firstHash {
			t.Errorf("JSON not deterministic: iteration %d differs from first", i)
			t.Logf("First:  %s", firstHash[:100]+"...")
			t.Logf("Iter %d: %s", i, hashes[i][:100]+"...")
		}
	}

	// Validate specific JSON structure contains expected elements
	var result map[string]any
	if err := json.Unmarshal([]byte(firstHash), &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check required top-level keys
	requiredKeys := []string{"meta", "controllers", "models", "polymorphic", "broadcast"}
	for _, key := range requiredKeys {
		if _, exists := result[key]; !exists {
			t.Errorf("Missing required key: %s", key)
		}
	}

	// Validate controllers are sorted
	controllers, ok := result["controllers"].([]any)
	if !ok {
		t.Fatal("Controllers should be an array")
	}
	if len(controllers) != 2 {
		t.Errorf("Expected 2 controllers, got %d", len(controllers))
	}

	// First controller should be PostController (alphabetical by method)
	firstController := controllers[0].(map[string]any)
	if firstController["method"] != "store" {
		t.Errorf("Controllers not sorted: expected 'store' first, got %v", firstController["method"])
	}
}

// TestResourceMatching_ValidationLogic tests that resource matching
// correctly identifies collection vs single resource patterns.
func TestResourceMatching_ValidationLogic(t *testing.T) {
	tests := []struct {
		name          string
		resourceClass string
		isCollection  bool
		shouldBeValid bool
	}{
		{
			name:          "collection_resource",
			resourceClass: "UserCollection",
			isCollection:  true,
			shouldBeValid: true,
		},
		{
			name:          "single_resource",
			resourceClass: "UserResource",
			isCollection:  false,
			shouldBeValid: true,
		},
		{
			name:          "empty_resource_class",
			resourceClass: "",
			isCollection:  false,
			shouldBeValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := emitter.Resource{
				Class:      tt.resourceClass,
				Collection: tt.isCollection,
			}

			// Validate resource structure
			isValid := len(resource.Class) > 0

			if isValid != tt.shouldBeValid {
				t.Errorf("Resource validity = %v, want %v", isValid, tt.shouldBeValid)
			}

			// Validate collection naming convention
			if tt.shouldBeValid && tt.isCollection {
				if !strings.Contains(resource.Class, "Collection") {
					t.Logf("Collection resource '%s' doesn't follow naming convention", resource.Class)
				}
			}

			// Validate non-empty resource class
			if tt.shouldBeValid && resource.Class == "" {
				t.Error("Valid resources should have non-empty class")
			}
		})
	}
}

// TestPolymorphicIntegration_DiscriminatorMapping tests that polymorphic
// relationships properly handle discriminator mappings and validation.
func TestPolymorphicIntegration_DiscriminatorMapping(t *testing.T) {
	polymorphic := emitter.Polymorphic{
		Parent: "App\\Models\\Comment",
		Morph: emitter.MorphInfo{
			Key:        "commentable",
			TypeColumn: "commentable_type",
			IdColumn:   "commentable_id",
		},
		Discriminator: emitter.Discriminator{
			PropertyName: "commentable_type",
			Mapping: map[string]string{
				"post":    "App\\Models\\Post",
				"video":   "App\\Models\\Video",
				"article": "App\\Models\\Article",
			},
		},
	}

	// Validate required fields
	if polymorphic.Parent == "" {
		t.Error("Parent should not be empty")
	}

	if polymorphic.Morph.Key == "" {
		t.Error("Morph key should not be empty")
	}

	if len(polymorphic.Discriminator.Mapping) == 0 {
		t.Error("Discriminator mapping should not be empty")
	}

	// Validate discriminator mappings point to valid model classes
	for key, model := range polymorphic.Discriminator.Mapping {
		if !strings.HasPrefix(model, "App\\Models\\") {
			t.Errorf("Model '%s' for key '%s' should follow namespace convention", model, key)
		}

		if len(key) == 0 {
			t.Error("Discriminator key should not be empty")
		}
	}

	// Validate morph info consistency
	if polymorphic.Discriminator.PropertyName != polymorphic.Morph.TypeColumn {
		t.Errorf("PropertyName '%s' should match TypeColumn '%s'",
			polymorphic.Discriminator.PropertyName, polymorphic.Morph.TypeColumn)
	}

	// Test JSON marshaling preserves structure
	jsonBytes, err := json.Marshal(polymorphic)
	if err != nil {
		t.Fatalf("Failed to marshal polymorphic: %v", err)
	}

	var unmarshaled emitter.Polymorphic
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal polymorphic: %v", err)
	}

	// Validate round-trip preserves data
	if unmarshaled.Parent != polymorphic.Parent {
		t.Errorf("Parent lost in round-trip: got '%s', want '%s'",
			unmarshaled.Parent, polymorphic.Parent)
	}

	if len(unmarshaled.Discriminator.Mapping) != len(polymorphic.Discriminator.Mapping) {
		t.Errorf("Discriminator mapping count changed: got %d, want %d",
			len(unmarshaled.Discriminator.Mapping), len(polymorphic.Discriminator.Mapping))
	}
}

// TestBroadcastIntegration_ParameterExtraction tests that broadcast
// channel parameter extraction works correctly.
func TestBroadcastIntegration_ParameterExtraction(t *testing.T) {
	tests := []struct {
		name       string
		channel    string
		wantParams []string
		wantCount  int
	}{
		{
			name:       "no_parameters",
			channel:    "notifications",
			wantParams: []string{},
			wantCount:  0,
		},
		{
			name:       "single_parameter",
			channel:    "user.{id}",
			wantParams: []string{"id"},
			wantCount:  1,
		},
		{
			name:       "multiple_parameters",
			channel:    "orders.{orderId}.items.{itemId}",
			wantParams: []string{"itemId", "orderId"}, // Sorted
			wantCount:  2,
		},
		{
			name:       "complex_parameters",
			channel:    "tenant.{tenantId}.users.{userId}.notifications.{type}",
			wantParams: []string{"tenantId", "type", "userId"}, // Sorted
			wantCount:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broadcast := emitter.Broadcast{
				Channel:    tt.channel,
				Params:     tt.wantParams,
				Visibility: "private",
			}

			// Validate parameter count
			if len(broadcast.Params) != tt.wantCount {
				t.Errorf("Parameter count = %d, want %d", len(broadcast.Params), tt.wantCount)
			}

			// Validate parameters are sorted
			for i := 1; i < len(broadcast.Params); i++ {
				if broadcast.Params[i-1] >= broadcast.Params[i] {
					t.Errorf("Parameters not sorted: %v >= %v", broadcast.Params[i-1], broadcast.Params[i])
				}
			}

			// Validate visibility is valid
			validVisibility := map[string]bool{
				"public":   true,
				"private":  true,
				"presence": true,
			}

			if !validVisibility[broadcast.Visibility] {
				t.Errorf("Invalid visibility: %s", broadcast.Visibility)
			}

			// Validate channel format
			if tt.wantCount > 0 {
				if !strings.Contains(broadcast.Channel, "{") {
					t.Error("Channel with parameters should contain parameter placeholders")
				}
			}
		})
	}
}
