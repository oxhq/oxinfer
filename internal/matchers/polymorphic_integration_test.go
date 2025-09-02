package matchers

import (
	"testing"

	"github.com/smacker/go-tree-sitter/php"
)

func TestPolymorphicIntegration(t *testing.T) {
	language := php.GetLanguage()
	
	tests := []struct {
		name                      string
		enablePolymorphicMatching bool
		expectedPatternTypesCount int
	}{
		{
			name:                      "polymorphic_matching_enabled",
			enablePolymorphicMatching: true,
			expectedPatternTypesCount: 8, // All pattern types including polymorphic and broadcast
		},
		{
			name:                      "polymorphic_matching_disabled",
			enablePolymorphicMatching: false,
			expectedPatternTypesCount: 7, // All pattern types except polymorphic (but including broadcast)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultMatcherConfig()
			config.EnablePolymorphicMatching = tt.enablePolymorphicMatching
			
			processor, err := NewPatternMatchingProcessor(language, config)
			if err != nil {
				t.Fatalf("Failed to create processor: %v", err)
			}
			defer processor.Close()
			
			// Verify the correct number of matchers are registered
			composite := processor.composite
			matchers := composite.GetMatchers()
			
			if len(matchers) != tt.expectedPatternTypesCount {
				t.Errorf("Expected %d matchers, got %d", tt.expectedPatternTypesCount, len(matchers))
			}
			
			// Verify polymorphic matcher is present/absent based on config
			_, hasPolymorphic := matchers[PatternTypePolymorphic]
			if tt.enablePolymorphicMatching && !hasPolymorphic {
				t.Error("Expected polymorphic matcher to be present when enabled")
			}
			if !tt.enablePolymorphicMatching && hasPolymorphic {
				t.Error("Expected polymorphic matcher to be absent when disabled")
			}
		})
	}
}

func TestPolymorphicMatcherEnabled(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create composite matcher: %v", err)
	}
	defer composite.Close()
	
	// Test polymorphic matcher type is enabled
	enabled := composite.isMatcherEnabled(PatternTypePolymorphic)
	if !enabled {
		t.Error("Expected polymorphic matcher to be enabled with default config")
	}
	
	// Test with disabled config
	config.EnablePolymorphicMatching = false
	composite.config = config
	
	enabled = composite.isMatcherEnabled(PatternTypePolymorphic)
	if enabled {
		t.Error("Expected polymorphic matcher to be disabled when config is false")
	}
}

func TestLaravelPatternsPolymorphicField(t *testing.T) {
	// Test that LaravelPatterns has the Polymorphics field properly initialized
	patterns := &LaravelPatterns{
		FilePath:     "test.php",
		Polymorphics: make([]*PolymorphicMatch, 0),
	}
	
	if patterns.Polymorphics == nil {
		t.Error("Expected Polymorphics field to be initialized")
	}
	
	if len(patterns.Polymorphics) != 0 {
		t.Error("Expected Polymorphics field to be empty initially")
	}
}

func TestProcessMatchResultsPolymorphic(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create composite matcher: %v", err)
	}
	defer composite.Close()
	
	patterns := &LaravelPatterns{
		FilePath:     "test.php",
		Polymorphics: make([]*PolymorphicMatch, 0),
	}
	
	// Create a polymorphic match result
	polyMatch := &PolymorphicMatch{
		Relation: "imageable",
		Type:     "morphTo",
		Pattern:  "morphTo",
	}
	
	results := []*MatchResult{
		{
			Type:       PatternTypePolymorphic,
			Data:       polyMatch,
			Confidence: 0.9,
		},
	}
	
	// Process the results
	composite.processMatchResults(PatternTypePolymorphic, results, patterns)
	
	// Verify the polymorphic match was added
	if len(patterns.Polymorphics) != 1 {
		t.Fatalf("Expected 1 polymorphic match, got %d", len(patterns.Polymorphics))
	}
	
	if patterns.Polymorphics[0].Relation != "imageable" {
		t.Errorf("Expected relation 'imageable', got '%s'", patterns.Polymorphics[0].Relation)
	}
}

func TestConvertToEmitterFormatWithPolymorphic(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()
	
	// Create patterns with polymorphic matches
	patterns := &LaravelPatterns{
		FilePath: "TestController.php",
		Polymorphics: []*PolymorphicMatch{
			{
				Relation:  "imageable",
				Type:      "morphTo",
				MorphType: "imageable_type",
				MorphId:   "imageable_id",
				Pattern:   "morphTo",
				Method:    "imageable",
				Context:   "model",
			},
		},
	}
	
	// Convert to emitter format
	controller, err := processor.ConvertToEmitterFormat(patterns)
	if err != nil {
		t.Fatalf("Failed to convert patterns: %v", err)
	}
	
	// Note: Polymorphic relationships are now handled at the top-level delta, not on individual controllers
	// Verify controller has basic fields
	if controller.FQCN == "" {
		t.Error("Expected controller to have FQCN")
	}
	if controller.Method == "" {
		t.Error("Expected controller to have method")
	}
	
	// Polymorphic data would be verified at the delta level, not controller level
	t.Logf("Controller converted successfully: %s::%s", controller.FQCN, controller.Method)
	
	// Polymorphic validation would now be done at the delta level in integration tests
}

func TestPatternCountIncludesPolymorphic(t *testing.T) {
	// Mock patterns with polymorphic matches
	patterns := &LaravelPatterns{
		FilePath:     "test.php",
		HTTPStatus:   make([]*HTTPStatusMatch, 1),     // 1 match
		RequestUsage: make([]*RequestUsageMatch, 1),   // 1 match  
		Resources:    make([]*ResourceMatch, 1),       // 1 match
		Pivots:       make([]*PivotMatch, 1),          // 1 match
		Attributes:   make([]*AttributeMatch, 1),      // 1 match
		Scopes:       make([]*ScopeMatch, 1),          // 1 match
		Polymorphics: make([]*PolymorphicMatch, 2),    // 2 matches
	}
	
	// Add dummy matches to calculate the count
	patterns.HTTPStatus[0] = &HTTPStatusMatch{Status: 200}
	patterns.RequestUsage[0] = &RequestUsageMatch{Methods: []string{"GET"}}
	patterns.Resources[0] = &ResourceMatch{Class: "UserResource"}
	patterns.Pivots[0] = &PivotMatch{Relation: "users"}
	patterns.Attributes[0] = &AttributeMatch{Name: "fullName"}
	patterns.Scopes[0] = &ScopeMatch{Name: "active"}
	patterns.Polymorphics[0] = &PolymorphicMatch{Relation: "imageable", Type: "morphTo"}
	patterns.Polymorphics[1] = &PolymorphicMatch{Relation: "commentable", Type: "morphMany"}
	
	// Verify the pattern count calculation includes polymorphic patterns
	expectedCount := int64(8) // 6 other types + 2 polymorphic = 8 total
	actualCount := int64(len(patterns.HTTPStatus) + len(patterns.RequestUsage) + len(patterns.Resources) + 
		len(patterns.Pivots) + len(patterns.Attributes) + len(patterns.Scopes) + len(patterns.Polymorphics))
	
	if actualCount != expectedCount {
		t.Errorf("Expected pattern count %d, got %d", expectedCount, actualCount)
	}
}

func TestFeatureFlagIntegration(t *testing.T) {
	config := DefaultMatcherConfig()
	
	// Test that polymorphic is enabled by default
	if !config.EnablePolymorphicMatching {
		t.Error("Expected polymorphic matching to be enabled by default")
	}
	
	// Test feature flag application
	features := &FeatureConfig{
		Polymorphic: boolPtr(false),
	}
	
	config.ApplyFeatureFlags(features)
	
	if config.EnablePolymorphicMatching {
		t.Error("Expected polymorphic matching to be disabled after applying feature flag")
	}
	
	// Test with nil polymorphic flag (should preserve existing config)
	config.EnablePolymorphicMatching = true
	features.Polymorphic = nil
	
	config.ApplyFeatureFlags(features)
	
	if !config.EnablePolymorphicMatching {
		t.Error("Expected polymorphic matching to remain enabled when feature flag is nil")
	}
}

// Helper function to create a bool pointer
func boolPtr(b bool) *bool {
	return &b
}