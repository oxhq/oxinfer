package emitter

import (
	"testing"
)

func TestDelta_StructureCompliance(t *testing.T) {
	tests := []struct {
		name   string
		delta  *Delta
		verify func(*testing.T, *Delta)
	}{
		{
			name: "empty delta structure",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: false,
					Stats: MetaStats{
						FilesParsed: 0,
						Skipped:     0,
						DurationMs:  0,
					},
				},
			},
			verify: func(t *testing.T, d *Delta) {
				if d.Meta.Partial != false {
					t.Error("expected Meta.Partial to be false")
				}
				if d.Meta.Stats.FilesParsed != 0 {
					t.Error("expected Meta.Stats.FilesParsed to be 0")
				}
			},
		},
		{
			name: "complete delta structure",
			delta: &Delta{
				Meta: MetaInfo{
					Partial: true,
					Stats: MetaStats{
						FilesParsed: 15,
						Skipped:     3,
						DurationMs:  1250,
					},
				},
				Controllers: []Controller{
					{
						FQCN:   "App\\Http\\Controllers\\UserController",
						Method: "index",
						HTTP: &HTTPInfo{
							Status:   &[]int{200}[0],
							Explicit: &[]bool{true}[0],
						},
						Request: &RequestInfo{
							ContentTypes: []string{"application/json"},
							Body:         NewOrderedObjectFromMap(map[string]any{"name": map[string]any{}}),
							Query:        NewOrderedObjectFromMap(map[string]any{"page": map[string]any{}}),
						},
						Resources: []Resource{
							{Class: "UserResource", Collection: true},
						},
						ScopesUsed: []ScopeUsed{
							{On: "User", Name: "active", Args: []string{"true"}},
						},
					},
				},
				Models: []Model{
					{
						FQCN: "App\\Models\\User",
						WithPivot: []PivotInfo{
							{
								Relation:   "roles",
								Columns:    []string{"permission_level", "granted_at"},
								Alias:      &[]string{"user_role"}[0],
								Timestamps: &[]bool{true}[0],
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
							IdColumn:   "commentable_id",
						},
						Discriminator: Discriminator{
							PropertyName: "type",
							Mapping: map[string]string{
								"post": "App\\Models\\Post",
								"user": "App\\Models\\User",
							},
						},
						DepthTruncated: &[]bool{false}[0],
					},
				},
				Broadcast: []Broadcast{
					{
						File:           &[]string{"routes/channels.php"}[0],
						Channel:        "user.{id}",
						Params:         []string{"id"},
						Visibility:     "private",
						PayloadLiteral: &[]bool{false}[0],
					},
				},
			},
			verify: func(t *testing.T, d *Delta) {
				if len(d.Controllers) != 1 {
					t.Errorf("expected 1 controller, got %d", len(d.Controllers))
				}
				if len(d.Models) != 1 {
					t.Errorf("expected 1 model, got %d", len(d.Models))
				}
				if len(d.Polymorphic) != 1 {
					t.Errorf("expected 1 polymorphic, got %d", len(d.Polymorphic))
				}
				if len(d.Broadcast) != 1 {
					t.Errorf("expected 1 broadcast, got %d", len(d.Broadcast))
				}

				// Verify controller structure
				controller := d.Controllers[0]
				if controller.FQCN != "App\\Http\\Controllers\\UserController" {
					t.Errorf("expected controller FQCN 'App\\Http\\Controllers\\UserController', got '%s'", controller.FQCN)
				}
				if controller.HTTP == nil || *controller.HTTP.Status != 200 {
					t.Error("expected HTTP status 200")
				}
				if len(controller.Request.ContentTypes) != 1 || controller.Request.ContentTypes[0] != "application/json" {
					t.Error("expected application/json content type")
				}

				// Verify model structure
				model := d.Models[0]
				if model.FQCN != "App\\Models\\User" {
					t.Errorf("expected model FQCN 'App\\Models\\User', got '%s'", model.FQCN)
				}
				if len(model.WithPivot) != 1 {
					t.Errorf("expected 1 pivot, got %d", len(model.WithPivot))
				}
				if len(model.Attributes) != 1 {
					t.Errorf("expected 1 attribute, got %d", len(model.Attributes))
				}

				// Verify polymorphic structure
				poly := d.Polymorphic[0]
				if poly.Parent != "App\\Models\\Comment" {
					t.Errorf("expected polymorphic parent 'App\\Models\\Comment', got '%s'", poly.Parent)
				}
				if len(poly.Discriminator.Mapping) != 2 {
					t.Errorf("expected 2 discriminator mappings, got %d", len(poly.Discriminator.Mapping))
				}

				// Verify broadcast structure
				broadcast := d.Broadcast[0]
				if broadcast.Channel != "user.{id}" {
					t.Errorf("expected broadcast channel 'user.{id}', got '%s'", broadcast.Channel)
				}
				if broadcast.Visibility != "private" {
					t.Errorf("expected broadcast visibility 'private', got '%s'", broadcast.Visibility)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.verify(t, tt.delta)
		})
	}
}

func TestMetaInfo_RequiredFields(t *testing.T) {
	tests := []struct {
		name     string
		meta     MetaInfo
		wantErr  bool
		errField string
	}{
		{
			name: "valid meta info",
			meta: MetaInfo{
				Partial: false,
				Stats: MetaStats{
					FilesParsed: 10,
					Skipped:     2,
					DurationMs:  500,
				},
			},
			wantErr: false,
		},
		{
			name: "partial true with stats",
			meta: MetaInfo{
				Partial: true,
				Stats: MetaStats{
					FilesParsed: 5,
					Skipped:     10,
					DurationMs:  750,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify meta has required fields based on schema
			if tt.meta.Stats.FilesParsed < 0 {
				t.Error("FilesParsed should be non-negative")
			}
			if tt.meta.Stats.Skipped < 0 {
				t.Error("Skipped should be non-negative")
			}
			if tt.meta.Stats.DurationMs < 0 {
				t.Error("DurationMs should be non-negative")
			}
		})
	}
}

func TestController_StructureValidation(t *testing.T) {
	tests := []struct {
		name       string
		controller Controller
		wantValid  bool
	}{
		{
			name: "minimal valid controller",
			controller: Controller{
				FQCN:   "App\\Http\\Controllers\\TestController",
				Method: "show",
			},
			wantValid: true,
		},
		{
			name: "controller with all fields",
			controller: Controller{
				FQCN:   "App\\Http\\Controllers\\CompleteController",
				Method: "index",
				HTTP: &HTTPInfo{
					Status:   &[]int{200}[0],
					Explicit: &[]bool{true}[0],
				},
				Request: &RequestInfo{
					ContentTypes: []string{"application/json", "multipart/form-data"},
					Body:         NewOrderedObjectFromMap(map[string]any{"field": map[string]any{}}),
					Query:        NewOrderedObjectFromMap(map[string]any{"filter": map[string]any{}}),
					Files:        NewOrderedObjectFromMap(map[string]any{"upload": map[string]any{}}),
				},
				Resources: []Resource{
					{Class: "TestResource", Collection: false},
					{Class: "CollectionResource", Collection: true},
				},
				ScopesUsed: []ScopeUsed{
					{On: "Model", Name: "active", Args: []string{"param"}},
				},
			},
			wantValid: true,
		},
		{
			name: "empty FQCN should be invalid",
			controller: Controller{
				FQCN:   "",
				Method: "index",
			},
			wantValid: false,
		},
		{
			name: "empty method should be invalid",
			controller: Controller{
				FQCN:   "App\\Http\\Controllers\\TestController",
				Method: "",
			},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate according to schema requirements
			isValid := len(tt.controller.FQCN) > 0 && len(tt.controller.Method) > 0

			if isValid != tt.wantValid {
				t.Errorf("controller validation = %v, want %v", isValid, tt.wantValid)
			}

			// Additional validation for HTTP status range
			if tt.controller.HTTP != nil && tt.controller.HTTP.Status != nil {
				status := *tt.controller.HTTP.Status
				if status < 100 || status > 599 {
					t.Errorf("HTTP status %d outside valid range 100-599", status)
				}
			}

			// Validate content types are from allowed enum
			if tt.controller.Request != nil && tt.controller.Request.ContentTypes != nil {
				allowedTypes := map[string]bool{
					"application/json":                  true,
					"multipart/form-data":               true,
					"application/x-www-form-urlencoded": true,
				}
				for _, contentType := range tt.controller.Request.ContentTypes {
					if !allowedTypes[contentType] {
						t.Errorf("invalid content type: %s", contentType)
					}
				}
			}
		})
	}
}

func TestModel_ValidationRequirements(t *testing.T) {
	tests := []struct {
		name      string
		model     Model
		wantValid bool
	}{
		{
			name: "minimal valid model",
			model: Model{
				FQCN: "App\\Models\\User",
			},
			wantValid: true,
		},
		{
			name: "model with pivot and attributes",
			model: Model{
				FQCN: "App\\Models\\CompleteModel",
				WithPivot: []PivotInfo{
					{
						Relation: "roles",
						Columns:  []string{"level", "granted_at"},
					},
				},
				Attributes: []Attribute{
					{Name: "computed_field", Via: "Attribute::make"},
				},
			},
			wantValid: true,
		},
		{
			name: "empty FQCN should be invalid",
			model: Model{
				FQCN: "",
			},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := len(tt.model.FQCN) > 0

			if isValid != tt.wantValid {
				t.Errorf("model validation = %v, want %v", isValid, tt.wantValid)
			}

			// Validate pivot info requirements
			for _, pivot := range tt.model.WithPivot {
				if len(pivot.Relation) == 0 {
					t.Error("pivot relation cannot be empty")
				}
				if len(pivot.Columns) == 0 {
					t.Error("pivot columns cannot be empty")
				}
			}

			// Validate attribute requirements
			for _, attr := range tt.model.Attributes {
				if len(attr.Name) == 0 {
					t.Error("attribute name cannot be empty")
				}
				if attr.Via != "Attribute::make" {
					t.Errorf("attribute via must be 'Attribute::make', got '%s'", attr.Via)
				}
			}
		})
	}
}

func TestPolymorphic_ValidationRequirements(t *testing.T) {
	tests := []struct {
		name        string
		polymorphic Polymorphic
		wantValid   bool
	}{
		{
			name: "valid polymorphic configuration",
			polymorphic: Polymorphic{
				Parent: "App\\Models\\Comment",
				Morph: MorphInfo{
					Key:        "commentable",
					TypeColumn: "commentable_type",
					IdColumn:   "commentable_id",
				},
				Discriminator: Discriminator{
					PropertyName: "type",
					Mapping: map[string]string{
						"post": "App\\Models\\Post",
						"user": "App\\Models\\User",
					},
				},
			},
			wantValid: true,
		},
		{
			name: "empty parent should be invalid",
			polymorphic: Polymorphic{
				Parent: "",
				Morph: MorphInfo{
					Key:        "commentable",
					TypeColumn: "commentable_type",
					IdColumn:   "commentable_id",
				},
				Discriminator: Discriminator{
					PropertyName: "type",
					Mapping:      map[string]string{"post": "App\\Models\\Post"},
				},
			},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := len(tt.polymorphic.Parent) > 0 &&
				len(tt.polymorphic.Morph.Key) > 0 &&
				len(tt.polymorphic.Morph.TypeColumn) > 0 &&
				len(tt.polymorphic.Morph.IdColumn) > 0 &&
				len(tt.polymorphic.Discriminator.PropertyName) > 0 &&
				len(tt.polymorphic.Discriminator.Mapping) > 0

			if isValid != tt.wantValid {
				t.Errorf("polymorphic validation = %v, want %v", isValid, tt.wantValid)
			}
		})
	}
}

func TestBroadcast_ValidationRequirements(t *testing.T) {
	tests := []struct {
		name      string
		broadcast Broadcast
		wantValid bool
	}{
		{
			name: "valid broadcast configuration",
			broadcast: Broadcast{
				Channel:    "user.{id}",
				Params:     []string{"id"},
				Visibility: "private",
			},
			wantValid: true,
		},
		{
			name: "valid visibility values",
			broadcast: Broadcast{
				Channel:    "public.notifications",
				Params:     []string{},
				Visibility: "public",
			},
			wantValid: true,
		},
		{
			name: "invalid visibility",
			broadcast: Broadcast{
				Channel:    "test.channel",
				Params:     []string{},
				Visibility: "invalid",
			},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowedVisibility := map[string]bool{
				"public":   true,
				"private":  true,
				"presence": true,
			}

			isValid := len(tt.broadcast.Channel) > 0 && allowedVisibility[tt.broadcast.Visibility]

			if isValid != tt.wantValid {
				t.Errorf("broadcast validation = %v, want %v", isValid, tt.wantValid)
			}
		})
	}
}
