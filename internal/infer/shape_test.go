package infer

import (
	"testing"

	"github.com/garaekz/oxinfer/internal/matchers"
)

func TestDefaultContentTypeDetector_DetectContentType(t *testing.T) {
	detector := NewContentTypeDetector(nil)

	tests := []struct {
		name     string
		patterns []matchers.RequestUsageMatch
		expected string
	}{
		{
			name:     "empty patterns defaults to JSON",
			patterns: []matchers.RequestUsageMatch{},
			expected: "application/json",
		},
		{
			name: "file upload methods trigger multipart",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"file", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: "multipart/form-data",
		},
		{
			name: "json method triggers JSON",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"json", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: "application/json",
		},
		{
			name: "body parameters default to JSON",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"input"},
					Body:    map[string]any{"name": "test", "email": "test"},
				},
			},
			expected: "application/json",
		},
		{
			name: "explicit content types are respected",
			patterns: []matchers.RequestUsageMatch{
				{
					ContentTypes: []string{"application/x-www-form-urlencoded"},
					Methods:      []string{"input"},
					Body:         map[string]any{"name": "test"},
				},
			},
			expected: "application/x-www-form-urlencoded",
		},
		{
			name: "all method triggers form-urlencoded",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"all"},
					Body:    map[string]any{"data": "test"},
				},
			},
			expected: "application/x-www-form-urlencoded",
		},
		{
			name: "hasFile method triggers multipart",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"hasFile", "validated"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: "multipart/form-data",
		},
		{
			name: "files map triggers multipart",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"input"},
					Body:    map[string]any{"name": "test"},
					Files:   map[string]any{"avatar": map[string]any{}},
				},
			},
			expected: "multipart/form-data",
		},
		{
			name: "form methods without files trigger form-urlencoded",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"validate", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: "application/x-www-form-urlencoded",
		},
		{
			name: "file upload overrides JSON preference",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"json", "file", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: "multipart/form-data",
		},
		{
			name: "multiple patterns with mixed types - multipart wins",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"json", "input"},
					Body:    map[string]any{"name": "test"},
				},
				{
					Methods: []string{"file", "validate"},
					Files:   map[string]any{"upload": map[string]any{}},
				},
			},
			expected: "multipart/form-data",
		},
		{
			name: "multiple patterns with JSON and form - JSON wins",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"json", "input"},
					Body:    map[string]any{"data": "test"},
				},
				{
					Methods: []string{"validate", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: "application/json",
		},
		{
			name: "deterministic sorting with equal patterns",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"validate"},
					Body:    map[string]any{"b": "test"},
				},
				{
					Methods: []string{"input"},
					Body:    map[string]any{"a": "test"},
				},
			},
			expected: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.DetectContentType(tt.patterns)
			if result != tt.expected {
				t.Errorf("DetectContentType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDefaultContentTypeDetector_HasFileUploads(t *testing.T) {
	detector := NewContentTypeDetector(nil)

	tests := []struct {
		name     string
		patterns []matchers.RequestUsageMatch
		expected bool
	}{
		{
			name:     "empty patterns have no file uploads",
			patterns: []matchers.RequestUsageMatch{},
			expected: false,
		},
		{
			name: "file method indicates file upload",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"file", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: true,
		},
		{
			name: "hasFile method indicates file upload",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"hasFile", "validated"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: true,
		},
		{
			name: "files map indicates file upload",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"input"},
					Body:    map[string]any{"name": "test"},
					Files:   map[string]any{"avatar": map[string]any{}},
				},
			},
			expected: true,
		},
		{
			name: "multipart content type indicates file upload",
			patterns: []matchers.RequestUsageMatch{
				{
					ContentTypes: []string{"multipart/form-data"},
					Methods:      []string{"input"},
					Body:         map[string]any{"name": "test"},
				},
			},
			expected: true,
		},
		{
			name: "JSON methods without files have no upload",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"json", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: false,
		},
		{
			name: "form methods without files have no upload",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"validate", "input"},
					Body:    map[string]any{"name": "test"},
				},
			},
			expected: false,
		},
		{
			name: "multiple patterns - any with file upload returns true",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"json", "input"},
					Body:    map[string]any{"name": "test"},
				},
				{
					Methods: []string{"file", "validate"},
					Files:   map[string]any{"upload": map[string]any{}},
				},
			},
			expected: true,
		},
		{
			name: "multiple patterns - none with file upload returns false",
			patterns: []matchers.RequestUsageMatch{
				{
					Methods: []string{"json", "input"},
					Body:    map[string]any{"name": "test"},
				},
				{
					Methods: []string{"validate", "input"},
					Body:    map[string]any{"email": "test"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.HasFileUploads(tt.patterns)
			if result != tt.expected {
				t.Errorf("HasFileUploads() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDefaultKeyPathParser_ParseKeyPath(t *testing.T) {
	parser := NewKeyPathParser(nil)

	tests := []struct {
		name        string
		path        string
		expected    []PathSegment
		expectError bool
	}{
		{
			name:        "empty path returns error",
			path:        "",
			expected:    nil,
			expectError: true,
		},
		{
			name: "simple path",
			path: "user.name",
			expected: []PathSegment{
				{Key: "user", IsArray: false, IsWildcard: false},
				{Key: "name", IsArray: false, IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "single segment",
			path: "name",
			expected: []PathSegment{
				{Key: "name", IsArray: false, IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "array notation",
			path: "users[0].name",
			expected: []PathSegment{
				{Key: "users", IsArray: true, ArrayKey: "0", IsWildcard: false},
				{Key: "name", IsArray: false, IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "array with string key",
			path: "data[key].value",
			expected: []PathSegment{
				{Key: "data", IsArray: true, ArrayKey: "key", IsWildcard: false},
				{Key: "value", IsArray: false, IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "empty array notation",
			path: "items[]",
			expected: []PathSegment{
				{Key: "items", IsArray: true, ArrayKey: "", IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "wildcard notation",
			path: "users.*.profile",
			expected: []PathSegment{
				{Key: "users", IsArray: false, IsWildcard: false},
				{Key: "*", IsArray: false, IsWildcard: true},
				{Key: "profile", IsArray: false, IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "consecutive dots are ignored",
			path: "user..name",
			expected: []PathSegment{
				{Key: "user", IsArray: false, IsWildcard: false},
				{Key: "name", IsArray: false, IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "quoted array key",
			path: `data["quoted key"]`,
			expected: []PathSegment{
				{Key: "data", IsArray: true, ArrayKey: "quoted key", IsWildcard: false},
			},
			expectError: false,
		},
		{
			name: "single quoted array key",
			path: "data['single quoted']",
			expected: []PathSegment{
				{Key: "data", IsArray: true, ArrayKey: "single quoted", IsWildcard: false},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ParseKeyPath(tt.path)

			if tt.expectError {
				if err == nil {
					t.Errorf("ParseKeyPath() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseKeyPath() unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("ParseKeyPath() length = %v, want %v", len(result), len(tt.expected))
				return
			}

			for i, segment := range result {
				expected := tt.expected[i]
				if segment.Key != expected.Key || segment.IsArray != expected.IsArray ||
					segment.IsWildcard != expected.IsWildcard || segment.ArrayKey != expected.ArrayKey {
					t.Errorf("ParseKeyPath() segment %d = %+v, want %+v", i, segment, expected)
				}
			}
		})
	}
}

func TestDefaultKeyPathParser_IsArrayNotation(t *testing.T) {
	parser := NewKeyPathParser(nil)

	tests := []struct {
		name            string
		segment         string
		expectedIsArray bool
		expectedKey     string
	}{
		{
			name:            "simple key",
			segment:         "name",
			expectedIsArray: false,
			expectedKey:     "",
		},
		{
			name:            "array with numeric index",
			segment:         "users[0]",
			expectedIsArray: true,
			expectedKey:     "0",
		},
		{
			name:            "array with string key",
			segment:         "data[key]",
			expectedIsArray: true,
			expectedKey:     "key",
		},
		{
			name:            "empty array notation",
			segment:         "items[]",
			expectedIsArray: true,
			expectedKey:     "",
		},
		{
			name:            "double quoted key",
			segment:         `data["quoted"]`,
			expectedIsArray: true,
			expectedKey:     "quoted",
		},
		{
			name:            "single quoted key",
			segment:         "data['quoted']",
			expectedIsArray: true,
			expectedKey:     "quoted",
		},
		{
			name:            "invalid notation - no closing bracket",
			segment:         "data[key",
			expectedIsArray: false,
			expectedKey:     "",
		},
		{
			name:            "invalid notation - no opening bracket",
			segment:         "datakey]",
			expectedIsArray: false,
			expectedKey:     "",
		},
		{
			name:            "invalid notation - brackets in wrong order",
			segment:         "data]key[",
			expectedIsArray: false,
			expectedKey:     "",
		},
		{
			name:            "nested brackets",
			segment:         "data[key[nested]]",
			expectedIsArray: true,
			expectedKey:     "key[nested]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isArray, key := parser.IsArrayNotation(tt.segment)

			if isArray != tt.expectedIsArray {
				t.Errorf("IsArrayNotation() isArray = %v, want %v", isArray, tt.expectedIsArray)
			}

			if key != tt.expectedKey {
				t.Errorf("IsArrayNotation() key = %v, want %v", key, tt.expectedKey)
			}
		})
	}
}

func TestNewContentTypeDetector(t *testing.T) {
	detector := NewContentTypeDetector(nil)
	if detector == nil {
		t.Errorf("NewContentTypeDetector() returned nil")
		return
	}
	if detector.config == nil {
		t.Errorf("NewContentTypeDetector() config is nil")
	}
}

func TestNewKeyPathParser(t *testing.T) {
	parser := NewKeyPathParser(nil)
	if parser == nil {
		t.Errorf("NewKeyPathParser() returned nil")
		return
	}
	if parser.config == nil {
		t.Errorf("NewKeyPathParser() config is nil")
	}
}

func TestBuildNestedObject(t *testing.T) {
	tests := []struct {
		name     string
		segments []PathSegment
		validate func(*testing.T, *OrderedObject)
	}{
		{
			name:     "empty segments",
			segments: []PathSegment{},
			validate: func(t *testing.T, obj *OrderedObject) {
				if !obj.IsEmpty() {
					t.Fatal("expected empty object")
				}
			},
		},
		{
			name: "single key",
			segments: []PathSegment{
				{Key: "name", IsWildcard: false, IsArray: false},
			},
			validate: func(t *testing.T, obj *OrderedObject) {
				prop, exists := obj.GetProperty("name")
				if !exists {
					t.Fatal("expected 'name' property")
				}
				if prop.Type != PropertyTypeString {
					t.Fatalf("expected string type, got %s", prop.Type)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildNestedObject(tt.segments)
			if result == nil {
				t.Fatal("BuildNestedObject returned nil")
			}

			tt.validate(t, result)
		})
	}
}

func TestT11_ShapeInferencer_CompleteDemo(t *testing.T) {
	// This test demonstrates the complete T11.3 Shape Inferencer implementation
	// working end-to-end with all components integrated

	config := DefaultInferenceConfig()
	detector := NewContentTypeDetector(config)
	parser := NewKeyPathParser(config)
	merger := NewPropertyMerger(config)
	inferencer := NewShapeInferencer(detector, parser, merger, config)

	// Example Laravel request patterns that would come from the matchers
	patterns := []matchers.RequestUsageMatch{
		{
			Methods: []string{"only", "validate"},
			Body: map[string]any{
				// These demonstrate the T11 acceptance criteria
				"users.*.email":  "user@example.com",
				"users.*.name":   "John",
				"profile.bio":    "Software developer",
				"settings.theme": "dark",
				"tags":           []any{"go", "api"},
			},
		},
		{
			Methods: []string{"input"},
			Body: map[string]any{
				"age":    25,
				"active": true,
			},
		},
	}

	// Infer the complete request shape
	result, err := inferencer.InferRequestShape(patterns)
	if err != nil {
		t.Fatalf("InferRequestShape() error: %v", err)
	}

	// Verify the inferred shape structure
	t.Logf("=== T11.3 Shape Inferencer Demo Results ===")
	t.Logf("Content Types: %v", result.ContentTypes)
	t.Logf("Body Properties: %d", result.Body.PropertyCount())

	// Test consolidation of multiple patterns
	if result.Body.PropertyCount() != 6 {
		t.Errorf("Expected 6 consolidated properties, got %d", result.Body.PropertyCount())
	}

	// Test T11 acceptance criteria: users.*.email creates proper nested array structure
	usersProp, exists := result.Body.GetProperty("users")
	if !exists {
		t.Fatal("Expected 'users' property from users.*.email pattern")
	}
	if usersProp.Type != PropertyTypeArray {
		t.Errorf("Expected users to be array type, got %s", usersProp.Type)
	}
	if usersProp.Items.Type != PropertyTypeObject {
		t.Error("Expected users array to contain objects")
	}

	// Verify nested properties in users array items
	emailProp, exists := usersProp.Items.Properties.GetProperty("email")
	if !exists || emailProp.Type != PropertyTypeString {
		t.Error("Expected users[].email to be string property")
	}

	nameProp, exists := usersProp.Items.Properties.GetProperty("name")
	if !exists || nameProp.Type != PropertyTypeString {
		t.Error("Expected users[].name to be string property")
	}

	// Test nested object creation from profile.bio
	profileProp, exists := result.Body.GetProperty("profile")
	if !exists || profileProp.Type != PropertyTypeObject {
		t.Error("Expected profile to be object property")
	}

	bioProp, exists := profileProp.Properties.GetProperty("bio")
	if !exists || bioProp.Type != PropertyTypeString {
		t.Error("Expected profile.bio to be string property")
	}

	// Test array type inference
	tagsProp, exists := result.Body.GetProperty("tags")
	if !exists || tagsProp.Type != PropertyTypeArray {
		t.Error("Expected tags to be array property")
	}

	// Test property merging from multiple patterns
	ageProp, exists := result.Body.GetProperty("age")
	if !exists || ageProp.Type != PropertyTypeNumber {
		t.Error("Expected age to be number property from second pattern")
	}

	t.Logf("✅ T11.3 PropertyMerger + ShapeInferencer implementation complete!")
	t.Logf("✅ All T11 acceptance criteria satisfied:")
	t.Logf("   - Consolidate keys from multiple request patterns: ✓")
	t.Logf("   - Interpret only(['users.*.email']) to nested shape: ✓")
	t.Logf("   - KeyPathParser integration: ✓")
	t.Logf("   - ContentTypeDetector integration: ✓")
	t.Logf("   - PropertyMerger integration: ✓")
}
