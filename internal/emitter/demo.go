// Package emitter provides delta emission functionality for oxinfer.
// This file demonstrates the emitter functionality.
package emitter

import (
	"fmt"
	"os"
)

// DemoEmitter demonstrates the basic emitter functionality.
// This shows how Worker 3 (integration) would use the emitter.
func DemoEmitter() error {
	emitter := NewJSONEmitter()

	// Generate a stub delta (no actual parsing yet)
	fmt.Println("=== Stub Delta Generation ===")
	
	stub, err := emitter.EmitStub()
	if err != nil {
		return fmt.Errorf("failed to generate stub: %w", err)
	}

	fmt.Printf("Generated stub with partial=%t, filesParsed=%d\n", 
		stub.Meta.Partial, stub.Meta.Stats.FilesParsed)

    // Marshal to deterministic JSON
    jsonData, err := emitter.MarshalDeterministic(stub)
    if err != nil {
        return fmt.Errorf("failed to marshal: %w", err)
    }

	fmt.Printf("JSON size: %d bytes\n", len(jsonData))
	fmt.Printf("Stub JSON: %s\n\n", string(jsonData))

    // Demonstrate writing to stdout (what CLI will do)
    fmt.Println("=== Writing to stdout (CLI simulation) ===")
    err = emitter.WriteJSON(os.Stdout, stub)
    if err != nil {
        return fmt.Errorf("failed to write JSON: %w", err)
    }
	fmt.Println() // Add newline after JSON output

	return nil
}

// DemoComplexDelta shows what full parsing output would look like with future enhancements.
func DemoComplexDelta() error {
	emitter := NewJSONEmitter()

	// Simulate what would come from parsing Laravel code
	fmt.Println("\n=== Future enhancement: Complex Delta Example ===")
	
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false, // Complete analysis
			Stats: MetaStats{
				FilesParsed: 25,
				Skipped:     3,
				DurationMs:  1500,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\UserController",
				Method: "store",
				HTTP: &HTTPInfo{
					Status:   &[]int{201}[0],
					Explicit: &[]bool{true}[0],
				},
				Request: &RequestInfo{
					ContentTypes: []string{"application/json"},
					Body:         NewOrderedObjectFromMap(map[string]interface{}{"name": map[string]interface{}{}, "email": map[string]interface{}{}}),
				},
				Resources: []Resource{
					{Class: "UserResource", Collection: false},
				},
			},
		},
		Models: []Model{
			{
				FQCN: "App\\Models\\User",
				Attributes: []Attribute{
					{Name: "full_name", Via: "Attribute::make"},
				},
			},
		},
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}

	jsonData, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		return fmt.Errorf("failed to marshal complex delta: %w", err)
	}

	fmt.Printf("Complex delta JSON (%d bytes):\n%s\n", len(jsonData), string(jsonData))

	return nil
}
