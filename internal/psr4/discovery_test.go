package psr4

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverClassesInMapping(t *testing.T) {
	// Create temporary directory structure
	tempDir := t.TempDir()
	
	// Create directory structure: app/Models/
	modelsDir := filepath.Join(tempDir, "app", "Models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	// Create test PHP files
	testFiles := map[string]string{
		"User.php": `<?php
namespace App\Models;
class User extends Model {
}`,
		"Post.php": `<?php
namespace App\Models;
class Post extends Model {
}`,
		"admin.php": `<?php
// This is not a class file
echo "admin functions";
`,
		"Helper/StringHelper.php": `<?php
namespace App\Models\Helper;
class StringHelper {
}`,
	}
	
	for filename, content := range testFiles {
		filePath := filepath.Join(modelsDir, filename)
		fileDir := filepath.Dir(filePath)
		if err := os.MkdirAll(fileDir, 0755); err != nil {
			t.Fatalf("Failed to create subdirectory for %s: %v", filename, err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}
	
	// Create resolver
	config := &ResolverConfig{
		ProjectRoot: tempDir,
		CacheEnabled: false,
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		t.Fatalf("Failed to create PSR-4 resolver: %v", err)
	}
	
	// Create test mapping
	mapping := NamespaceMapping{
		Namespace: "App\\Models\\",
		Paths:     []string{"app/Models"},
		IsDevDependency: false,
	}
	
	// Test discovery
	ctx := context.Background()
	classes, err := resolver.discoverClassesInMapping(ctx, mapping)
	if err != nil {
		t.Fatalf("discoverClassesInMapping failed: %v", err)
	}
	
	// Verify results
	expectedClasses := []string{
		"App\\Models\\User",
		"App\\Models\\Post", 
		"App\\Models\\Helper\\StringHelper",
	}
	
	if len(classes) != len(expectedClasses) {
		t.Errorf("Expected %d classes, got %d", len(expectedClasses), len(classes))
	}
	
	for _, expected := range expectedClasses {
		if _, found := classes[expected]; !found {
			t.Errorf("Expected class %s not found in results", expected)
		}
	}
	
	// Verify admin.php was skipped
	if _, found := classes["App\\Models\\admin"]; found {
		t.Error("admin.php should have been skipped but was included")
	}
}

func TestPathToClassName(t *testing.T) {
	resolver := &DefaultPSR4Resolver{}
	
	tests := []struct {
		namespace string
		relPath   string
		expected  string
	}{
		{"App\\Models\\", "User.php", "App\\Models\\User"},
		{"App\\", "Http/Controllers/UserController.php", "App\\Http\\Controllers\\UserController"},
		{"", "GlobalClass.php", "GlobalClass"},
		{"MyNamespace\\", "Sub/Dir/Class.php", "MyNamespace\\Sub\\Dir\\Class"},
	}
	
	for _, test := range tests {
		result := resolver.pathToClassName(test.namespace, test.relPath)
		if result != test.expected {
			t.Errorf("pathToClassName(%q, %q) = %q, expected %q", 
				test.namespace, test.relPath, result, test.expected)
		}
	}
}

func TestShouldSkipFile(t *testing.T) {
	resolver := &DefaultPSR4Resolver{}
	
	tests := []struct {
		filename string
		skip     bool
	}{
		{"User.php", false},
		{"PostController.php", false},
		{"index.php", true},
		{"web.php", true},
		{"api.php", true},
		{"bootstrap.php", true},
		{"config.php", true},
		{"user.blade.php", true},
		{"admin.php", true}, // lowercase files should be skipped
	}
	
	for _, test := range tests {
		result := resolver.shouldSkipFile(test.filename)
		if result != test.skip {
			t.Errorf("shouldSkipFile(%q) = %t, expected %t", test.filename, result, test.skip)
		}
	}
}

func TestDiscoverMultiplePaths(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create two directories with classes
	dir1 := filepath.Join(tempDir, "src1")
	dir2 := filepath.Join(tempDir, "src2")
	
	if err := os.MkdirAll(dir1, 0755); err != nil {
		t.Fatalf("Failed to create dir1: %v", err)
	}
	if err := os.MkdirAll(dir2, 0755); err != nil {
		t.Fatalf("Failed to create dir2: %v", err)
	}
	
	// Create classes in both directories
	file1 := filepath.Join(dir1, "ClassOne.php")
	file2 := filepath.Join(dir2, "ClassTwo.php")
	
	if err := os.WriteFile(file1, []byte(`<?php
namespace Test;
class ClassOne {}
`), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}
	
	if err := os.WriteFile(file2, []byte(`<?php
namespace Test;
class ClassTwo {}
`), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}
	
	// Create resolver
	config := &ResolverConfig{
		ProjectRoot: tempDir,
		CacheEnabled: false,
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		t.Fatalf("Failed to create PSR-4 resolver: %v", err)
	}
	
	// Create mapping with multiple paths
	mapping := NamespaceMapping{
		Namespace: "Test\\",
		Paths:     []string{"src1", "src2"},
		IsDevDependency: false,
	}
	
	// Test discovery
	ctx := context.Background()
	classes, err := resolver.discoverClassesInMapping(ctx, mapping)
	if err != nil {
		t.Fatalf("discoverClassesInMapping failed: %v", err)
	}
	
	// Should find both classes
	if len(classes) != 2 {
		t.Errorf("Expected 2 classes, got %d", len(classes))
	}
	
	if _, found := classes["Test\\ClassOne"]; !found {
		t.Error("ClassOne not found")
	}
	
	if _, found := classes["Test\\ClassTwo"]; !found {
		t.Error("ClassTwo not found")
	}
}