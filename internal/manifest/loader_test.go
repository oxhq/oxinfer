package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/garaekz/oxinfer/internal/cli"
)

func TestManifestLoader_LoadFromReader(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := t.TempDir()
	testProjectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(testProjectDir, 0755); err != nil {
		t.Fatalf("failed to create test project dir: %v", err)
	}

	// Create a test composer.json file
	composerFile := filepath.Join(testProjectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "test/project"}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	// Create test target directories
	appDir := filepath.Join(testProjectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	validator := NewValidator()
	loader := NewLoader(validator)

	tests := []struct {
		name           string
		jsonInput      string
		wantErr        bool
		errorType      cli.ExitCode
		expectManifest func(*testing.T, *Manifest)
	}{
		{
			name: "valid minimal manifest",
			jsonInput: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": ["app"]
				}
			}`, testProjectDir),
			wantErr: false,
			expectManifest: func(t *testing.T, m *Manifest) {
				if m.Project.Root != testProjectDir {
					t.Errorf("expected root %s, got %s", testProjectDir, m.Project.Root)
				}
				if len(m.Scan.Targets) != 1 || m.Scan.Targets[0] != "app" {
					t.Errorf("expected targets [app], got %v", m.Scan.Targets)
				}
				if m.Project.Composer != composerFile {
					t.Errorf("expected composer %s, got %s", composerFile, m.Project.Composer)
				}
				// Check that defaults were applied
				if len(m.Scan.Globs) == 0 {
					t.Error("expected default globs to be applied")
				}
			},
		},
		{
			name: "valid full manifest",
			jsonInput: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": ["app"],
					"vendor_whitelist": ["laravel/framework"],
					"globs": ["**/*.php"]
				},
				"limits": {
					"max_workers": 4,
					"max_files": 2000,
					"max_depth": 5
				},
				"cache": {
					"enabled": true,
					"kind": "sha256+mtime"
				},
				"features": {
					"http_status": true,
					"request_usage": true,
					"resource_usage": false,
					"with_pivot": false,
					"attribute_make": true,
					"scopes_used": true,
					"polymorphic": false,
					"broadcast_channels": true
				}
			}`, testProjectDir),
			wantErr: false,
			expectManifest: func(t *testing.T, m *Manifest) {
				if m.Project.Root != testProjectDir {
					t.Errorf("expected root %s, got %s", testProjectDir, m.Project.Root)
				}
				if len(m.Scan.Targets) != 1 {
					t.Errorf("expected 1 scan target, got %d", len(m.Scan.Targets))
				}
				if m.Limits != nil {
					if *m.Limits.MaxWorkers != 4 {
						t.Errorf("expected max_workers 4, got %d", *m.Limits.MaxWorkers)
					}
					if *m.Limits.MaxFiles != 2000 {
						t.Errorf("expected max_files 2000, got %d", *m.Limits.MaxFiles)
					}
					if *m.Limits.MaxDepth != 5 {
						t.Errorf("expected max_depth 5, got %d", *m.Limits.MaxDepth)
					}
				}
				if m.Cache != nil && (m.Cache.Enabled == nil || !*m.Cache.Enabled) {
					t.Error("expected cache to be enabled")
				}
				// Check feature flags
				if m.Features != nil {
					if m.Features.HTTPStatus == nil || !*m.Features.HTTPStatus {
						t.Error("expected http_status feature to be true")
					}
					if m.Features.ResourceUsage != nil && *m.Features.ResourceUsage {
						t.Error("expected resource_usage feature to be false")
					}
				}
			},
		},
		{
			name: "manifest with optional fields omitted",
			jsonInput: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": ["app"]
				}
			}`, testProjectDir),
			wantErr: false,
			expectManifest: func(t *testing.T, m *Manifest) {
				if m.Project.Root != testProjectDir {
					t.Errorf("expected root %s, got %s", testProjectDir, m.Project.Root)
				}
				if len(m.Scan.Targets) != 1 || m.Scan.Targets[0] != "app" {
					t.Errorf("expected targets [app], got %v", m.Scan.Targets)
				}
				// Optional fields should be nil when not specified
				if m.Limits != nil && m.Limits.MaxFiles != nil {
					t.Error("expected limits.max_files to be nil when not specified")
				}
			},
		},
		{
			name:      "invalid JSON syntax",
			jsonInput: `{"project": {"root": "/tmp"}, "scan": {`,
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "missing required project field",
			jsonInput: `{
				"scan": {
					"targets": ["app"]
				}
			}`,
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "missing required scan field",
			jsonInput: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				}
			}`, testProjectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "invalid field type",
			jsonInput: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": "app"
				}
			}`, testProjectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "additional properties not allowed",
			jsonInput: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json",
					"unknown_field": "value"
				},
				"scan": {
					"targets": ["app"]
				}
			}`, testProjectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "invalid limits values",
			jsonInput: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": ["app"]
				},
				"limits": {
					"max_workers": 0
				}
			}`, testProjectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name:      "empty JSON",
			jsonInput: `{}`,
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name:      "null JSON",
			jsonInput: `null`,
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.jsonInput)
			manifest, err := loader.LoadFromReader(reader)

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadFromReader() error = nil, wantErr %v", tt.wantErr)
					return
				}

				// Check error type
				if cliErr, ok := err.(*cli.CLIError); ok {
					if cliErr.ExitCode != tt.errorType {
						t.Errorf("LoadFromReader() error type = %v, want %v", cliErr.ExitCode, tt.errorType)
					}
				} else {
					t.Errorf("LoadFromReader() error should be CLIError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("LoadFromReader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if manifest == nil {
				t.Error("LoadFromReader() returned nil manifest")
				return
			}

			// Run custom validation if provided
			if tt.expectManifest != nil {
				tt.expectManifest(t, manifest)
			}
		})
	}
}

func TestManifestLoader_LoadFromFile(t *testing.T) {
	tempDir := t.TempDir()
	testProjectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(testProjectDir, 0755); err != nil {
		t.Fatalf("failed to create test project dir: %v", err)
	}

	// Create a test composer.json file
	composerFile := filepath.Join(testProjectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "test/project"}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	// Create test target directory
	appDir := filepath.Join(testProjectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	// Create a valid manifest file
	manifestPath := filepath.Join(tempDir, "manifest.json")
	manifestContent := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app"]
		}
	}`, testProjectDir)

	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write manifest file: %v", err)
	}

	validator := NewValidator()
	loader := NewLoader(validator)

	tests := []struct {
		name      string
		filePath  string
		wantErr   bool
		errorType cli.ExitCode
	}{
		{
			name:     "valid manifest file",
			filePath: manifestPath,
			wantErr:  false,
		},
		{
			name:      "nonexistent file",
			filePath:  "/nonexistent/file.json",
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := loader.LoadFromFile(tt.filePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadFromFile() error = nil, wantErr %v", tt.wantErr)
					return
				}

				if cliErr, ok := err.(*cli.CLIError); ok {
					if cliErr.ExitCode != tt.errorType {
						t.Errorf("LoadFromFile() error type = %v, want %v", cliErr.ExitCode, tt.errorType)
					}
				} else {
					t.Errorf("LoadFromFile() error should be CLIError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("LoadFromFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if manifest == nil {
				t.Error("LoadFromFile() returned nil manifest")
				return
			}

			// Basic validation
			if manifest.Project.Root != testProjectDir {
				t.Errorf("expected root %s, got %s", testProjectDir, manifest.Project.Root)
			}
		})
	}
}

func TestManifestLoader_LoadFromReaderEmptyReader(t *testing.T) {
	validator := NewValidator()
	loader := NewLoader(validator)
	reader := strings.NewReader("")
	manifest, err := loader.LoadFromReader(reader)

	if manifest != nil {
		t.Error("LoadFromReader() with empty reader should not return manifest")
	}

	if err == nil {
		t.Error("LoadFromReader() with empty reader should return error")
		return
	}

	if cliErr, ok := err.(*cli.CLIError); ok {
		if cliErr.ExitCode != cli.ExitInputError {
			t.Errorf("LoadFromReader() empty reader error type = %v, want %v", cliErr.ExitCode, cli.ExitInputError)
		}
	}
}

func TestManifestLoader_LoadFromReaderInvalidJSON(t *testing.T) {
	validator := NewValidator()
	loader := NewLoader(validator)
	reader := strings.NewReader("invalid json content")
	manifest, err := loader.LoadFromReader(reader)

	if manifest != nil {
		t.Error("LoadFromReader() with invalid JSON should not return manifest")
	}

	if err == nil {
		t.Error("LoadFromReader() with invalid JSON should return error")
		return
	}

	if cliErr, ok := err.(*cli.CLIError); ok {
		if cliErr.ExitCode != cli.ExitInputError {
			t.Errorf("LoadFromReader() invalid JSON error type = %v, want %v", cliErr.ExitCode, cli.ExitInputError)
		}
	}
}

func TestManifestValidator_ValidateSchema(t *testing.T) {
	tempDir := t.TempDir()
	testProjectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(testProjectDir, 0755); err != nil {
		t.Fatalf("failed to create test project dir: %v", err)
	}

	validator := NewValidator()

	tests := []struct {
		name      string
		jsonData  string
		wantErr   bool
		errorType cli.ExitCode
	}{
		{
			name: "valid schema",
			jsonData: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": ["app"]
				}
			}`, testProjectDir),
			wantErr: false,
		},
		{
			name: "missing required project",
			jsonData: `{
				"scan": {
					"targets": ["app"]
				}
			}`,
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "missing required root",
			jsonData: `{
				"project": {
					"composer": "composer.json"
				},
				"scan": {
					"targets": ["app"]
				}
			}`,
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "invalid targets type",
			jsonData: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": "app"
				}
			}`, testProjectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name:      "invalid JSON",
			jsonData:  `{invalid}`,
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSchema([]byte(tt.jsonData))

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSchema() error = nil, wantErr %v", tt.wantErr)
					return
				}

				if cliErr, ok := err.(*cli.CLIError); ok {
					if cliErr.ExitCode != tt.errorType {
						t.Errorf("ValidateSchema() error type = %v, want %v", cliErr.ExitCode, tt.errorType)
					}
				} else {
					t.Errorf("ValidateSchema() error should be CLIError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManifestValidator_ValidatePaths(t *testing.T) {
	tempDir := t.TempDir()
	testProjectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(testProjectDir, 0755); err != nil {
		t.Fatalf("failed to create test project dir: %v", err)
	}

	// Create composer.json
	composerFile := filepath.Join(testProjectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	// Create app directory
	appDir := filepath.Join(testProjectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	validator := NewValidator()

	tests := []struct {
		name      string
		manifest  *Manifest
		wantErr   bool
		errorType cli.ExitCode
	}{
		{
			name: "valid manifest",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     testProjectDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr: false,
		},
		{
			name: "empty root path",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     "",
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "nonexistent root path",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     "/nonexistent/path",
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "empty targets",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     testProjectDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "empty globs",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     testProjectDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{},
				},
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidatePaths(tt.manifest)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidatePaths() error = nil, wantErr %v", tt.wantErr)
					return
				}

				if cliErr, ok := err.(*cli.CLIError); ok {
					if cliErr.ExitCode != tt.errorType {
						t.Errorf("ValidatePaths() error type = %v, want %v", cliErr.ExitCode, tt.errorType)
					}
				} else {
					t.Errorf("ValidatePaths() error should be CLIError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidatePaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestLoadDeterminism tests that loading the same manifest produces identical results
func TestLoadDeterminism(t *testing.T) {
	tempDir := t.TempDir()
	testProjectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(testProjectDir, 0755); err != nil {
		t.Fatalf("failed to create test project dir: %v", err)
	}

	// Create composer.json
	composerFile := filepath.Join(testProjectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	// Create app directory
	appDir := filepath.Join(testProjectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	jsonInput := fmt.Sprintf(`{
		"project": {
			"root": "%s",
			"composer": "composer.json"
		},
		"scan": {
			"targets": ["app"],
			"vendor_whitelist": ["laravel/framework"],
			"globs": ["**/*.php"]
		},
		"limits": {
			"max_files": 2000,
			"max_workers": 4,
			"max_depth": 3
		}
	}`, testProjectDir)

	validator := NewValidator()
	loader := NewLoader(validator)

	// Load the same manifest multiple times
	var manifests []*Manifest
	for i := 0; i < 3; i++ {
		reader := strings.NewReader(jsonInput)
		manifest, err := loader.LoadFromReader(reader)
		if err != nil {
			t.Fatalf("LoadFromReader() attempt %d failed: %v", i, err)
		}
		manifests = append(manifests, manifest)
	}

	// Compare all manifests - they should be identical
	for i := 1; i < len(manifests); i++ {
		m1, m2 := manifests[0], manifests[i]

		if m1.Project.Root != m2.Project.Root {
			t.Errorf("LoadFromReader() not deterministic: root differs between loads")
		}
		if m1.Project.Composer != m2.Project.Composer {
			t.Errorf("LoadFromReader() not deterministic: composer differs between loads")
		}
		if len(m1.Scan.Targets) != len(m2.Scan.Targets) {
			t.Errorf("LoadFromReader() not deterministic: targets count differs between loads")
		}
		if m1.Limits != nil && m2.Limits != nil {
			if *m1.Limits.MaxFiles != *m2.Limits.MaxFiles {
				t.Errorf("LoadFromReader() not deterministic: limits differ between loads")
			}
		}
	}
}

func TestNewLoader(t *testing.T) {
	validator := NewValidator()
	loader := NewLoader(validator)

	if loader == nil {
		t.Error("NewLoader() returned nil")
	}

	// Test that loader implements the interface
	var _ ManifestLoader = loader
}

func TestNewValidator(t *testing.T) {
	validator := NewValidator()

	if validator == nil {
		t.Error("NewValidator() returned nil")
	}

	// Test that validator implements the interface
	var _ ManifestValidator = validator
}
