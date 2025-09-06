package matchers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

func TestPivotMatcher_NewPivotMatcher(t *testing.T) {
	tests := []struct {
		name        string
		language    *sitter.Language
		config      *MatcherConfig
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid_configuration",
			language: php.GetLanguage(),
			config:   DefaultMatcherConfig(),
			wantErr:  false,
		},
		{
			name:     "nil_config_uses_defaults",
			language: php.GetLanguage(),
			config:   nil,
			wantErr:  false,
		},
		{
			name:        "nil_language_returns_error",
			language:    nil,
			config:      DefaultMatcherConfig(),
			wantErr:     true,
			errContains: "language cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewPivotMatcher(tt.language, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewPivotMatcher() expected error but got none")
					return
				}
				if tt.errContains != "" && !pivotContainsString(err.Error(), tt.errContains) {
					t.Errorf("NewPivotMatcher() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewPivotMatcher() unexpected error = %v", err)
				return
			}

			if matcher == nil {
				t.Error("NewPivotMatcher() returned nil matcher")
				return
			}

			// Verify interface compliance
			if matcher.GetType() != PatternTypePivot {
				t.Errorf("GetType() = %v, want %v", matcher.GetType(), PatternTypePivot)
			}

			if !matcher.IsInitialized() {
				t.Error("IsInitialized() = false, want true")
			}

			if len(matcher.GetQueries()) == 0 {
				t.Error("GetQueries() returned empty slice")
			}
		})
	}
}

func TestPivotMatcher_Match(t *testing.T) {
	matcher := createTestPivotMatcher(t)
	defer matcher.Close()

	tests := []struct {
		name             string
		phpContent       string
		wantMatchCount   int
		wantPivotMethods []string
		wantErr          bool
		errContains      string
	}{
		{
			name: "simple_with_pivot",
			phpContent: `<?php
class User extends Model {
    public function roles() {
        return $this->belongsToMany(Role::class)
            ->withPivot('permissions', 'granted_at');
    }
}`,
			wantMatchCount:   1,
			wantPivotMethods: []string{"withPivot"},
			wantErr:          false,
		},
		{
			name: "with_timestamps_only",
			phpContent: `<?php
class User extends Model {
    public function roles() {
        return $this->belongsToMany(Role::class)
            ->withTimestamps();
    }
}`,
			wantMatchCount:   1,
			wantPivotMethods: []string{"withTimestamps"},
			wantErr:          false,
		},
		{
			name: "pivot_with_alias",
			phpContent: `<?php
class User extends Model {
    public function tags() {
        return $this->belongsToMany(Tag::class)
            ->as('tagging');
    }
}`,
			wantMatchCount:   1,
			wantPivotMethods: []string{"as"},
			wantErr:          false,
		},
		{
			name: "chained_pivot_methods",
			phpContent: `<?php
class User extends Model {
    public function projects() {
        return $this->belongsToMany(Project::class)
            ->withPivot('role', 'status')
            ->withTimestamps()
            ->as('membership');
    }
}`,
			wantMatchCount:   3,
			wantPivotMethods: []string{"withPivot", "withTimestamps", "as"},
			wantErr:          false,
		},
		{
			name: "no_pivot_methods",
			phpContent: `<?php
class User extends Model {
    public function basic() {
        return $this->belongsToMany(Basic::class);
    }
}`,
			wantMatchCount: 0,
			wantErr:        false,
		},
		{
			name: "single_pivot_field",
			phpContent: `<?php
class Order extends Model {
    public function products() {
        return $this->belongsToMany(Product::class)
            ->withPivot('quantity');
    }
}`,
			wantMatchCount:   1,
			wantPivotMethods: []string{"withPivot"},
			wantErr:          false,
		},
		{
			name:           "invalid_syntax_tree",
			phpContent:     "", // Empty content
			wantMatchCount: 0,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tree := createSyntaxTree(t, tt.phpContent)

			results, err := matcher.Match(ctx, tree, "test.php")

			if tt.wantErr {
				if err == nil {
					t.Errorf("Match() expected error but got none")
					return
				}
				if tt.errContains != "" && !pivotContainsString(err.Error(), tt.errContains) {
					t.Errorf("Match() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Match() unexpected error = %v", err)
				return
			}

			if len(results) != tt.wantMatchCount {
				t.Errorf("Match() found %d matches, want %d", len(results), tt.wantMatchCount)
				return
			}

			// Verify match types and methods
			foundMethods := make([]string, 0, len(results))
			for _, result := range results {
				if result.Type != PatternTypePivot {
					t.Errorf("Match() result type = %v, want %v", result.Type, PatternTypePivot)
				}

				pivotMatch, ok := result.Data.(*PivotMatch)
				if !ok {
					t.Errorf("Match() result data is not *PivotMatch, got %T", result.Data)
					continue
				}

				foundMethods = append(foundMethods, pivotMatch.Method)
			}

			if !pivotContainsAllStrings(foundMethods, tt.wantPivotMethods) {
				t.Errorf("Match() found methods %v, want to contain %v", foundMethods, tt.wantPivotMethods)
			}
		})
	}
}

func TestPivotMatcher_MatchPivots(t *testing.T) {
	matcher := createTestPivotMatcher(t)
	defer matcher.Close()

	tests := []struct {
		name           string
		phpContent     string
		wantCount      int
		wantFields     [][]string // Expected pivot fields for each match
		wantTimestamps []bool     // Expected timestamps for each match
		wantAliases    []string   // Expected aliases for each match
	}{
		{
			name: "pivot_with_multiple_fields",
			phpContent: `<?php
class User extends Model {
    public function roles() {
        return $this->belongsToMany(Role::class)
            ->withPivot('permissions', 'granted_at', 'level');
    }
}`,
			wantCount:      1,
			wantFields:     [][]string{{"permissions", "granted_at", "level"}},
			wantTimestamps: []bool{false},
			wantAliases:    []string{""},
		},
		{
			name: "pivot_with_timestamps",
			phpContent: `<?php
class Order extends Model {
    public function products() {
        return $this->belongsToMany(Product::class)
            ->withTimestamps();
    }
}`,
			wantCount:      1,
			wantFields:     [][]string{nil},
			wantTimestamps: []bool{true},
			wantAliases:    []string{""},
		},
		{
			name: "pivot_with_alias",
			phpContent: `<?php
class Team extends Model {
    public function members() {
        return $this->belongsToMany(User::class)
            ->as('membership');
    }
}`,
			wantCount:      1,
			wantFields:     [][]string{nil},
			wantTimestamps: []bool{false},
			wantAliases:    []string{"membership"},
		},
		{
			name: "comprehensive_pivot_setup",
			phpContent: `<?php
class Project extends Model {
    public function users() {
        return $this->belongsToMany(User::class)
            ->withPivot('role', 'salary', 'start_date')
            ->withTimestamps()
            ->as('project_member');
    }
}`,
			wantCount:      3,
			wantFields:     [][]string{{"role", "salary", "start_date"}, nil, nil},
			wantTimestamps: []bool{false, true, false},
			wantAliases:    []string{"", "", "project_member"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tree := createSyntaxTree(t, tt.phpContent)

			pivotMatches, err := matcher.MatchPivots(ctx, tree, "test.php")

			if err != nil {
				t.Errorf("MatchPivots() unexpected error = %v", err)
				return
			}

			if len(pivotMatches) != tt.wantCount {
				t.Errorf("MatchPivots() found %d matches, want %d", len(pivotMatches), tt.wantCount)
				return
			}

			for i, match := range pivotMatches {
				if i >= len(tt.wantFields) {
					continue // Skip if we don't have expected data
				}

				// Check fields
				if !equalStringSlices(match.Fields, tt.wantFields[i]) {
					t.Errorf("MatchPivots()[%d] fields = %v, want %v", i, match.Fields, tt.wantFields[i])
				}

				// Check timestamps
				if i < len(tt.wantTimestamps) && match.Timestamps != tt.wantTimestamps[i] {
					t.Errorf("MatchPivots()[%d] timestamps = %v, want %v", i, match.Timestamps, tt.wantTimestamps[i])
				}

				// Check aliases
				if i < len(tt.wantAliases) && match.Alias != tt.wantAliases[i] {
					t.Errorf("MatchPivots()[%d] alias = %q, want %q", i, match.Alias, tt.wantAliases[i])
				}
			}
		})
	}
}

func TestPivotMatcher_RealFixtures(t *testing.T) {
	matcher := createTestPivotMatcher(t)
	defer matcher.Close()

	// Test against real fixture files
	fixtures := []struct {
		filename        string
		minMatches      int
		expectedMethods []string
	}{
		{
			filename:        "simple_pivot.php",
			minMatches:      3, // Should find at least 3 pivot method calls
			expectedMethods: []string{"withPivot", "withTimestamps", "as"},
		},
		{
			filename:        "complex_pivot.php",
			minMatches:      5, // Should find multiple pivot configurations
			expectedMethods: []string{"withPivot", "withTimestamps", "as"},
		},
		{
			filename:        "chained_methods.php",
			minMatches:      8, // Should find various chaining patterns
			expectedMethods: []string{"withPivot", "withTimestamps", "as"},
		},
		{
			filename:        "edge_cases.php",
			minMatches:      4, // Should handle edge cases appropriately
			expectedMethods: []string{"withPivot", "withTimestamps", "as"},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.filename, func(t *testing.T) {
			content := loadFixtureFile(t, filepath.Join("pivot", fixture.filename))
			tree := createSyntaxTree(t, content)

			ctx := context.Background()
			results, err := matcher.Match(ctx, tree, fixture.filename)

			if err != nil {
				t.Errorf("Match() on %s failed: %v", fixture.filename, err)
				return
			}

			if len(results) < fixture.minMatches {
				t.Errorf("Match() on %s found %d matches, want at least %d", fixture.filename, len(results), fixture.minMatches)
			}

			// Verify we found some of the expected methods
			foundMethods := make(map[string]bool)
			for _, result := range results {
				if pivotMatch, ok := result.Data.(*PivotMatch); ok {
					foundMethods[pivotMatch.Method] = true
				}
			}

			for _, expectedMethod := range fixture.expectedMethods {
				if !foundMethods[expectedMethod] {
					t.Errorf("Match() on %s did not find expected method %q", fixture.filename, expectedMethod)
				}
			}
		})
	}
}

func TestPivotMatcher_ExtractPivotFields(t *testing.T) {
	matcher := createTestPivotMatcher(t)
	defer matcher.Close()

	tests := []struct {
		name        string
		phpContent  string
		wantFields  []string
		description string
	}{
		{
			name: "single_field",
			phpContent: `<?php
class Test extends Model {
    public function relation() {
        return $this->belongsToMany(Other::class)
            ->withPivot('status');
    }
}`,
			wantFields:  []string{"status"},
			description: "should extract single pivot field",
		},
		{
			name: "multiple_fields",
			phpContent: `<?php
class Test extends Model {
    public function relation() {
        return $this->belongsToMany(Other::class)
            ->withPivot('field1', 'field2', 'field3');
    }
}`,
			wantFields:  []string{"field1", "field2", "field3"},
			description: "should extract multiple pivot fields",
		},
		{
			name: "mixed_quotes",
			phpContent: `<?php
class Test extends Model {
    public function relation() {
        return $this->belongsToMany(Other::class)
            ->withPivot("double_field", 'single_field');
    }
}`,
			wantFields:  []string{"double_field", "single_field"},
			description: "should handle mixed quote styles",
		},
		{
			name: "no_fields",
			phpContent: `<?php
class Test extends Model {
    public function relation() {
        return $this->belongsToMany(Other::class)
            ->withPivot();
    }
}`,
			wantFields:  []string{},
			description: "should return empty slice for withPivot with no arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tree := createSyntaxTree(t, tt.phpContent)

			pivotMatches, err := matcher.MatchPivots(ctx, tree, "test.php")

			if err != nil {
				t.Errorf("MatchPivots() unexpected error = %v", err)
				return
			}

			// Find the withPivot match
			var pivotMatch *PivotMatch
			for _, match := range pivotMatches {
				if match.Method == "withPivot" {
					pivotMatch = match
					break
				}
			}

			if pivotMatch == nil {
				if len(tt.wantFields) > 0 {
					t.Errorf("MatchPivots() did not find withPivot match when expected")
				}
				return
			}

			if !equalStringSlices(pivotMatch.Fields, tt.wantFields) {
				t.Errorf("MatchPivots() fields = %v, want %v (%s)", pivotMatch.Fields, tt.wantFields, tt.description)
			}
		})
	}
}

func TestPivotMatcher_Close(t *testing.T) {
	matcher := createTestPivotMatcher(t)

	if !matcher.IsInitialized() {
		t.Error("Matcher should be initialized before Close()")
	}

	err := matcher.Close()
	if err != nil {
		t.Errorf("Close() returned error = %v", err)
	}

	// After closing, matcher should not be initialized
	if matcher.IsInitialized() {
		t.Error("Matcher should not be initialized after Close()")
	}
}

// Helper functions

func createTestPivotMatcher(t *testing.T) *DefaultPivotMatcher {
	config := DefaultMatcherConfig()
	config.MinConfidenceThreshold = 0.5 // Lower threshold for testing

	matcher, err := NewPivotMatcher(php.GetLanguage(), config)
	if err != nil {
		t.Fatalf("Failed to create test pivot matcher: %v", err)
	}
	return matcher
}

func createSyntaxTree(t *testing.T, phpContent string) *parser.SyntaxTree {
	// Create a minimal SyntaxTree for testing
	return &parser.SyntaxTree{
		Root: &parser.SyntaxNode{
			Type: "program",
			Text: phpContent,
		},
		Source:   []byte(phpContent),
		Language: "php",
		ParsedAt: time.Now(),
	}
}

func loadFixtureFile(t *testing.T, relativePath string) string {
	fixturesDir := filepath.Join("..", "..", "test", "fixtures", "matchers")
	filePath := filepath.Join(fixturesDir, relativePath)

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to load fixture file %s: %v", filePath, err)
	}

	return string(content)
}

func pivotContainsString(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && (s == substr || pivotContainsString(s[1:], substr) || (len(s) > 0 && pivotContainsString(s[:len(s)-1], substr)))
}

func pivotContainsAllStrings(haystack, needles []string) bool {
	for _, needle := range needles {
		found := false
		for _, straw := range haystack {
			if straw == needle {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// Benchmarks

func BenchmarkPivotMatcher_Match(b *testing.B) {
	matcher := createBenchPivotMatcher(b)
	defer matcher.Close()

	phpContent := `<?php
class User extends Model {
    public function roles() {
        return $this->belongsToMany(Role::class)
            ->withPivot('permissions', 'granted_at', 'level')
            ->withTimestamps()
            ->as('user_role');
    }
    
    public function projects() {
        return $this->belongsToMany(Project::class)
            ->withPivot('role', 'salary', 'start_date', 'end_date')
            ->withTimestamps();
    }
}`

	tree := createBenchSyntaxTree(b, phpContent)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := matcher.Match(ctx, tree, "test.php")
		if err != nil {
			b.Fatalf("Match() failed: %v", err)
		}
	}
}

func createBenchPivotMatcher(b *testing.B) *DefaultPivotMatcher {
	matcher, err := NewPivotMatcher(php.GetLanguage(), DefaultMatcherConfig())
	if err != nil {
		b.Fatalf("Failed to create bench pivot matcher: %v", err)
	}
	return matcher
}

func createBenchSyntaxTree(b *testing.B, phpContent string) *parser.SyntaxTree {
	return &parser.SyntaxTree{
		Root: &parser.SyntaxNode{
			Type: "program",
			Text: phpContent,
		},
		Source:   []byte(phpContent),
		Language: "php",
		ParsedAt: time.Now(),
	}
}
