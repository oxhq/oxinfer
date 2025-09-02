package emitter

import (
	"bytes"
	"fmt"
)

// ExampleJSONEmitter_EmitStub demonstrates the basic usage
func ExampleJSONEmitter_EmitStub() {
	emitter := NewJSONEmitter()
	
	// Generate a stub delta (empty but schema-valid)
	delta, err := emitter.EmitStub()
	if err != nil {
		panic(err)
	}
	
	// Marshal to JSON with deterministic ordering
	jsonData, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		panic(err)
	}
	
	fmt.Println(string(jsonData))
	
	// Output:
	// {"meta":{"partial":false,"stats":{"filesParsed":0,"skipped":0,"durationMs":0}},"controllers":[],"models":[],"polymorphic":[],"broadcast":[]}
}

// ExampleJSONEmitter_WriteJSON demonstrates writing JSON to a writer
func ExampleJSONEmitter_WriteJSON() {
	emitter := NewJSONEmitter()
	
	// Create a simple delta
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 5,
				Skipped:     1,
				DurationMs:  250,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\ApiController",
				Method: "index",
			},
		},
		Models:      []Model{},
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}
	
	// Write to buffer (could be any io.Writer like os.Stdout)
	var buf bytes.Buffer
	err := emitter.WriteJSON(&buf, delta)
	if err != nil {
		panic(err)
	}
	
	fmt.Println(buf.String())
	
	// Output:
	// {"meta":{"partial":false,"stats":{"filesParsed":5,"skipped":1,"durationMs":250}},"controllers":[{"fqcn":"App\\Http\\Controllers\\ApiController","method":"index"}],"models":[],"polymorphic":[],"broadcast":[]}
}

// ExampleDelta_structure demonstrates the complete Delta structure
func ExampleDelta_structure() {
	// This example shows all the fields available in a Delta
	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 10,
				Skipped:     2,
				DurationMs:  500,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\UserController",
				Method: "show",
				HTTP: &HTTPInfo{
					Status:   &[]int{200}[0],
					Explicit: &[]bool{true}[0],
				},
            Request: &RequestInfo{
                ContentTypes: []string{"application/json"},
                Body:         NewOrderedObjectFromMap(map[string]interface{}{"id": map[string]interface{}{}}),
            },
				Resources: []Resource{
					{Class: "UserResource", Collection: false},
				},
				ScopesUsed: []ScopeUsed{
					{On: "User", Name: "active"},
				},
			},
		},
		Models: []Model{
			{
				FQCN: "App\\Models\\User",
				WithPivot: []PivotInfo{
					{
						Relation: "roles",
						Columns:  []string{"level"},
					},
				},
				Attributes: []Attribute{
					{Name: "full_name", Via: "Attribute::make"},
				},
			},
		},
		Polymorphic: []Polymorphic{
			{
				Parent: "App\\Models\\Comment",
				Morph: MorphInfo{
					Key:        "commentable",
					TypeColumn: "commentable_type",
					IDColumn:   "commentable_id",
				},
				Discriminator: Discriminator{
					PropertyName: "type",
					Mapping: map[string]string{
						"post": "App\\Models\\Post",
					},
				},
			},
		},
		Broadcast: []Broadcast{
			{
				Channel:    "user.{id}",
				Params:     []string{"id"},
				Visibility: "private",
			},
		},
	}
	
	emitter := NewJSONEmitter()
	jsonData, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		panic(err)
	}
	
	fmt.Printf("Delta contains %d controllers, %d models, %d polymorphic, %d broadcast\n", 
		len(delta.Controllers), len(delta.Models), len(delta.Polymorphic), len(delta.Broadcast))
	fmt.Printf("JSON length: %d bytes\n", len(jsonData))
	
	// Output:
	// Delta contains 1 controllers, 1 models, 1 polymorphic, 1 broadcast
	// JSON length: 821 bytes
}
