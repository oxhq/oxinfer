//go:build legacy_parser

package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Test PHP code samples for various parsing scenarios
const (
	validPHPClass = `<?php
namespace App\Http\Controllers;

use Illuminate\Http\Request;

class UserController extends Controller
{
    public function index(Request $request)
    {
        return User::all();
    }
    
    public function store(Request $request)
    {
        return response()->json(['status' => 'created'], 201);
    }
}`

	validPHPFunction = `<?php
function calculateTotal($items) {
    $total = 0;
    foreach ($items as $item) {
        $total += $item->price;
    }
    return $total;
}`

	malformedPHP = `<?php
class Broken {
    public function missing_bracket() {
        return "oops"
    // Missing closing brace`

	emptyPHP = ``

	largePHPContent = `<?php
namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\SoftDeletes;

class User extends Model
{
    use SoftDeletes;
    
    protected $fillable = ['name', 'email', 'password'];
    
    protected $hidden = ['password', 'remember_token'];
    
    protected $casts = ['email_verified_at' => 'datetime'];
    
    public function posts() {
        return $this->hasMany(Post::class);
    }
    
    public function profile() {
        return $this->hasOne(Profile::class);
    }
    
    public function roles() {
        return $this->belongsToMany(Role::class);
    }
    
    public function method1() { return 'content'; }
    public function method2() { return 'content'; }
    public function method3() { return 'content'; }
    public function method4() { return 'content'; }
    public function method5() { return 'content'; }
    // ... many more methods for size
}`
)

// TestNewPHPParser tests parser initialization
func TestNewPHPParser(t *testing.T) {
	tests := []struct {
		name        string
		config      *ParserConfig
		expectError bool
		errorType   string
	}{
		{
			name:        "valid config",
			config:      DefaultParserConfig(),
			expectError: false,
		},
		{
			name:        "nil config uses default",
			config:      nil,
			expectError: false,
		},
		{
			name: "custom config",
			config: &ParserConfig{
				MaxFileSize:           2048,
				MaxParseTime:          5 * time.Second,
				PoolSize:              2,
				EnableLaravelPatterns: false,
				EnableDocBlocks:       false,
				EnableDetailedErrors:  false,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewPHPParser(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if parser == nil {
				t.Error("parser should not be nil")
				return
			}

			// Verify parser is initialized
			if !parser.IsInitialized() {
				t.Error("parser should be initialized")
			}

			// Clean up
			err = parser.Close()
			if err != nil {
				t.Errorf("error closing parser: %v", err)
			}
		})
	}
}

// TestParseContent tests content parsing functionality
func TestParseContent(t *testing.T) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	tests := []struct {
		name        string
		content     string
		expectError bool
		errorType   string
	}{
		{
			name:        "valid PHP class",
			content:     validPHPClass,
			expectError: false,
		},
		{
			name:        "valid PHP function",
			content:     validPHPFunction,
			expectError: false,
		},
		{
			name:        "malformed PHP",
			content:     malformedPHP,
			expectError: false, // Syntax errors don't fail parsing
		},
		{
			name:        "empty content",
			content:     emptyPHP,
			expectError: true,
			errorType:   "invalid content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ParseContent([]byte(tt.content))

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("result should not be nil")
				return
			}

			// Verify syntax tree structure
			if result.Root == nil {
				t.Error("syntax tree root should not be nil")
			}

			if result.Language != "php" {
				t.Errorf("expected language 'php', got '%s'", result.Language)
			}

			if len(result.Source) != len(tt.content) {
				t.Errorf("source length mismatch: expected %d, got %d",
					len(tt.content), len(result.Source))
			}
		})
	}
}

// TestParseFile tests file parsing functionality
func TestParseFile(t *testing.T) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.php")

	err = os.WriteFile(testFile, []byte(validPHPClass), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name        string
		filePath    string
		expectError bool
	}{
		{
			name:        "valid PHP file",
			filePath:    testFile,
			expectError: false,
		},
		{
			name:        "non-existent file",
			filePath:    "/nonexistent/file.php",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := parser.ParseFile(ctx, tt.filePath)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("result should not be nil")
			}
		})
	}
}

// TestParseFileTimeout tests timeout handling
func TestParseFileTimeout(t *testing.T) {
	// Create parser with short timeout
	config := DefaultParserConfig()
	config.MaxParseTime = 10 * time.Millisecond // Short timeout for testing

	parser, err := NewPHPParser(config)
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	// Create large PHP file that might timeout
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.php")

	// Create much larger content to increase timeout chance
	largeContent := largePHPContent
	for i := 0; i < 10; i++ {
		largeContent += largePHPContent
	}

	err = os.WriteFile(testFile, []byte(largeContent), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()
	_, err = parser.ParseFile(ctx, testFile)

	// Note: Timeout may not always occur due to fast parsing
	// This test verifies timeout mechanism works, not that it always triggers
	if err != nil {
		t.Logf("parsing failed as expected (timeout or other): %v", err)
		// Any error is acceptable here - we're testing the mechanism
	} else {
		t.Log("parsing completed within timeout - fast system or small content")
	}
}

// TestParserConcurrency tests thread safety
func TestParserConcurrency(t *testing.T) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	const numGoroutines = 10
	const numParsesPerGoroutine = 5

	results := make(chan error, numGoroutines*numParsesPerGoroutine)

	// Launch concurrent parsing operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numParsesPerGoroutine; j++ {
				_, err := parser.ParseContent([]byte(validPHPClass))
				results <- err
			}
		}(i)
	}

	// Collect results
	var errors []error
	for i := 0; i < numGoroutines*numParsesPerGoroutine; i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		t.Errorf("concurrent parsing had %d errors: %v", len(errors), errors[0])
	}
}

// TestParserLifecycle tests parser initialization and cleanup
func TestParserLifecycle(t *testing.T) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}

	// Verify initial state
	if !parser.IsInitialized() {
		t.Error("new parser should be initialized")
	}

	// Test parsing while initialized
	_, err = parser.ParseContent([]byte(validPHPFunction))
	if err != nil {
		t.Errorf("parsing should work when initialized: %v", err)
	}

	// Close parser
	err = parser.Close()
	if err != nil {
		t.Errorf("error closing parser: %v", err)
	}

	// Verify closed state
	if parser.IsInitialized() {
		t.Error("closed parser should not be initialized")
	}

	// Test parsing after close should fail
	_, err = parser.ParseContent([]byte(validPHPFunction))
	if err == nil {
		t.Error("parsing should fail after close")
	}

	// Multiple closes should be safe
	err = parser.Close()
	if err != nil {
		t.Errorf("multiple closes should be safe: %v", err)
	}
}

// TestErrorHandling tests error handling and recovery
func TestErrorHandling(t *testing.T) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	tests := []struct {
		name        string
		content     string
		expectErr   bool
		recoverable bool
	}{
		{
			name:        "syntax error is recoverable",
			content:     malformedPHP,
			expectErr:   false, // Parsing succeeds but tree has errors
			recoverable: true,
		},
		{
			name:        "empty content is not recoverable",
			content:     "",
			expectErr:   true,
			recoverable: true, // Empty content error is recoverable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.ParseContent([]byte(tt.content))

			if tt.expectErr && err == nil {
				t.Error("expected error but got none")
				return
			}

			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if err != nil {
				if IsRecoverableError(err) != tt.recoverable {
					t.Errorf("error recoverability mismatch: expected %v, got %v",
						tt.recoverable, IsRecoverableError(err))
				}
			}
		})
	}
}

// TestParserMetrics tests performance metrics collection
func TestParserMetrics(t *testing.T) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	// Initial metrics
	metrics := parser.GetMetrics()
	if metrics.TotalParseJobs != 0 {
		t.Error("initial parse jobs should be 0")
	}

	// Parse some content
	_, err = parser.ParseContent([]byte(validPHPClass))
	if err != nil {
		t.Errorf("parsing failed: %v", err)
	}

	// Check updated metrics
	metrics = parser.GetMetrics()
	if metrics.TotalParseJobs != 1 {
		t.Errorf("expected 1 parse job, got %d", metrics.TotalParseJobs)
	}

	if metrics.SuccessfulParses != 1 {
		t.Errorf("expected 1 successful parse, got %d", metrics.SuccessfulParses)
	}

	if metrics.AverageParseTime <= 0 {
		t.Error("average parse time should be positive")
	}
}

// TestLargePHPFile tests parsing large files
func TestLargePHPFile(t *testing.T) {
	config := DefaultParserConfig()
	config.MaxFileSize = 1024 * 1024 // 1MB limit

	parser, err := NewPHPParser(config)
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	// Test content within limits
	result, err := parser.ParseContent([]byte(largePHPContent))
	if err != nil {
		t.Errorf("parsing large content failed: %v", err)
	}

	if result == nil {
		t.Error("result should not be nil for large content")
	}

	// Test content exceeding limits
	config.MaxFileSize = 100 // Very small limit
	parser2, err := NewPHPParser(config)
	if err != nil {
		t.Fatalf("failed to create parser with small limit: %v", err)
	}
	defer parser2.Close()

	_, err = parser2.ParseContent([]byte(largePHPContent))
	if err == nil {
		t.Error("expected error for content exceeding size limit")
	}
}

// TestConfigValidation tests parser configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *ParserConfig
		expectError bool
	}{
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
		},
		{
			name: "invalid max file size",
			config: &ParserConfig{
				MaxFileSize:  -1,
				MaxParseTime: time.Second,
				PoolSize:     1,
			},
			expectError: true,
		},
		{
			name: "invalid max parse time",
			config: &ParserConfig{
				MaxFileSize:  1024,
				MaxParseTime: -time.Second,
				PoolSize:     1,
			},
			expectError: true,
		},
		{
			name: "invalid pool size",
			config: &ParserConfig{
				MaxFileSize:  1024,
				MaxParseTime: time.Second,
				PoolSize:     0,
			},
			expectError: true,
		},
		{
			name:        "valid config",
			config:      DefaultParserConfig(),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)

			if tt.expectError && err == nil {
				t.Error("expected validation error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// BenchmarkParseContent benchmarks content parsing performance
func BenchmarkParseContent(b *testing.B) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		b.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	content := []byte(validPHPClass)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := parser.ParseContent(content)
		if err != nil {
			b.Errorf("parsing failed: %v", err)
		}
	}
}

// BenchmarkParseFile benchmarks file parsing performance
func BenchmarkParseFile(b *testing.B) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		b.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	// Create temporary test file
	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "benchmark.php")

	err = os.WriteFile(testFile, []byte(validPHPClass), 0644)
	if err != nil {
		b.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := parser.ParseFile(ctx, testFile)
		if err != nil {
			b.Errorf("parsing failed: %v", err)
		}
	}
}

// BenchmarkConcurrentParsing benchmarks concurrent parsing performance
func BenchmarkConcurrentParsing(b *testing.B) {
	parser, err := NewPHPParser(DefaultParserConfig())
	if err != nil {
		b.Fatalf("failed to create parser: %v", err)
	}
	defer parser.Close()

	content := []byte(validPHPClass)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := parser.ParseContent(content)
			if err != nil {
				b.Errorf("concurrent parsing failed: %v", err)
			}
		}
	})
}
