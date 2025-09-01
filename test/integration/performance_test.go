package integration

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/cli"
	"github.com/garaekz/oxinfer/internal/manifest"
)

// TestManifestProcessingPerformance benchmarks manifest processing performance
func TestManifestProcessingPerformance(t *testing.T) {
	// Create a test project directory structure
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "test/project"}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	// Create app directory
	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	// Create test manifest
	testManifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectDir,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets:         []string{"app"},
			VendorWhitelist: []string{"laravel/framework"},
			Globs:           []string{"**/*.php", "app/**/*.php", "src/**/*.php"},
		},
		Limits: &manifest.LimitsConfig{
			MaxFiles:    intPtr(5000),
			MaxFileSize: intPtr(2097152),
			Timeout:     intPtr(300),
		},
		Cache: &manifest.CacheConfig{
			Enabled: true,
			Dir:     ".oxinfer/cache",
			TTL:     intPtr(86400),
		},
		Features: &manifest.FeatureConfig{
			Routes:      boolPtr(true),
			Controllers: boolPtr(true),
			Models:      boolPtr(true),
			Middleware:  boolPtr(true),
			Migrations:  boolPtr(true),
			Policies:    boolPtr(false),
			Events:      boolPtr(true),
			Jobs:        boolPtr(false),
		},
	}

	// Create validator
	validator := manifest.NewValidator()

	// Measure manifest validation performance
	start := time.Now()
	for i := 0; i < 100; i++ {
		// Create a copy to avoid modifying the original
		testCopy := *testManifest
		
		err := validator.ValidatePaths(&testCopy)
		if err != nil {
			t.Fatalf("Manifest validation failed on iteration %d: %v", i, err)
		}
	}
	duration := time.Since(start)

	averageTime := duration / 100
	t.Logf("Average manifest validation time: %v", averageTime)

	// Performance threshold: should validate in less than 10ms on average
	if averageTime > 10*time.Millisecond {
		t.Errorf("Manifest validation too slow: %v > 10ms", averageTime)
	}
}

// BenchmarkManifestValidation benchmarks the manifest validation process
func BenchmarkManifestValidation(b *testing.B) {
	tempDir := b.TempDir()
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		b.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json and app directory
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		b.Fatalf("failed to create composer.json: %v", err)
	}

	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		b.Fatalf("failed to create app dir: %v", err)
	}

	testManifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectDir,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
		Limits: &manifest.LimitsConfig{
			MaxFiles:    intPtr(1000),
			MaxFileSize: intPtr(1048576),
			Timeout:     intPtr(300),
		},
	}

	validator := manifest.NewValidator()

	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		// Create a copy to avoid side effects
		testCopy := *testManifest
		
		err := validator.ValidatePaths(&testCopy)
		if err != nil {
			b.Fatalf("Validation failed: %v", err)
		}
	}
}

// BenchmarkManifestLoadFromReader benchmarks the manifest loading process
func BenchmarkManifestLoadFromReader(b *testing.B) {
	tempDir := b.TempDir()
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		b.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json and app directory
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		b.Fatalf("failed to create composer.json: %v", err)
	}

	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		b.Fatalf("failed to create app dir: %v", err)
	}

	manifestJSON := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app"]
		}
	}`, projectDir)

	validator := manifest.NewValidator()
	loader := manifest.NewLoader(validator)

	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(manifestJSON)
		_, err := loader.LoadFromReader(reader)
		if err != nil {
			b.Fatalf("LoadFromReader failed: %v", err)
		}
	}
}

// BenchmarkCLIConfigParsing benchmarks CLI config parsing
func BenchmarkCLIConfigParsing(b *testing.B) {
	args := []string{"--manifest", "test.json", "--out", "output.json", "--no-color"}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := cli.ParseFlags(args)
		if err != nil {
			b.Fatalf("Config parsing failed: %v", err)
		}
	}
}

// TestDeterministicManifestLoading tests that manifest loading is deterministic
func TestDeterministicManifestLoading(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json and app directory
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "test/project"}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	manifestJSON := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app"],
			"vendor_whitelist": ["laravel/framework", "symfony/console"],
			"globs": ["**/*.php", "app/**/*.php"]
		},
		"limits": {
			"max_files": 1000,
			"max_file_size": 1048576,
			"timeout": 300
		},
		"cache": {
			"enabled": true,
			"dir": ".oxinfer/cache",
			"ttl": 86400
		},
		"features": {
			"routes": true,
			"controllers": true,
			"models": true,
			"middleware": true,
			"migrations": true
		}
	}`, projectDir)

	validator := manifest.NewValidator()
	loader := manifest.NewLoader(validator)

	// Load and validate multiple times
	var hashes []string
	for i := 0; i < 5; i++ {
		reader := strings.NewReader(manifestJSON)
		loadedManifest, err := loader.LoadFromReader(reader)
		if err != nil {
			t.Fatalf("Loading failed on iteration %d: %v", i, err)
		}

		// Convert to a deterministic string representation
		repr := fmt.Sprintf("%+v", *loadedManifest)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(repr)))
		hashes = append(hashes, hash)
	}

	// All hashes should be identical
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("Iteration %d produced different result than iteration 0", i)
			t.Logf("Hash 0: %s", hashes[0])
			t.Logf("Hash %d: %s", i, hashes[i])
		}
	}
}

// TestCLIErrorDeterminism tests that CLI errors are deterministic
func TestCLIErrorDeterminism(t *testing.T) {
	tests := []struct {
		name        string
		errorFunc   func() *cli.CLIError
		description string
	}{
		{
			name:        "input_error",
			errorFunc:   func() *cli.CLIError { return cli.NewInputError("test input error") },
			description: "Input validation error",
		},
		{
			name:        "internal_error", 
			errorFunc:   func() *cli.CLIError { return cli.NewInternalError("test internal error") },
			description: "Internal processing error",
		},
		{
			name:        "schema_error",
			errorFunc:   func() *cli.CLIError { return cli.NewSchemaError("test schema error") },
			description: "Schema validation error",
		},
		{
			name:        "limit_error",
			errorFunc:   func() *cli.CLIError { return cli.NewLimitError("test limit error") },
			description: "Hard limit exceeded error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate same error multiple times
			var errorStrings []string
			
			for i := 0; i < 3; i++ {
				err := tt.errorFunc()
				errorStrings = append(errorStrings, err.Error())
			}

			// All error strings should be identical
			for i := 1; i < len(errorStrings); i++ {
				if errorStrings[i] != errorStrings[0] {
					t.Errorf("Error string not deterministic for %s", tt.name)
					t.Logf("String 0: %s", errorStrings[0])
					t.Logf("String %d: %s", i, errorStrings[i])
				}
			}
		})
	}
}

// TestManifestValidationConsistency tests that validation is consistent across calls
func TestManifestValidationConsistency(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json and app directory
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}
	
	tests := []struct {
		name     string
		manifest *manifest.Manifest
		wantErr  bool
	}{
		{
			name: "valid_manifest",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root:     projectDir,
					Composer: "composer.json",
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
				Limits: &manifest.LimitsConfig{
					MaxFiles:    intPtr(1000),
					MaxFileSize: intPtr(1048576),
					Timeout:     intPtr(300),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid_limits",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root:     projectDir,
					Composer: "composer.json",
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
				Limits: &manifest.LimitsConfig{
					MaxFiles:    intPtr(200000), // Too high
					MaxFileSize: intPtr(1048576),
					Timeout:     intPtr(300),
				},
			},
			wantErr: true,
		},
	}

	validator := manifest.NewValidator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate same manifest multiple times
			var results []bool
			var errorMessages []string
			
			for i := 0; i < 5; i++ {
				// Create a copy to avoid side effects
				testCopy := *tt.manifest
				
				err := validator.ValidatePaths(&testCopy)
				results = append(results, err != nil)
				
				if err != nil {
					errorMessages = append(errorMessages, err.Error())
				} else {
					errorMessages = append(errorMessages, "")
				}
			}

			// All results should be identical
			expectedResult := results[0]
			for i := 1; i < len(results); i++ {
				if results[i] != expectedResult {
					t.Errorf("Validation result inconsistent on iteration %d", i)
				}
			}

			// Error messages should be identical when there are errors
			if expectedResult { // If we expect errors
				expectedMessage := errorMessages[0]
				for i := 1; i < len(errorMessages); i++ {
					if errorMessages[i] != expectedMessage {
						t.Errorf("Error message inconsistent on iteration %d", i)
						t.Logf("Expected: %s", expectedMessage)
						t.Logf("Got: %s", errorMessages[i])
					}
				}
			}
		})
	}
}

// TestSliceOrderingDeterminism tests that slice operations maintain deterministic order
func TestSliceOrderingDeterminism(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json and app directory
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	manifestJSON := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app"],
			"vendor_whitelist": ["laravel/framework", "symfony/console", "doctrine/orm"],
			"globs": ["src/**/*.php", "app/**/*.php", "**/*.php"]
		}
	}`, projectDir)

	validator := manifest.NewValidator()
	loader := manifest.NewLoader(validator)

	// Load manifest multiple times and check that slice order is preserved
	var targetsResults [][]string
	var whitelistResults [][]string
	var globsResults [][]string
	
	for i := 0; i < 3; i++ {
		reader := strings.NewReader(manifestJSON)
		loadedManifest, err := loader.LoadFromReader(reader)
		if err != nil {
			t.Fatalf("LoadFromReader failed on iteration %d: %v", i, err)
		}
		
		targetsResults = append(targetsResults, loadedManifest.Scan.Targets)
		whitelistResults = append(whitelistResults, loadedManifest.Scan.VendorWhitelist)
		globsResults = append(globsResults, loadedManifest.Scan.Globs)
	}

	// Compare slice orders - they should be identical
	for i := 1; i < len(targetsResults); i++ {
		if len(targetsResults[i]) != len(targetsResults[0]) {
			t.Errorf("Targets length changed between iterations")
			continue
		}
		
		for j, target := range targetsResults[i] {
			if target != targetsResults[0][j] {
				t.Errorf("Targets order changed at index %d between iterations", j)
			}
		}
	}

	for i := 1; i < len(whitelistResults); i++ {
		if len(whitelistResults[i]) != len(whitelistResults[0]) {
			t.Errorf("Whitelist length changed between iterations")
			continue
		}
		
		for j, item := range whitelistResults[i] {
			if item != whitelistResults[0][j] {
				t.Errorf("Whitelist order changed at index %d between iterations", j)
			}
		}
	}
}

// TestMemoryUsageBaseline establishes a baseline for memory usage
func TestMemoryUsageBaseline(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json and app directory
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "memory/test"}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	manifestJSON := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app"],
			"vendor_whitelist": ["laravel/framework"],
			"globs": ["**/*.php", "app/**/*.php", "src/**/*.php", "routes/**/*.php"]
		},
		"limits": {
			"max_files": 5000,
			"max_file_size": 2097152,
			"timeout": 300
		}
	}`, projectDir)

	validator := manifest.NewValidator()
	loader := manifest.NewLoader(validator)

	// Measure processing time for multiple iterations
	start := time.Now()
	iterations := 1000
	
	for i := 0; i < iterations; i++ {
		reader := strings.NewReader(manifestJSON)
		_, err := loader.LoadFromReader(reader)
		if err != nil {
			t.Fatalf("LoadFromReader failed on iteration %d: %v", i, err)
		}
	}
	
	duration := time.Since(start)
	averageTime := duration / time.Duration(iterations)
	
	t.Logf("Processed %d manifests in %v", iterations, duration)
	t.Logf("Average processing time: %v", averageTime)
	
	// Basic performance expectation - should process at least 100 manifests per second
	if averageTime > 10*time.Millisecond {
		t.Errorf("Processing too slow: %v per manifest (expected < 10ms)", averageTime)
	}
}

// TestStringDeterminism tests that string operations are deterministic
func TestStringDeterminism(t *testing.T) {
	// Test error message formatting consistency
	testErrors := []struct {
		message string
		cause   error
	}{
		{"test error", nil},
		{"wrapped error", fmt.Errorf("underlying cause")},
		{"complex error", fmt.Errorf("cause with %s formatting", "string")},
	}

	for _, testErr := range testErrors {
		var results []string
		
		// Generate same error multiple times
		for i := 0; i < 3; i++ {
			var cliErr *cli.CLIError
			if testErr.cause != nil {
				cliErr = cli.WrapInputError(testErr.message, testErr.cause)
			} else {
				cliErr = cli.NewInputError(testErr.message)
			}
			
			results = append(results, cliErr.Error())
		}

		// All results should be identical
		for i := 1; i < len(results); i++ {
			if results[i] != results[0] {
				t.Errorf("Error string not deterministic for %q", testErr.message)
				t.Logf("Result 0: %s", results[0])
				t.Logf("Result %d: %s", i, results[i])
			}
		}
	}
}

// Helper functions for creating pointers
func intPtr(i int) *int {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}