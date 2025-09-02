package matchers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/parser"
	"github.com/smacker/go-tree-sitter/php"
)

// TestBroadcastMatcherIntegration tests integration with the broader matcher ecosystem.
func TestBroadcastMatcherIntegration(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()

	t.Run("broadcast_matcher_in_config", func(t *testing.T) {
		// Verify broadcast matching is enabled in default config
		if !config.EnableBroadcastMatching {
			t.Errorf("Expected broadcast matching to be enabled in default config")
		}
	})

	t.Run("broadcast_pattern_type_defined", func(t *testing.T) {
		// Verify the broadcast pattern type constant is defined
		if PatternTypeBroadcast == "" {
			t.Errorf("Expected broadcast pattern type to be defined")
		}
		
		expected := PatternType("broadcast")
		if PatternTypeBroadcast != expected {
			t.Errorf("Expected pattern type %q, got %q", expected, PatternTypeBroadcast)
		}
	})

	t.Run("broadcast_queries_compilation", func(t *testing.T) {
		// Test that broadcast queries compile successfully
		compiler := NewQueryCompiler(language)
		defer compiler.Close()

		queries, err := compiler.CompileQueries(BroadcastUsageQueries)
		if err != nil {
			t.Errorf("Failed to compile broadcast queries: %v", err)
		}

		if len(queries) != len(BroadcastUsageQueries) {
			t.Errorf("Expected %d compiled queries, got %d", len(BroadcastUsageQueries), len(queries))
		}

		// Verify each query compiled successfully
		for i, query := range queries {
			if query == nil {
				t.Errorf("Query %d failed to compile", i)
			}
		}
	})

	t.Run("matcher_integration_with_patterns", func(t *testing.T) {
		matcher, err := NewBroadcastMatcher(language, config)
		if err != nil {
			t.Fatalf("Failed to create broadcast matcher: %v", err)
		}
		defer matcher.Close()

		// Create a LaravelPatterns structure and verify broadcast field exists
		patterns := &LaravelPatterns{
			FilePath:     "test.php",
			Broadcasts:   []*BroadcastMatch{},
			ProcessedAt:  time.Now().Unix(),
			ProcessingMs: 100,
		}

		// Verify the broadcast field can be populated
		testMatch := &BroadcastMatch{
			Channel:    "test",
			Visibility: "public",
			Method:     "channel",
			Params:     []string{},
			Pattern:    "test_pattern",
			File:       "test.php",
		}

		patterns.Broadcasts = append(patterns.Broadcasts, testMatch)

		if len(patterns.Broadcasts) != 1 {
			t.Errorf("Expected 1 broadcast match, got %d", len(patterns.Broadcasts))
		}

		if patterns.Broadcasts[0].Channel != "test" {
			t.Errorf("Expected channel 'test', got %q", patterns.Broadcasts[0].Channel)
		}
	})

	t.Run("feature_config_integration", func(t *testing.T) {
		// Test that broadcast feature flag integration works
		featureConfig := &FeatureConfig{
			BroadcastChannels: boolPtr(false), // Disable broadcast matching
		}

		testConfig := DefaultMatcherConfig()
		testConfig.ApplyFeatureFlags(featureConfig)

		if testConfig.EnableBroadcastMatching {
			t.Errorf("Expected broadcast matching to be disabled after applying feature flags")
		}

		// Test enabling
		featureConfig.BroadcastChannels = boolPtr(true)
		testConfig.ApplyFeatureFlags(featureConfig)

		if !testConfig.EnableBroadcastMatching {
			t.Errorf("Expected broadcast matching to be enabled after applying feature flags")
		}
	})

	t.Run("query_definition_lookup", func(t *testing.T) {
		// Test that broadcast query lookup functions work
		queryName := "broadcast_channel_public"
		queryDef, found := GetBroadcastUsageQuery(queryName)
		
		if !found {
			t.Errorf("Expected to find query %q", queryName)
		}
		
		if queryDef.Name != queryName {
			t.Errorf("Expected query name %q, got %q", queryName, queryDef.Name)
		}
		
		if queryDef.Pattern == "" {
			t.Errorf("Expected non-empty pattern for query %q", queryName)
		}
		
		if queryDef.Confidence <= 0 {
			t.Errorf("Expected positive confidence for query %q, got %f", queryName, queryDef.Confidence)
		}

		// Test non-existent query
		_, found = GetBroadcastUsageQuery("non_existent_query")
		if found {
			t.Errorf("Expected not to find non-existent query")
		}
	})

	t.Run("match_result_structure", func(t *testing.T) {
		// Test that MatchResult can hold BroadcastMatch data
		broadcastMatch := &BroadcastMatch{
			Channel:    "orders.{id}",
			Visibility: "private",
			Method:     "private",
			Params:     []string{"id"},
			Pattern:    "broadcast_private_channel",
			File:       "channels.php",
		}

		matchResult := &MatchResult{
			Type:       PatternTypeBroadcast,
			Position:   parser.Point{Row: 10, Column: 5},
			Content:    "Broadcast::private('orders.{id}')",
			Confidence: 1.0,
			Data:       broadcastMatch,
			Context: &MatchContext{
				FilePath: "routes/channels.php",
				Explicit: true,
			},
		}

		// Verify the structure
		if matchResult.Type != PatternTypeBroadcast {
			t.Errorf("Expected pattern type %v, got %v", PatternTypeBroadcast, matchResult.Type)
		}

		// Verify data can be type asserted back to BroadcastMatch
		if extractedMatch, ok := matchResult.Data.(*BroadcastMatch); ok {
			if extractedMatch.Channel != "orders.{id}" {
				t.Errorf("Expected channel 'orders.{id}', got %q", extractedMatch.Channel)
			}
			if len(extractedMatch.Params) != 1 || extractedMatch.Params[0] != "id" {
				t.Errorf("Expected params [\"id\"], got %v", extractedMatch.Params)
			}
		} else {
			t.Errorf("Failed to type assert MatchResult.Data to *BroadcastMatch")
		}
	})

	t.Run("interface_compliance_comprehensive", func(t *testing.T) {
		matcher, err := NewBroadcastMatcher(language, config)
		if err != nil {
			t.Fatalf("Failed to create matcher: %v", err)
		}
		defer matcher.Close()

		// Test PatternMatcher interface compliance
		var pm PatternMatcher = matcher
		if pm.GetType() != PatternTypeBroadcast {
			t.Errorf("PatternMatcher.GetType() = %v, want %v", pm.GetType(), PatternTypeBroadcast)
		}

		if !pm.IsInitialized() {
			t.Errorf("PatternMatcher should be initialized")
		}

		queries := pm.GetQueries()
		if len(queries) == 0 {
			t.Errorf("PatternMatcher should have compiled queries")
		}

		// Test BroadcastMatcher interface compliance
		var bm BroadcastMatcher = matcher

		// Create minimal syntax tree for testing
		tree := &parser.SyntaxTree{
			Source:   []byte("<?php // test"),
			Root:     &parser.SyntaxNode{Type: "program"},
			Language: "php",
			ParsedAt: time.Now(),
		}

		ctx := context.Background()
		broadcastMatches, err := bm.MatchBroadcast(ctx, tree, "test.php")
		if err != nil {
			// This is expected since we're using a minimal mock tree
			t.Logf("MatchBroadcast returned error (expected with mock tree): %v", err)
		} else {
			// If no error, verify result type
			if broadcastMatches == nil {
				t.Errorf("MatchBroadcast should return non-nil slice")
			}
		}
	})

	t.Run("end_to_end_broadcast_pipeline", func(t *testing.T) {
		// Test the complete pipeline: patterns → matcher → processor → emitter format
		testCases := []struct {
			name     string
			phpCode  string
			expected []string // expected channel names
		}{
			{
				name: "basic_broadcast_channels",
				phpCode: `<?php
				Broadcast::channel('orders.{id}', function ($user, $id) {
					return $user->id === Order::find($id)->user_id;
				});
				
				Broadcast::private('chat.{roomId}', function ($user, $roomId) {
					return $user->canAccessRoom($roomId);
				});`,
				expected: []string{"orders.{id}", "chat.{roomId}"},
			},
			{
				name: "routes_channels_php",
				phpCode: `<?php
				channel('notifications', function () {
					return true;
				});
				
				private('user.{id}', function ($user, $id) {
					return (int) $user->id === (int) $id;
				});`,
				expected: []string{"notifications", "user.{id}"},
			},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				// 1. Parse PHP code
				phpParser, err := parser.NewPHPParser(nil)
				if err != nil {
					t.Fatalf("Failed to create parser: %v", err)
				}
				defer phpParser.Close()

				syntaxTree, err := phpParser.ParseContent([]byte(testCase.phpCode))
				if err != nil {
					t.Fatalf("Failed to parse PHP: %v", err)
				}

				// 2. Create pattern matching processor with broadcast enabled
				config := DefaultMatcherConfig()
				config.EnableBroadcastMatching = true
				processor, err := NewPatternMatchingProcessor(language, config)
				if err != nil {
					t.Fatalf("Failed to create processor: %v", err)
				}
				defer processor.Close()

				// 3. Process file to get patterns
				ctx := context.Background()
				patterns, err := processor.ProcessFile(ctx, syntaxTree, "test.php")
				if err != nil {
					t.Fatalf("Failed to process file: %v", err)
				}

				// 4. Verify broadcast patterns were detected
				if len(patterns.Broadcasts) == 0 {
					t.Fatalf("No broadcast patterns detected, expected %d", len(testCase.expected))
				}

				// 5. Convert to emitter format
				broadcasts, err := processor.ConvertToBroadcastFormat(patterns)
				if err != nil {
					t.Fatalf("Failed to convert to broadcast format: %v", err)
				}

				// 6. Verify we have at least the expected number of broadcasts
				// (might detect more due to multiple query patterns matching)
				if len(broadcasts) < len(testCase.expected) {
					t.Errorf("Expected at least %d broadcasts, got %d", len(testCase.expected), len(broadcasts))
				}

				// Verify channel names are correct
				foundChannels := make(map[string]bool)
				for _, broadcast := range broadcasts {
					foundChannels[broadcast.Channel] = true
				}

				for _, expectedChannel := range testCase.expected {
					if !foundChannels[expectedChannel] {
						t.Errorf("Expected channel %q not found in broadcasts", expectedChannel)
					}
				}

				// 7. Verify deterministic output (channels should be sorted)
				for i := 1; i < len(broadcasts); i++ {
					if broadcasts[i-1].Channel > broadcasts[i].Channel {
						t.Errorf("Broadcasts not sorted: %q comes after %q", broadcasts[i-1].Channel, broadcasts[i].Channel)
					}
				}

				// 8. Verify visibility is correctly detected
				for _, broadcast := range broadcasts {
					if broadcast.Visibility == "" {
						t.Errorf("Broadcast %q has empty visibility", broadcast.Channel)
					}
				}
			})
		}
	})

	t.Run("feature_flag_integration", func(t *testing.T) {
		// Test that feature flags from manifest work correctly
		phpCode := `<?php
		Broadcast::channel('test.channel', function () {
			return true;
		});`

		phpParser, err := parser.NewPHPParser(nil)
		if err != nil {
			t.Fatalf("Failed to create parser: %v", err)
		}
		defer phpParser.Close()

		syntaxTree, err := phpParser.ParseContent([]byte(phpCode))
		if err != nil {
			t.Fatalf("Failed to parse PHP: %v", err)
		}

		// Test with broadcast matching disabled
		configDisabled := DefaultMatcherConfig()
		configDisabled.EnableBroadcastMatching = false
		
		processorDisabled, err := NewPatternMatchingProcessor(language, configDisabled)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processorDisabled.Close()

		ctx := context.Background()
		patternsDisabled, err := processorDisabled.ProcessFile(ctx, syntaxTree, "test.php")
		if err != nil {
			t.Fatalf("Failed to process file: %v", err)
		}

		// Should have no broadcast patterns when disabled
		if len(patternsDisabled.Broadcasts) != 0 {
			t.Errorf("Expected no broadcast patterns when disabled, got %d", len(patternsDisabled.Broadcasts))
		}

		// Test with broadcast matching enabled
		configEnabled := DefaultMatcherConfig()
		configEnabled.EnableBroadcastMatching = true
		
		processorEnabled, err := NewPatternMatchingProcessor(language, configEnabled)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processorEnabled.Close()

		patternsEnabled, err := processorEnabled.ProcessFile(ctx, syntaxTree, "test.php")
		if err != nil {
			t.Fatalf("Failed to process file: %v", err)
		}

		// Should have broadcast patterns when enabled
		if len(patternsEnabled.Broadcasts) == 0 {
			t.Errorf("Expected broadcast patterns when enabled, got none")
		}
	})
}

// boolPtr helper function is defined in polymorphic_integration_test.go

// TestBroadcastMatcherMemoryManagement tests that the matcher properly manages memory.
func TestBroadcastMatcherMemoryManagement(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()

	// Create and close multiple matchers to test for memory leaks
	for i := 0; i < 10; i++ {
		matcher, err := NewBroadcastMatcher(language, config)
		if err != nil {
			t.Fatalf("Iteration %d: Failed to create matcher: %v", i, err)
		}

		// Verify it's initialized
		if !matcher.IsInitialized() {
			t.Errorf("Iteration %d: Matcher not initialized", i)
		}

		// Close the matcher
		if err := matcher.Close(); err != nil {
			t.Errorf("Iteration %d: Error closing matcher: %v", i, err)
		}

		// Verify it's no longer initialized after closing
		if matcher.IsInitialized() {
			t.Errorf("Iteration %d: Matcher should not be initialized after closing", i)
		}
	}
}

// TestBroadcastMatcherConcurrency tests basic thread safety of matcher creation.
func TestBroadcastMatcherConcurrency(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()

	// Test concurrent matcher creation
	const numGoroutines = 5
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			matcher, err := NewBroadcastMatcher(language, config)
			if err != nil {
				errors <- err
				return
			}
			defer matcher.Close()

			if !matcher.IsInitialized() {
				errors <- fmt.Errorf("goroutine %d: matcher not initialized", id)
				return
			}

			errors <- nil
		}(i)
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		if err := <-errors; err != nil {
			t.Errorf("Concurrent creation failed: %v", err)
		}
	}
}