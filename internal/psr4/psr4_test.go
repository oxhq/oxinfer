package psr4

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// Test fixtures and utilities

func createTestComposer(dir string, composerContent map[string]interface{}) error {
	composerPath := filepath.Join(dir, "composer.json")
	
	// Marshal the content to JSON
	data, err := json.MarshalIndent(composerContent, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(composerPath, data, 0644)
}

func createTestProject(t *testing.T) (string, func()) {
	tempDir, err := os.MkdirTemp("", "psr4-integration-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	
	// Create typical Laravel project structure
	dirs := []string{
		"app/Http/Controllers",
		"app/Models",
		"app/Services",
		"tests/Unit",
		"tests/Feature",
		"database/migrations",
		"routes",
	}
	
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
	
	// Create test PHP files
	testFiles := map[string]string{
		"app/Http/Controllers/UserController.php": `<?php
namespace App\Http\Controllers;
class UserController extends Controller {
    public function index() {}
}`,
		"app/Models/User.php": `<?php
namespace App\Models;
class User extends Model {
    protected $fillable = ['name', 'email'];
}`,
		"app/Services/EmailService.php": `<?php
namespace App\Services;
class EmailService {
    public function send() {}
}`,
		"tests/Unit/UserTest.php": `<?php
namespace Tests\Unit;
class UserTest extends TestCase {
    public function test_user_creation() {}
}`,
		"tests/Feature/AuthTest.php": `<?php
namespace Tests\Feature;
class AuthTest extends TestCase {
    public function test_login() {}
}`,
	}
	
	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filePath, err)
		}
	}
	
	// Create Laravel-style composer.json
	composerContent := map[string]interface{}{
		"name": "laravel/laravel",
		"type": "project",
		"autoload": map[string]interface{}{
			"psr-4": map[string]interface{}{
				"App\\": "app/",
			},
		},
		"autoload-dev": map[string]interface{}{
			"psr-4": map[string]interface{}{
				"Tests\\": "tests/",
			},
		},
	}
	
	if err := createTestComposer(tempDir, composerContent); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}
	
	cleanup := func() {
		os.RemoveAll(tempDir)
	}
	
	return tempDir, cleanup
}

func createTestManifest(projectRoot, composerPath string) *manifest.Manifest {
	return &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectRoot,
			Composer: composerPath,
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app", "routes"},
		},
		Cache: &manifest.CacheConfig{
			Enabled: &[]bool{true}[0],
		},
	}
}

// Integration tests

func TestNewPSR4Resolver(t *testing.T) {
	tests := []struct {
		name      string
		config    *ResolverConfig
		wantError bool
		errorMsg  string
	}{
		{
			name:      "nil config",
			config:    nil,
			wantError: true,
			errorMsg:  "config cannot be nil",
		},
		{
			name: "empty project root",
			config: &ResolverConfig{
				ProjectRoot: "",
			},
			wantError: true,
			errorMsg:  "project root cannot be empty",
		},
		{
			name: "valid config with defaults",
			config: &ResolverConfig{
				ProjectRoot:  ".",
				CacheEnabled: true,
			},
			wantError: false,
		},
		{
			name: "custom cache size",
			config: &ResolverConfig{
				ProjectRoot:  ".",
				CacheEnabled: true,
				CacheSize:    2000,
			},
			wantError: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := NewPSR4Resolver(tt.config)
			
			if tt.wantError {
				if err == nil {
					t.Errorf("NewPSR4Resolver() expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}
			
			if err != nil {
				t.Errorf("NewPSR4Resolver() unexpected error: %v", err)
				return
			}
			
			if resolver == nil {
				t.Error("NewPSR4Resolver() returned nil resolver")
				return
			}
			
			// Verify configuration is properly set
			config := resolver.GetConfig()
			if config.ComposerPath == "" {
				t.Error("Expected default composer path to be set")
			}
			if config.CacheSize <= 0 {
				t.Error("Expected default cache size to be positive")
			}
		})
	}
}

func TestNewPSR4ResolverFromManifest(t *testing.T) {
	projectDir, cleanup := createTestProject(t)
	defer cleanup()
	
	tests := []struct {
		name      string
		manifest  *manifest.Manifest
		wantError bool
		errorMsg  string
	}{
		{
			name:      "nil manifest",
			manifest:  nil,
			wantError: true,
			errorMsg:  "manifest cannot be nil",
		},
		{
			name: "valid manifest",
			manifest: createTestManifest(projectDir, "composer.json"),
			wantError: false,
		},
		{
			name: "manifest with custom composer path",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root:     projectDir,
					Composer: "custom-composer.json",
				},
			},
			wantError: true, // custom composer file doesn't exist
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := NewPSR4ResolverFromManifest(tt.manifest)
			
			if tt.wantError {
				if err == nil {
					t.Errorf("NewPSR4ResolverFromManifest() expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}
			
			if err != nil {
				t.Errorf("NewPSR4ResolverFromManifest() unexpected error: %v", err)
				return
			}
			
			if resolver == nil {
				t.Error("NewPSR4ResolverFromManifest() returned nil resolver")
				return
			}
			
			// Verify the resolver is properly initialized
			if !resolver.IsInitialized() {
				t.Error("Expected resolver to be initialized")
			}
		})
	}
}

func TestPSR4Resolver_ResolveClass(t *testing.T) {
	projectDir, cleanup := createTestProject(t)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	
	ctx := context.Background()
	
	tests := []struct {
		name      string
		fqcn      string
		wantPath  string
		wantError bool
		errorType string
	}{
		{
			name:     "resolve App\\Models\\User",
			fqcn:     "App\\Models\\User",
			wantPath: filepath.Join(projectDir, "app/Models/User.php"),
		},
		{
			name:     "resolve App\\Http\\Controllers\\UserController",
			fqcn:     "App\\Http\\Controllers\\UserController",
			wantPath: filepath.Join(projectDir, "app/Http/Controllers/UserController.php"),
		},
		{
			name:     "resolve Tests\\Unit\\UserTest",
			fqcn:     "Tests\\Unit\\UserTest",
			wantPath: filepath.Join(projectDir, "tests/Unit/UserTest.php"),
		},
		{
			name:      "invalid FQCN format",
			fqcn:      "invalid/class",
			wantError: true,
			errorType: "MappingError",
		},
		{
			name:      "empty FQCN",
			fqcn:      "",
			wantError: true,
			errorType: "MappingError",
		},
		{
			name:      "non-existent class",
			fqcn:      "App\\Models\\NonExistentClass",
			wantError: true,
			errorType: "ResolutionError",
		},
		{
			name:      "unmappable namespace",
			fqcn:      "Unknown\\Namespace\\Class",
			wantError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, err := resolver.ResolveClass(ctx, tt.fqcn)
			
			if tt.wantError {
				if err == nil {
					t.Errorf("ResolveClass() expected error but got none")
					return
				}
				
				// Check specific error types if specified
				if tt.errorType != "" {
					switch tt.errorType {
					case "MappingError":
						var mappingErr *MappingError
						if !strings.Contains(err.Error(), "MappingError") && !errors.As(err, &mappingErr) {
							t.Errorf("Expected MappingError, got %T: %v", err, err)
						}
					case "ResolutionError":
						var resolutionErr *ResolutionError
						if !strings.Contains(err.Error(), "ResolutionError") && !errors.As(err, &resolutionErr) {
							t.Errorf("Expected ResolutionError, got %T: %v", err, err)
						}
					}
				}
				return
			}
			
			if err != nil {
				t.Errorf("ResolveClass() unexpected error: %v", err)
				return
			}
			
			// Resolve expected path symlinks for comparison (handles macOS /private differences)
			expectedResolved := tt.wantPath
			if tt.wantPath != "" {
				if resolved, err := filepath.EvalSymlinks(tt.wantPath); err == nil {
					expectedResolved = resolved
				}
			}
			
			if gotPath != expectedResolved {
				t.Errorf("ResolveClass() = %v, want %v", gotPath, expectedResolved)
			}
			
			// Verify the resolved file actually exists
			if _, err := os.Stat(gotPath); os.IsNotExist(err) {
				t.Errorf("Resolved path does not exist: %s", gotPath)
			}
		})
	}
}

func TestPSR4Resolver_ResolveClass_Caching(t *testing.T) {
	projectDir, cleanup := createTestProject(t)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	
	ctx := context.Background()
	fqcn := "App\\Models\\User"
	
	// First resolution - should cache the result
	start := time.Now()
	path1, err := resolver.ResolveClass(ctx, fqcn)
	firstDuration := time.Since(start)
	
	if err != nil {
		t.Fatalf("First ResolveClass() failed: %v", err)
	}
	
	// Second resolution - should be faster due to caching
	start = time.Now()
	path2, err := resolver.ResolveClass(ctx, fqcn)
	secondDuration := time.Since(start)
	
	if err != nil {
		t.Fatalf("Second ResolveClass() failed: %v", err)
	}
	
	// Results should be identical
	if path1 != path2 {
		t.Errorf("Cached result differs: %s != %s", path1, path2)
	}
	
	// Second call should be significantly faster (cached)
	if secondDuration > firstDuration {
		t.Logf("Warning: cached resolution (%v) was slower than initial (%v)", secondDuration, firstDuration)
		// This is not a hard failure as timing can vary, but log for awareness
	}
}

func TestPSR4Resolver_GetAllClasses(t *testing.T) {
	projectDir, cleanup := createTestProject(t)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	
	ctx := context.Background()
	
	classes, err := resolver.GetAllClasses(ctx)
	if err != nil {
		t.Fatalf("GetAllClasses() failed: %v", err)
	}
	
	// The implementation returns empty map as file discovery is not yet implemented
	// This test verifies the method works without error
	if classes == nil {
		t.Error("GetAllClasses() returned nil map")
	}
	
	// Verify deterministic behavior - calling multiple times should return same result
	classes2, err := resolver.GetAllClasses(ctx)
	if err != nil {
		t.Fatalf("Second GetAllClasses() failed: %v", err)
	}
	
	if len(classes) != len(classes2) {
		t.Errorf("GetAllClasses() returned different results on subsequent calls")
	}
}

func TestPSR4Resolver_Refresh(t *testing.T) {
	projectDir, cleanup := createTestProject(t)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	
	ctx := context.Background()
	fqcn := "App\\Models\\User"
	
	// Resolve a class to populate cache
	_, err = resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		t.Fatalf("ResolveClass() failed: %v", err)
	}
	
	// Refresh the resolver
	err = resolver.Refresh()
	if err != nil {
		t.Fatalf("Refresh() failed: %v", err)
	}
	
	// Verify resolver is still initialized
	if !resolver.IsInitialized() {
		t.Error("Expected resolver to remain initialized after refresh")
	}
	
	// Should still be able to resolve classes
	_, err = resolver.ResolveClass(ctx, fqcn)
	if err != nil {
		t.Errorf("ResolveClass() failed after refresh: %v", err)
	}
}

func TestPSR4Resolver_GetNamespaces(t *testing.T) {
	projectDir, cleanup := createTestProject(t)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	
	namespaces, err := resolver.GetNamespaces()
	if err != nil {
		t.Fatalf("GetNamespaces() failed: %v", err)
	}
	
	// Should contain both App\ and Tests\ namespaces
	expectedNamespaces := []string{"App\\", "Tests\\"}
	
	if len(namespaces) != len(expectedNamespaces) {
		t.Errorf("Expected %d namespaces, got %d", len(expectedNamespaces), len(namespaces))
	}
	
	// Verify expected namespaces are present
	nsMap := make(map[string]bool)
	for _, ns := range namespaces {
		nsMap[ns] = true
	}
	
	for _, expected := range expectedNamespaces {
		if !nsMap[expected] {
			t.Errorf("Expected namespace %s not found in result", expected)
		}
	}
	
	// Verify deterministic ordering (should be sorted)
	namespaces2, err := resolver.GetNamespaces()
	if err != nil {
		t.Fatalf("Second GetNamespaces() failed: %v", err)
	}
	
	if len(namespaces) != len(namespaces2) {
		t.Error("GetNamespaces() returned different length on subsequent calls")
	}
	
	for i, ns := range namespaces {
		if i < len(namespaces2) && ns != namespaces2[i] {
			t.Errorf("GetNamespaces() order differs at index %d: %s != %s", i, ns, namespaces2[i])
		}
	}
}

func TestPSR4Resolver_ContextCancellation(t *testing.T) {
	projectDir, cleanup := createTestProject(t)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	
	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	
	// Try to resolve a class with cancelled context
	_, err = resolver.ResolveClass(ctx, "App\\Models\\User")
	
	// Should handle cancellation gracefully
	if err == nil {
		t.Error("Expected error for cancelled context")
		return
	}
	
	// Error should indicate cancellation
	if !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected cancellation error, got: %v", err)
	}
}

func TestPSR4Resolver_ComplexComposerConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "psr4-complex-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create complex directory structure
	dirs := []string{
		"src/Core",
		"src/Api/V1",
		"lib/Utils",
		"tests/Unit",
		"app/Controllers",
	}
	
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
	
	// Create test files
	testFiles := map[string]string{
		"src/Core/Engine.php": `<?php
namespace MyApp\Core;
class Engine {}`,
		"src/Api/V1/UserApi.php": `<?php
namespace MyApp\Api\V1;
class UserApi {}`,
		"lib/Utils/Helper.php": `<?php
namespace MyApp\Utils;
class Helper {}`,
		"app/Controllers/HomeController.php": `<?php
namespace App\Controllers;
class HomeController {}`,
	}
	
	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filePath, err)
		}
	}
	
	// Create complex composer.json with multiple namespace mappings
	composerContent := map[string]interface{}{
		"name": "myapp/complex",
		"autoload": map[string]interface{}{
			"psr-4": map[string]interface{}{
				"MyApp\\Core\\": "src/Core/",
				"MyApp\\Api\\":  "src/Api/",
				"MyApp\\Utils\\": []string{"lib/Utils/", "src/Utils/"},
				"App\\": "app/",
			},
		},
	}
	
	if err := createTestComposer(tempDir, composerContent); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}
	
	// Create resolver
	manifest := createTestManifest(tempDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	
	ctx := context.Background()
	
	// Test resolution of classes from different namespaces
	tests := []struct {
		fqcn     string
		wantFile string
	}{
		{
			fqcn:     "MyApp\\Core\\Engine",
			wantFile: "src/Core/Engine.php",
		},
		{
			fqcn:     "MyApp\\Api\\V1\\UserApi",
			wantFile: "src/Api/V1/UserApi.php",
		},
		{
			fqcn:     "MyApp\\Utils\\Helper",
			wantFile: "lib/Utils/Helper.php", // First path should be tried
		},
		{
			fqcn:     "App\\Controllers\\HomeController",
			wantFile: "app/Controllers/HomeController.php",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.fqcn, func(t *testing.T) {
			resolvedPath, err := resolver.ResolveClass(ctx, tt.fqcn)
			if err != nil {
				t.Fatalf("ResolveClass(%s) failed: %v", tt.fqcn, err)
			}
			
			expectedPath := filepath.Join(tempDir, tt.wantFile)
			
			// Resolve expected path symlinks for comparison (handles macOS /private differences)
			expectedResolved, err := filepath.EvalSymlinks(expectedPath)
			if err != nil {
				expectedResolved = expectedPath // fallback to original if can't resolve
			}
			
			if resolvedPath != expectedResolved {
				t.Errorf("ResolveClass(%s) = %s, want %s", tt.fqcn, resolvedPath, expectedResolved)
			}
		})
	}
}

// Benchmark tests for performance validation

func BenchmarkPSR4Resolver_ResolveClass(b *testing.B) {
	projectDir, cleanup := createTestProjectB(b)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}
	
	ctx := context.Background()
	fqcn := "App\\Models\\User"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := resolver.ResolveClass(ctx, fqcn)
		if err != nil {
			b.Fatalf("ResolveClass() failed: %v", err)
		}
	}
}

func BenchmarkPSR4Resolver_ResolveClass_WithoutCache(b *testing.B) {
	projectDir, cleanup := createTestProjectB(b)
	defer cleanup()
	
	config := &ResolverConfig{
		ProjectRoot:  projectDir,
		ComposerPath: "composer.json",
		IncludeDev:   true,
		CacheEnabled: false, // Disable caching
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}
	
	if err := resolver.Refresh(); err != nil {
		b.Fatalf("Failed to initialize resolver: %v", err)
	}
	
	ctx := context.Background()
	fqcn := "App\\Models\\User"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := resolver.ResolveClass(ctx, fqcn)
		if err != nil {
			b.Fatalf("ResolveClass() failed: %v", err)
		}
	}
}

func BenchmarkPSR4Resolver_GetNamespaces(b *testing.B) {
	projectDir, cleanup := createTestProjectB(b)
	defer cleanup()
	
	manifest := createTestManifest(projectDir, "composer.json")
	resolver, err := NewPSR4ResolverFromManifest(manifest)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := resolver.GetNamespaces()
		if err != nil {
			b.Fatalf("GetNamespaces() failed: %v", err)
		}
	}
}

// Helper functions for tests

func createTestProjectB(t testing.TB) (string, func()) {
	tempDir, err := os.MkdirTemp("", "psr4-integration-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	
	// Create typical Laravel project structure
	dirs := []string{
		"app/Http/Controllers",
		"app/Models",
		"app/Services",
		"tests/Unit",
		"tests/Feature",
	}
	
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
	
	// Create test PHP files
	testFiles := map[string]string{
		"app/Http/Controllers/UserController.php": `<?php
namespace App\Http\Controllers;
class UserController extends Controller {}`,
		"app/Models/User.php": `<?php
namespace App\Models;
class User extends Model {}`,
		"app/Services/EmailService.php": `<?php
namespace App\Services;
class EmailService {}`,
		"tests/Unit/UserTest.php": `<?php
namespace Tests\Unit;
class UserTest extends TestCase {}`,
	}
	
	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filePath, err)
		}
	}
	
	// Create Laravel-style composer.json
	composerContent := map[string]interface{}{
		"name": "laravel/laravel",
		"type": "project",
		"autoload": map[string]interface{}{
			"psr-4": map[string]interface{}{
				"App\\": "app/",
			},
		},
		"autoload-dev": map[string]interface{}{
			"psr-4": map[string]interface{}{
				"Tests\\": "tests/",
			},
		},
	}
	
	if err := createTestComposer(tempDir, composerContent); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}
	
	cleanup := func() {
		os.RemoveAll(tempDir)
	}
	
	return tempDir, cleanup
}