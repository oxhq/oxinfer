package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileDiscovererImpl_ValidateTargets(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir := t.TempDir()

	// Create test directories
	appDir := filepath.Join(tempDir, "app")
	routesDir := filepath.Join(tempDir, "routes")
	err := os.MkdirAll(appDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create app directory: %v", err)
	}
	err = os.MkdirAll(routesDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create routes directory: %v", err)
	}

	// Create a file (not directory) for testing
	testFile := filepath.Join(tempDir, "testfile.php")
	err = os.WriteFile(testFile, []byte("<?php echo 'test';"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	discoverer := NewFileDiscoverer()

	tests := []struct {
		name        string
		targets     []string
		baseDir     string
		expectError bool
		errorType   error
	}{
		{
			name:        "valid targets",
			targets:     []string{"app", "routes"},
			baseDir:     tempDir,
			expectError: false,
		},
		{
			name:        "empty base directory",
			targets:     []string{"app"},
			baseDir:     "",
			expectError: true,
			errorType:   ErrInvalidPath,
		},
		{
			name:        "non-existent base directory",
			targets:     []string{"app"},
			baseDir:     "/non/existent/path",
			expectError: true,
			errorType:   ErrTargetNotFound,
		},
		{
			name:        "non-existent target",
			targets:     []string{"nonexistent"},
			baseDir:     tempDir,
			expectError: true,
			errorType:   ErrTargetNotFound,
		},
		{
			name:        "path traversal attempt",
			targets:     []string{"../etc"},
			baseDir:     tempDir,
			expectError: true,
			errorType:   ErrPathTraversal,
		},
		{
			name:        "target is a file not directory",
			targets:     []string{"testfile.php"},
			baseDir:     tempDir,
			expectError: true,
			errorType:   ErrInvalidPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := discoverer.ValidateTargets(tt.targets, tt.baseDir)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}

				// Check if the error contains the expected error type
				if tt.errorType != nil {
					var discoveryErr *DiscoveryError
					if !isErrorType(err, tt.errorType) && !getDiscoveryError(err, &discoveryErr) {
						t.Errorf("Expected error type %v, got %v", tt.errorType, err)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestFileDiscovererImpl_DiscoverFiles(t *testing.T) {
	// Create temporary directory structure
	tempDir := t.TempDir()

	// Create Laravel-like directory structure
	dirs := []string{
		"app/Http/Controllers",
		"app/Models",
		"routes",
		"database/migrations",
	}

	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tempDir, dir), 0755)
		if err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create test PHP files
	phpFiles := map[string]string{
		"app/Http/Controllers/UserController.php":    "<?php class UserController {}",
		"app/Models/User.php":                        "<?php class User {}",
		"routes/web.php":                             "<?php Route::get('/', function() {});",
		"routes/api.php":                             "<?php Route::prefix('api')->group(function() {});",
		"database/migrations/create_users_table.php": "<?php use Illuminate\\Database\\Migrations\\Migration;",
	}

	for filePath, content := range phpFiles {
		fullPath := filepath.Join(tempDir, filePath)
		err := os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create file %s: %v", filePath, err)
		}
	}

	// Create non-PHP files that should be ignored
	nonPhpFiles := map[string]string{
		"app/test.txt":     "text file",
		"routes/README.md": "# Routes",
		"package.json":     "{}",
	}

	for filePath, content := range nonPhpFiles {
		fullPath := filepath.Join(tempDir, filePath)
		err := os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create non-PHP file %s: %v", filePath, err)
		}
	}

	discoverer := NewFileDiscoverer()
	ctx := context.Background()

	tests := []struct {
		name          string
		targets       []string
		globs         []string
		baseDir       string
		expectedFiles []string
		expectError   bool
	}{
		{
			name:    "discover app directory with recursive glob",
			targets: []string{"app"},
			globs:   []string{"**/*.php"},
			baseDir: tempDir,
			expectedFiles: []string{
				"app/Http/Controllers/UserController.php",
				"app/Models/User.php",
			},
			expectError: false,
		},
		{
			name:    "discover routes directory",
			targets: []string{"routes"},
			globs:   []string{"*.php"},
			baseDir: tempDir,
			expectedFiles: []string{
				"routes/api.php",
				"routes/web.php",
			},
			expectError: false,
		},
		{
			name:    "discover multiple targets",
			targets: []string{"app", "routes"},
			globs:   []string{"**/*.php"},
			baseDir: tempDir,
			expectedFiles: []string{
				"app/Http/Controllers/UserController.php",
				"app/Models/User.php",
				"routes/api.php",
				"routes/web.php",
			},
			expectError: false,
		},
		{
			name:        "invalid target",
			targets:     []string{"nonexistent"},
			globs:       []string{"*.php"},
			baseDir:     tempDir,
			expectError: true,
		},
		{
			name:          "no matches",
			targets:       []string{"database"},
			globs:         []string{"*.js"},
			baseDir:       tempDir,
			expectedFiles: []string{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := discoverer.DiscoverFiles(ctx, tt.targets, tt.globs, tt.baseDir)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			// Check that we got the expected number of files
			if len(files) != len(tt.expectedFiles) {
				t.Errorf("Expected %d files, got %d", len(tt.expectedFiles), len(files))
				t.Logf("Got files: %v", getFilePaths(files))
				return
			}

			// Check that files are sorted deterministically
			for i := 1; i < len(files); i++ {
				if files[i-1].Path >= files[i].Path {
					t.Errorf("Files not sorted deterministically: %s >= %s", files[i-1].Path, files[i].Path)
				}
			}

			// Check specific expected files
			actualPaths := getFilePaths(files)
			for i, expectedPath := range tt.expectedFiles {
				if i >= len(actualPaths) || actualPaths[i] != expectedPath {
					t.Errorf("Expected file %s at position %d, got %v", expectedPath, i, actualPaths)
				}
			}

			// Validate file info structure
			for _, file := range files {
				if file.Path == "" {
					t.Errorf("File has empty Path")
				}
				if file.AbsPath == "" {
					t.Errorf("File has empty AbsPath")
				}
				if file.Size < 0 {
					t.Errorf("File has negative size: %d", file.Size)
				}
				if file.ModTime.IsZero() {
					t.Errorf("File has zero ModTime")
				}
				if file.IsDirectory {
					t.Errorf("File is marked as directory: %s", file.Path)
				}
			}
		})
	}
}

func TestFileDiscovererImpl_FilterPHPFiles(t *testing.T) {
	discoverer := NewFileDiscoverer()

	files := []FileInfo{
		{Path: "app/User.php", IsDirectory: false},
		{Path: "routes/web.php", IsDirectory: false},
		{Path: "README.md", IsDirectory: false},
		{Path: "package.json", IsDirectory: false},
		{Path: "test.txt", IsDirectory: false},
		{Path: "config.XML", IsDirectory: false},   // Test case sensitivity
		{Path: "script.PHP", IsDirectory: false},   // Test case sensitivity
		{Path: "app/directory", IsDirectory: true}, // Should be excluded
	}

	phpFiles := discoverer.FilterPHPFiles(files)

	expectedPHPFiles := []string{
		"app/User.php",
		"routes/web.php",
		"script.PHP",
	}

	if len(phpFiles) != len(expectedPHPFiles) {
		t.Errorf("Expected %d PHP files, got %d", len(expectedPHPFiles), len(phpFiles))
	}

	actualPaths := getFilePaths(phpFiles)
	for i, expectedPath := range expectedPHPFiles {
		if i >= len(actualPaths) || actualPaths[i] != expectedPath {
			t.Errorf("Expected PHP file %s at position %d, got %v", expectedPath, i, actualPaths)
		}
	}

	// Ensure no directories are included
	for _, file := range phpFiles {
		if file.IsDirectory {
			t.Errorf("Directory incorrectly included in PHP files: %s", file.Path)
		}
	}
}

func TestFileDiscovererImpl_ContextCancellation(t *testing.T) {
	// Create a large directory structure that would take some time to traverse
	tempDir := t.TempDir()

	// Create many directories and files
	for i := 0; i < 100; i++ {
		dir := filepath.Join(tempDir, "app", "subdir", string(rune('a'+i%26)))
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		// Create multiple PHP files in each directory
		for j := 0; j < 10; j++ {
			filePath := filepath.Join(dir, "test"+string(rune('a'+j%26))+".php")
			err := os.WriteFile(filePath, []byte("<?php"), 0644)
			if err != nil {
				t.Fatalf("Failed to create file: %v", err)
			}
		}
	}

	discoverer := NewFileDiscoverer()

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := discoverer.DiscoverFiles(ctx, []string{"app"}, []string{"**/*.php"}, tempDir)

	// We expect either success or context cancellation
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("Expected context cancellation or success, got: %v", err)
	}
}

func TestDiscoveryConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      DiscoveryConfig
		expectError bool
		errorType   error
	}{
		{
			name: "valid config",
			config: DiscoveryConfig{
				Targets:     []string{"app", "routes"},
				Globs:       []string{"**/*.php"},
				ProjectRoot: "/tmp",
				MaxDepth:    0,
			},
			expectError: false,
		},
		{
			name: "empty project root",
			config: DiscoveryConfig{
				Targets: []string{"app"},
				Globs:   []string{"*.php"},
			},
			expectError: true,
			errorType:   ErrInvalidPath,
		},
		{
			name: "empty targets",
			config: DiscoveryConfig{
				Globs:       []string{"*.php"},
				ProjectRoot: "/tmp",
			},
			expectError: true,
			errorType:   ErrInvalidPath,
		},
		{
			name: "empty globs",
			config: DiscoveryConfig{
				Targets:     []string{"app"},
				ProjectRoot: "/tmp",
			},
			expectError: true,
			errorType:   ErrInvalidGlob,
		},
		{
			name: "negative max depth",
			config: DiscoveryConfig{
				Targets:     []string{"app"},
				Globs:       []string{"*.php"},
				ProjectRoot: "/tmp",
				MaxDepth:    -1,
			},
			expectError: true,
			errorType:   ErrInvalidPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}

				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("Expected error type %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestDefaultDiscoveryConfig(t *testing.T) {
	config := DefaultDiscoveryConfig()

	expectedTargets := []string{"app", "routes"}
	if len(config.Targets) != len(expectedTargets) {
		t.Errorf("Expected %d targets, got %d", len(expectedTargets), len(config.Targets))
	}

	for i, expected := range expectedTargets {
		if i >= len(config.Targets) || config.Targets[i] != expected {
			t.Errorf("Expected target %s at position %d, got %v", expected, i, config.Targets)
		}
	}

	expectedGlobs := []string{"**/*.php"}
	if len(config.Globs) != len(expectedGlobs) {
		t.Errorf("Expected %d globs, got %d", len(expectedGlobs), len(config.Globs))
	}

	if config.MaxDepth != 0 {
		t.Errorf("Expected MaxDepth 0, got %d", config.MaxDepth)
	}
}

// Helper functions

func getFilePaths(files []FileInfo) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func isErrorType(err error, target error) bool {
	var discoveryErr *DiscoveryError
	if getDiscoveryError(err, &discoveryErr) {
		return discoveryErr.Err == target
	}
	return err == target
}

func getDiscoveryError(err error, target **DiscoveryError) bool {
	if err == nil {
		return false
	}

	if de, ok := err.(*DiscoveryError); ok {
		*target = de
		return true
	}

	return false
}

// Benchmark tests

func BenchmarkFileDiscovery(b *testing.B) {
	// Create temporary directory structure
	tempDir := b.TempDir()

	// Create a realistic Laravel project structure
	dirs := []string{
		"app/Http/Controllers",
		"app/Http/Middleware",
		"app/Models",
		"app/Services",
		"routes",
		"database/migrations",
		"database/seeders",
	}

	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tempDir, dir), 0755)
		if err != nil {
			b.Fatalf("Failed to create directory: %v", err)
		}
	}

	// Create 500 PHP files across the structure
	fileCount := 500
	for i := 0; i < fileCount; i++ {
		dir := dirs[i%len(dirs)]
		filePath := filepath.Join(tempDir, dir, "File"+string(rune('A'+i%26))+".php")
		err := os.WriteFile(filePath, []byte("<?php class File{}"), 0644)
		if err != nil {
			b.Fatalf("Failed to create file: %v", err)
		}
	}

	discoverer := NewFileDiscoverer()
	ctx := context.Background()
	targets := []string{"app", "routes", "database"}
	globs := []string{"**/*.php"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := discoverer.DiscoverFiles(ctx, targets, globs, tempDir)
		if err != nil {
			b.Fatalf("Discovery failed: %v", err)
		}
	}
}
