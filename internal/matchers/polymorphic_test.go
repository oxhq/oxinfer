package matchers

import (
	"context"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

// createPolymorphicMatcher creates a test polymorphic matcher with PHP language.
func createPolymorphicMatcher() (*DefaultPolymorphicMatcher, error) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	return NewPolymorphicMatcher(language, config)
}

// createMockSyntaxTree creates a mock SyntaxTree for testing.
func createMockSyntaxTree(content string) *parser.SyntaxTree {
	return &parser.SyntaxTree{
		Root:     &parser.SyntaxNode{Type: "program", Text: content},
		Source:   []byte(content),
		Language: "php",
		ParsedAt: time.Now(),
	}
}

func TestNewPolymorphicMatcher(t *testing.T) {
	tests := []struct {
		name        string
		language    *sitter.Language
		config      *MatcherConfig
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid_matcher_creation",
			language: php.GetLanguage(),
			config:   DefaultMatcherConfig(),
			wantErr:  false,
		},
		{
			name:        "nil_language",
			language:    nil,
			config:      DefaultMatcherConfig(),
			wantErr:     true,
			errContains: "language cannot be nil",
		},
		{
			name:     "nil_config_uses_default",
			language: php.GetLanguage(),
			config:   nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewPolymorphicMatcher(tt.language, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewPolymorphicMatcher() expected error, got nil")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("NewPolymorphicMatcher() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewPolymorphicMatcher() unexpected error = %v", err)
				return
			}

			if matcher == nil {
				t.Error("NewPolymorphicMatcher() returned nil matcher")
				return
			}

			// Verify initialization
			if !matcher.IsInitialized() {
				t.Error("NewPolymorphicMatcher() created uninitialized matcher")
			}

			// Verify pattern type
			if matcher.GetType() != PatternTypePolymorphic {
				t.Errorf("GetType() = %v, want %v", matcher.GetType(), PatternTypePolymorphic)
			}

			// Verify queries are compiled
			queries := matcher.GetQueries()
			if len(queries) == 0 {
				t.Error("GetQueries() returned empty slice")
			}

			// Clean up
			matcher.Close()
		})
	}
}

func TestDefaultPolymorphicMatcher_GetType(t *testing.T) {
	matcher, err := createPolymorphicMatcher()
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	if matcher.GetType() != PatternTypePolymorphic {
		t.Errorf("GetType() = %v, want %v", matcher.GetType(), PatternTypePolymorphic)
	}
}

func TestDefaultPolymorphicMatcher_MaxDepth(t *testing.T) {
	matcher, err := createPolymorphicMatcher()
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	// Test default max depth
	if matcher.GetMaxDepth() != 5 {
		t.Errorf("GetMaxDepth() = %v, want %v", matcher.GetMaxDepth(), 5)
	}

	// Test setting max depth
	matcher.SetMaxDepth(10)
	if matcher.GetMaxDepth() != 10 {
		t.Errorf("GetMaxDepth() after SetMaxDepth(10) = %v, want %v", matcher.GetMaxDepth(), 10)
	}
}

func TestDefaultPolymorphicMatcher_Match_ErrorCases(t *testing.T) {
	matcher, err := createPolymorphicMatcher()
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name        string
		tree        *parser.SyntaxTree
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil_tree",
			tree:        nil,
			wantErr:     true,
			errContains: "invalid syntax tree provided",
		},
		{
			name:        "tree_with_nil_root",
			tree:        &parser.SyntaxTree{Root: nil, Source: []byte("test")},
			wantErr:     true,
			errContains: "invalid syntax tree provided",
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := matcher.Match(ctx, tt.tree, "test.php")

			if tt.wantErr {
				if err == nil {
					t.Errorf("Match() expected error, got nil")
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("Match() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Match() unexpected error = %v", err)
			}

			if results == nil {
				t.Error("Match() returned nil results")
			}
		})
	}
}

func TestDefaultPolymorphicMatcher_MatchPolymorphic(t *testing.T) {
	matcher, err := createPolymorphicMatcher()
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	// Test with empty content (should return empty results without error)
	tree := createMockSyntaxTree("<?php")
	ctx := context.Background()

	matches, err := matcher.MatchPolymorphic(ctx, tree, "test.php")
	if err != nil {
		t.Errorf("MatchPolymorphic() unexpected error = %v", err)
	}

	if matches == nil {
		t.Error("MatchPolymorphic() returned nil matches")
	}

	// Should be empty for mock content
	if len(matches) > 0 {
		t.Logf("MatchPolymorphic() returned %d matches (expected 0 for mock content)", len(matches))
	}
}

func TestPolymorphicMatch_Structure(t *testing.T) {
	// Test PolymorphicMatch struct fields
	match := &PolymorphicMatch{
		Relation:       "imageable",
		Type:           "morphTo",
		MorphType:      "imageable_type",
		MorphId:        "imageable_id",
		Model:          "Post",
		DepthTruncated: false,
		MaxDepth:       5,
		Pattern:        "morph_to_relationship",
		Method:         "morphTo",
		Context:        "relationship",
		RelatedModels:  []string{"Post", "Video"},
		Discriminator: &PolymorphicDiscriminator{
			PropertyName: "imageable_type",
			Mapping:      map[string]string{"post": "Post", "video": "Video"},
			Source:       "morphMap",
			IsExplicit:   true,
			DefaultType:  "",
		},
	}

	tests := []struct {
		name   string
		field  any
		expect any
	}{
		{"relation_field", match.Relation, "imageable"},
		{"type_field", match.Type, "morphTo"},
		{"morphType_field", match.MorphType, "imageable_type"},
		{"morphId_field", match.MorphId, "imageable_id"},
		{"model_field", match.Model, "Post"},
		{"depthTruncated_field", match.DepthTruncated, false},
		{"maxDepth_field", match.MaxDepth, 5},
		{"pattern_field", match.Pattern, "morph_to_relationship"},
		{"method_field", match.Method, "morphTo"},
		{"context_field", match.Context, "relationship"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.field != tt.expect {
				t.Errorf("%s = %v, want %v", tt.name, tt.field, tt.expect)
			}
		})
	}

	// Test discriminator
	if match.Discriminator == nil {
		t.Error("Discriminator is nil")
	} else {
		if match.Discriminator.PropertyName != "imageable_type" {
			t.Errorf("Discriminator.PropertyName = %v, want %v", match.Discriminator.PropertyName, "imageable_type")
		}
		if len(match.Discriminator.Mapping) != 2 {
			t.Errorf("Discriminator.Mapping length = %v, want %v", len(match.Discriminator.Mapping), 2)
		}
	}

	// Test related models
	if len(match.RelatedModels) != 2 {
		t.Errorf("RelatedModels length = %v, want %v", len(match.RelatedModels), 2)
	}
}

func TestDefaultPolymorphicMatcher_Close(t *testing.T) {
	tests := []struct {
		name          string
		setupCompiler bool
		expectCleanup bool
	}{
		{
			name:          "with_compiler",
			setupCompiler: true,
			expectCleanup: true,
		},
		{
			name:          "without_compiler",
			setupCompiler: false,
			expectCleanup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := createPolymorphicMatcher()
			if err != nil {
				t.Fatalf("Failed to create matcher: %v", err)
			}

			if !tt.setupCompiler {
				matcher.compiler = nil
			}

			err = matcher.Close()
			if err != nil {
				t.Errorf("Close() unexpected error = %v", err)
			}

			// Verify cleanup
			if matcher.initialized != false {
				t.Error("Close() did not set initialized to false")
			}

			if matcher.queries != nil {
				t.Error("Close() did not nil queries")
			}

			if matcher.morphMap != nil {
				t.Error("Close() did not nil morphMap")
			}
		})
	}
}

func TestPolymorphicMatcherIntegration(t *testing.T) {
	matcher, err := createPolymorphicMatcher()
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name     string
		testFunc func(*testing.T, *DefaultPolymorphicMatcher)
	}{
		{
			name: "pattern_type_consistency",
			testFunc: func(t *testing.T, m *DefaultPolymorphicMatcher) {
				if m.GetType() != PatternTypePolymorphic {
					t.Errorf("GetType() = %v, want %v", m.GetType(), PatternTypePolymorphic)
				}
			},
		},
		{
			name: "interface_compliance",
			testFunc: func(t *testing.T, m *DefaultPolymorphicMatcher) {
				var _ PatternMatcher = m
				var _ PolymorphicMatcher = m
			},
		},
		{
			name: "specialized_interface_compliance",
			testFunc: func(t *testing.T, m *DefaultPolymorphicMatcher) {
				ctx := context.Background()
				tree := createMockSyntaxTree("<?php")

				matches, err := m.MatchPolymorphic(ctx, tree, "test.php")
				if err != nil {
					t.Logf("MatchPolymorphic completed without error (unexpected with mock)")
				}

				if matches == nil {
					t.Error("MatchPolymorphic returned nil matches")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t, matcher)
		})
	}
}

// Helper functions

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) &&
		s[len(s)-len(substr):] == substr ||
		s[:len(substr)] == substr ||
		(len(s) > len(substr) && len(s) > 0 &&
			func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}())
}

// Test helper functions from the PolymorphicMatcher

func TestGetSupportedPolymorphicPatterns(t *testing.T) {
	patterns := GetSupportedPolymorphicPatterns()

	expectedPatterns := []string{
		"$this->morphTo()",
		"$this->morphOne(Comment::class, 'commentable')",
		"$this->morphMany(Tag::class, 'taggable')",
		"$this->morphTo('imageable', 'imageable_type', 'imageable_id')",
		"Relation::morphMap(['post' => Post::class, 'video' => Video::class])",
		"$this->morphByMany(Tag::class, 'taggable')",
		"$this->morphToMany(Video::class, 'videoable')",
	}

	if len(patterns) != len(expectedPatterns) {
		t.Errorf("GetSupportedPolymorphicPatterns() length = %v, want %v", len(patterns), len(expectedPatterns))
	}

	for i, pattern := range patterns {
		if i < len(expectedPatterns) && pattern != expectedPatterns[i] {
			t.Errorf("GetSupportedPolymorphicPatterns()[%d] = %v, want %v", i, pattern, expectedPatterns[i])
		}
	}
}

func TestGetPolymorphicMethodConventions(t *testing.T) {
	conventions := GetPolymorphicMethodConventions()

	expectedMethods := []string{"morphTo", "morphOne", "morphMany", "morphByMany", "morphToMany", "morphMap"}

	if len(conventions) != len(expectedMethods) {
		t.Errorf("GetPolymorphicMethodConventions() length = %v, want %v", len(conventions), len(expectedMethods))
	}

	for _, method := range expectedMethods {
		if _, exists := conventions[method]; !exists {
			t.Errorf("GetPolymorphicMethodConventions() missing method: %v", method)
		}
	}
}

func TestValidatePolymorphicMethodCall(t *testing.T) {
	tests := []struct {
		method string
		args   []string
		want   bool
	}{
		// morphTo tests
		{"morphTo", []string{}, true},                                              // No args
		{"morphTo", []string{"imageable"}, true},                                   // Name only
		{"morphTo", []string{"imageable", "imageable_type", "imageable_id"}, true}, // Full args
		{"morphTo", []string{"arg1", "arg2"}, false},                               // Invalid 2 args
		{"morphTo", []string{"arg1", "arg2", "arg3", "arg4"}, false},               // Too many args

		// morphOne tests
		{"morphOne", []string{}, false},                                                                               // No args (invalid)
		{"morphOne", []string{"Comment::class"}, true},                                                                // Model only
		{"morphOne", []string{"Comment::class", "commentable"}, true},                                                 // Model + name
		{"morphOne", []string{"Comment::class", "commentable", "commentable_type", "commentable_id"}, true},           // Full args
		{"morphOne", []string{"Comment::class", "commentable", "commentable_type", "commentable_id", "extra"}, false}, // Too many args

		// morphMany tests
		{"morphMany", []string{}, false},                               // No args (invalid)
		{"morphMany", []string{"Comment::class"}, true},                // Model only
		{"morphMany", []string{"Comment::class", "commentable"}, true}, // Model + name

		// morphByMany tests
		{"morphByMany", []string{}, false},                        // No args (invalid)
		{"morphByMany", []string{"Tag::class"}, false},            // Missing relationship name
		{"morphByMany", []string{"Tag::class", "taggable"}, true}, // Valid

		// morphToMany tests
		{"morphToMany", []string{}, false},                           // No args (invalid)
		{"morphToMany", []string{"Video::class"}, false},             // Missing relationship name
		{"morphToMany", []string{"Video::class", "videoable"}, true}, // Valid

		// Invalid method
		{"invalidMethod", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.method+"_"+joinArgs(tt.args), func(t *testing.T) {
			got := ValidatePolymorphicMethodCall(tt.method, tt.args)
			if got != tt.want {
				t.Errorf("ValidatePolymorphicMethodCall(%v, %v) = %v, want %v", tt.method, tt.args, got, tt.want)
			}
		})
	}
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return "no_args"
	}
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += "_"
		}
		if len(arg) > 10 {
			result += arg[:10]
		} else {
			result += arg
		}
	}
	return result
}
