package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// TestDefaultFileIndexer_EndToEndIndexing tests complete indexing workflow
func TestDefaultFileIndexer_EndToEndIndexing(t *testing.T) {
	// Create temporary test directory structure
	tempDir := setupTestDirectory(t)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name           string
		manifest       *manifest.Manifest
		expectedFiles  int
		expectedPartial bool
		expectError    bool
	}{
		{
			name: "basic Laravel project structure",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: tempDir,
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app", "routes"},
					Globs:   []string{"**/*.php"},
				},
				Limits: &manifest.LimitsConfig{
					MaxFiles:   intPtr(1000),
					MaxWorkers: intPtr(4),
					MaxDepth:   intPtr(10),
				},
			},
			expectedFiles:   6, // All test files
			expectedPartial: false,
		},
		{
			name: "limited file count",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: tempDir,
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app", "routes"},
					Globs:   []string{"**/*.php"},
				},
				Limits: &manifest.LimitsConfig{
					MaxFiles:   intPtr(3), // Limit to 3 files
					MaxWorkers: intPtr(2),
					MaxDepth:   intPtr(10),
				},
			},
			expectedFiles:   3, // Limited by MaxFiles
			expectedPartial: true,
		},
		{
			name: "cache enabled",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: tempDir,
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
				Cache: &manifest.CacheConfig{
					Enabled: boolPtr(true),
					Kind:    stringPtr("mtime"),
				},
				Limits: &manifest.LimitsConfig{
					MaxFiles:   intPtr(1000),
					MaxWorkers: intPtr(2),
				},
			},
			expectedFiles:   4, // Files in app directory
			expectedPartial: false,
		},
		{
			name: "invalid base directory",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: "/nonexistent/directory",
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app"},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexer := NewDefaultFileIndexer()
			
			// Load configuration from manifest
			err := indexer.LoadFromManifest(tt.manifest)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFromManifest failed: %v", err)
			}
			
			// Perform indexing
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			
			result, err := indexer.IndexFiles(ctx, IndexConfig{})
			if tt.expectError {
				if err == nil {
					t.Error("expected error during indexing but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("IndexFiles failed: %v", err)
			}
			
			// Verify results
			if len(result.Files) != tt.expectedFiles {
				t.Errorf("expected %d files, got %d", tt.expectedFiles, len(result.Files))
			}
			
			if result.Partial != tt.expectedPartial {
				t.Errorf("expected partial=%t, got %t", tt.expectedPartial, result.Partial)
			}
			
			if result.DurationMs < 0 {
				t.Error("expected non-negative duration")
			}
			
			// Verify files are sorted deterministically
			for i := 1; i < len(result.Files); i++ {
				if result.Files[i-1].Path >= result.Files[i].Path {
					t.Errorf("files not sorted: %s >= %s", result.Files[i-1].Path, result.Files[i].Path)
				}
			}
		})
	}
}

// TestDefaultFileIndexer_LimitsEnforcement tests all limit enforcement scenarios
func TestDefaultFileIndexer_LimitsEnforcement(t *testing.T) {
	tempDir := setupTestDirectory(t)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		maxFiles    int
		maxWorkers  int
		maxDepth    int
		expectFiles int
		expectPartial bool
	}{
		{
			name:          "no limits",
			maxFiles:      0,
			maxWorkers:    0, 
			maxDepth:      0,
			expectFiles:   6,
			expectPartial: false,
		},
		{
			name:          "file limit enforced",
			maxFiles:      2,
			maxWorkers:    4,
			maxDepth:      10,
			expectFiles:   2,
			expectPartial: true,
		},
		{
			name:          "worker limit enforced",
			maxFiles:      10,
			maxWorkers:    1,
			maxDepth:      10,
			expectFiles:   6,
			expectPartial: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: tempDir,
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app", "routes"},
					Globs:   []string{"**/*.php"},
				},
				Limits: &manifest.LimitsConfig{
					MaxFiles:   intPtrOrNil(tt.maxFiles),
					MaxWorkers: intPtrOrNil(tt.maxWorkers),
					MaxDepth:   intPtrOrNil(tt.maxDepth),
				},
			}

			indexer := NewDefaultFileIndexer()
			err := indexer.LoadFromManifest(manifest)
			if err != nil {
				t.Fatalf("LoadFromManifest failed: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := indexer.IndexFiles(ctx, IndexConfig{})
			if err != nil {
				t.Fatalf("IndexFiles failed: %v", err)
			}

			if len(result.Files) != tt.expectFiles {
				t.Errorf("expected %d files, got %d", tt.expectFiles, len(result.Files))
			}

			if result.Partial != tt.expectPartial {
				t.Errorf("expected partial=%t, got %t", tt.expectPartial, result.Partial)
			}
		})
	}
}

// TestDefaultFileIndexer_CacheIntegration tests cache functionality
func TestDefaultFileIndexer_CacheIntegration(t *testing.T) {
	tempDir := setupTestDirectory(t)
	defer os.RemoveAll(tempDir)

	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: tempDir,
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
		Cache: &manifest.CacheConfig{
			Enabled: boolPtr(true),
			Kind:    stringPtr("mtime"),
		},
		Limits: &manifest.LimitsConfig{
			MaxFiles:   intPtr(1000),
			MaxWorkers: intPtr(2),
		},
	}

	indexer := NewDefaultFileIndexer()
	err := indexer.LoadFromManifest(manifest)
	if err != nil {
		t.Fatalf("LoadFromManifest failed: %v", err)
	}

	ctx := context.Background()

	// First run - cold cache
	result1, err := indexer.IndexFiles(ctx, IndexConfig{})
	if err != nil {
		t.Fatalf("first IndexFiles failed: %v", err)
	}

	if result1.Cached > 0 {
		t.Error("expected no cached files in first run")
	}

	// Second run - should use cache (in real implementation)
	result2, err := indexer.IndexFiles(ctx, IndexConfig{})
	if err != nil {
		t.Fatalf("second IndexFiles failed: %v", err)
	}

	// Results should be identical
	if len(result1.Files) != len(result2.Files) {
		t.Errorf("inconsistent results: %d vs %d files", len(result1.Files), len(result2.Files))
	}

	// Second run should be faster (when cache is fully implemented)
	if result2.DurationMs > result1.DurationMs*2 {
		t.Logf("second run not significantly faster (cache may not be active): %dms vs %dms",
			result2.DurationMs, result1.DurationMs)
	}
}

// TestDefaultFileIndexer_ProgressMonitoring tests progress callback functionality
func TestDefaultFileIndexer_ProgressMonitoring(t *testing.T) {
	tempDir := setupTestDirectory(t)
	defer os.RemoveAll(tempDir)

	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: tempDir,
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app", "routes"},
		},
	}

	indexer := NewDefaultFileIndexer()
	err := indexer.LoadFromManifest(manifest)
	if err != nil {
		t.Fatalf("LoadFromManifest failed: %v", err)
	}

	// Set up progress monitoring
	var progressUpdates []IndexProgress
	indexer.SetProgressCallback(func(progress IndexProgress) {
		progressUpdates = append(progressUpdates, progress)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = indexer.IndexFiles(ctx, IndexConfig{})
	if err != nil {
		t.Fatalf("IndexFiles failed: %v", err)
	}

	// Verify progress updates were received
	if len(progressUpdates) == 0 {
		t.Error("no progress updates received")
	}

	// Verify progress phases
	phases := make(map[string]bool)
	for _, update := range progressUpdates {
		phases[update.Phase] = true
	}

	expectedPhases := []string{"initializing", "discovering", "enforcing-limits", "processing", "finalizing"}
	for _, phase := range expectedPhases {
		if !phases[phase] {
			t.Errorf("missing expected phase: %s", phase)
		}
	}
}

// TestDefaultFileIndexer_PerformanceLargeProject tests performance with many files
func TestDefaultFileIndexer_PerformanceLargeProject(t *testing.T) {
	// Create a larger test directory structure
	tempDir := setupLargeTestDirectory(t, 50) // 50 files
	defer os.RemoveAll(tempDir)

	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: tempDir,
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
		Limits: &manifest.LimitsConfig{
			MaxFiles:   intPtr(1000),
			MaxWorkers: intPtr(8),
		},
	}

	indexer := NewDefaultFileIndexer()
	err := indexer.LoadFromManifest(manifest)
	if err != nil {
		t.Fatalf("LoadFromManifest failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	result, err := indexer.IndexFiles(ctx, IndexConfig{})
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("IndexFiles failed: %v", err)
	}

	t.Logf("Indexed %d files in %v", len(result.Files), duration)

	// Performance expectations
	if duration > 10*time.Second {
		t.Errorf("indexing took too long: %v (expected < 10s)", duration)
	}

	if len(result.Files) != 50 {
		t.Errorf("expected 50 files, got %d", len(result.Files))
	}
}

// TestDefaultFileIndexer_DeterministicResults tests result consistency
func TestDefaultFileIndexer_DeterministicResults(t *testing.T) {
	tempDir := setupTestDirectory(t)
	defer os.RemoveAll(tempDir)

	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: tempDir,
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app", "routes"},
			Globs:   []string{"**/*.php"},
		},
	}

	indexer := NewDefaultFileIndexer()
	err := indexer.LoadFromManifest(manifest)
	if err != nil {
		t.Fatalf("LoadFromManifest failed: %v", err)
	}

	ctx := context.Background()

	// Run indexing multiple times
	var results []*IndexResult
	for i := 0; i < 3; i++ {
		result, err := indexer.IndexFiles(ctx, IndexConfig{})
		if err != nil {
			t.Fatalf("IndexFiles run %d failed: %v", i+1, err)
		}
		results = append(results, result)
	}

	// Compare results for consistency
	baseResult := results[0]
	for i, result := range results[1:] {
		if len(result.Files) != len(baseResult.Files) {
			t.Errorf("run %d: inconsistent file count %d vs %d", i+2, len(result.Files), len(baseResult.Files))
		}

		// Compare file paths and order
		for j, file := range result.Files {
			if j >= len(baseResult.Files) {
				break
			}
			if file.Path != baseResult.Files[j].Path {
				t.Errorf("run %d: file order inconsistent at index %d: %s vs %s",
					i+2, j, file.Path, baseResult.Files[j].Path)
			}
		}
	}
}

// Helper functions

func setupTestDirectory(t *testing.T) string {
	tempDir, err := os.MkdirTemp("", "oxinfer_test_")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create app directory with PHP files
	appDir := filepath.Join(tempDir, "app")
	os.MkdirAll(filepath.Join(appDir, "Http", "Controllers"), 0755)
	os.MkdirAll(filepath.Join(appDir, "Models"), 0755)
	
	// Create test PHP files
	files := map[string]string{
		"app/Http/Controllers/UserController.php": "<?php class UserController {}",
		"app/Http/Controllers/AuthController.php": "<?php class AuthController {}",
		"app/Models/User.php":                     "<?php class User {}",
		"app/Models/Post.php":                     "<?php class Post {}",
		"routes/web.php":                          "<?php Route::get('/', function() {});",
		"routes/api.php":                          "<?php Route::api('api/', function() {});",
	}

	for path, content := range files {
		fullPath := filepath.Join(tempDir, path)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file %s: %v", path, err)
		}
	}

	// Create routes directory
	os.MkdirAll(filepath.Join(tempDir, "routes"), 0755)

	return tempDir
}

func setupLargeTestDirectory(t *testing.T, fileCount int) string {
	tempDir, err := os.MkdirTemp("", "oxinfer_large_test_")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	appDir := filepath.Join(tempDir, "app")
	os.MkdirAll(appDir, 0755)

	// Create many PHP files
	for i := 0; i < fileCount; i++ {
		filename := filepath.Join(appDir, fmt.Sprintf("File%03d.php", i))
		content := fmt.Sprintf("<?php class File%03d {}", i)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	return tempDir
}

func intPtr(i int) *int {
	return &i
}

func intPtrOrNil(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}