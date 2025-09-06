//go:build goexperiment.jsonv2

package infer

import (
	"encoding/json/v2"
	"testing"
)

func TestOrderedObject_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		object   *OrderedObject
		expected string
	}{
		{
			name:     "nil object",
			object:   nil,
			expected: "{}",
		},
		{
			name: "empty object",
			object: &OrderedObject{
				Properties: make(map[string]*PropertyInfo),
				Required:   []string{},
				Order:      []string{},
			},
			expected: `{"order":[]}`,
		},
		{
			name: "single property with order",
			object: &OrderedObject{
				Properties: map[string]*PropertyInfo{
					"name": {Type: PropertyTypeString, Description: "User name"},
				},
				Order: []string{"name"},
			},
			expected: `{"name":{"type":"string","description":"User name"},"order":["name"]}`,
		},
		{
			name: "multiple properties with order",
			object: &OrderedObject{
				Properties: map[string]*PropertyInfo{
					"name": {Type: PropertyTypeString, Description: "User name"},
					"age":  {Type: PropertyTypeNumber, Description: "User age"},
				},
				Order: []string{"name", "age"},
			},
			expected: `{"name":{"type":"string","description":"User name"},"age":{"type":"number","description":"User age"},"order":["name","age"]}`,
		},
		{
			name: "properties with required fields",
			object: &OrderedObject{
				Properties: map[string]*PropertyInfo{
					"name": {Type: PropertyTypeString, Description: "User name"},
					"age":  {Type: PropertyTypeNumber, Description: "User age"},
				},
				Required: []string{"name"},
				Order:    []string{"name", "age"},
			},
			expected: `{"name":{"type":"string","description":"User name"},"age":{"type":"number","description":"User age"},"required":["name"],"order":["name","age"]}`,
		},
		{
			name: "no order provided - should sort keys",
			object: &OrderedObject{
				Properties: map[string]*PropertyInfo{
					"zebra": {Type: PropertyTypeString},
					"alpha": {Type: PropertyTypeString},
				},
				Order: []string{},
			},
			expected: `{"alpha":{"type":"string"},"zebra":{"type":"string"},"order":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.object.MarshalJSON()
			if err != nil {
				t.Errorf("MarshalJSON() error = %v", err)
				return
			}
			if string(result) != tt.expected {
				t.Errorf("MarshalJSON() = %v, expected %v", string(result), tt.expected)
			}
		})
	}
}

func TestOrderedObject_AddProperty(t *testing.T) {
	obj := CreateEmptyOrderedObject()

	prop1 := CreateStringProperty("First property", "")
	prop2 := CreateNumberProperty("Second property", "")

	obj.AddProperty("first", prop1)
	obj.AddProperty("second", prop2)

	if obj.PropertyCount() != 2 {
		t.Errorf("Expected 2 properties, got %d", obj.PropertyCount())
	}

	if len(obj.Order) != 2 {
		t.Errorf("Expected 2 items in order, got %d", len(obj.Order))
	}

	if obj.Order[0] != "first" || obj.Order[1] != "second" {
		t.Errorf("Expected order [first, second], got %v", obj.Order)
	}

	// Test adding duplicate property
	obj.AddProperty("first", prop1)
	if obj.PropertyCount() != 2 {
		t.Errorf("Expected 2 properties after duplicate add, got %d", obj.PropertyCount())
	}
	if len(obj.Order) != 2 {
		t.Errorf("Expected 2 items in order after duplicate add, got %d", len(obj.Order))
	}
}

func TestOrderedObject_GetProperty(t *testing.T) {
	obj := CreateEmptyOrderedObject()
	prop := CreateStringProperty("Test property", "")
	obj.AddProperty("test", prop)

	// Test existing property
	retrieved, exists := obj.GetProperty("test")
	if !exists {
		t.Error("Expected property to exist")
	}
	if retrieved.Type != PropertyTypeString {
		t.Errorf("Expected string type, got %v", retrieved.Type)
	}

	// Test non-existing property
	_, exists = obj.GetProperty("nonexistent")
	if exists {
		t.Error("Expected property to not exist")
	}
}

func TestOrderedObject_HasProperty(t *testing.T) {
	obj := CreateEmptyOrderedObject()
	obj.AddProperty("test", CreateStringProperty("Test", ""))

	if !obj.HasProperty("test") {
		t.Error("Expected HasProperty to return true for existing property")
	}

	if obj.HasProperty("nonexistent") {
		t.Error("Expected HasProperty to return false for non-existing property")
	}
}

func TestOrderedObject_IsEmpty(t *testing.T) {
	obj := CreateEmptyOrderedObject()
	if !obj.IsEmpty() {
		t.Error("Expected empty object to return true for IsEmpty")
	}

	obj.AddProperty("test", CreateStringProperty("Test", ""))
	if obj.IsEmpty() {
		t.Error("Expected object with properties to return false for IsEmpty")
	}
}

func TestOrderedObject_AddRequired(t *testing.T) {
	obj := CreateEmptyOrderedObject()

	obj.AddRequired("field1")
	obj.AddRequired("field2")
	obj.AddRequired("field1") // Duplicate

	if len(obj.Required) != 2 {
		t.Errorf("Expected 2 required fields, got %d", len(obj.Required))
	}

	// Should be sorted
	if obj.Required[0] != "field1" || obj.Required[1] != "field2" {
		t.Errorf("Expected sorted required fields [field1, field2], got %v", obj.Required)
	}

	if !obj.IsRequired("field1") {
		t.Error("Expected field1 to be required")
	}

	if obj.IsRequired("field3") {
		t.Error("Expected field3 to not be required")
	}
}

func TestPropertyCreators(t *testing.T) {
	// Test string property
	stringProp := CreateStringProperty("A string", "email")
	if stringProp.Type != PropertyTypeString {
		t.Errorf("Expected string type, got %v", stringProp.Type)
	}
	if stringProp.Description != "A string" {
		t.Errorf("Expected description 'A string', got %v", stringProp.Description)
	}
	if stringProp.Format != "email" {
		t.Errorf("Expected format 'email', got %v", stringProp.Format)
	}

	// Test number property
	numberProp := CreateNumberProperty("A number", "float")
	if numberProp.Type != PropertyTypeNumber {
		t.Errorf("Expected number type, got %v", numberProp.Type)
	}

	// Test file property
	fileProp := CreateFileProperty("A file")
	if fileProp.Type != PropertyTypeFile {
		t.Errorf("Expected file type, got %v", fileProp.Type)
	}
	if fileProp.Format != "binary" {
		t.Errorf("Expected binary format, got %v", fileProp.Format)
	}

	// Test array property
	itemProp := CreateStringProperty("Array item", "")
	arrayProp := CreateArrayProperty(itemProp, "An array")
	if arrayProp.Type != PropertyTypeArray {
		t.Errorf("Expected array type, got %v", arrayProp.Type)
	}
	if arrayProp.Items == nil {
		t.Error("Expected array items to be set")
	}
	if arrayProp.Items.Type != PropertyTypeString {
		t.Errorf("Expected string item type, got %v", arrayProp.Items.Type)
	}

	// Test object property
	obj := CreateEmptyOrderedObject()
	objProp := CreateObjectProperty(obj, "An object")
	if objProp.Type != PropertyTypeObject {
		t.Errorf("Expected object type, got %v", objProp.Type)
	}
	if objProp.Properties == nil {
		t.Error("Expected object properties to be set")
	}
}

func TestPropertyInfo_Clone(t *testing.T) {
	// Test nil clone
	var nilProp *PropertyInfo
	cloned := nilProp.Clone()
	if cloned != nil {
		t.Error("Expected nil clone to return nil")
	}

	// Test simple property clone
	original := CreateStringProperty("Original", "email")
	cloned = original.Clone()

	if cloned.Type != original.Type {
		t.Error("Expected cloned type to match original")
	}
	if cloned.Description != original.Description {
		t.Error("Expected cloned description to match original")
	}
	if cloned.Format != original.Format {
		t.Error("Expected cloned format to match original")
	}

	// Modify original and ensure clone is unaffected
	original.Description = "Modified"
	if cloned.Description == "Modified" {
		t.Error("Clone should not be affected by original modification")
	}

	// Test complex property with nested structures
	obj := CreateEmptyOrderedObject()
	obj.AddProperty("nested", CreateStringProperty("Nested", ""))

	itemProp := CreateStringProperty("Item", "")
	arrayProp := CreateArrayProperty(itemProp, "Array")
	objectProp := CreateObjectProperty(obj, "Object")

	arrayClone := arrayProp.Clone()
	objectClone := objectProp.Clone()

	if arrayClone.Items == nil {
		t.Error("Expected cloned array to have items")
	}
	if objectClone.Properties == nil {
		t.Error("Expected cloned object to have properties")
	}
}

func TestOrderedObject_Clone(t *testing.T) {
	// Test nil clone
	var nilObj *OrderedObject
	cloned := nilObj.Clone()
	if cloned != nil {
		t.Error("Expected nil clone to return nil")
	}

	// Test complex object clone
	original := CreateEmptyOrderedObject()
	original.AddProperty("name", CreateStringProperty("Name", ""))
	original.AddProperty("age", CreateNumberProperty("Age", ""))
	original.AddRequired("name")

	cloned = original.Clone()

	if cloned.PropertyCount() != original.PropertyCount() {
		t.Error("Expected cloned object to have same property count")
	}

	if len(cloned.Required) != len(original.Required) {
		t.Error("Expected cloned object to have same required count")
	}

	if len(cloned.Order) != len(original.Order) {
		t.Error("Expected cloned object to have same order count")
	}

	// Test independence of clone
	original.AddProperty("email", CreateStringProperty("Email", ""))
	if cloned.PropertyCount() == original.PropertyCount() {
		t.Error("Clone should not be affected by original modification")
	}
}

func TestShapeInferenceError(t *testing.T) {
	// Test error without context
	err := NewShapeInferenceError(ErrorTypeKeyPathParsing, "Invalid path", "")
	expected := "KEY_PATH_PARSING: Invalid path"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}

	// Test error with context
	err = NewShapeInferenceError(ErrorTypeKeyPathParsing, "Invalid path", "user.profile.name")
	expected = "KEY_PATH_PARSING: Invalid path (context: user.profile.name)"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

func TestDefaultInferenceConfig(t *testing.T) {
	config := DefaultInferenceConfig()

	if config.MaxDepth != 5 {
		t.Errorf("Expected MaxDepth 5, got %d", config.MaxDepth)
	}
	if config.MaxProperties != 100 {
		t.Errorf("Expected MaxProperties 100, got %d", config.MaxProperties)
	}
	if !config.InferRequired {
		t.Error("Expected InferRequired to be true")
	}
	if config.MinPropertyConfidence != 0.7 {
		t.Errorf("Expected MinPropertyConfidence 0.7, got %f", config.MinPropertyConfidence)
	}
	if !config.MergeSimilarTypes {
		t.Error("Expected MergeSimilarTypes to be true")
	}

	expectedContentTypes := []string{
		"application/json",
		"multipart/form-data",
		"application/x-www-form-urlencoded",
	}
	if len(config.PreferredContentTypes) != len(expectedContentTypes) {
		t.Errorf("Expected %d content types, got %d", len(expectedContentTypes), len(config.PreferredContentTypes))
	}
	for i, expected := range expectedContentTypes {
		if config.PreferredContentTypes[i] != expected {
			t.Errorf("Expected content type %q at index %d, got %q", expected, i, config.PreferredContentTypes[i])
		}
	}
}

func TestInferenceStats_String(t *testing.T) {
	stats := &InferenceStats{
		PatternsProcessed:  5,
		PropertiesInferred: 12,
		ContentTypesFound:  2,
		AverageConfidence:  0.85,
		ProcessingTimeMs:   150,
		ErrorsEncountered:  1,
	}

	result := stats.String()
	expected := "InferenceStats{patterns: 5, properties: 12, contentTypes: 2, confidence: 0.85, time: 150ms, errors: 1}"
	if result != expected {
		t.Errorf("Expected stats string %q, got %q", expected, result)
	}
}

func TestConsolidatedRequest_JSON(t *testing.T) {
	// Test JSON marshaling of ConsolidatedRequest
	req := &ConsolidatedRequest{
		ContentTypes: []string{"application/json"},
		Body: map[string]*PropertyInfo{
			"name": CreateStringProperty("User name", ""),
		},
		Methods: []string{"input", "validate"},
		Sources: []*RequestUsageSource{
			{
				FilePath:   "/app/Http/Controllers/UserController.php",
				Method:     "store",
				Confidence: 0.9,
				Pattern:    "request_input",
			},
		},
	}

	data, err := json.Marshal(req, json.Deterministic(true))
	if err != nil {
		t.Errorf("Failed to marshal ConsolidatedRequest: %v", err)
	}

	// Unmarshal to verify structure
	var unmarshaled ConsolidatedRequest
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Errorf("Failed to unmarshal ConsolidatedRequest: %v", err)
	}

	if len(unmarshaled.ContentTypes) != 1 {
		t.Errorf("Expected 1 content type, got %d", len(unmarshaled.ContentTypes))
	}
	if len(unmarshaled.Methods) != 2 {
		t.Errorf("Expected 2 methods, got %d", len(unmarshaled.Methods))
	}
	if len(unmarshaled.Sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(unmarshaled.Sources))
	}
}

func TestRequestInfo_JSON(t *testing.T) {
	// Test JSON marshaling of RequestInfo
	body := CreateEmptyOrderedObject()
	body.AddProperty("name", CreateStringProperty("User name", ""))
	body.AddProperty("email", CreateStringProperty("User email", "email"))
	body.AddRequired("name")

	info := &RequestInfo{
		ContentTypes: []string{"application/json", "multipart/form-data"},
		Body:         *body,
	}

	data, err := json.Marshal(info, json.Deterministic(true))
	if err != nil {
		t.Errorf("Failed to marshal RequestInfo: %v", err)
	}

	// Verify the JSON contains expected structure
	var jsonMap map[string]any
	err = json.Unmarshal(data, &jsonMap)
	if err != nil {
		t.Errorf("Failed to unmarshal RequestInfo JSON: %v", err)
	}

	bodyMap, ok := jsonMap["body"].(map[string]any)
	if !ok {
		t.Error("Expected body to be an object")
	}

	if _, hasName := bodyMap["name"]; !hasName {
		t.Error("Expected body to contain name property")
	}
}

// Benchmark tests for performance validation
func BenchmarkOrderedObject_MarshalJSON(b *testing.B) {
	obj := CreateEmptyOrderedObject()
	for i := 0; i < 50; i++ {
		key := "property" + string(rune('0'+i%10))
		obj.AddProperty(key, CreateStringProperty("Test property", ""))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := obj.MarshalJSON()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPropertyInfo_Clone(b *testing.B) {
	// Create a complex property structure
	nestedObj := CreateEmptyOrderedObject()
	nestedObj.AddProperty("deep", CreateStringProperty("Deep property", ""))

	prop := CreateObjectProperty(nestedObj, "Complex object")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = prop.Clone()
	}
}
