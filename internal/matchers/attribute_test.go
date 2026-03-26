package matchers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
	php "github.com/smacker/go-tree-sitter/php"
)

func TestNewAttributeMatcher(t *testing.T) {
	tests := []struct {
		name      string
		language  *sitter.Language
		config    *MatcherConfig
		wantError bool
	}{
		{
			name:      "valid_matcher_creation",
			language:  php.GetLanguage(),
			config:    DefaultMatcherConfig(),
			wantError: false,
		},
		{
			name:      "nil_language",
			language:  nil,
			config:    DefaultMatcherConfig(),
			wantError: true,
		},
		{
			name:      "nil_config_uses_default",
			language:  php.GetLanguage(),
			config:    nil,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewAttributeMatcher(tt.language, tt.config)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewAttributeMatcher() expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("NewAttributeMatcher() unexpected error: %v", err)
				return
			}

			if matcher == nil {
				t.Errorf("NewAttributeMatcher() returned nil matcher")
				return
			}

			if !matcher.IsInitialized() {
				t.Errorf("NewAttributeMatcher() matcher not initialized")
			}

			if matcher.GetType() != PatternTypeAttribute {
				t.Errorf("NewAttributeMatcher() wrong pattern type: got %v, want %v",
					matcher.GetType(), PatternTypeAttribute)
			}

			queries := matcher.GetQueries()
			if len(queries) == 0 {
				t.Errorf("NewAttributeMatcher() no queries compiled")
			}

			// Clean up
			matcher.Close()
		})
	}
}

func TestAttributeMatcherModernAttributes(t *testing.T) {
	matcher, err := NewAttributeMatcher(php.GetLanguage(), DefaultMatcherConfig())
	if err != nil {
		t.Fatalf("Failed to create attribute matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name           string
		phpCode        string
		wantMatches    int
		wantAttributes []AttributeMatch
		debug          bool
	}{
		{
			name: "modern_attribute_with_return_type",
			phpCode: `<?php
class User extends Model {
    public function fullName(): Attribute {
        return Attribute::make(
            get: fn ($value, $attributes) => $attributes['first_name'] . ' ' . $attributes['last_name']
        );
    }
}`,
			wantMatches: 1,
			debug:       true,
			wantAttributes: []AttributeMatch{
				{
					Name:     "full_name",
					Type:     "Attribute",
					Accessor: true,
					Mutator:  true,
					IsModern: true,
					Pattern:  "modern_attribute_method",
					Method:   "fullName",
				},
			},
		},
		{
			name: "multiple_modern_attributes",
			phpCode: `<?php
class User extends Model {
    public function firstName(): Attribute {
        return Attribute::make(
            get: fn ($value) => ucfirst($value),
            set: fn ($value) => strtolower($value)
        );
    }
    
    public function emailVerified(): Attribute {
        return Attribute::make(
            get: fn ($value) => (bool) $value
        );
    }
}`,
			wantMatches: 2,
			wantAttributes: []AttributeMatch{
				{
					Name:     "first_name",
					Type:     "Attribute",
					Accessor: true,
					Mutator:  true,
					IsModern: true,
					Pattern:  "modern_attribute_method",
					Method:   "firstName",
				},
				{
					Name:     "email_verified",
					Type:     "Attribute",
					Accessor: true,
					Mutator:  true,
					IsModern: true,
					Pattern:  "modern_attribute_method",
					Method:   "emailVerified",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := createAttributeTestSyntaxTree(t, tt.phpCode)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			matches, err := matcher.MatchAttributes(ctx, tree, "test.php")
			if err != nil {
				t.Errorf("MatchAttributes() error: %v", err)
				return
			}

			if len(matches) != tt.wantMatches {
				t.Errorf("MatchAttributes() got %d matches, want %d", len(matches), tt.wantMatches)
			}

			for i, wantAttr := range tt.wantAttributes {
				if i >= len(matches) {
					t.Errorf("Missing expected match %d", i)
					continue
				}

				gotAttr := matches[i]
				if gotAttr.Name != wantAttr.Name {
					t.Errorf("Match %d name: got %v, want %v", i, gotAttr.Name, wantAttr.Name)
				}
				if gotAttr.Type != wantAttr.Type {
					t.Errorf("Match %d type: got %v, want %v", i, gotAttr.Type, wantAttr.Type)
				}
				if gotAttr.IsModern != wantAttr.IsModern {
					t.Errorf("Match %d isModern: got %v, want %v", i, gotAttr.IsModern, wantAttr.IsModern)
				}
				if gotAttr.Method != wantAttr.Method {
					t.Errorf("Match %d method: got %v, want %v", i, gotAttr.Method, wantAttr.Method)
				}
			}
		})
	}
}

func TestAttributeMatcherLegacyAttributes(t *testing.T) {
	matcher, err := NewAttributeMatcher(php.GetLanguage(), DefaultMatcherConfig())
	if err != nil {
		t.Fatalf("Failed to create attribute matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name           string
		phpCode        string
		wantMatches    int
		wantAttributes []AttributeMatch
	}{
		{
			name: "legacy_get_attribute",
			phpCode: `<?php
class User extends Model {
    public function getFirstNameAttribute($value) {
        return ucfirst($value);
    }
}`,
			wantMatches: 1,
			wantAttributes: []AttributeMatch{
				{
					Name:     "first_name",
					Accessor: true,
					Mutator:  false,
					IsModern: false,
					Pattern:  "legacy_get_attribute",
					Method:   "getFirstNameAttribute",
				},
			},
		},
		{
			name: "legacy_set_attribute",
			phpCode: `<?php
class User extends Model {
    public function setPasswordAttribute($value) {
        $this->attributes['password'] = Hash::make($value);
    }
}`,
			wantMatches: 1,
			wantAttributes: []AttributeMatch{
				{
					Name:     "password",
					Accessor: false,
					Mutator:  true,
					IsModern: false,
					Pattern:  "legacy_set_attribute",
					Method:   "setPasswordAttribute",
				},
			},
		},
		{
			name: "mixed_legacy_attributes",
			phpCode: `<?php
class User extends Model {
    public function getFullNameAttribute() {
        return $this->first_name . ' ' . $this->last_name;
    }
    
    public function setEmailAttribute($value) {
        $this->attributes['email'] = strtolower($value);
    }
    
    public function getAgeAttribute() {
        return Carbon::parse($this->birth_date)->age;
    }
}`,
			wantMatches: 3,
			wantAttributes: []AttributeMatch{
				{
					Name:     "full_name",
					Accessor: true,
					Mutator:  false,
					IsModern: false,
					Pattern:  "legacy_get_attribute",
					Method:   "getFullNameAttribute",
				},
				{
					Name:     "email",
					Accessor: false,
					Mutator:  true,
					IsModern: false,
					Pattern:  "legacy_set_attribute",
					Method:   "setEmailAttribute",
				},
				{
					Name:     "age",
					Accessor: true,
					Mutator:  false,
					IsModern: false,
					Pattern:  "legacy_get_attribute",
					Method:   "getAgeAttribute",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := createAttributeTestSyntaxTree(t, tt.phpCode)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			matches, err := matcher.MatchAttributes(ctx, tree, "test.php")
			if err != nil {
				t.Errorf("MatchAttributes() error: %v", err)
				return
			}

			if len(matches) != tt.wantMatches {
				t.Errorf("MatchAttributes() got %d matches, want %d", len(matches), tt.wantMatches)
			}

			for i, wantAttr := range tt.wantAttributes {
				if i >= len(matches) {
					t.Errorf("Missing expected match %d", i)
					continue
				}

				gotAttr := matches[i]
				if gotAttr.Name != wantAttr.Name {
					t.Errorf("Match %d name: got %v, want %v", i, gotAttr.Name, wantAttr.Name)
				}
				if gotAttr.Accessor != wantAttr.Accessor {
					t.Errorf("Match %d accessor: got %v, want %v", i, gotAttr.Accessor, wantAttr.Accessor)
				}
				if gotAttr.Mutator != wantAttr.Mutator {
					t.Errorf("Match %d mutator: got %v, want %v", i, gotAttr.Mutator, wantAttr.Mutator)
				}
				if gotAttr.IsModern != wantAttr.IsModern {
					t.Errorf("Match %d isModern: got %v, want %v", i, gotAttr.IsModern, wantAttr.IsModern)
				}
			}
		})
	}
}

func TestAttributeMatcherErrorCases(t *testing.T) {
	matcher, err := NewAttributeMatcher(php.GetLanguage(), DefaultMatcherConfig())
	if err != nil {
		t.Fatalf("Failed to create attribute matcher: %v", err)
	}
	defer matcher.Close()

	tests := []struct {
		name    string
		tree    *parser.SyntaxTree
		wantErr bool
	}{
		{
			name:    "nil_tree",
			tree:    nil,
			wantErr: true,
		},
		{
			name:    "tree_with_nil_root",
			tree:    &parser.SyntaxTree{Root: nil, Source: []byte("<?php")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := matcher.Match(ctx, tt.tree, "test.php")

			if tt.wantErr && err == nil {
				t.Errorf("Match() expected error, got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Match() unexpected error: %v", err)
			}
		})
	}
}

func TestAttributeMatcherContextCancellation(t *testing.T) {
	matcher, err := NewAttributeMatcher(php.GetLanguage(), DefaultMatcherConfig())
	if err != nil {
		t.Fatalf("Failed to create attribute matcher: %v", err)
	}
	defer matcher.Close()

	phpCode := `<?php
class User extends Model {
    public function fullName(): Attribute {
        return Attribute::make();
    }
}`

	tree := createAttributeTestSyntaxTree(t, phpCode)

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = matcher.Match(ctx, tree, "test.php")
	if err != context.Canceled {
		t.Errorf("Match() with cancelled context should return context.Canceled, got: %v", err)
	}
}

func TestAttributeMatcherDeduplication(t *testing.T) {
	config := DefaultMatcherConfig()
	config.DeduplicateMatches = true

	matcher, err := NewAttributeMatcher(php.GetLanguage(), config)
	if err != nil {
		t.Fatalf("Failed to create attribute matcher: %v", err)
	}
	defer matcher.Close()

	// Code with potential duplicate matches
	phpCode := `<?php
class User extends Model {
    public function firstName(): Attribute {
        return Attribute::make();
    }
}
`

	tree := createAttributeTestSyntaxTree(t, phpCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	matches, err := matcher.MatchAttributes(ctx, tree, "test.php")
	if err != nil {
		t.Errorf("MatchAttributes() error: %v", err)
		return
	}

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, match := range matches {
		key := match.Name + ":" + match.Method
		if seen[key] {
			t.Errorf("Duplicate match found: %s", key)
		}
		seen[key] = true
	}
}

func TestExtractAttributeNameFromMethod(t *testing.T) {
	matcher, _ := NewAttributeMatcher(php.GetLanguage(), DefaultMatcherConfig())
	defer matcher.Close()

	tests := []struct {
		methodName string
		want       string
	}{
		{"fullName", "full_name"},
		{"firstName", "first_name"},
		{"emailAddress", "email_address"},
		{"isActive", "is_active"},
		{"createdAt", "created_at"},
		{"userID", "user_id"},
	}

	for _, tt := range tests {
		t.Run(tt.methodName, func(t *testing.T) {
			got := matcher.camelToSnake(tt.methodName)
			if got != tt.want {
				t.Errorf("camelToSnake(%v) = %v, want %v", tt.methodName, got, tt.want)
			}
		})
	}
}

func TestExtractAttributeNameFromLegacyMethod(t *testing.T) {
	matcher, _ := NewAttributeMatcher(php.GetLanguage(), DefaultMatcherConfig())
	defer matcher.Close()

	tests := []struct {
		methodName string
		prefix     string
		suffix     string
		want       string
	}{
		{"getFirstNameAttribute", "get", "Attribute", "first_name"},
		{"setLastNameAttribute", "set", "Attribute", "last_name"},
		{"getEmailAddressAttribute", "get", "Attribute", "email_address"},
		{"setPasswordAttribute", "set", "Attribute", "password"},
		{"invalidMethod", "get", "Attribute", ""},
		{"getInvalidMethod", "get", "Attribute", ""},
	}

	for _, tt := range tests {
		t.Run(tt.methodName, func(t *testing.T) {
			got := matcher.extractAttributeNameFromLegacyMethod(tt.methodName, tt.prefix, tt.suffix)
			if got != tt.want {
				t.Errorf("extractAttributeNameFromLegacyMethod(%v) = %v, want %v", tt.methodName, got, tt.want)
			}
		})
	}
}

func TestValidateAttributeMethodCall(t *testing.T) {
	tests := []struct {
		methodName string
		isModern   bool
		want       bool
	}{
		{"fullName", true, true},
		{"firstName", true, true},
		{"getFirstNameAttribute", false, true},
		{"setLastNameAttribute", false, true},
		{"invalidMethod", false, false},
		{"getAttribute", false, false},
		{"", true, false},
		{"", false, false},
	}

	for _, tt := range tests {
		modernStr := "false"
		if tt.isModern {
			modernStr = "true"
		}
		t.Run(tt.methodName+"_modern_"+modernStr, func(t *testing.T) {
			got := ValidateAttributeMethodCall(tt.methodName, tt.isModern)
			if got != tt.want {
				t.Errorf("ValidateAttributeMethodCall(%v, %v) = %v, want %v",
					tt.methodName, tt.isModern, got, tt.want)
			}
		})
	}
}

func TestGetSupportedAttributePatterns(t *testing.T) {
	patterns := GetSupportedAttributePatterns()

	if len(patterns) == 0 {
		t.Error("GetSupportedAttributePatterns() returned empty slice")
	}

	expectedPatterns := []string{
		"public function fullName(): Attribute",
		"return Attribute::make(get: fn ($value) => strtoupper($value))",
		"public function getFirstNameAttribute($value)",
		"protected $casts = ['created_at' => 'datetime']",
	}

	for _, expected := range expectedPatterns {
		found := false
		for _, pattern := range patterns {
			if strings.Contains(pattern, strings.Split(expected, " ")[2]) { // Check key parts
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected pattern containing '%s' not found in supported patterns", expected)
		}
	}
}

func TestGetAttributeMethodConventions(t *testing.T) {
	conventions := GetAttributeMethodConventions()

	if len(conventions) == 0 {
		t.Error("GetAttributeMethodConventions() returned empty map")
	}

	expectedKeys := []string{"modern_accessor", "legacy_accessor", "legacy_mutator", "casts"}
	for _, key := range expectedKeys {
		if _, exists := conventions[key]; !exists {
			t.Errorf("Expected convention key '%s' not found", key)
		}
	}
}

// Helper function to create a syntax tree from PHP code
func createAttributeTestSyntaxTree(t *testing.T, phpCode string) *parser.SyntaxTree {
	phpParser := sitter.NewParser()
	phpParser.SetLanguage(php.GetLanguage())

	tree, err := phpParser.ParseCtx(context.Background(), nil, []byte(phpCode))
	if err != nil {
		t.Fatalf("Failed to parse PHP code: %v", err)
	}

	sourceBytes := []byte(phpCode)
	return &parser.SyntaxTree{
		Root:     convertAttributeTestSitterNode(tree.RootNode(), sourceBytes),
		Source:   sourceBytes,
		Language: "php",
		ParsedAt: time.Now(),
	}
}

// Helper function to convert tree-sitter node to parser.SyntaxNode
func convertAttributeTestSitterNode(node *sitter.Node, source []byte) *parser.SyntaxNode {
	if node == nil {
		return nil
	}

	syntaxNode := &parser.SyntaxNode{
		Type:       node.Type(),
		Text:       string(node.Content(source)),
		StartByte:  int(node.StartByte()),
		EndByte:    int(node.EndByte()),
		StartPoint: parser.Point{Row: int(node.StartPoint().Row), Column: int(node.StartPoint().Column)},
		EndPoint:   parser.Point{Row: int(node.EndPoint().Row), Column: int(node.EndPoint().Column)},
		Children:   make([]*parser.SyntaxNode, 0, int(node.ChildCount())),
	}

	for i := uint32(0); i < node.ChildCount(); i++ {
		child := convertAttributeTestSitterNode(node.Child(int(i)), source)
		if child != nil {
			child.Parent = syntaxNode
			syntaxNode.Children = append(syntaxNode.Children, child)
		}
	}

	return syntaxNode
}
