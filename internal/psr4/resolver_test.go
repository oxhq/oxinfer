package psr4

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewPathResolver(t *testing.T) {
	tests := []struct {
		name      string
		baseDir   string
		wantError bool
		setupFunc func() (string, func()) // returns path and cleanup function
	}{
		{
			name: "valid existing directory",
			setupFunc: func() (string, func()) {
				tempDir, err := os.MkdirTemp("", "psr4-test-*")
				if err != nil {
					t.Fatalf("Failed to create temp dir: %v", err)
				}
				return tempDir, func() { os.RemoveAll(tempDir) }
			},
			wantError: false,
		},
		{
			name: "relative path gets converted to absolute",
			setupFunc: func() (string, func()) {
				tempDir, err := os.MkdirTemp("", "psr4-test-*")
				if err != nil {
					t.Fatalf("Failed to create temp dir: %v", err)
				}
				// Return relative path that should be converted to absolute
				relPath, _ := filepath.Rel(".", tempDir)
				return relPath, func() { os.RemoveAll(tempDir) }
			},
			wantError: false,
		},
		{
			name:      "non-existent directory",
			baseDir:   "/non/existent/directory",
			wantError: true,
		},
		{
			name: "file instead of directory",
			setupFunc: func() (string, func()) {
				tempFile, err := os.CreateTemp("", "psr4-test-file-*")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				tempFile.Close()
				return tempFile.Name(), func() { os.Remove(tempFile.Name()) }
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var baseDir string
			var cleanup func()
			
			if tt.setupFunc != nil {
				baseDir, cleanup = tt.setupFunc()
				defer cleanup()
			} else {
				baseDir = tt.baseDir
			}

			resolver, err := NewPathResolver(baseDir)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewPathResolver() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("NewPathResolver() unexpected error: %v", err)
				return
			}

			if resolver == nil {
				t.Error("NewPathResolver() returned nil resolver")
			}
		})
	}
}

func TestPathResolver_ResolvePath(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "psr4-resolver-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test file structure
	testFiles := []string{
		"app/Http/Controllers/UserController.php",
		"app/Models/User.php",
		"tests/Unit/UserTest.php",
		"src/Service/EmailService.php",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(tempDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", file, err)
		}
		if err := os.WriteFile(fullPath, []byte("<?php // test file"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	tests := []struct {
		name       string
		candidates []string
		baseDir    string
		wantPath   string
		wantError  bool
		errorType  string
	}{
		{
			name:       "first candidate exists",
			candidates: []string{"app/Http/Controllers/UserController.php", "app/Models/User.php"},
			wantPath:   filepath.Join(tempDir, "app/Http/Controllers/UserController.php"),
			wantError:  false,
		},
		{
			name:       "second candidate exists",
			candidates: []string{"non/existent/file.php", "app/Models/User.php"},
			wantPath:   filepath.Join(tempDir, "app/Models/User.php"),
			wantError:  false,
		},
		{
			name:       "absolute path candidate",
			candidates: []string{filepath.Join(tempDir, "src/Service/EmailService.php")},
			wantPath:   filepath.Join(tempDir, "src/Service/EmailService.php"),
			wantError:  false,
		},
		{
			name:       "no candidates exist",
			candidates: []string{"non/existent/file1.php", "non/existent/file2.php"},
			wantError:  true,
			errorType:  "ResolutionError",
		},
		{
			name:       "empty candidates list",
			candidates: []string{},
			wantError:  true,
			errorType:  "ResolutionError",
		},
		{
			name:       "path traversal attack",
			candidates: []string{"../../../etc/passwd"},
			wantError:  true,
			errorType:  "ResolutionError",
		},
		{
			name:       "path with null byte",
			candidates: []string{"app/test\x00file.php"},
			wantError:  true,
			errorType:  "ResolutionError",
		},
		{
			name:       "custom base directory",
			candidates: []string{"Unit/UserTest.php"},
			baseDir:    filepath.Join(tempDir, "tests"),
			wantPath:   filepath.Join(tempDir, "tests/Unit/UserTest.php"),
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			
			gotPath, err := resolver.ResolvePath(ctx, tt.candidates, tt.baseDir)

			if tt.wantError {
				if err == nil {
					t.Errorf("ResolvePath() expected error but got none")
					return
				}
				
				if tt.errorType == "ResolutionError" {
					if _, ok := err.(*ResolutionError); !ok {
						t.Errorf("ResolvePath() expected ResolutionError, got %T", err)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("ResolvePath() unexpected error: %v", err)
				return
			}

			if gotPath != tt.wantPath {
				t.Errorf("ResolvePath() = %v, want %v", gotPath, tt.wantPath)
			}
		})
	}
}

func TestPathResolver_ResolvePath_ContextCancellation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "psr4-resolver-cancel-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	candidates := []string{"app/Models/User.php"}
	_, err = resolver.ResolvePath(ctx, candidates, "")

	if err == nil {
		t.Error("ResolvePath() expected error for cancelled context but got none")
		return
	}

	resErr, ok := err.(*ResolutionError)
	if !ok {
		t.Errorf("ResolvePath() expected ResolutionError, got %T", err)
		return
	}

	if !strings.Contains(resErr.Message, "cancelled") {
		t.Errorf("ResolutionError message should mention cancellation, got: %s", resErr.Message)
	}
}

func TestPathResolver_ResolvePath_Timeout(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "psr4-resolver-timeout-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	
	// Give context time to expire
	time.Sleep(1 * time.Millisecond)

	candidates := []string{"app/Models/User.php"}
	_, err = resolver.ResolvePath(ctx, candidates, "")

	if err == nil {
		t.Error("ResolvePath() expected timeout error but got none")
		return
	}

	resErr, ok := err.(*ResolutionError)
	if !ok {
		t.Errorf("ResolvePath() expected ResolutionError, got %T", err)
		return
	}

	if resErr.Cause != context.DeadlineExceeded {
		t.Errorf("ResolutionError cause should be context.DeadlineExceeded, got: %v", resErr.Cause)
	}
}

func TestPathResolver_FileExists(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "psr4-fileexists-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.php")
	if err := os.WriteFile(testFile, []byte("<?php"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a test directory
	testDir := filepath.Join(tempDir, "testdir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "existing file",
			path:     testFile,
			expected: true,
		},
		{
			name:     "non-existent file",
			path:     filepath.Join(tempDir, "nonexistent.php"),
			expected: false,
		},
		{
			name:     "directory (not a regular file)",
			path:     testDir,
			expected: false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "invalid path with null byte",
			path:     "test\x00file.php",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.FileExists(tt.path)
			if result != tt.expected {
				t.Errorf("FileExists(%s) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestPathResolver_PathSecurity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "psr4-security-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	ctx := context.Background()
	
	// Test various path traversal attempts
	maliciousCandidates := [][]string{
		{"../../../etc/passwd"},
		{"..\\..\\..\\windows\\system32\\config\\sam"},
		{"../../../../../../../etc/shadow"},
		{"....//....//....//etc/passwd"},
		{"./../../../etc/passwd"},
		{"app/../../../../../../etc/passwd"},
		{"app/../../../etc/passwd"},
	}

	for i, candidates := range maliciousCandidates {
		t.Run(fmt.Sprintf("path_traversal_%d", i), func(t *testing.T) {
			_, err := resolver.ResolvePath(ctx, candidates, "")
			
			if err == nil {
				t.Errorf("ResolvePath() should reject path traversal attempt: %v", candidates)
				return
			}

			resErr, ok := err.(*ResolutionError)
			if !ok {
				t.Errorf("Expected ResolutionError for path traversal, got %T", err)
				return
			}

			// Error should indicate security issue - check both message and cause chain
			errorText := strings.ToLower(resErr.Message + " " + resErr.Cause.Error())
			if !strings.Contains(errorText, "traversal") {
				t.Errorf("Error should mention path traversal, got message: %s, cause: %v", resErr.Message, resErr.Cause)
			}
		})
	}
}

func TestPathResolver_CrossPlatformPaths(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "psr4-crossplatform-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test file with cross-platform path
	testFile := filepath.Join(tempDir, "app", "Http", "Controllers", "UserController.php")
	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		t.Fatalf("Failed to create directory structure: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("<?php"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name       string
		candidates []string
		wantExists bool
	}{
		{
			name:       "unix-style path separators",
			candidates: []string{"app/Http/Controllers/UserController.php"},
			wantExists: true,
		},
		{
			name:       "mixed path separators",
			candidates: []string{"app\\Http/Controllers\\UserController.php"},
			wantExists: true,
		},
		{
			name:       "normalized path with dots",
			candidates: []string{"app/./Http/../Http/Controllers/UserController.php"},
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolvedPath, err := resolver.ResolvePath(ctx, tt.candidates, "")
			
			if tt.wantExists {
				if err != nil {
					t.Errorf("ResolvePath() unexpected error: %v", err)
					return
				}
				if resolvedPath == "" {
					t.Error("ResolvePath() returned empty path")
					return
				}
				// Verify the resolved path actually exists
				if !resolver.FileExists(resolvedPath) {
					t.Errorf("Resolved path does not exist: %s", resolvedPath)
				}
			} else {
				if err == nil {
					t.Error("ResolvePath() expected error but got none")
				}
			}
		})
	}
}

// Benchmark tests for performance validation
func BenchmarkPathResolver_FileExists(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "psr4-bench-*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "test.php")
	if err := os.WriteFile(testFile, []byte("<?php"), 0644); err != nil {
		b.Fatalf("Failed to create test file: %v", err)
	}

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.FileExists(testFile)
	}
}

func BenchmarkPathResolver_ResolvePath(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "psr4-bench-*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "app", "Models", "User.php")
	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		b.Fatalf("Failed to create directory: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("<?php"), 0644); err != nil {
		b.Fatalf("Failed to create test file: %v", err)
	}

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}

	ctx := context.Background()
	candidates := []string{"app/Models/User.php"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.ResolvePath(ctx, candidates, "")
	}
}

func BenchmarkPathResolver_ResolvePath_MultipleCandidates(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "psr4-bench-multi-*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create one existing file at the end of candidates list
	testFile := filepath.Join(tempDir, "src", "Service", "EmailService.php")
	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		b.Fatalf("Failed to create directory: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("<?php"), 0644); err != nil {
		b.Fatalf("Failed to create test file: %v", err)
	}

	resolver, err := NewPathResolver(tempDir)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}

	ctx := context.Background()
	candidates := []string{
		"app/Models/User.php",
		"app/Http/Controllers/UserController.php",
		"lib/Service/EmailService.php",
		"vendor/Service/EmailService.php",
		"src/Service/EmailService.php", // This one exists
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.ResolvePath(ctx, candidates, "")
	}
}