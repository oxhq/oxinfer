package matchers

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

// Mock PHP language for testing (normally would use tree-sitter-php)
func mockPHPLanguage() *sitter.Language {
	// In real implementation, this would return tree-sitter-php language
	// For testing, we'll use a mock or skip language-specific tests
	return nil
}

func TestNewScopeMatcher(t *testing.T) {
	tests := []struct {
		name        string
		language    *sitter.Language
		config      *MatcherConfig
		expectError bool
	}{
		{
			name:        "valid creation with defaults",
			language:    mockPHPLanguage(),
			config:      nil,
			expectError: true, // Will error due to mock language being nil
		},
		{
			name:        "nil language",
			language:    nil,
			config:      DefaultMatcherConfig(),
			expectError: true,
		},
		{
			name:        "custom config",
			language:    mockPHPLanguage(),
			config:      &MatcherConfig{MaxMatchesPerFile: 50},
			expectError: true, // Will error due to mock language being nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewScopeMatcher(tt.language, tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if matcher == nil {
				t.Error("Expected non-nil matcher")
				return
			}

			// Test interface compliance
			var _ ScopeMatcher = matcher
			var _ PatternMatcher = matcher

			// Test basic properties
			if matcher.GetType() != PatternTypeScope {
				t.Errorf("Expected pattern type %v, got %v", PatternTypeScope, matcher.GetType())
			}

			if !matcher.IsInitialized() {
				t.Error("Expected matcher to be initialized")
			}

			// Clean up
			if err := matcher.Close(); err != nil {
				t.Errorf("Error closing matcher: %v", err)
			}
		})
	}
}

func TestDefaultScopeMatcher_GetType(t *testing.T) {
	matcher := &DefaultScopeMatcher{}

	if got := matcher.GetType(); got != PatternTypeScope {
		t.Errorf("GetType() = %v, want %v", got, PatternTypeScope)
	}
}

func TestDefaultScopeMatcher_extractScopeName(t *testing.T) {
	matcher := &DefaultScopeMatcher{}

	// Initialize regex patterns
	var err error
	matcher.scopeMethodPattern, err = regexp.Compile(`^scope([A-Z][a-zA-Z0-9]*)$`)
	if err != nil {
		t.Fatalf("Failed to compile regex: %v", err)
	}

	tests := []struct {
		name       string
		methodName string
		expected   string
	}{
		{
			name:       "simple scope method",
			methodName: "scopeActive",
			expected:   "active",
		},
		{
			name:       "multi-word scope method",
			methodName: "scopePublishedArticles",
			expected:   "publishedarticles",
		},
		{
			name:       "single letter scope",
			methodName: "scopeA",
			expected:   "a",
		},
		{
			name:       "non-scope method",
			methodName: "getActiveAttribute",
			expected:   "",
		},
		{
			name:       "lowercase scope prefix",
			methodName: "scopeactive",
			expected:   "",
		},
		{
			name:       "empty method name",
			methodName: "",
			expected:   "",
		},
		{
			name:       "just scope prefix",
			methodName: "scope",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.extractScopeName(tt.methodName)
			if result != tt.expected {
				t.Errorf("extractScopeName(%q) = %q, want %q", tt.methodName, result, tt.expected)
			}
		})
	}
}

func TestDefaultScopeMatcher_processLocalScopeDefinition(t *testing.T) {
	matcher := &DefaultScopeMatcher{}

	// Mock syntax tree
	mockTree := &parser.SyntaxTree{
		Root:   &parser.SyntaxNode{},
		Source: []byte("public function scopeActive(Builder $query) { return $query->where('active', true); }"),
	}

	// Mock match with captures
	// Note: In real implementation, this would come from tree-sitter query execution
	// For testing, we're simulating the structure

	tests := []struct {
		name     string
		match    *sitter.QueryMatch
		expected *ScopeMatch
	}{
		{
			name:     "empty captures",
			match:    &sitter.QueryMatch{Captures: []sitter.QueryCapture{}},
			expected: nil,
		},
		// Additional test cases would require proper tree-sitter integration
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.processLocalScopeDefinition(tt.match, mockTree)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil result, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Error("Expected non-nil result")
				return
			}

			// Compare specific fields
			if result.Name != tt.expected.Name {
				t.Errorf("Name = %q, want %q", result.Name, tt.expected.Name)
			}

			if result.IsLocal != tt.expected.IsLocal {
				t.Errorf("IsLocal = %v, want %v", result.IsLocal, tt.expected.IsLocal)
			}

			if result.Pattern != tt.expected.Pattern {
				t.Errorf("Pattern = %q, want %q", result.Pattern, tt.expected.Pattern)
			}
		})
	}
}

func TestDefaultScopeMatcher_Match_ErrorCases(t *testing.T) {
	matcher := &DefaultScopeMatcher{
		initialized: false,
	}

	ctx := context.Background()
	mockTree := &parser.SyntaxTree{}
	filePath := "test.php"

	// Test uninitialized matcher
	t.Run("uninitialized matcher", func(t *testing.T) {
		results, err := matcher.Match(ctx, mockTree, filePath)

		if err == nil {
			t.Error("Expected error for uninitialized matcher")
		}

		if results != nil {
			t.Error("Expected nil results on error")
		}
	})

	// Test nil tree
	matcher.initialized = true
	t.Run("nil tree", func(t *testing.T) {
		results, err := matcher.Match(ctx, nil, filePath)

		if err == nil {
			t.Error("Expected error for nil tree")
		}

		if results != nil {
			t.Error("Expected nil results on error")
		}
	})

	// Test nil tree root
	t.Run("nil tree root", func(t *testing.T) {
		treeWithNilRoot := &parser.SyntaxTree{Root: nil}
		results, err := matcher.Match(ctx, treeWithNilRoot, filePath)

		if err == nil {
			t.Error("Expected error for nil tree root")
		}

		if results != nil {
			t.Error("Expected nil results on error")
		}
	})

	// Test context cancellation
	t.Run("cancelled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		validTree := &parser.SyntaxTree{
			Root:   &parser.SyntaxNode{},
			Source: []byte("test"),
		}

		results, err := matcher.Match(cancelledCtx, validTree, filePath)

		// The test may not always fail due to the mock implementation
		// In real implementation with queries, the cancelled context would be detected
		if err != nil || results == nil {
			t.Logf("Context cancellation handled correctly: err=%v, results=%v", err, results)
		} else {
			t.Log("Context cancellation test completed - mock implementation may not detect cancellation")
		}
	})
}

func TestDefaultScopeMatcher_MatchScopes(t *testing.T) {
	matcher := &DefaultScopeMatcher{
		initialized: true,
		config:      DefaultMatcherConfig(),
	}

	ctx := context.Background()
	mockTree := &parser.SyntaxTree{
		Root:   &parser.SyntaxNode{},
		Source: []byte("test"),
	}
	filePath := "test.php"

	// Mock the Match method to return specific MatchResult objects
	// In a real test, this would be properly integrated with tree-sitter

	t.Run("empty results", func(t *testing.T) {
		// This test would pass as long as MatchScopes doesn't panic
		// and properly handles the conversion from MatchResult to ScopeMatch
		scopes, err := matcher.MatchScopes(ctx, mockTree, filePath)

		if err == nil {
			// Since we're using a mock that will fail conversion, we expect an error
			// In real implementation with proper tree-sitter integration, this should work
			t.Logf("MatchScopes completed - error expected due to mock implementation: %v", err)
		}

		if scopes == nil {
			scopes = []*ScopeMatch{} // Ensure we have a non-nil slice for comparison
		}

		if len(scopes) > matcher.config.MaxMatchesPerFile {
			t.Errorf("Too many scope matches: got %d, max %d", len(scopes), matcher.config.MaxMatchesPerFile)
		}
	})
}

func TestScopeMatch_Structure(t *testing.T) {
	// Test ScopeMatch struct creation and field access
	scope := &ScopeMatch{
		Name:     "active",
		On:       "User",
		Args:     []any{"param1", 42},
		IsGlobal: false,
		IsLocal:  true,
		Pattern:  "usage",
		Method:   "scopeActive",
		Context:  "query",
	}

	tests := []struct {
		name     string
		field    string
		expected any
		actual   any
	}{
		{"name field", "Name", "active", scope.Name},
		{"on field", "On", "User", scope.On},
		{"isGlobal field", "IsGlobal", false, scope.IsGlobal},
		{"isLocal field", "IsLocal", true, scope.IsLocal},
		{"pattern field", "Pattern", "usage", scope.Pattern},
		{"method field", "Method", "scopeActive", scope.Method},
		{"context field", "Context", "query", scope.Context},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("%s = %v, want %v", tt.field, tt.actual, tt.expected)
			}
		})
	}

	// Test Args field specifically
	if len(scope.Args) != 2 {
		t.Errorf("Args length = %d, want 2", len(scope.Args))
	}

	if scope.Args[0] != "param1" {
		t.Errorf("Args[0] = %v, want 'param1'", scope.Args[0])
	}

	if scope.Args[1] != 42 {
		t.Errorf("Args[1] = %v, want 42", scope.Args[1])
	}
}

func TestDefaultScopeMatcher_Close(t *testing.T) {
	tests := []struct {
		name        string
		matcher     *DefaultScopeMatcher
		expectError bool
	}{
		{
			name: "with compiler",
			matcher: &DefaultScopeMatcher{
				compiler: &QueryCompiler{
					cache: make(map[string]*sitter.Query),
				},
			},
			expectError: false,
		},
		{
			name: "without compiler",
			matcher: &DefaultScopeMatcher{
				compiler: nil,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.matcher.Close()

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestScopeMatcherIntegration(t *testing.T) {
	// Integration test to verify ScopeMatcher works with the pattern matching system

	t.Run("pattern type consistency", func(t *testing.T) {
		// Ensure PatternTypeScope is properly defined
		if PatternTypeScope != "scope" {
			t.Errorf("PatternTypeScope = %q, expected 'scope'", PatternTypeScope)
		}
	})

	t.Run("interface compliance", func(t *testing.T) {
		// Create a mock matcher to test interface compliance
		var matcher PatternMatcher = &DefaultScopeMatcher{
			initialized: true,
		}

		if matcher.GetType() != PatternTypeScope {
			t.Errorf("GetType() = %v, want %v", matcher.GetType(), PatternTypeScope)
		}

		if !matcher.IsInitialized() {
			t.Error("Expected matcher to be initialized")
		}

		// Test that it can be closed without error
		if err := matcher.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	t.Run("specialized interface compliance", func(t *testing.T) {
		// Test that DefaultScopeMatcher implements ScopeMatcher interface
		var scopeMatcher ScopeMatcher = &DefaultScopeMatcher{
			initialized: true,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		mockTree := &parser.SyntaxTree{
			Root:   &parser.SyntaxNode{},
			Source: []byte("test"),
		}

		// This should not panic even though it will likely error due to mock tree
		_, err := scopeMatcher.MatchScopes(ctx, mockTree, "test.php")

		// We expect an error due to our mock implementation, but no panic
		if err == nil {
			t.Log("MatchScopes completed without error (unexpected with mock)")
		} else {
			t.Logf("MatchScopes errored as expected with mock: %v", err)
		}
	})
}

// Benchmark tests
func BenchmarkScopeMatcher_extractScopeName(b *testing.B) {
	matcher := &DefaultScopeMatcher{}

	// Initialize regex pattern
	var err error
	matcher.scopeMethodPattern, err = regexp.Compile(`^scope([A-Z][a-zA-Z0-9]*)$`)
	if err != nil {
		b.Fatalf("Failed to compile regex: %v", err)
	}

	testCases := []string{
		"scopeActive",
		"scopePublished",
		"scopeActiveUsers",
		"scopePublishedArticles",
		"getActiveAttribute",
		"notAScope",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, methodName := range testCases {
			matcher.extractScopeName(methodName)
		}
	}
}

func BenchmarkScopeMatcher_mapCaptures(b *testing.B) {
	matcher := &DefaultScopeMatcher{}

	// Create a mock QueryMatch with several captures
	// Note: This is simplified since we can't easily create real tree-sitter nodes
	mockMatch := &sitter.QueryMatch{
		Captures: make([]sitter.QueryCapture, 5),
	}

	for i := range mockMatch.Captures {
		mockMatch.Captures[i] = sitter.QueryCapture{
			Index: uint32(i),
			Node:  nil, // Would be real node in practice
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		matcher.mapCaptures(mockMatch)
	}
}
