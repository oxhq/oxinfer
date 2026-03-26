package matchers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

func TestNewBroadcastMatcher(t *testing.T) {
	language := php.GetLanguage()

	tests := []struct {
		name        string
		language    *sitter.Language
		config      *MatcherConfig
		expectError bool
		description string
	}{
		{
			name:        "valid_matcher_creation",
			language:    language,
			config:      DefaultMatcherConfig(),
			expectError: false,
			description: "Should create matcher successfully with valid inputs",
		},
		{
			name:        "nil_language",
			language:    nil,
			config:      DefaultMatcherConfig(),
			expectError: true,
			description: "Should fail when language is nil",
		},
		{
			name:        "nil_config_uses_default",
			language:    language,
			config:      nil,
			expectError: false,
			description: "Should use default config when config is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewBroadcastMatcher(tt.language, tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if matcher != nil {
					t.Errorf("Expected nil matcher when error occurs")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if matcher == nil {
					t.Errorf("Expected non-nil matcher when no error")
				} else {
					if !matcher.IsInitialized() {
						t.Errorf("Matcher should be initialized")
					}
					if matcher.GetType() != PatternTypeBroadcast {
						t.Errorf("Expected pattern type %v, got %v", PatternTypeBroadcast, matcher.GetType())
					}
					if len(matcher.GetQueries()) == 0 {
						t.Errorf("Expected queries to be compiled")
					}
				}
			}
		})
	}
}

func TestBroadcastMatcherErrorCases(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewBroadcastMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name        string
		tree        *parser.SyntaxTree
		expectError bool
		description string
	}{
		{
			name:        "nil_tree",
			tree:        nil,
			expectError: true,
			description: "Should handle nil tree gracefully",
		},
		{
			name: "tree_with_nil_root",
			tree: &parser.SyntaxTree{
				Source:   []byte("<?php echo 'test';"),
				Root:     nil,
				Language: "php",
				ParsedAt: time.Now(),
			},
			expectError: true,
			description: "Should handle tree with nil root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			matches, err := matcher.Match(ctx, tt.tree, "routes/channels.php")

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if matches == nil {
					t.Errorf("Expected non-nil matches slice")
				}
			}
		})
	}
}

func TestBroadcastMatcherContextCancellation(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewBroadcastMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tree := &parser.SyntaxTree{
		Source: []byte(`<?php Broadcast::channel('test', function () { return true; });`),
		Root: &parser.SyntaxNode{
			Type: "program",
			Text: `<?php Broadcast::channel('test', function () { return true; });`,
		},
		Language: "php",
		ParsedAt: time.Now(),
	}

	matches, err := matcher.Match(ctx, tree, "routes/channels.php")

	// Should handle cancelled context gracefully
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
	if matches != nil {
		t.Errorf("Expected nil matches when context is cancelled")
	}
}

func TestExtractChannelParameters(t *testing.T) {
	language := php.GetLanguage()
	matcher, err := NewBroadcastMatcher(language, nil)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name        string
		channelName string
		expected    []string
		description string
	}{
		{
			name:        "no_parameters",
			channelName: "notifications",
			expected:    []string{},
			description: "Channel without parameters",
		},
		{
			name:        "single_parameter",
			channelName: "user.{id}",
			expected:    []string{"id"},
			description: "Channel with single parameter",
		},
		{
			name:        "multiple_parameters",
			channelName: "orders.{orderId}.items.{itemId}",
			expected:    []string{"itemId", "orderId"}, // Sorted alphabetically
			description: "Channel with multiple parameters",
		},
		{
			name:        "complex_channel_name",
			channelName: "app.{tenant}.users.{userId}.notifications",
			expected:    []string{"tenant", "userId"}, // Sorted alphabetically
			description: "Complex channel name with parameters",
		},
		{
			name:        "empty_channel",
			channelName: "",
			expected:    []string{},
			description: "Empty channel name",
		},
		{
			name:        "parameter_with_underscore",
			channelName: "chat.{room_id}",
			expected:    []string{"room_id"},
			description: "Parameter with underscore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.extractChannelParameters(tt.channelName)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d parameters, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("Expected param[%d] = %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestExtractStringLiteral(t *testing.T) {
	language := php.GetLanguage()
	matcher, err := NewBroadcastMatcher(language, nil)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single_quotes",
			input:    "'notifications'",
			expected: "notifications",
		},
		{
			name:     "double_quotes",
			input:    `"user.{id}"`,
			expected: "user.{id}",
		},
		{
			name:     "no_quotes",
			input:    "notifications",
			expected: "notifications",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace_padding",
			input:    "  'test'  ",
			expected: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.extractStringLiteral(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBroadcastMatcherInterface(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewBroadcastMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	t.Run("pattern_type_consistency", func(t *testing.T) {
		if matcher.GetType() != PatternTypeBroadcast {
			t.Errorf("Expected pattern type %v, got %v", PatternTypeBroadcast, matcher.GetType())
		}
	})

	t.Run("interface_compliance", func(t *testing.T) {
		// Verify it implements PatternMatcher interface
		var pm PatternMatcher = matcher
		_ = pm // Verify interface compliance
	})

	t.Run("specialized_interface_compliance", func(t *testing.T) {
		// Verify it implements BroadcastMatcher interface
		var bm BroadcastMatcher = matcher
		_ = bm // Verify interface compliance

		// Test specialized method
		tree := &parser.SyntaxTree{
			Source: []byte(`<?php Broadcast::channel('test', function() { return true; });`),
			Root: &parser.SyntaxNode{
				Type: "program",
				Text: `<?php Broadcast::channel('test', function() { return true; });`,
			},
			Language: "php",
			ParsedAt: time.Now(),
		}
		matches, err := bm.MatchBroadcast(context.Background(), tree, "test.php")
		if err != nil {
			t.Errorf("MatchBroadcast should not error on valid input: %v", err)
		}
		if matches == nil {
			t.Errorf("MatchBroadcast should return non-nil slice")
		}
	})
}

func TestBuildDisplayContent(t *testing.T) {
	language := php.GetLanguage()
	matcher, err := NewBroadcastMatcher(language, nil)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name        string
		methodName  string
		channelName string
		expected    string
		description string
	}{
		{
			name:        "public_channel",
			methodName:  "channel",
			channelName: "notifications",
			expected:    "Broadcast::channel('notifications')",
			description: "Public channel display",
		},
		{
			name:        "private_channel",
			methodName:  "private",
			channelName: "user.{id}",
			expected:    "Broadcast::private('user.{id}')",
			description: "Private channel display",
		},
		{
			name:        "no_method_name",
			methodName:  "",
			channelName: "test",
			expected:    "channel('test')",
			description: "Fallback when method name is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.buildDisplayContent(tt.methodName, tt.channelName)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestIsExplicitBroadcastUsage(t *testing.T) {
	language := php.GetLanguage()
	matcher, err := NewBroadcastMatcher(language, nil)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name        string
		patternName string
		expected    bool
	}{
		{
			name:        "broadcast_channel_public",
			patternName: "broadcast_channel_public",
			expected:    true,
		},
		{
			name:        "broadcast_private_channel",
			patternName: "broadcast_private_channel",
			expected:    true,
		},
		{
			name:        "channel_parameter_pattern",
			patternName: "channel_parameter_pattern",
			expected:    false,
		},
		{
			name:        "closure_with_user_param",
			patternName: "closure_with_user_param",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.isExplicitBroadcastUsage(tt.patternName)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestValidateBroadcastChannelCall(t *testing.T) {
	tests := []struct {
		name        string
		methodName  string
		channelName string
		hasCallback bool
		expected    bool
		description string
	}{
		{
			name:        "valid_public_channel",
			methodName:  "channel",
			channelName: "notifications",
			hasCallback: true,
			expected:    true,
			description: "Public channel with callback should be valid",
		},
		{
			name:        "valid_public_channel_no_callback",
			methodName:  "channel",
			channelName: "notifications",
			hasCallback: false,
			expected:    true,
			description: "Public channel without callback should be valid",
		},
		{
			name:        "valid_private_channel",
			methodName:  "private",
			channelName: "user.{id}",
			hasCallback: true,
			expected:    true,
			description: "Private channel with callback should be valid",
		},
		{
			name:        "invalid_private_channel_no_callback",
			methodName:  "private",
			channelName: "user.{id}",
			hasCallback: false,
			expected:    false,
			description: "Private channel without callback should be invalid",
		},
		{
			name:        "empty_channel_name",
			methodName:  "channel",
			channelName: "",
			hasCallback: true,
			expected:    false,
			description: "Empty channel name should be invalid",
		},
		{
			name:        "invalid_method",
			methodName:  "invalid",
			channelName: "test",
			hasCallback: true,
			expected:    false,
			description: "Invalid method name should be invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateBroadcastChannelCall(tt.methodName, tt.channelName, tt.hasCallback)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetSupportedBroadcastPatterns(t *testing.T) {
	patterns := GetSupportedBroadcastPatterns()

	if len(patterns) == 0 {
		t.Errorf("Expected at least one supported pattern")
	}

	// Check that patterns contain expected broadcast method calls
	foundChannel := false
	foundPrivate := false
	foundPresence := false

	for _, pattern := range patterns {
		if strings.Contains(pattern, "Broadcast::channel") {
			foundChannel = true
		}
		if strings.Contains(pattern, "Broadcast::private") {
			foundPrivate = true
		}
		if strings.Contains(pattern, "Broadcast::presence") {
			foundPresence = true
		}
	}

	if !foundChannel {
		t.Errorf("Expected to find channel pattern")
	}
	if !foundPrivate {
		t.Errorf("Expected to find private pattern")
	}
	if !foundPresence {
		t.Errorf("Expected to find presence pattern")
	}
}

func TestGetBroadcastMethodConventions(t *testing.T) {
	conventions := GetBroadcastMethodConventions()

	expectedMethods := []string{"channel", "private", "presence"}

	for _, method := range expectedMethods {
		if _, exists := conventions[method]; !exists {
			t.Errorf("Expected convention for method %q", method)
		}
	}
}

func TestBroadcastMatcher_DeterministicOutput(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewBroadcastMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	sourceCode := `<?php
Broadcast::channel('notifications.{type}', function ($user, $type) {
    return true;
});
Broadcast::private('user.{id}', function ($user, $id) {
    return $user->id == $id;
});
Broadcast::presence('chat.{room}', function ($user, $room) {
    return ['id' => $user->id, 'name' => $user->name];
});
`

	tree := &parser.SyntaxTree{
		Source: []byte(sourceCode),
		Root: &parser.SyntaxNode{
			Type: "program",
			Text: sourceCode,
		},
		Language: "php",
		ParsedAt: time.Now(),
	}

	ctx := context.Background()

	// Run multiple times to check determinism
	var allResults [][]*MatchResult
	for i := 0; i < 5; i++ {
		matches, err := matcher.Match(ctx, tree, "channels.php")
		if err != nil {
			t.Fatalf("Match failed on iteration %d: %v", i, err)
		}
		allResults = append(allResults, matches)
	}

	// Compare all results
	firstResult := allResults[0]
	for i := 1; i < len(allResults); i++ {
		if len(allResults[i]) != len(firstResult) {
			t.Errorf("Iteration %d: expected %d matches, got %d", i, len(firstResult), len(allResults[i]))
			continue
		}

		for j, match := range allResults[i] {
			if j >= len(firstResult) {
				t.Errorf("Iteration %d: unexpected extra match at index %d", i, j)
				continue
			}

			expectedMatch := firstResult[j]
			if match.Content != expectedMatch.Content {
				t.Errorf("Iteration %d, match %d: content differs. Expected %q, got %q", i, j, expectedMatch.Content, match.Content)
			}

			if match.Confidence != expectedMatch.Confidence {
				t.Errorf("Iteration %d, match %d: confidence differs. Expected %f, got %f", i, j, expectedMatch.Confidence, match.Confidence)
			}
		}
	}

	t.Logf("Determinism verified: %d iterations produced identical results", len(allResults))
}
