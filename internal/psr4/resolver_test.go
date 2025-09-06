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

func TestNewPathResolver_Symlinks(t *testing.T) {
	// Create temporary directories for testing symlinks
	realDir, err := os.MkdirTemp("", "psr4-real-*")
	if err != nil {
		t.Fatalf("Failed to create real temp dir: %v", err)
	}
	defer os.RemoveAll(realDir)

	// Create a test file in the real directory
	testFile := filepath.Join(realDir, "test.php")
	if err := os.WriteFile(testFile, []byte("<?php"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name          string
		setupFunc     func() (string, func(), error) // returns symlink path, cleanup, error
		wantError     bool
		errorMsg      string
		expectResolve bool // whether symlink should be resolved
	}{
		{
			name: "valid symlink to existing directory",
			setupFunc: func() (string, func(), error) {
				symlinkDir, err := os.MkdirTemp("", "psr4-symlink-*")
				if err != nil {
					return "", nil, err
				}

				symlinkPath := filepath.Join(symlinkDir, "linked")
				err = os.Symlink(realDir, symlinkPath)
				if err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}

				cleanup := func() { os.RemoveAll(symlinkDir) }
				return symlinkPath, cleanup, nil
			},
			wantError:     false,
			expectResolve: true,
		},
		{
			name: "broken symlink (target doesn't exist)",
			setupFunc: func() (string, func(), error) {
				symlinkDir, err := os.MkdirTemp("", "psr4-symlink-broken-*")
				if err != nil {
					return "", nil, err
				}

				nonExistentTarget := filepath.Join(symlinkDir, "nonexistent")
				symlinkPath := filepath.Join(symlinkDir, "broken-link")
				err = os.Symlink(nonExistentTarget, symlinkPath)
				if err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}

				cleanup := func() { os.RemoveAll(symlinkDir) }
				return symlinkPath, cleanup, nil
			},
			wantError: true,
			errorMsg:  "failed to resolve symlinks",
		},
		{
			name: "circular symlink",
			setupFunc: func() (string, func(), error) {
				symlinkDir, err := os.MkdirTemp("", "psr4-symlink-circular-*")
				if err != nil {
					return "", nil, err
				}

				link1 := filepath.Join(symlinkDir, "link1")
				link2 := filepath.Join(symlinkDir, "link2")

				// Create circular symlinks: link1 -> link2, link2 -> link1
				if err = os.Symlink(link2, link1); err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}
				if err = os.Symlink(link1, link2); err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}

				cleanup := func() { os.RemoveAll(symlinkDir) }
				return link1, cleanup, nil
			},
			wantError: true,
			errorMsg:  "failed to resolve symlinks",
		},
		{
			name: "symlink to file instead of directory",
			setupFunc: func() (string, func(), error) {
				symlinkDir, err := os.MkdirTemp("", "psr4-symlink-file-*")
				if err != nil {
					return "", nil, err
				}

				// Create a file and symlink to it
				tempFile := filepath.Join(symlinkDir, "target.txt")
				if err := os.WriteFile(tempFile, []byte("test"), 0644); err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}

				symlinkPath := filepath.Join(symlinkDir, "file-link")
				err = os.Symlink(tempFile, symlinkPath)
				if err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}

				cleanup := func() { os.RemoveAll(symlinkDir) }
				return symlinkPath, cleanup, nil
			},
			wantError: true,
			errorMsg:  "base directory is not a directory",
		},
		{
			name: "deeply nested symlink resolution",
			setupFunc: func() (string, func(), error) {
				symlinkDir, err := os.MkdirTemp("", "psr4-symlink-nested-*")
				if err != nil {
					return "", nil, err
				}

				// Create chain: realDir <- link1 <- link2 <- link3
				link1 := filepath.Join(symlinkDir, "link1")
				link2 := filepath.Join(symlinkDir, "link2")
				link3 := filepath.Join(symlinkDir, "link3")

				if err = os.Symlink(realDir, link1); err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}
				if err = os.Symlink(link1, link2); err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}
				if err = os.Symlink(link2, link3); err != nil {
					os.RemoveAll(symlinkDir)
					return "", nil, err
				}

				cleanup := func() { os.RemoveAll(symlinkDir) }
				return link3, cleanup, nil
			},
			wantError:     false,
			expectResolve: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symlinkPath, cleanup, setupErr := tt.setupFunc()
			if setupErr != nil {
				t.Skipf("Failed to setup symlink test: %v", setupErr)
				return
			}
			defer cleanup()

			resolver, err := NewPathResolver(symlinkPath)

			if tt.wantError {
				if err == nil {
					t.Errorf("NewPathResolver() expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("NewPathResolver() unexpected error: %v", err)
				return
			}

			if resolver == nil {
				t.Error("NewPathResolver() returned nil resolver")
				return
			}

			// If we expect symlink resolution, verify the resolved path differs from input
			if tt.expectResolve {
				// Get the resolver's internal baseDir through a resolution attempt
				ctx := context.Background()
				candidates := []string{"test.php"}
				resolvedPath, err := resolver.ResolvePath(ctx, candidates, "")

				if err != nil {
					t.Errorf("Failed to resolve test file: %v", err)
					return
				}

				// The resolved path should point to the real directory, not the symlink
				expectedPath := filepath.Join(realDir, "test.php")

				// Resolve expected path symlinks for comparison (handles macOS /private differences)
				expectedResolved, err := filepath.EvalSymlinks(expectedPath)
				if err != nil {
					t.Errorf("Failed to resolve expected path symlinks: %v", err)
					return
				}

				if resolvedPath != expectedResolved {
					t.Errorf("Expected resolved path %s to be %s", resolvedPath, expectedResolved)
				}

				// Verify the symlink was actually resolved by checking that the base directory
				// is the real directory, not the symlink directory
				if strings.Contains(resolvedPath, filepath.Base(symlinkPath)) {
					t.Errorf("Resolved path %s still contains symlink component %s",
						resolvedPath, filepath.Base(symlinkPath))
				}
			}
		})
	}
}

func TestPathResolver_ResolvePath_Symlinks(t *testing.T) {
	// Create temporary directories for testing
	realDir, err := os.MkdirTemp("", "psr4-resolve-real-*")
	if err != nil {
		t.Fatalf("Failed to create real temp dir: %v", err)
	}
	defer os.RemoveAll(realDir)

	symlinkDir, err := os.MkdirTemp("", "psr4-resolve-symlink-*")
	if err != nil {
		t.Fatalf("Failed to create symlink temp dir: %v", err)
	}
	defer os.RemoveAll(symlinkDir)

	// Create test file structure in real directory
	testFiles := []string{
		"app/Models/User.php",
		"src/Service/EmailService.php",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(realDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", file, err)
		}
		if err := os.WriteFile(fullPath, []byte("<?php"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	// Create symlink to real directory
	symlinkPath := filepath.Join(symlinkDir, "project-link")
	if err := os.Symlink(realDir, symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Test resolution through symlinked base directory
	resolver, err := NewPathResolver(symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name       string
		candidates []string
		wantFile   string // relative to real directory
		wantError  bool
	}{
		{
			name:       "resolve through symlink base directory",
			candidates: []string{"app/Models/User.php"},
			wantFile:   "app/Models/User.php",
			wantError:  false,
		},
		{
			name:       "resolve multiple candidates through symlink",
			candidates: []string{"nonexistent.php", "src/Service/EmailService.php"},
			wantFile:   "src/Service/EmailService.php",
			wantError:  false,
		},
		{
			name:       "symlink with custom base directory parameter",
			candidates: []string{"Models/User.php"},
			wantFile:   "app/Models/User.php", // should resolve against app subdirectory
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var customBase string
			if tt.name == "symlink with custom base directory parameter" {
				// Test custom base directory through symlink
				customBase = filepath.Join(symlinkPath, "app")
			}

			resolvedPath, err := resolver.ResolvePath(ctx, tt.candidates, customBase)

			if tt.wantError {
				if err == nil {
					t.Errorf("ResolvePath() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ResolvePath() unexpected error: %v", err)
				return
			}

			// Verify the resolved path points to the real directory, not symlink
			expectedPath := filepath.Join(realDir, tt.wantFile)

			// Resolve expected path symlinks for comparison (handles macOS /private differences)
			expectedResolved, err := filepath.EvalSymlinks(expectedPath)
			if err != nil {
				t.Errorf("Failed to resolve expected path symlinks: %v", err)
				return
			}

			if resolvedPath != expectedResolved {
				t.Errorf("ResolvePath() = %v, want %v", resolvedPath, expectedResolved)
			}

			// Verify file actually exists at resolved path
			if !resolver.FileExists(resolvedPath) {
				t.Errorf("Resolved path does not exist: %s", resolvedPath)
			}

			// Verify symlink was resolved (path should not contain symlink component)
			if strings.Contains(resolvedPath, filepath.Base(symlinkPath)) {
				t.Errorf("Resolved path %s still contains symlink component %s",
					resolvedPath, filepath.Base(symlinkPath))
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

	// Get the resolved tempDir for accurate test expectations
	resolvedTempDir, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatalf("Failed to resolve tempDir symlinks: %v", err)
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
			wantPath:   filepath.Join(resolvedTempDir, "app/Http/Controllers/UserController.php"),
			wantError:  false,
		},
		{
			name:       "second candidate exists",
			candidates: []string{"non/existent/file.php", "app/Models/User.php"},
			wantPath:   filepath.Join(resolvedTempDir, "app/Models/User.php"),
			wantError:  false,
		},
		{
			name:       "absolute path candidate",
			candidates: []string{filepath.Join(resolvedTempDir, "src/Service/EmailService.php")},
			wantPath:   filepath.Join(resolvedTempDir, "src/Service/EmailService.php"),
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
			baseDir:    filepath.Join(resolvedTempDir, "tests"),
			wantPath:   filepath.Join(resolvedTempDir, "tests/Unit/UserTest.php"),
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

			// Resolve expected path symlinks for comparison (handles macOS /private differences)
			expectedResolved := tt.wantPath
			if tt.wantPath != "" {
				if resolved, err := filepath.EvalSymlinks(tt.wantPath); err == nil {
					expectedResolved = resolved
				}
			}

			if gotPath != expectedResolved {
				t.Errorf("ResolvePath() = %v, want %v", gotPath, expectedResolved)
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
