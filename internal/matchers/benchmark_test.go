// Package matchers provides comprehensive benchmark tests for pattern matching performance.
package matchers

import (
	"context"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

// BenchmarkQueryExecution tests individual query execution performance
func BenchmarkQueryExecution(b *testing.B) {
	language := php.GetLanguage()
	if language == nil {
		b.Fatal("Failed to get PHP language")
	}

	benchmarkCases := []struct {
		name       string
		phpContent string
		matchers   []func(*sitter.Language, *MatcherConfig) (PatternMatcher, error)
	}{
		{
			name: "SimpleController",
			phpContent: `<?php
namespace App\Http\Controllers;
use Illuminate\Http\Request;
class SimpleController extends Controller {
    public function store(Request $request) {
        $data = $request->all();
        return response()->json($data, 201);
    }
}`,
			matchers: []func(*sitter.Language, *MatcherConfig) (PatternMatcher, error){
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewHTTPStatusMatcher(lang, cfg)
				},
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewRequestUsageMatcher(lang, cfg)
				},
			},
		},
		{
			name: "ComplexController",
			phpContent: `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class ComplexController extends Controller {
    public function process(Request $request) {
        $validated = $request->validate(['name' => 'required']);
        $json = $request->json();
        $file = $request->file('upload');
        $filtered = $request->only(['name', 'email']);
        
        if (!$validated) abort(422, 'Validation failed');
        
        $user = User::create($validated);
        return response()->json(UserResource::collection([$user]), 201);
    }
}`,
			matchers: []func(*sitter.Language, *MatcherConfig) (PatternMatcher, error){
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewHTTPStatusMatcher(lang, cfg)
				},
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewRequestUsageMatcher(lang, cfg)
				},
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewResourceMatcher(lang, cfg)
				},
			},
		},
		{
			name:       "LargeController",
			phpContent: generateLargeController(50), // Generate a controller with 50 methods
			matchers: []func(*sitter.Language, *MatcherConfig) (PatternMatcher, error){
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewHTTPStatusMatcher(lang, cfg)
				},
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewRequestUsageMatcher(lang, cfg)
				},
				func(lang *sitter.Language, cfg *MatcherConfig) (PatternMatcher, error) {
					return NewResourceMatcher(lang, cfg)
				},
			},
		},
	}

	config := DefaultMatcherConfig()

	for _, bc := range benchmarkCases {
		b.Run(bc.name, func(b *testing.B) {
			// Parse once and reuse
			tree, err := parsePHPContent(bc.phpContent, language)
			if err != nil {
				b.Fatalf("Failed to parse PHP content: %v", err)
			}
			defer tree.Close()

			syntaxTree := &parser.SyntaxTree{
				Root:   convertNode(tree.RootNode()),
				Source: []byte(bc.phpContent),
			}

			for _, matcherFactory := range bc.matchers {
				matcher, err := matcherFactory(language, config)
				if err != nil {
					b.Fatalf("Failed to create matcher: %v", err)
				}

				matcherName := string(matcher.GetType())
				b.Run(matcherName, func(b *testing.B) {
					ctx := context.Background()
					b.ResetTimer()

					for i := 0; i < b.N; i++ {
						_, err := matcher.Match(ctx, syntaxTree, "benchmark.php")
						if err != nil {
							b.Fatalf("Matcher failed: %v", err)
						}
					}
				})
			}
		})
	}
}

// BenchmarkQueryCompilation tests query compilation performance
func BenchmarkQueryCompilation(b *testing.B) {
	language := php.GetLanguage()
	if language == nil {
		b.Fatal("Failed to get PHP language")
	}

	queryTests := []struct {
		name    string
		queries []QueryDefinition
	}{
		{
			name:    "HTTPStatusQueries",
			queries: HTTPStatusQueries,
		},
		{
			name:    "RequestUsageQueries",
			queries: RequestUsageQueries,
		},
		{
			name:    "ResourceUsageQueries",
			queries: ResourceUsageQueries,
		},
	}

	for _, qt := range queryTests {
		b.Run(qt.name, func(b *testing.B) {
			compiler := NewQueryCompiler(language)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Clear cache to test fresh compilation
				compiler.ClearCache()

				_, err := compiler.CompileQueries(qt.queries)
				if err != nil {
					b.Fatalf("Failed to compile queries: %v", err)
				}
			}
		})
	}
}

// BenchmarkCompositeMatching tests the performance of using multiple matchers
func BenchmarkCompositeMatching(b *testing.B) {
	language := php.GetLanguage()
	if language == nil {
		b.Fatal("Failed to get PHP language")
	}

	config := DefaultMatcherConfig()
	composite, err := NewCompositePatternMatcher(language, config)
	if err != nil {
		b.Fatalf("Failed to create composite matcher: %v", err)
	}

	// Add all matchers
	httpMatcher, _ := NewHTTPStatusMatcher(language, config)
	requestMatcher, _ := NewRequestUsageMatcher(language, config)
	resourceMatcher, _ := NewResourceMatcher(language, config)

	composite.AddMatcher(httpMatcher)
	composite.AddMatcher(requestMatcher)
	composite.AddMatcher(resourceMatcher)

	testCases := []struct {
		name       string
		phpContent string
	}{
		{
			name: "SmallController",
			phpContent: `<?php
class TestController extends Controller {
    public function test(Request $request) {
        return response()->json(new UserResource($request->all()), 201);
    }
}`,
		},
		{
			name:       "MediumController",
			phpContent: generateMediumController(),
		},
		{
			name:       "LargeController",
			phpContent: generateLargeController(20),
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			tree, err := parsePHPContent(tc.phpContent, language)
			if err != nil {
				b.Fatalf("Failed to parse PHP content: %v", err)
			}
			defer tree.Close()

			syntaxTree := &parser.SyntaxTree{
				Root:   convertNode(tree.RootNode()),
				Source: []byte(tc.phpContent),
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx := context.Background()
				_, err := composite.MatchAll(ctx, syntaxTree, "benchmark.php")
				if err != nil {
					b.Fatalf("Composite matcher failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkPatternProcessor tests full processing pipeline performance
func BenchmarkPatternProcessor(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		b.Fatalf("Failed to create processor: %v", err)
	}

	phpContent := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class ProcessorBenchmark extends Controller {
    public function complexMethod(Request $request) {
        $validated = $request->validate(['name' => 'required', 'email' => 'required']);
        $json = $request->json();
        $file = $request->file('avatar');
        $filtered = $request->only(['name', 'email']);
        
        if (!$validated) abort(422, 'Validation failed');
        
        $user = User::create($validated);
        return response()->json(UserResource::make($user), 201);
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

	b.Run("ProcessFile", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := context.Background()
			patterns, err := processor.ProcessFile(ctx, syntaxTree, "benchmark.php")
			if err != nil {
				b.Fatalf("Processor failed: %v", err)
			}

			// Include conversion in benchmark
			_, err = processor.ConvertToEmitterFormat(patterns)
			if err != nil {
				b.Fatalf("Failed to convert to emitter format: %v", err)
			}
		}
	})
}

// BenchmarkMemoryUsage tests memory usage patterns
func BenchmarkMemoryUsage(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()

	// Test with large controller
	phpContent := generateLargeController(100)

	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		b.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	b.Run("HTTPStatusMatcher", func(b *testing.B) {
		matcher, _ := NewHTTPStatusMatcher(language, config)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ctx := context.Background()
			_, err := matcher.Match(ctx, syntaxTree, "benchmark.php")
			if err != nil {
				b.Fatalf("Matcher failed: %v", err)
			}
		}
	})

	b.Run("AllMatchers", func(b *testing.B) {
		processor, _ := NewPatternMatchingProcessor(language, config)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ctx := context.Background()
			_, err := processor.ProcessFile(ctx, syntaxTree, "benchmark.php")
			if err != nil {
				b.Fatalf("Processor failed: %v", err)
			}
		}
	})
}

// BenchmarkConcurrency tests concurrent pattern matching
func BenchmarkConcurrency(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		b.Fatalf("Failed to create processor: %v", err)
	}

	phpContent := generateMediumController()
	tree, err := parsePHPContent(phpContent, language)
	if err != nil {
		b.Fatalf("Failed to parse PHP content: %v", err)
	}
	defer tree.Close()

	syntaxTree := &parser.SyntaxTree{
		Root:   convertNode(tree.RootNode()),
		Source: []byte(phpContent),
	}

	b.Run("Sequential", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := context.Background()
			_, err := processor.ProcessFile(ctx, syntaxTree, "benchmark.php")
			if err != nil {
				b.Fatalf("Processor failed: %v", err)
			}
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				ctx := context.Background()
				_, err := processor.ProcessFile(ctx, syntaxTree, "benchmark.php")
				if err != nil {
					b.Fatalf("Processor failed: %v", err)
				}
			}
		})
	})
}

// BenchmarkRealWorldScenarios tests performance with realistic controller sizes
func BenchmarkRealWorldScenarios(b *testing.B) {
	language := php.GetLanguage()
	config := DefaultMatcherConfig()
	processor, err := NewPatternMatchingProcessor(language, config)
	if err != nil {
		b.Fatalf("Failed to create processor: %v", err)
	}

	scenarios := []struct {
		name           string
		methodCount    int
		patternDensity string // "light", "medium", "heavy"
	}{
		{"SmallController", 5, "light"},
		{"MediumController", 15, "medium"},
		{"LargeController", 30, "heavy"},
		{"VeryLargeController", 50, "heavy"},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			phpContent := generateControllerWithPatterns(scenario.methodCount, scenario.patternDensity)

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
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				patterns, err := processor.ProcessFile(ctx, syntaxTree, "benchmark.php")
				cancel()

				if err != nil {
					b.Fatalf("Processor failed: %v", err)
				}

				if patterns == nil {
					b.Fatal("Expected patterns result")
				}
			}
		})
	}
}

// Helper functions for generating test content

func generateLargeController(methodCount int) string {
	content := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use App\Http\Resources\PostResource;
use Illuminate\Http\Request;
class LargeController extends Controller {`

	for i := 0; i < methodCount; i++ {
		content += `
    public function method` + string(rune('A'+i%26)) + `(Request $request) {
        $data = $request->all();
        $json = $request->json();
        if ($request->hasFile('file')) {
            $file = $request->file('file');
        }
        $user = User::create($data);
        return response()->json(new UserResource($user), 201);
    }`
	}

	content += `
}`
	return content
}

func generateMediumController() string {
	return `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use Illuminate\Http\Request;
class MediumController extends Controller {
    public function index(Request $request) {
        $search = $request->input('search');
        $users = User::when($search, function($q) use ($search) {
            return $q->where('name', 'like', "%{$search}%");
        })->get();
        return UserResource::collection($users);
    }
    
    public function store(Request $request) {
        $validated = $request->validate(['name' => 'required']);
        $file = $request->file('avatar');
        $user = User::create($validated);
        return response()->json(new UserResource($user), 201);
    }
    
    public function show($id) {
        $user = User::findOrFail($id);
        return UserResource::make($user);
    }
    
    public function update(Request $request, $id) {
        $user = User::findOrFail($id);
        $data = $request->only(['name', 'email']);
        $user->update($data);
        return response()->json(new UserResource($user));
    }
    
    public function destroy($id) {
        $user = User::findOrFail($id);
        $user->delete();
        return response(null, 204);
    }
}`
}

func generateControllerWithPatterns(methodCount int, density string) string {
	content := `<?php
namespace App\Http\Controllers;
use App\Http\Resources\UserResource;
use App\Http\Resources\PostResource;
use Illuminate\Http\Request;
class PatternController extends Controller {`

	for i := 0; i < methodCount; i++ {
		methodName := "method" + string(rune('A'+i%26))

		content += "\n    public function " + methodName + "(Request $request) {\n"

		switch density {
		case "light":
			content += `        $data = $request->all();
        return response()->json($data);`
		case "medium":
			content += `        $data = $request->validate(['name' => 'required']);
        $file = $request->file('upload');
        return response()->json(new UserResource($data), 201);`
		case "heavy":
			content += `        $validated = $request->validate(['name' => 'required']);
        $json = $request->json();
        $file = $request->file('upload');
        $filtered = $request->only(['name', 'email']);
        if (!$validated) abort(422, 'Failed');
        $user = User::create($validated);
        return response()->json(UserResource::collection([$user]), 201);`
		}

		content += "\n    }"
	}

	content += "\n}"
	return content
}
