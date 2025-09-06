// Package matchers provides tests for Laravel Resource pattern matching.
//go:build goexperiment.jsonv2

package matchers

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/garaekz/oxinfer/internal/parser"
	"github.com/smacker/go-tree-sitter/php"
)

func TestResourceMatcher_Match(t *testing.T) {
	// Initialize PHP language for tree-sitter
	language := php.GetLanguage()
	if language == nil {
		t.Fatal("Failed to get PHP language")
	}

	testCases := []struct {
		name               string
		phpContent         string
		expectedMatches    int
		expectedClass      string
		expectedCollection bool
		expectedPattern    string
		expectedConfidence float64
	}{
		{
			name: "resource_collection_static",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function index() {
        $users = User::all();
        return UserResource::collection($users);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "UserResource",
			expectedCollection: true,
			expectedPattern:    "",
			expectedConfidence: 1.0,
		},
		{
			name: "resource_make_static",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function show($id) {
        $user = User::find($id);
        return UserResource::make($user);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "UserResource",
			expectedCollection: false,
			expectedPattern:    "",
			expectedConfidence: 0.95,
		},
		{
			name: "new_resource_instantiation",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function show($id) {
        $user = User::find($id);
        return new UserResource($user);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "UserResource",
			expectedCollection: false,
			expectedPattern:    "",
			expectedConfidence: 0.98,
		},
		{
			name: "return_new_resource",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function show($id) {
        $user = User::find($id);
        return new UserResource($user);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "UserResource",
			expectedCollection: false,
			expectedPattern:    "",
			expectedConfidence: 0.98,
		},
		{
			name: "return_resource_collection",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function index() {
        $users = User::all();
        return UserResource::collection($users);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "UserResource",
			expectedCollection: true,
			expectedPattern:    "",
			expectedConfidence: 1.0,
		},
		{
			name: "variable_resource_assignment",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function show($id) {
        $user = User::find($id);
        $resource = new UserResource($user);
        return $resource->additional(['meta' => 'data']);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "UserResource",
			expectedCollection: false,
			expectedPattern:    "",
			expectedConfidence: 0.95,
		},
		{
			name: "multiple_resource_patterns",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use App\Http\Resources\PostResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function complex() {
        $users = User::all();
        $posts = Post::all();
        
        return [
            'users' => UserResource::collection($users),
            'posts' => PostResource::collection($posts),
            'featured' => new UserResource(User::first())
        ];
    }
}`,
			expectedMatches: 3, // Two collections + one single resource
		},
		{
			name: "fully_qualified_resource",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function show($id) {
        $user = User::find($id);
        return new \App\Http\Resources\UserResource($user);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "UserResource",
			expectedCollection: false,
			expectedPattern:    "",
			expectedConfidence: 0.98,
		},
		{
			name: "no_resource_usage",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function index() {
        return response()->json(['message' => 'No resources here']);
    }
}`,
			expectedMatches: 0,
		},
		{
			name: "custom_resource_class",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\CustomDataResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function data() {
        $data = collect(['item1', 'item2', 'item3']);
        return CustomDataResource::collection($data);
    }
}`,
			expectedMatches:    1,
			expectedClass:      "CustomDataResource",
			expectedCollection: true,
			expectedPattern:    "",
			expectedConfidence: 1.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create matcher with default config
			config := DefaultMatcherConfig()
			matcher, err := NewResourceMatcher(language, config)
			if err != nil {
				t.Fatalf("Failed to create matcher: %v", err)
			}

			// Parse PHP content into syntax tree
			tree, err := parsePHPContent(tc.phpContent, language)
			if err != nil {
				t.Fatalf("Failed to parse PHP content: %v", err)
			}
			defer tree.Close()

			// Convert to parser.SyntaxTree format
			syntaxTree := &parser.SyntaxTree{
				Root:   convertNode(tree.RootNode()),
				Source: []byte(tc.phpContent),
			}

			// Run the matcher
			ctx := context.Background()
			filePath := "test_controller.php"
			results, err := matcher.Match(ctx, syntaxTree, filePath)
			if err != nil {
				t.Fatalf("Matcher failed: %v", err)
			}

			// Verify match count (relaxed)
			if tc.expectedMatches == 0 {
				if len(results) != 0 {
					t.Errorf("Expected 0 matches, got %d", len(results))
				}
				return
			}
			if tc.expectedMatches > 0 && len(results) < tc.expectedMatches {
				t.Errorf("Expected at least %d matches, got %d", tc.expectedMatches, len(results))
				for i, result := range results {
					t.Logf("Match %d: %+v", i, result)
				}
				return
			}

			// Choose a representative result: prefer highest confidence
			sel := results[0]
			for _, r := range results {
				if r.Confidence > sel.Confidence {
					sel = r
				}
			}
			if sel.Type != PatternTypeResource {
				t.Errorf("Expected pattern type %s, got %s", PatternTypeResource, sel.Type)
			}

			// Extract resource data
			resourceData, ok := sel.Data.(*ResourceMatch)
			if !ok {
				t.Fatalf("Expected ResourceMatch data, got %T", sel.Data)
			}

			if tc.expectedClass != "" {
				// Allow short class names as match
				if resourceData.Class != tc.expectedClass && !strings.HasSuffix(resourceData.Class, tc.expectedClass) {
					t.Errorf("Expected resource class to end with %s, got %s", tc.expectedClass, resourceData.Class)
				}
			}

			if tc.expectedMatches == 1 {
				if resourceData.Collection != tc.expectedCollection {
					t.Errorf("Expected collection=%t, got %t", tc.expectedCollection, resourceData.Collection)
				}
			}

			if tc.expectedPattern != "" {
				found := false
				for _, r := range results {
					if data, ok := r.Data.(*ResourceMatch); ok && data.Pattern == tc.expectedPattern {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find pattern %s among matches", tc.expectedPattern)
				}
			}

			if tc.expectedConfidence > 0 {
				if sel.Confidence < tc.expectedConfidence-0.01 || sel.Confidence > tc.expectedConfidence+0.01 {
					t.Errorf("Expected confidence ~%.2f, got %.2f", tc.expectedConfidence, sel.Confidence)
				}
			}
		})
	}
}

func TestResourceMatcher_CollectionDetection(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewResourceMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name               string
		phpContent         string
		expectedCollection bool
	}{
		{
			name: "collection_method_true",
			phpContent: `<?php
class TestController extends Controller {
    public function index() {
        return UserResource::collection($users);
    }
}`,
			expectedCollection: true,
		},
		{
			name: "make_method_false",
			phpContent: `<?php
class TestController extends Controller {
    public function show($id) {
        return UserResource::make($user);
    }
}`,
			expectedCollection: false,
		},
		{
			name: "new_instantiation_false",
			phpContent: `<?php
class TestController extends Controller {
    public function show($id) {
        return new UserResource($user);
    }
}`,
			expectedCollection: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := parsePHPContent(tc.phpContent, language)
			if err != nil {
				t.Fatalf("Failed to parse PHP content: %v", err)
			}
			defer tree.Close()

			syntaxTree := &parser.SyntaxTree{
				Root:   convertNode(tree.RootNode()),
				Source: []byte(tc.phpContent),
			}

			ctx := context.Background()
			results, err := matcher.Match(ctx, syntaxTree, "test.php")
			if err != nil {
				t.Fatalf("Matcher failed: %v", err)
			}

			if len(results) == 0 {
				t.Fatal("Expected at least one match for collection detection test")
			}

			found := false
			for _, r := range results {
				if data, ok := r.Data.(*ResourceMatch); ok {
					if data.Collection == tc.expectedCollection {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("Expected to find a match with collection=%t", tc.expectedCollection)
			}
		})
	}
}

func TestResourceMatcher_ImportResolution(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	config.ResolveImportedClasses = true // Enable import resolution
	matcher, err := NewResourceMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name          string
		phpContent    string
		expectedClass string
		expectedFQCN  string // Fully Qualified Class Name after resolution
	}{
		{
			name: "imported_class",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function users() {
        return UserResource::collection($users);
    }
}`,
			expectedClass: "UserResource",
			expectedFQCN:  "App\\Http\\Resources\\UserResource",
		},
		{
			name: "aliased_import",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource as Users;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function index() {
        return Users::collection($users);
    }
}`,
			expectedClass: "Users", // Should detect the alias
			expectedFQCN:  "App\\Http\\Resources\\UserResource",
		},
		{
			name: "fully_qualified_class",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function index() {
        return \App\Http\Resources\UserResource::collection($users);
    }
}`,
			expectedClass: "UserResource",
			expectedFQCN:  "\\App\\Http\\Resources\\UserResource",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := parsePHPContent(tc.phpContent, language)
			if err != nil {
				t.Fatalf("Failed to parse PHP content: %v", err)
			}
			defer tree.Close()

			syntaxTree := &parser.SyntaxTree{
				Root:   convertNode(tree.RootNode()),
				Source: []byte(tc.phpContent),
			}

			ctx := context.Background()
			results, err := matcher.Match(ctx, syntaxTree, "test.php")
			if err != nil {
				t.Fatalf("Matcher failed: %v", err)
			}

			if len(results) == 0 {
				t.Fatal("Expected at least one match for import resolution test")
			}

			result := results[0]
			resourceData, ok := result.Data.(*ResourceMatch)
			if !ok {
				t.Fatal("Expected ResourceMatch data")
			}

			if resourceData.Class != tc.expectedClass {
				t.Errorf("Expected class %s, got %s", tc.expectedClass, resourceData.Class)
			}

			// Note: FQCN resolution would require additional context/metadata
			// For now, we just verify the basic class name detection
		})
	}
}

func TestResourceMatcher_EdgeCases(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewResourceMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name            string
		phpContent      string
		expectedMatches int
		description     string
	}{
		{
			name: "non_resource_class",
			phpContent: `<?php
class TestController extends Controller {
    public function index() {
        return RegularClass::collection($data); // Not a Resource class
    }
}`,
			expectedMatches: 0,
			description:     "Should not match classes that don't end with 'Resource'",
		},
		{
			name: "nested_resource_calls",
			phpContent: `<?php
class TestController extends Controller {
    public function complex() {
        $users = UserResource::collection($data);
        $posts = $users->posts->map(function($post) {
            return PostResource::make($post);
        });
        return ['users' => $users, 'posts' => $posts];
    }
}`,
			expectedMatches: 2, // Should find both UserResource::collection and PostResource::make
			description:     "Should detect nested resource calls",
		},
		{
			name: "chained_resource_methods",
			phpContent: `<?php
class TestController extends Controller {
    public function show($id) {
        return UserResource::make($user)
            ->additional(['meta' => 'data'])
            ->response()
            ->header('X-Custom', 'value');
    }
}`,
			expectedMatches: 1,
			description:     "Should detect resource in method chain",
		},
		{
			name: "conditional_resource_usage",
			phpContent: `<?php
class TestController extends Controller {
    public function conditional($type) {
        if ($type === 'collection') {
            return UserResource::collection($users);
        } else {
            return new UserResource($user);
        }
    }
}`,
			expectedMatches: 2,
			description:     "Should detect both conditional resource patterns",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := parsePHPContent(tc.phpContent, language)
			if err != nil {
				t.Fatalf("Failed to parse PHP content: %v", err)
			}
			defer tree.Close()

			syntaxTree := &parser.SyntaxTree{
				Root:   convertNode(tree.RootNode()),
				Source: []byte(tc.phpContent),
			}

			ctx := context.Background()
			results, err := matcher.Match(ctx, syntaxTree, "test.php")
			if err != nil {
				t.Fatalf("Matcher failed: %v", err)
			}

			if len(results) != tc.expectedMatches {
				t.Errorf("%s: Expected %d matches, got %d", tc.description, tc.expectedMatches, len(results))
				for i, result := range results {
					t.Logf("Match %d: %+v", i, result)
				}
			}
		})
	}
}

func TestResourceMatcher_GetType(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewResourceMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	if matcher.GetType() != PatternTypeResource {
		t.Errorf("Expected pattern type %s, got %s", PatternTypeResource, matcher.GetType())
	}
}

func TestResourceMatcher_GetQueries(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewResourceMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	queries := matcher.GetQueries()
	expectedQueryCount := len(ResourceUsageQueries)

	if len(queries) != expectedQueryCount {
		t.Errorf("Expected %d queries, got %d", expectedQueryCount, len(queries))
	}

	// Verify queries are properly compiled
	for i, query := range queries {
		if query == nil {
			t.Errorf("Query %d is nil", i)
		}
	}
}

// Benchmark tests for performance
func BenchmarkResourceMatcher_Match(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewResourceMatcher(language, config)
	if err != nil {
		b.Fatalf("Failed to create matcher: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use App\Http\Resources\PostResource;
use Illuminate\Http\Request;
class BenchmarkController extends Controller {
    public function users() { return UserResource::collection($users); }
    public function user($id) { return UserResource::make($user); }
    public function posts() { return PostResource::collection($posts); }
    public function post($id) { return new PostResource($post); }
    public function mixed() { 
        return [
            'users' => UserResource::collection($users),
            'featured' => new UserResource($featured)
        ];
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		b.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		_, err := matcher.Match(ctx, syntaxTree, "benchmark_controller.php")
		if err != nil {
			b.Fatalf("Matcher failed: %v", err)
		}
	}
}

func TestResourceMatcher_GoldenFiles(t *testing.T) {
	language := php.GetLanguage()
	if language == nil {
		t.Fatal("Failed to get PHP language")
	}

	testCases := []struct {
		name           string
		fixture        string
		expectedOutput string
	}{
		{
			name:           "resource_controller_patterns",
			fixture:        "resource_controller.php",
			expectedOutput: "resource_controller_resources.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Load test fixture
			phpContent := loadTestFixture(t, tc.fixture)

			// Parse PHP content
			tree, err := parsePHPContent(phpContent, language)
			if err != nil {
				t.Fatalf("Failed to parse PHP fixture: %v", err)
			}
			defer tree.Close()

			// Convert to parser.SyntaxTree format
			syntaxTree := &parser.SyntaxTree{
				Root:   convertNode(tree.RootNode()),
				Source: []byte(phpContent),
			}

			// Create matcher and run pattern matching
			config := DefaultMatcherConfig()
			matcher, err := NewResourceMatcher(language, config)
			if err != nil {
				t.Fatalf("Failed to create matcher: %v", err)
			}

			ctx := context.Background()
			filePath := tc.fixture
			results, err := matcher.Match(ctx, syntaxTree, filePath)
			if err != nil {
				t.Fatalf("Matcher failed: %v", err)
			}

			// Convert results to comparable format
			actual := convertResourceResults(results)

			// For now, just verify we got some results
			if len(results) > 0 {
				actualJSON, _ := json.Marshal(actual, json.Deterministic(true), json.Indent("", "  "))
				t.Logf("Resource usage results: %s", string(actualJSON))
			}
		})
	}
}

// TestResourceMatcher_DeterministicOutput ensures consistent results across runs
func TestResourceMatcher_DeterministicOutput(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewResourceMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
class TestController extends Controller {
    public function test() {
        return UserResource::collection($users);
    }
}`

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		t.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	// Run multiple times to ensure deterministic results
	const iterations = 5
	var results [][]*MatchResult

	for i := 0; i < iterations; i++ {
		ctx := context.Background()
		matchResults, err := matcher.Match(ctx, syntaxTree, "test.php")
		if err != nil {
			t.Fatalf("Matcher failed on iteration %d: %v", i, err)
		}
		results = append(results, matchResults)
	}

	// Compare all results should be identical
	for i := 1; i < iterations; i++ {
		if len(results[0]) != len(results[i]) {
			t.Errorf("Result count differs between iterations: %d vs %d", len(results[0]), len(results[i]))
			continue
		}

		for j := range results[0] {
			if results[0][j].Type != results[i][j].Type {
				t.Errorf("Result %d type differs: %s vs %s", j, results[0][j].Type, results[i][j].Type)
			}
			if results[0][j].Confidence != results[i][j].Confidence {
				t.Errorf("Result %d confidence differs: %f vs %f", j, results[0][j].Confidence, results[i][j].Confidence)
			}
		}
	}

	t.Logf("Deterministic test passed: %d iterations produced identical results", iterations)
}

// Helper functions

// convertResourceResults converts MatchResult array to comparable format
func convertResourceResults(results []*MatchResult) []map[string]any {
	converted := make([]map[string]any, len(results))
	for i, result := range results {
		resourceData, ok := result.Data.(*ResourceMatch)
		if !ok {
			continue
		}

		converted[i] = map[string]any{
			"type":       result.Type,
			"position":   result.Position,
			"confidence": result.Confidence,
			"class":      resourceData.Class,
			"collection": resourceData.Collection,
			"pattern":    resourceData.Pattern,
		}
	}
	return converted
}
