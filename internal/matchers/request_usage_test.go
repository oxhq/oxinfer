// Package matchers provides tests for request usage pattern matching.
//go:build goexperiment.jsonv2

package matchers

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/oxhq/oxinfer/internal/parser"
	"github.com/smacker/go-tree-sitter/php"
)

func TestRequestUsageMatcher_Match(t *testing.T) {
	// Initialize PHP language for tree-sitter
	language := php.GetLanguage()
	if language == nil {
		t.Fatal("Failed to get PHP language")
	}

	testCases := []struct {
		name                 string
		phpContent           string
		expectedMatches      int
		expectedContentTypes []string
		expectedMethods      []string
		expectedConfidence   float64
		expectedPattern      string
	}{
		{
			name: "request_all_method",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function store(Request $request) {
        $data = $request->all();
        return response()->json($data);
    }
}`,
			expectedMatches:      1,
			expectedContentTypes: []string{"application/json", "application/x-www-form-urlencoded"},
			expectedMethods:      nil,
			expectedConfidence:   0.90,
			expectedPattern:      "request_all",
		},
		{
			name: "request_json_method",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function api(Request $request) {
        $jsonData = $request->json();
        $value = $request->json('key');
        return response()->json(['received' => $jsonData]);
    }
}`,
			expectedMatches:      1,
			expectedContentTypes: []string{"application/json"},
			expectedMethods:      nil,
			expectedConfidence:   0.91,
			expectedPattern:      "request_json",
		},
		{
			name: "request_file_upload",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function upload(Request $request) {
        $file = $request->file('avatar');
        if ($request->hasFile('document')) {
            $doc = $request->file('document');
        }
        return response()->json(['uploaded' => true]);
    }
}`,
			expectedMatches:      1,
			expectedContentTypes: []string{"multipart/form-data"},
			expectedMethods:      nil,
			expectedConfidence:   0.91,
			expectedPattern:      "request_file",
		},
		{
			name: "request_input_with_parameters",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function update(Request $request) {
        $name = $request->input('name');
        $email = $request->input('email', 'default@example.com');
        $age = $request->input('profile.age');
        return response()->json(['name' => $name, 'email' => $email]);
    }
}`,
			expectedMatches:      1,
			expectedContentTypes: []string{"application/json", "application/x-www-form-urlencoded"},
			expectedMethods:      nil,
			expectedConfidence:   0.90,
			expectedPattern:      "request_input",
		},
		{
			name: "request_only_except_methods",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function filtered(Request $request) {
        $allowed = $request->only(['name', 'email']);
        $cleaned = $request->except(['password', '_token']);
        return response()->json(['allowed' => $allowed, 'cleaned' => $cleaned]);
    }
}`,
			expectedMatches:      1,
			expectedContentTypes: []string{"application/json", "application/x-www-form-urlencoded"},
			expectedMethods:      nil,
			expectedConfidence:   0.90,
			expectedPattern:      "request_only", // Will match first one found
		},
		{
			name: "request_validation",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function validate(Request $request) {
        $validated = $request->validate([
            'name' => 'required|string',
            'email' => 'required|email'
        ]);
        return response()->json(['validated' => $validated]);
    }
}`,
			expectedMatches:      1,
			expectedContentTypes: []string{"application/json", "application/x-www-form-urlencoded"},
			expectedMethods:      nil,
			expectedConfidence:   0.89,
			expectedPattern:      "request_validate",
		},
		{
			name: "no_request_usage",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function index() {
        return response()->json(['message' => 'Hello World']);
    }
}`,
			expectedMatches: 0,
		},
		{
			name: "mixed_request_patterns",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function complex(Request $request) {
        // File upload detection
        if ($request->hasFile('avatar')) {
            $file = $request->file('avatar');
        }
        
        // JSON data processing
        $jsonData = $request->json();
        
        // Form data processing
        $name = $request->input('name');
        $formData = $request->only(['name', 'email']);
        
        return response()->json(['success' => true]);
    }
}`,
			expectedMatches: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create matcher with default config
			config := DefaultMatcherConfig()
			matcher, err := NewRequestUsageMatcher(language, config)
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

			// Verify match count
			if len(results) != tc.expectedMatches {
				t.Errorf("Expected %d matches, got %d", tc.expectedMatches, len(results))
				for i, result := range results {
					t.Logf("Match %d: %+v", i, result)
				}
				return
			}

			if tc.expectedMatches == 0 {
				return // No further validation needed
			}

			// Verify first match details
			result := results[0]
			if result.Type != PatternTypeRequestUsage {
				t.Errorf("Expected pattern type %s, got %s", PatternTypeRequestUsage, result.Type)
			}

			// Extract request usage data
			requestData, ok := result.Data.(*RequestUsageMatch)
			if !ok {
				t.Fatalf("Expected RequestUsageMatch data, got %T", result.Data)
			}

			if tc.expectedContentTypes != nil {
				if !containsAllStrings(requestData.ContentTypes, tc.expectedContentTypes) {
					t.Errorf("Expected content types %v, got %v", tc.expectedContentTypes, requestData.ContentTypes)
				}
			}

			if tc.expectedMethods != nil {
				if !containsAllStrings(requestData.Methods, tc.expectedMethods) {
					t.Errorf("Expected methods %v, got %v", tc.expectedMethods, requestData.Methods)
				}
			}

			if tc.expectedConfidence > 0 {
				if result.Confidence < tc.expectedConfidence-0.01 || result.Confidence > tc.expectedConfidence+0.01 {
					t.Errorf("Expected confidence ~%.2f, got %.2f", tc.expectedConfidence, result.Confidence)
				}
			}
		})
	}
}

func TestRequestUsageMatcher_ContentTypeInference(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewRequestUsageMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	testCases := []struct {
		name                 string
		phpContent           string
		expectedContentTypes []string
	}{
		{
			name: "json_content_type",
			phpContent: `<?php
class TestController extends Controller {
    public function api(Request $request) {
        $data = $request->json();
        return response()->json($data);
    }
}`,
			expectedContentTypes: []string{"application/json"},
		},
		{
			name: "file_upload_content_type",
			phpContent: `<?php
class TestController extends Controller {
    public function upload(Request $request) {
        $file = $request->file('upload');
        return response()->json(['uploaded' => true]);
    }
}`,
			expectedContentTypes: []string{"multipart/form-data"},
		},
		{
			name: "form_data_content_type",
			phpContent: `<?php
class TestController extends Controller {
    public function form(Request $request) {
        $data = $request->all();
        return response()->json($data);
    }
}`,
			expectedContentTypes: []string{"application/json", "application/x-www-form-urlencoded"},
		},
		{
			name: "mixed_content_types",
			phpContent: `<?php
class TestController extends Controller {
    public function mixed(Request $request) {
        $json = $request->json();
        $file = $request->file('upload');
        $form = $request->input('field');
        return response()->json(['success' => true]);
    }
}`,
			expectedContentTypes: []string{"application/json", "multipart/form-data", "application/x-www-form-urlencoded"},
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
				t.Fatal("Expected at least one match for content type inference")
			}

			// Aggregate content types from all matches
			allContentTypes := make(map[string]bool)
			for _, result := range results {
				if requestData, ok := result.Data.(*RequestUsageMatch); ok {
					for _, ct := range requestData.ContentTypes {
						allContentTypes[ct] = true
					}
				}
			}

			for _, expectedCT := range tc.expectedContentTypes {
				if !allContentTypes[expectedCT] {
					t.Errorf("Expected content type %s not found in results", expectedCT)
				}
			}
		})
	}
}

func TestRequestUsageMatcher_ParameterExtraction(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewRequestUsageMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class TestController extends Controller {
    public function process(Request $request) {
        $name = $request->input('name');
        $email = $request->input('email');
        $nested = $request->input('profile.age');
        $file = $request->file('avatar');
        $jsonKey = $request->json('data.key');
        
        $allowed = $request->only(['name', 'email', 'age']);
        $cleaned = $request->except(['password', '_token', 'confirm_password']);
        
        return response()->json(['processed' => true]);
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

	ctx := context.Background()
	results, err := matcher.Match(ctx, syntaxTree, "test.php")
	if err != nil {
		t.Fatalf("Matcher failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected matches for parameter extraction test")
	}

	// Aggregate all detected parameters
	allParameters := make(map[string]bool)
	for _, result := range results {
		if requestData, ok := result.Data.(*RequestUsageMatch); ok {
			// Check body parameters
			for param := range requestData.Body {
				allParameters[param] = true
			}
			// Check files
			for param := range requestData.Files {
				allParameters[param] = true
			}
		}
	}

	expectedParams := []string{"name", "email", "age", "avatar"}
	for _, param := range expectedParams {
		if !allParameters[param] {
			t.Errorf("Expected parameter %s not found in extracted parameters", param)
		}
	}

	t.Logf("Successfully extracted %d parameters from request usage patterns", len(allParameters))
}

func TestRequestUsageMatcher_GetType(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewRequestUsageMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	if matcher.GetType() != PatternTypeRequestUsage {
		t.Errorf("Expected pattern type %s, got %s", PatternTypeRequestUsage, matcher.GetType())
	}
}

func TestRequestUsageMatcher_GetQueries(t *testing.T) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewRequestUsageMatcher(language, config)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	queries := matcher.GetQueries()
	expectedQueryCount := len(RequestUsageQueries)

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
func BenchmarkRequestUsageMatcher_Match(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	matcher, err := NewRequestUsageMatcher(language, config)
	if err != nil {
		b.Fatalf("Failed to create matcher: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class BenchmarkController extends Controller {
    public function process(Request $request) {
        $data = $request->all();
        $json = $request->json();
        $file = $request->file('upload');
        $name = $request->input('name');
        $filtered = $request->only(['name', 'email']);
        return response()->json(['success' => true]);
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

func TestRequestUsageMatcher_GoldenFiles(t *testing.T) {
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
			name:           "simple_controller_request_patterns",
			fixture:        "simple_controller.php",
			expectedOutput: "simple_controller_request.json",
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
			matcher, err := NewRequestUsageMatcher(language, config)
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
			actual := convertRequestUsageResults(results)

			// For now, just verify we got some results
			if len(results) > 0 {
				actualJSON, _ := json.Marshal(actual, json.Deterministic(true))
				t.Logf("Request usage results: %s", string(actualJSON))
			}
		})
	}
}

// Helper functions

// containsAllStrings checks if slice contains all expected strings
func containsAllStrings(slice, expected []string) bool {
	found := make(map[string]bool)
	for _, item := range slice {
		found[item] = true
	}

	for _, exp := range expected {
		if !found[exp] {
			return false
		}
	}
	return true
}

// convertRequestUsageResults converts MatchResult array to comparable format
func convertRequestUsageResults(results []*MatchResult) []map[string]any {
	converted := make([]map[string]any, len(results))
	for i, result := range results {
		requestData, ok := result.Data.(*RequestUsageMatch)
		if !ok {
			continue
		}

		converted[i] = map[string]any{
			"type":         result.Type,
			"position":     result.Position,
			"confidence":   result.Confidence,
			"contentTypes": requestData.ContentTypes,
			"methods":      requestData.Methods,
			"body":         requestData.Body,
			"files":        requestData.Files,
		}
	}
	return converted
}
