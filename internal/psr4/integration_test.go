package psr4

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPSR4ResolverIntegration(t *testing.T) {
	// Create a temporary Laravel-like project structure
	tempDir := t.TempDir()

	// Create directories
	dirs := []string{
		"app/Models",
		"app/Http/Controllers",
		"app/Services",
		"tests/Unit",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create composer.json
	composerContent := `{
		"name": "test/laravel-app",
		"type": "project",
		"autoload": {
			"psr-4": {
				"App\\": "app/",
				"Tests\\": "tests/"
			}
		},
		"autoload-dev": {
			"psr-4": {
				"Tests\\": "tests/"
			}
		}
	}`

	composerPath := filepath.Join(tempDir, "composer.json")
	if err := os.WriteFile(composerPath, []byte(composerContent), 0644); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	// Create test PHP files
	testFiles := map[string]string{
		"app/Models/User.php": `<?php
namespace App\Models;
use Illuminate\Database\Eloquent\Model;
class User extends Model {}`,
		"app/Models/Post.php": `<?php
namespace App\Models;
class Post extends Model {}`,
		"app/Http/Controllers/UserController.php": `<?php
namespace App\Http\Controllers;
class UserController extends Controller {}`,
		"app/Services/EmailService.php": `<?php
namespace App\Services;
class EmailService {}`,
		"tests/Unit/UserTest.php": `<?php
namespace Tests\Unit;
class UserTest extends TestCase {}`,
		// Non-class files that should be skipped
		"app/helpers.php": `<?php
// helper functions`,
		"app/bootstrap.php": `<?php
// bootstrap code`,
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filePath, err)
		}
	}

	// Create resolver
	config := &ResolverConfig{
		ProjectRoot:  tempDir,
		ComposerPath: "composer.json",
		IncludeDev:   true,
		CacheEnabled: false,
	}

	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		t.Fatalf("Failed to create PSR-4 resolver: %v", err)
	}

	// Initialize resolver by loading composer data
	ctx := context.Background()
	allClasses, err := resolver.GetAllClasses(ctx)
	if err != nil {
		t.Fatalf("GetAllClasses failed: %v", err)
	}

	// Verify expected classes are found
	expectedClasses := map[string]bool{
		"App\\Models\\User":                      true,
		"App\\Models\\Post":                      true,
		"App\\Http\\Controllers\\UserController": true,
		"App\\Services\\EmailService":            true,
		"Tests\\Unit\\UserTest":                  true,
	}

	// Check that all expected classes were found
	for expectedClass := range expectedClasses {
		if _, found := allClasses[expectedClass]; !found {
			t.Errorf("Expected class %s not found in results", expectedClass)
		}
	}

	// Verify non-class files were not included
	unexpectedClasses := []string{
		"App\\helpers",
		"App\\bootstrap",
	}

	for _, unexpectedClass := range unexpectedClasses {
		if _, found := allClasses[unexpectedClass]; found {
			t.Errorf("Unexpected class %s found in results", unexpectedClass)
		}
	}

	// Test individual class resolution
	userPath, err := resolver.ResolveClass(ctx, "App\\Models\\User")
	if err != nil {
		t.Fatalf("Failed to resolve App\\Models\\User: %v", err)
	}

	// Use filepath.EvalSymlinks to handle macOS /var -> /private/var symlink
	expectedPath := filepath.Join(tempDir, "app", "Models", "User.php")
	resolvedExpectedPath, err := filepath.EvalSymlinks(expectedPath)
	if err == nil {
		expectedPath = resolvedExpectedPath
	}

	resolvedUserPath, err := filepath.EvalSymlinks(userPath)
	if err == nil {
		userPath = resolvedUserPath
	}

	if userPath != expectedPath {
		t.Errorf("ResolveClass returned wrong path: got %s, expected %s", userPath, expectedPath)
	}

	// Test non-existent class
	_, err = resolver.ResolveClass(ctx, "App\\Models\\NonExistent")
	if err == nil {
		t.Error("Expected error when resolving non-existent class")
	}

	// Test GetNamespaces
	namespaces, err := resolver.GetNamespaces()
	if err != nil {
		t.Fatalf("GetNamespaces failed: %v", err)
	}

	expectedNamespaces := []string{"App\\", "Tests\\"}
	if len(namespaces) < len(expectedNamespaces) {
		t.Errorf("Expected at least %d namespaces, got %d", len(expectedNamespaces), len(namespaces))
	}

	for _, expected := range expectedNamespaces {
		found := false
		for _, ns := range namespaces {
			if ns == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected namespace %s not found in results", expected)
		}
	}
}

func TestPSR4ResolverWithoutDevDependencies(t *testing.T) {
	tempDir := t.TempDir()

	// Create structure with dev and non-dev classes
	dirs := []string{"app/Models", "tests/Unit"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	composerContent := `{
		"autoload": {
			"psr-4": {
				"App\\": "app/"
			}
		},
		"autoload-dev": {
			"psr-4": {
				"Tests\\": "tests/"
			}
		}
	}`

	if err := os.WriteFile(filepath.Join(tempDir, "composer.json"), []byte(composerContent), 0644); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	// Create files
	testFiles := map[string]string{
		"app/Models/User.php":     `<?php namespace App\Models; class User {}`,
		"tests/Unit/UserTest.php": `<?php namespace Tests\Unit; class UserTest {}`,
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", filePath, err)
		}
	}

	// Create resolver WITHOUT dev dependencies
	config := &ResolverConfig{
		ProjectRoot:  tempDir,
		IncludeDev:   false, // Don't include dev dependencies
		CacheEnabled: false,
	}

	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	ctx := context.Background()
	allClasses, err := resolver.GetAllClasses(ctx)
	if err != nil {
		t.Fatalf("GetAllClasses failed: %v", err)
	}

	// Should find App\ classes but not Tests\ classes
	if _, found := allClasses["App\\Models\\User"]; !found {
		t.Error("App\\Models\\User should be found")
	}

	if _, found := allClasses["Tests\\Unit\\UserTest"]; found {
		t.Error("Tests\\Unit\\UserTest should NOT be found when dev dependencies are excluded")
	}
}
