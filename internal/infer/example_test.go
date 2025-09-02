package infer_test

import (
	"fmt"

	"github.com/garaekz/oxinfer/internal/infer"
	"github.com/garaekz/oxinfer/internal/matchers"
)

// Example demonstrates how shape inference types integrate with the matchers package.
func Example() {
	// Create a sample request usage match (simulating output from matchers package)
	requestMatch := matchers.RequestUsageMatch{
		ContentTypes: []string{"application/json"},
		Body: map[string]interface{}{
			"name":  map[string]interface{}{},
			"email": map[string]interface{}{},
			"age":   map[string]interface{}{},
		},
		Methods: []string{"input", "validate"},
	}

	// Create a ConsolidatedRequest to show how patterns would be merged
	consolidated := &infer.ConsolidatedRequest{
		ContentTypes: requestMatch.ContentTypes,
		Body: map[string]*infer.PropertyInfo{
			"name":  infer.CreateStringProperty("User name", ""),
			"email": infer.CreateStringProperty("User email", "email"),
			"age":   infer.CreateNumberProperty("User age", ""),
		},
		Methods: requestMatch.Methods,
		Sources: []*infer.RequestUsageSource{
			{
				FilePath:   "/app/Http/Controllers/UserController.php",
				Method:     "store",
				Confidence: 0.9,
				Pattern:    "request_input",
			},
		},
	}

	// Create the final RequestInfo structure
	body := infer.CreateEmptyOrderedObject()
	body.AddProperty("name", infer.CreateStringProperty("User name", ""))
	body.AddProperty("email", infer.CreateStringProperty("User email", "email"))
	body.AddProperty("age", infer.CreateNumberProperty("User age", ""))
	body.AddRequired("name")
	body.AddRequired("email")

	requestInfo := &infer.RequestInfo{
		ContentTypes: consolidated.ContentTypes,
		Body:         *body,
	}

	fmt.Printf("Content Types: %v\n", requestInfo.ContentTypes)
	fmt.Printf("Body Properties: %d\n", requestInfo.Body.PropertyCount())
	fmt.Printf("Required Fields: %v\n", requestInfo.Body.Required)
	fmt.Printf("Property Order: %v\n", requestInfo.Body.Order)

	// Output:
	// Content Types: [application/json]
	// Body Properties: 3
	// Required Fields: [email name]
	// Property Order: [name email age]
}

// ExampleOrderedObject_MarshalJSON demonstrates deterministic JSON output.
func ExampleOrderedObject_MarshalJSON() {
	// Create an object with properties added in a specific order
	obj := infer.CreateEmptyOrderedObject()
	obj.AddProperty("zebra", infer.CreateStringProperty("Last alphabetically", ""))
	obj.AddProperty("alpha", infer.CreateStringProperty("First alphabetically", ""))
	obj.AddProperty("beta", infer.CreateStringProperty("Second alphabetically", ""))

	// JSON output will maintain the insertion order
	jsonData, err := obj.MarshalJSON()
	if err != nil {
		panic(err)
	}

	fmt.Printf("JSON: %s\n", string(jsonData))

	// Output:
	// JSON: {"zebra":{"type":"string","description":"Last alphabetically"},"alpha":{"type":"string","description":"First alphabetically"},"beta":{"type":"string","description":"Second alphabetically"},"order":["zebra","alpha","beta"]}
}

// ExampleCreateStringProperty demonstrates creating different property types.
func ExampleCreateStringProperty() {
	// String property
	stringProp := infer.CreateStringProperty("User email", "email")
	fmt.Printf("String property: type=%s, format=%s\n", stringProp.Type, stringProp.Format)

	// Number property
	numberProp := infer.CreateNumberProperty("User age", "int32")
	fmt.Printf("Number property: type=%s, format=%s\n", numberProp.Type, numberProp.Format)

	// File property
	fileProp := infer.CreateFileProperty("Profile avatar")
	fmt.Printf("File property: type=%s, format=%s\n", fileProp.Type, fileProp.Format)

	// Array property
	itemProp := infer.CreateStringProperty("Tag name", "")
	arrayProp := infer.CreateArrayProperty(itemProp, "User tags")
	fmt.Printf("Array property: type=%s, items.type=%s\n", arrayProp.Type, arrayProp.Items.Type)

	// Object property
	nestedObj := infer.CreateEmptyOrderedObject()
	nestedObj.AddProperty("street", infer.CreateStringProperty("Street address", ""))
	nestedObj.AddProperty("city", infer.CreateStringProperty("City name", ""))
	objectProp := infer.CreateObjectProperty(nestedObj, "User address")
	fmt.Printf("Object property: type=%s, properties.count=%d\n", objectProp.Type, objectProp.Properties.PropertyCount())

	// Output:
	// String property: type=string, format=email
	// Number property: type=number, format=int32
	// File property: type=file, format=binary
	// Array property: type=array, items.type=string
	// Object property: type=object, properties.count=2
}

// ExampleNewShapeInferenceError demonstrates error handling.
func ExampleNewShapeInferenceError() {
	// Create different types of shape inference errors
	errors := []*infer.ShapeInferenceError{
		infer.NewShapeInferenceError(infer.ErrorTypeKeyPathParsing, "Invalid dot notation", "user..profile"),
		infer.NewShapeInferenceError(infer.ErrorTypeContentTypeInfer, "No content types detected", ""),
		infer.NewShapeInferenceError(infer.ErrorTypePropertyConversion, "Cannot convert to property", "complex nested structure"),
	}

	for _, err := range errors {
		fmt.Printf("Error: %s\n", err.Error())
	}

	// Output:
	// Error: KEY_PATH_PARSING: Invalid dot notation (context: user..profile)
	// Error: CONTENT_TYPE_INFER: No content types detected
	// Error: PROPERTY_CONVERSION: Cannot convert to property (context: complex nested structure)
}