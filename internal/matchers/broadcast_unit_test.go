package matchers

import (
	"strings"
	"testing"

	"github.com/smacker/go-tree-sitter/php"
)

// TestBroadcastMatcherUnit focuses on unit testing the core broadcast matcher functions
// without requiring full tree-sitter parsing integration.

func TestBroadcastMatcher_ExtractChannelParameters(t *testing.T) {
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
		{
			name:        "parameter_with_numbers",
			channelName: "event.{event123}",
			expected:    []string{"event123"},
			description: "Parameter with numbers",
		},
		{
			name:        "malformed_parameters_ignored",
			channelName: "test.{}.invalid.{validParam}",
			expected:    []string{"validParam"},
			description: "Malformed empty braces should be ignored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.extractChannelParameters(tt.channelName)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d parameters, got %d", len(tt.expected), len(result))
				t.Logf("Expected: %v, Got: %v", tt.expected, result)
			}

			for i, expected := range tt.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("Expected param[%d] = %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

func TestBroadcastMatcher_ExtractStringLiteral(t *testing.T) {
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
		{
			name:     "empty_quoted_string",
			input:    "''",
			expected: "",
		},
		{
			name:     "complex_channel_quoted",
			input:    "'orders.{id}.comments.{commentId}'",
			expected: "orders.{id}.comments.{commentId}",
		},
		{
			name:     "mixed_quotes_single_wins",
			input:    "'test\"value'",
			expected: "test\"value",
		},
		{
			name:     "partial_quotes",
			input:    "'incomplete",
			expected: "'incomplete",
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

func TestBroadcastMatcher_BuildBroadcastMatch(t *testing.T) {
	language := php.GetLanguage()
	matcher, err := NewBroadcastMatcher(language, nil)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	queryDef := QueryDefinition{
		Name:        "test_pattern",
		Description: "Test pattern",
		Pattern:     "test",
		Confidence:  0.9,
	}

	tests := []struct {
		name               string
		methodName         string
		channelName        string
		expectedVisibility string
		expectedParams     []string
		description        string
	}{
		{
			name:               "public_channel",
			methodName:         "channel",
			channelName:        "notifications",
			expectedVisibility: "public",
			expectedParams:     []string{},
			description:        "Public channel should have public visibility",
		},
		{
			name:               "private_channel",
			methodName:         "private",
			channelName:        "user.{id}",
			expectedVisibility: "private",
			expectedParams:     []string{"id"},
			description:        "Private channel should have private visibility and extract parameters",
		},
		{
			name:               "presence_channel",
			methodName:         "presence",
			channelName:        "chat.{room}",
			expectedVisibility: "presence",
			expectedParams:     []string{"room"},
			description:        "Presence channel should have presence visibility and extract parameters",
		},
		{
			name:               "unknown_method_defaults_public",
			methodName:         "unknown",
			channelName:        "test",
			expectedVisibility: "public",
			expectedParams:     []string{},
			description:        "Unknown method should default to public visibility",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := matcher.buildBroadcastMatch(tt.methodName, tt.channelName, queryDef, "test.php")

			if match.Channel != tt.channelName {
				t.Errorf("Expected channel %q, got %q", tt.channelName, match.Channel)
			}

			if match.Method != tt.methodName {
				t.Errorf("Expected method %q, got %q", tt.methodName, match.Method)
			}

			if match.Visibility != tt.expectedVisibility {
				t.Errorf("Expected visibility %q, got %q", tt.expectedVisibility, match.Visibility)
			}

			if len(match.Params) != len(tt.expectedParams) {
				t.Errorf("Expected %d params, got %d", len(tt.expectedParams), len(match.Params))
			} else {
				for i, param := range tt.expectedParams {
					if i >= len(match.Params) || match.Params[i] != param {
						t.Errorf("Expected param[%d] = %q, got %q", i, param, match.Params[i])
					}
				}
			}

			if match.Pattern != queryDef.Name {
				t.Errorf("Expected pattern %q, got %q", queryDef.Name, match.Pattern)
			}

			if match.File != "test.php" {
				t.Errorf("Expected file %q, got %q", "test.php", match.File)
			}
		})
	}
}

func TestBroadcastMatcher_BuildDisplayContent(t *testing.T) {
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
			name:        "private_channel_with_param",
			methodName:  "private",
			channelName: "user.{id}",
			expected:    "Broadcast::private('user.{id}')",
			description: "Private channel with parameter display",
		},
		{
			name:        "presence_channel",
			methodName:  "presence",
			channelName: "chat.{room}",
			expected:    "Broadcast::presence('chat.{room}')",
			description: "Presence channel display",
		},
		{
			name:        "no_method_name",
			methodName:  "",
			channelName: "test",
			expected:    "channel('test')",
			description: "Fallback when method name is empty",
		},
		{
			name:        "empty_channel_name",
			methodName:  "channel",
			channelName: "",
			expected:    "Broadcast::channel('')",
			description: "Display with empty channel name",
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

func TestBroadcastMatcher_IsExplicitBroadcastUsage(t *testing.T) {
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
			name:        "broadcast_channel_public_explicit",
			patternName: "broadcast_channel_public",
			expected:    true,
		},
		{
			name:        "broadcast_private_channel_explicit",
			patternName: "broadcast_private_channel",
			expected:    true,
		},
		{
			name:        "broadcast_presence_channel_explicit",
			patternName: "broadcast_presence_channel",
			expected:    true,
		},
		{
			name:        "broadcast_facade_call_explicit",
			patternName: "broadcast_facade_call",
			expected:    true,
		},
		{
			name:        "channel_parameter_pattern_not_explicit",
			patternName: "channel_parameter_pattern",
			expected:    false,
		},
		{
			name:        "closure_with_user_param_not_explicit",
			patternName: "closure_with_user_param",
			expected:    false,
		},
		{
			name:        "return_auth_check_not_explicit",
			patternName: "return_auth_check",
			expected:    false,
		},
		{
			name:        "unknown_pattern_not_explicit",
			patternName: "unknown_pattern",
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

func TestBroadcastMatcher_IsChannelParameterQuery(t *testing.T) {
	language := php.GetLanguage()
	matcher, err := NewBroadcastMatcher(language, nil)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name      string
		queryName string
		expected  bool
	}{
		{
			name:      "channel_parameter_pattern",
			queryName: "channel_parameter_pattern",
			expected:  true,
		},
		{
			name:      "broadcast_channel_public",
			queryName: "broadcast_channel_public",
			expected:  false,
		},
		{
			name:      "unknown_query",
			queryName: "unknown_query",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.isChannelParameterQuery(tt.queryName)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestBroadcastMatcher_ValidationFunctions(t *testing.T) {
	tests := []struct {
		name        string
		methodName  string
		channelName string
		hasCallback bool
		expected    bool
		description string
	}{
		{
			name:        "valid_public_channel_with_callback",
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
			name:        "valid_private_channel_with_callback",
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
			name:        "valid_presence_channel_with_callback",
			methodName:  "presence",
			channelName: "chat.{room}",
			hasCallback: true,
			expected:    true,
			description: "Presence channel with callback should be valid",
		},
		{
			name:        "invalid_presence_channel_no_callback",
			methodName:  "presence",
			channelName: "chat.{room}",
			hasCallback: false,
			expected:    false,
			description: "Presence channel without callback should be invalid",
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

func TestBroadcastMatcher_UtilityFunctions(t *testing.T) {
	t.Run("get_supported_patterns", func(t *testing.T) {
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
	})

	t.Run("get_method_conventions", func(t *testing.T) {
		conventions := GetBroadcastMethodConventions()

		expectedMethods := []string{"channel", "private", "presence"}

		for _, method := range expectedMethods {
			if _, exists := conventions[method]; !exists {
				t.Errorf("Expected convention for method %q", method)
			}
		}
	})
}

func TestBroadcastMatcher_InterfaceCompliance(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewBroadcastMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	t.Run("pattern_matcher_interface", func(t *testing.T) {
		// Verify it implements PatternMatcher interface
		var pm PatternMatcher = matcher
		_ = pm // Verify interface compliance

		// Test interface methods
		if matcher.GetType() != PatternTypeBroadcast {
			t.Errorf("Expected pattern type %v, got %v", PatternTypeBroadcast, matcher.GetType())
		}

		if !matcher.IsInitialized() {
			t.Errorf("Matcher should be initialized")
		}

		queries := matcher.GetQueries()
		if len(queries) == 0 {
			t.Errorf("Expected compiled queries")
		}
	})

	t.Run("broadcast_matcher_interface", func(t *testing.T) {
		// Verify it implements BroadcastMatcher interface
		var bm BroadcastMatcher = matcher
		_ = bm // Verify interface compliance
	})
}
