//go:build legacy_parser

// Package parser provides comprehensive integration tests for PHP project parsing.
// Tests end-to-end integration of all core components and real Laravel project analysis.
package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/manifest"
)

// TestNewPHPProjectParser tests creation of PHP project parser with defaults.
func TestNewPHPProjectParser(t *testing.T) {
	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create PHP project parser: %v", err)
	}
	defer parser.Close()

	if parser == nil {
		t.Fatal("Expected non-nil parser")
	}

	// Verify default configuration
	config := parser.config
	if config.ProjectRoot != "." {
		t.Errorf("Expected default project root '.', got %s", config.ProjectRoot)
	}
	if config.MaxWorkers != 4 {
		t.Errorf("Expected default max workers 4, got %d", config.MaxWorkers)
	}
	if !config.CacheEnabled {
		t.Error("Expected caching enabled by default")
	}
	if !config.ExtractClasses {
		t.Error("Expected class extraction enabled by default")
	}

	// Verify progress initialization
	progress := parser.GetProgress()
	if progress.Phase != ProjectParserPhaseInitializing {
		t.Errorf("Expected initializing phase, got %v", progress.Phase)
	}
	if progress.IsComplete {
		t.Error("Expected parser not complete initially")
	}
}

// TestNewPHPProjectParserFromManifest tests manifest-driven configuration.
func TestNewPHPProjectParserFromManifest(t *testing.T) {
	// Create temporary directory for test
	tempDir := t.TempDir()

	// Create composer.json for PSR-4 resolver
	composerContent := `{
		"name": "test/project",
		"autoload": {
			"psr-4": {
				"App\\": "app/"
			}
		}
	}`
	composerPath := filepath.Join(tempDir, "composer.json")
	err := os.WriteFile(composerPath, []byte(composerContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	// Create test manifest
	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     tempDir,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app", "routes", "tests"},
			Globs:   []string{"**/*.php"},
		},
		Limits: &manifest.LimitsConfig{
			MaxWorkers: intPtr(8),
			MaxFiles:   intPtr(500),
			MaxDepth:   intPtr(5),
		},
		Cache: &manifest.CacheConfig{
			Enabled: boolPtr(true),
			Kind:    stringPtr("sha256+mtime"),
		},
		Features: &manifest.FeatureConfig{
			HTTPStatus:   boolPtr(true),
			RequestUsage: boolPtr(false),
		},
	}

	parser, err := NewPHPProjectParserFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to create parser from manifest: %v", err)
	}
	defer parser.Close()

	// Verify manifest configuration applied
	config := parser.config
	if config.ProjectRoot != tempDir {
		t.Errorf("Expected project root '%s', got %s", tempDir, config.ProjectRoot)
	}
	if config.MaxWorkers != 8 {
		t.Errorf("Expected max workers 8, got %d", config.MaxWorkers)
	}
	if config.MaxFiles != 500 {
		t.Errorf("Expected max files 500, got %d", config.MaxFiles)
	}
	if config.CacheKind != "sha256+mtime" {
		t.Errorf("Expected cache kind 'sha256+mtime', got %s", config.CacheKind)
	}
	if len(config.Targets) != 3 {
		t.Errorf("Expected 3 scan targets, got %d", len(config.Targets))
	}

	// Verify stored manifest reference
	if parser.manifest != manifest {
		t.Error("Expected manifest reference stored")
	}
}

// TestLoadFromManifest tests dynamic manifest loading.
func TestLoadFromManifest(t *testing.T) {
	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer parser.Close()

	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create required files and directories
	composerContent := `{
		"autoload": {
			"psr-4": {
				"App\\": "src/"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tempDir, "custom-composer.json"), []byte(composerContent), 0644); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	// Create src directory
	if err := os.MkdirAll(filepath.Join(tempDir, "src"), 0755); err != nil {
		t.Fatalf("Failed to create src directory: %v", err)
	}

	// Load configuration from manifest
	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     tempDir,
			Composer: "custom-composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"src"},
			Globs:   []string{"**/*.php", "**/*.inc"},
		},
		Limits: &manifest.LimitsConfig{
			MaxWorkers: intPtr(2),
			MaxFiles:   intPtr(100),
		},
	}

	err = parser.LoadFromManifest(manifest)
	if err != nil {
		t.Fatalf("Failed to load manifest: %v", err)
	}

	// Verify configuration updated
	config := parser.config
	if config.ProjectRoot != tempDir {
		t.Errorf("Expected project root '%s', got %s", tempDir, config.ProjectRoot)
	}
	if len(config.Targets) != 1 || config.Targets[0] != "src" {
		t.Errorf("Expected targets ['src'], got %v", config.Targets)
	}
	if config.MaxWorkers != 2 {
		t.Errorf("Expected max workers 2, got %d", config.MaxWorkers)
	}
}

// TestProgressTracking tests real-time progress monitoring during parsing.
func TestProgressTracking(t *testing.T) {
	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer parser.Close()

	// Track progress updates
	var progressUpdates []ProjectParserProgress
	parser.SetProgressCallback(func(progress ProjectParserProgress) {
		progressUpdates = append(progressUpdates, progress)
	})

	// Simulate progress updates
	parser.updateProgress(ProjectParserPhaseDiscovering, "Test discovery")
	parser.updateProgress(ProjectParserPhaseParsing, "Test parsing")
	parser.updateProgress(ProjectParserPhaseCompleted, "Test complete")

	// Verify progress callback worked
	if len(progressUpdates) != 3 {
		t.Fatalf("Expected 3 progress updates, got %d", len(progressUpdates))
	}

	// Verify progress phases
	expectedPhases := []ProjectParserPhase{
		ProjectParserPhaseDiscovering,
		ProjectParserPhaseParsing,
		ProjectParserPhaseCompleted,
	}

	for i, expected := range expectedPhases {
		if progressUpdates[i].Phase != expected {
			t.Errorf("Progress update %d: expected phase %v, got %v",
				i, expected, progressUpdates[i].Phase)
		}
	}

	// Test current progress access
	currentProgress := parser.GetProgress()
	if currentProgress.Phase != ProjectParserPhaseCompleted {
		t.Errorf("Expected current phase completed, got %v", currentProgress.Phase)
	}
}

// TestParseProjectWithEmptyDirectory tests parsing an empty directory.
func TestParseProjectWithEmptyDirectory(t *testing.T) {
	// Create temporary directory with empty app subdirectory
	tempDir := t.TempDir()
	appDir := filepath.Join(tempDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app directory: %v", err)
	}

	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer parser.Close()

	// Configure for empty directory
	config := ProjectParserConfig{
		ProjectRoot:    tempDir,
		ComposerPath:   filepath.Join(tempDir, "composer.json"),
		Targets:        []string{"app"},
		Globs:          []string{"**/*.php"},
		MaxFiles:       100,
		MaxDepth:       5,
		MaxWorkers:     2,
		ParseTimeout:   10 * time.Second,
		CacheEnabled:   false,
		ExtractClasses: true,
		ExtractMethods: true,
	}

	ctx := context.Background()
	result, err := parser.ParseProject(ctx, config)

	// Should succeed with no files
	if err != nil {
		t.Fatalf("Expected success with empty directory, got error: %v", err)
	}

	// Verify empty results
	if len(result.DiscoveredFiles) != 0 {
		t.Errorf("Expected 0 discovered files, got %d", len(result.DiscoveredFiles))
	}
	if len(result.ParsedFiles) != 0 {
		t.Errorf("Expected 0 parsed files, got %d", len(result.ParsedFiles))
	}
	if len(result.Classes) != 0 {
		t.Errorf("Expected 0 classes, got %d", len(result.Classes))
	}

	// Verify statistics
	if result.Stats.FilesDiscovered != 0 {
		t.Errorf("Expected 0 files discovered, got %d", result.Stats.FilesDiscovered)
	}
	if result.Stats.FilesParsed != 0 {
		t.Errorf("Expected 0 files parsed, got %d", result.Stats.FilesParsed)
	}
}

// TestParseProjectWithSimplePHPFiles tests parsing a directory with basic PHP files.
func TestParseProjectWithSimplePHPFiles(t *testing.T) {
	// Create temporary directory with PHP files
	tempDir := t.TempDir()

	// Create app directory
	appDir := filepath.Join(tempDir, "app")
	err := os.MkdirAll(appDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create app directory: %v", err)
	}

	// Create simple PHP files
	testFiles := map[string]string{
		"app/User.php": `<?php
namespace App;

class User {
    public function getName() {
        return "test";
    }
}`,
		"app/Controller/HomeController.php": `<?php
namespace App\Controller;

class HomeController {
    public function index() {
        return "home";
    }
}`,
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		dir := filepath.Dir(fullPath)
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to write file %s: %v", fullPath, err)
		}
	}

	// Create composer.json
	composerContent := `{
    "name": "test/project",
    "autoload": {
        "psr-4": {
            "App\\": "app/"
        }
    }
}`
	composerPath := filepath.Join(tempDir, "composer.json")
	err = os.WriteFile(composerPath, []byte(composerContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	// Parse project
	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer parser.Close()

	config := ProjectParserConfig{
		ProjectRoot:    tempDir,
		ComposerPath:   composerPath,
		Targets:        []string{"app"},
		Globs:          []string{"**/*.php"},
		MaxFiles:       100,
		MaxDepth:       10,
		MaxWorkers:     2,
		ParseTimeout:   30 * time.Second,
		CacheEnabled:   false,
		CacheKind:      "mtime",
		ExtractClasses: true,
		ExtractMethods: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := parser.ParseProject(ctx, config)
	if err != nil {
		t.Fatalf("Failed to parse project: %v", err)
	}

	// Verify files were discovered
	if len(result.DiscoveredFiles) != 2 {
		t.Errorf("Expected 2 discovered files, got %d", len(result.DiscoveredFiles))
	}

	// Verify statistics
	if result.Stats.FilesDiscovered != 2 {
		t.Errorf("Expected 2 files discovered, got %d", result.Stats.FilesDiscovered)
	}

	// Verify duration tracking
	if result.Stats.TotalDuration <= 0 {
		t.Error("Expected positive total duration")
	}
	if result.Stats.DiscoveryTime <= 0 {
		t.Error("Expected positive discovery time")
	}

	// Verify no partial flag (under limits)
	if result.Partial {
		t.Error("Expected complete results, got partial")
	}
}

// TestProjectParserConfigValidation tests configuration validation.
func TestProjectParserConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  ProjectParserConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			config:  DefaultProjectParserConfig(),
			wantErr: false,
		},
		{
			name: "empty project root",
			config: ProjectParserConfig{
				ProjectRoot:  "",
				ComposerPath: "composer.json",
				Targets:      []string{"app"},
				Globs:        []string{"**/*.php"},
				MaxFiles:     100,
				MaxDepth:     5,
				MaxWorkers:   4,
				ParseTimeout: 30 * time.Second,
				CacheKind:    "mtime",
			},
			wantErr: true,
			errMsg:  "project root cannot be empty",
		},
		{
			name: "zero max workers",
			config: ProjectParserConfig{
				ProjectRoot:  "/test",
				ComposerPath: "composer.json",
				Targets:      []string{"app"},
				Globs:        []string{"**/*.php"},
				MaxFiles:     100,
				MaxDepth:     5,
				MaxWorkers:   0,
				ParseTimeout: 30 * time.Second,
				CacheKind:    "mtime",
			},
			wantErr: true,
			errMsg:  "max workers must be positive",
		},
		{
			name: "invalid cache kind",
			config: ProjectParserConfig{
				ProjectRoot:  "/test",
				ComposerPath: "composer.json",
				Targets:      []string{"app"},
				Globs:        []string{"**/*.php"},
				MaxFiles:     100,
				MaxDepth:     5,
				MaxWorkers:   4,
				ParseTimeout: 30 * time.Second,
				CacheEnabled: true,
				CacheKind:    "invalid",
			},
			wantErr: true,
			errMsg:  "invalid cache kind",
		},
		{
			name: "too many workers",
			config: ProjectParserConfig{
				ProjectRoot:  "/test",
				ComposerPath: "composer.json",
				Targets:      []string{"app"},
				Globs:        []string{"**/*.php"},
				MaxFiles:     100,
				MaxDepth:     5,
				MaxWorkers:   100,
				ParseTimeout: 30 * time.Second,
				CacheKind:    "mtime",
			},
			wantErr: true,
			errMsg:  "max workers too high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectParserConfig(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected validation error, got nil")
				} else if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

// TestContextCancellation tests that parsing respects context cancellation.
func TestContextCancellation(t *testing.T) {
	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer parser.Close()

	config := DefaultProjectParserConfig()
	config.ProjectRoot = t.TempDir()

	// Create context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Attempt to parse with cancelled context
	result, err := parser.ParseProject(ctx, config)

	// Should respect cancellation
	if err == nil {
		t.Error("Expected error due to cancelled context")
	}
	if result == nil {
		t.Error("Expected partial result even with cancellation")
	}
}

// TestMemoryEstimation tests memory usage estimation.
func TestMemoryEstimation(t *testing.T) {
	tests := []struct {
		name     string
		config   ProjectParserConfig
		expected int64 // Rough expected memory in MB
	}{
		{
			name: "small project",
			config: ProjectParserConfig{
				MaxWorkers: 2,
				MaxFiles:   100,
			},
			expected: 12, // Base 10MB + 2*2MB workers + 1MB files
		},
		{
			name: "large project",
			config: ProjectParserConfig{
				MaxWorkers: 8,
				MaxFiles:   1000,
			},
			expected: 37, // Base 10MB + 8*2MB workers + 11MB files
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			estimated := EstimateMemoryUsage(tt.config)
			estimatedMB := estimated / (1024 * 1024)

			if estimatedMB < tt.expected-5 || estimatedMB > tt.expected+5 {
				t.Errorf("Expected memory estimate around %d MB, got %d MB",
					tt.expected, estimatedMB)
			}
		})
	}
}

// TestResourceWarnings tests resource usage warnings.
func TestResourceWarnings(t *testing.T) {
	// High resource config should generate warnings
	highResourceConfig := ProjectParserConfig{
		MaxWorkers:   16,              // High worker count (exceeds 8)
		MaxFiles:     50000,           // Large file count (exceeds 5000)
		ParseTimeout: 1 * time.Second, // Short timeout (below 5s)
	}

	warnings := WarnOnHighResourceUsage(highResourceConfig)
	if len(warnings) == 0 {
		t.Error("Expected warnings for high resource usage")
	}

	// Look for specific warning types
	hasMemoryWarning := false
	hasWorkerWarning := false
	hasFileWarning := false
	hasTimeoutWarning := false

	for _, warning := range warnings {
		if contains(warning, "memory usage") {
			hasMemoryWarning = true
		}
		if contains(warning, "worker count") {
			hasWorkerWarning = true
		}
		if contains(warning, "file limit") {
			hasFileWarning = true
		}
		if contains(warning, "timeout") {
			hasTimeoutWarning = true
		}
	}

	if !hasMemoryWarning {
		t.Error("Expected memory usage warning")
	}
	if !hasWorkerWarning {
		t.Error("Expected worker count warning")
	}
	if !hasFileWarning {
		t.Error("Expected file limit warning")
	}
	if !hasTimeoutWarning {
		t.Error("Expected timeout warning")
	}
}

// TestPhaseStringConversion tests phase to string conversion.
func TestPhaseStringConversion(t *testing.T) {
	tests := []struct {
		phase    ProjectParserPhase
		expected string
	}{
		{ProjectParserPhaseInitializing, "Initializing"},
		{ProjectParserPhaseDiscovering, "Discovering Files"},
		{ProjectParserPhaseResolving, "Resolving Namespaces"},
		{ProjectParserPhaseParsing, "Parsing PHP Files"},
		{ProjectParserPhaseExtracting, "Extracting Constructs"},
		{ProjectParserPhaseCompleted, "Completed"},
		{ProjectParserPhaseFailed, "Failed"},
		{ProjectParserPhase(999), "Unknown"},
	}

	for _, tt := range tests {
		result := ProjectParserPhaseString(tt.phase)
		if result != tt.expected {
			t.Errorf("Phase %v: expected '%s', got '%s'", tt.phase, tt.expected, result)
		}
	}
}

// TestConcurrentParsing tests that concurrent parsing works correctly.
func TestConcurrentParsing(t *testing.T) {
	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}
	defer parser.Close()

	// Test with multiple workers
	config := DefaultProjectParserConfig()
	config.MaxWorkers = 4
	config.ProjectRoot = t.TempDir()

	// Verify concurrent parser configured correctly
	if parser.concurrentParser.GetActiveWorkers() > config.MaxWorkers {
		t.Error("Too many active workers configured")
	}
}

// TestParserCleanup tests proper resource cleanup.
func TestParserCleanup(t *testing.T) {
	parser, err := NewPHPProjectParser()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	// Close should succeed
	err = parser.Close()
	if err != nil {
		t.Errorf("Failed to close parser: %v", err)
	}

	// Second close should be safe
	err = parser.Close()
	if err != nil {
		t.Errorf("Second close failed: %v", err)
	}

	// Operations on closed parser should fail
	err = parser.LoadFromManifest(&manifest.Manifest{})
	if err == nil {
		t.Error("Expected error when loading manifest on closed parser")
	}
}

// Helper functions

func intPtr(i int) *int {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
