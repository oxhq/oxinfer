//go:build goexperiment.jsonv2

package emitter

import (
	"encoding/json/v2"
	"testing"
)

func TestJSONEmitter_RequestFieldMetadata(t *testing.T) {
	emitter := NewJSONEmitter()

	delta := &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 1,
				Skipped:     0,
				DurationMs:  10,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\HardeningController",
				Method: "store",
				Request: &RequestInfo{
					ContentTypes: []string{"multipart/form-data", "application/json"},
					Fields: []RequestField{
						{
							Location: "query",
							Path:     "filter.published",
							Kind:     "scalar",
							Type:     "string",
							ScalarType: "string",
							Wrappers: []string{"Optional"},
							Optional: boolPtr(true),
							Required: boolPtr(false),
							AllowedValues: []string{"draft", "published"},
							Source:   "query-builder",
							Via:      "allowedFilters",
						},
						{
							Location:   "body",
							Path:       "preview",
							Kind:       "object",
							Type:       "App\\Data\\SeoData",
							ItemType:   "App\\Data\\SeoData",
							Wrappers:   []string{"Optional", "Lazy"},
							Required:   boolPtr(false),
							Optional:   boolPtr(true),
							Nullable:   boolPtr(false),
							IsArray:    boolPtr(false),
							Collection: boolPtr(false),
							Source:     "laravel-data",
							Via:        "data",
						},
						{
							Location:   "files",
							Path:       "attachments",
							Kind:       "collection",
							Type:       "array",
							ItemType:   "file",
							IsArray:    boolPtr(true),
							Collection: boolPtr(true),
							Source:     "medialibrary",
							Via:        "media-library",
						},
					},
				},
			},
		},
		Models:      []Model{},
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}

	data, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		t.Fatalf("MarshalDeterministic() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	controllers := result["controllers"].([]any)
	if len(controllers) != 1 {
		t.Fatalf("expected 1 controller, got %d", len(controllers))
	}

	request := controllers[0].(map[string]any)["request"].(map[string]any)
	fields := request["fields"].([]any)
	if len(fields) != 3 {
		t.Fatalf("expected 3 request fields, got %d", len(fields))
	}

	first := fields[0].(map[string]any)
	if first["location"] != "body" || first["path"] != "preview" {
		t.Fatalf("first field mismatch: %#v", first)
	}
	if wrappers, ok := first["wrappers"].([]any); !ok || len(wrappers) != 2 || wrappers[0] != "Lazy" || wrappers[1] != "Optional" {
		t.Fatalf("wrappers not preserved: %#v", first["wrappers"])
	}
	if scalarType := first["type"]; scalarType != "App\\Data\\SeoData" {
		t.Fatalf("body field type mismatch: %#v", first["type"])
	}
	if required, ok := first["required"].(bool); !ok || required {
		t.Fatalf("body field required mismatch: %#v", first["required"])
	}

	second := fields[1].(map[string]any)
	if second["location"] != "files" || second["path"] != "attachments" {
		t.Fatalf("second field mismatch: %#v", second)
	}

	third := fields[2].(map[string]any)
	if third["location"] != "query" || third["path"] != "filter.published" {
		t.Fatalf("third field mismatch: %#v", third)
	}
	if allowedValues, ok := third["allowedValues"].([]any); !ok || len(allowedValues) != 2 || allowedValues[0] != "draft" || allowedValues[1] != "published" {
		t.Fatalf("query field allowedValues mismatch: %#v", third["allowedValues"])
	}
	if via := third["via"]; via != "allowedFilters" {
		t.Fatalf("query field via mismatch: %#v", third["via"])
	}

	// Verify deterministic output by marshaling twice.
	data2, err := emitter.MarshalDeterministic(delta)
	if err != nil {
		t.Fatalf("second MarshalDeterministic() error = %v", err)
	}
	if string(data) != string(data2) {
		t.Fatal("request field metadata output is not deterministic")
	}
}
