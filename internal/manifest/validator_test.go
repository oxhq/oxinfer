package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/garaekz/oxinfer/internal/cli"
)

func TestManifestValidator_ValidatePaths_Project(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir := t.TempDir()
	validDir := filepath.Join(tempDir, "valid-project")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatalf("failed to create valid project dir: %v", err)
	}

	// Create composer.json
	composerFile := filepath.Join(validDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "test/project"}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	// Create app directory for scan targets
	appDir := filepath.Join(validDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}

	// Create a file instead of directory for negative testing
	invalidFile := filepath.Join(tempDir, "not-a-directory")
	if err := os.WriteFile(invalidFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	validator := NewValidator()

	tests := []struct {
		name      string
		manifest  *Manifest
		wantErr   bool
		errorType cli.ExitCode
		checkRoot func(*testing.T, string)
	}{
		{
			name: "valid manifest",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     validDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr: false,
			checkRoot: func(t *testing.T, root string) {
				if root != validDir {
					t.Errorf("root should be %s, got %s", validDir, root)
				}
			},
		},
		{
			name: "valid manifest with relative path",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     ".",
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"."},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr: false,
			checkRoot: func(t *testing.T, root string) {
				if !filepath.IsAbs(root) {
					t.Errorf("relative path should be converted to absolute, got %s", root)
				}
			},
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
			name: "nonexistent path",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     "/nonexistent/path/that/does/not/exist",
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
			name: "path is file not directory",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     invalidFile,
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
			name: "nonexistent composer.json",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     validDir,
					Composer: "nonexistent-composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Handle relative path test case by changing working directory
			if tt.name == "valid manifest with relative path" {
				originalWd, _ := os.Getwd()
				os.Chdir(validDir)
				defer os.Chdir(originalWd)
			}
			
			err := validator.ValidatePaths(tt.manifest)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidatePaths() error = nil, wantErr %v", tt.wantErr)
					return
				}

				// Check error type
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
				return
			}

			// Run custom root validation if provided
			if tt.checkRoot != nil {
				tt.checkRoot(t, tt.manifest.Project.Root)
			}

			// For successful validations, verify the paths exist
			if _, statErr := os.Stat(tt.manifest.Project.Root); statErr != nil {
				t.Errorf("validated root path should exist: %v", statErr)
			}

			// Verify composer path exists and is normalized
			if _, statErr := os.Stat(tt.manifest.Project.Composer); statErr != nil {
				t.Errorf("validated composer path should exist: %v", statErr)
			}
		})
	}
}

func TestManifestValidator_ValidatePaths_Scan(t *testing.T) {
	// Create test directory structure
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create composer.json
	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	// Create valid target directories
	appDir := filepath.Join(projectDir, "app")
	srcDir := filepath.Join(projectDir, "src")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}

	// Create a file (not directory) for negative testing
	fileNotDir := filepath.Join(projectDir, "file.txt")
	if err := os.WriteFile(fileNotDir, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	validator := NewValidator()

	tests := []struct {
		name      string
		manifest  *Manifest
		wantErr   bool
		errorType cli.ExitCode
	}{
		{
			name: "valid scan config",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     projectDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app", "src"},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr: false,
		},
		{
			name: "empty targets",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     projectDir,
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
					Root:     projectDir,
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
		{
			name: "nonexistent target directory",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     projectDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"nonexistent"},
					Globs:   []string{"**/*.php"},
				},
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "target is file not directory",
			manifest: &Manifest{
				Project: ProjectConfig{
					Root:     projectDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"file.txt"},
					Globs:   []string{"**/*.php"},
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

func TestManifestValidator_ValidatePaths_Limits(t *testing.T) {
	// Create test directory structure
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
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

	validator := NewValidator()

	tests := []struct {
		name      string
		limits    *LimitsConfig
		wantErr   bool
		errorType cli.ExitCode
	}{
		{
			name:    "nil limits (valid)",
			limits:  nil,
			wantErr: false,
		},
		{
			name: "valid limits",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(1000),
				MaxFileSize: intPtr(1048576),
				Timeout:     intPtr(300),
			},
			wantErr: false,
		},
		{
			name: "max files too low",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(0),
				MaxFileSize: intPtr(1048576),
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "max files too high",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(100001),
				MaxFileSize: intPtr(1048576),
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "max file size too low",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(1000),
				MaxFileSize: intPtr(1023),
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "max file size too high",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(1000),
				MaxFileSize: intPtr(104857601),
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "timeout too low",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(1000),
				MaxFileSize: intPtr(1048576),
				Timeout:     intPtr(0),
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "timeout too high",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(1000),
				MaxFileSize: intPtr(1048576),
				Timeout:     intPtr(3601),
			},
			wantErr:   true,
			errorType: cli.ExitInputError,
		},
		{
			name: "boundary values minimum",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(1),
				MaxFileSize: intPtr(1024),
				Timeout:     intPtr(1),
			},
			wantErr: false,
		},
		{
			name: "boundary values maximum",
			limits: &LimitsConfig{
				MaxFiles:    intPtr(100000),
				MaxFileSize: intPtr(104857600),
				Timeout:     intPtr(3600),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := &Manifest{
				Project: ProjectConfig{
					Root:     projectDir,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
				Limits: tt.limits,
			}

			err := validator.ValidatePaths(manifest)

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

func TestManifestValidator_ValidateSchema_Integration(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	validator := NewValidator()

	tests := []struct {
		name      string
		jsonData  string
		wantErr   bool
		errorType cli.ExitCode
	}{
		{
			name: "valid complete manifest",
			jsonData: fmt.Sprintf(`{
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
					"controllers": false
				}
			}`, projectDir),
			wantErr: false,
		},
		{
			name: "schema validation - missing project",
			jsonData: `{
				"scan": {
					"targets": ["app"]
				}
			}`,
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "schema validation - invalid type",
			jsonData: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": "app"
				}
			}`, projectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "schema validation - additional property",
			jsonData: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json",
					"unknown": "field"
				},
				"scan": {
					"targets": ["app"]
				}
			}`, projectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
		},
		{
			name: "schema validation - limits out of range",
			jsonData: fmt.Sprintf(`{
				"project": {
					"root": "%s",
					"composer": "composer.json"
				},
				"scan": {
					"targets": ["app"]
				},
				"limits": {
					"max_files": 200000
				}
			}`, projectDir),
			wantErr:   true,
			errorType: cli.ExitSchemaError,
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

func TestManifestValidator_PathNormalization(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
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

	validator := NewValidator()

	tests := []struct {
		name        string
		inputPath   string
		expectedAbs bool
	}{
		{
			name:        "absolute path unchanged",
			inputPath:   projectDir,
			expectedAbs: true,
		},
		{
			name:        "relative path converted to absolute",
			inputPath:   ".",
			expectedAbs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Change to a known directory for consistent relative path testing
			originalWd, _ := os.Getwd()
			if tt.inputPath == "." {
				os.Chdir(projectDir)
				defer os.Chdir(originalWd)
			}

			manifest := &Manifest{
				Project: ProjectConfig{
					Root:     tt.inputPath,
					Composer: "composer.json",
				},
				Scan: ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"**/*.php"},
				},
			}

			err := validator.ValidatePaths(manifest)
			if err != nil {
				t.Errorf("ValidatePaths() failed: %v", err)
				return
			}

			// Check path normalization
			if tt.expectedAbs && !filepath.IsAbs(manifest.Project.Root) {
				t.Errorf("path should be absolute after validation, got %s", manifest.Project.Root)
			}

			// Verify the path exists
			if _, statErr := os.Stat(manifest.Project.Root); statErr != nil {
				t.Errorf("normalized path should exist: %v", statErr)
			}

			// Verify composer path is also normalized
			if !filepath.IsAbs(manifest.Project.Composer) {
				t.Errorf("composer path should be absolute after validation, got %s", manifest.Project.Composer)
			}
		})
	}
}

func TestManifestValidator_ErrorMessages(t *testing.T) {
	tempDir := t.TempDir()
	validDir := filepath.Join(tempDir, "valid")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatalf("failed to create valid dir: %v", err)
	}

	// Create composer.json for tests that need it
	composerFile := filepath.Join(validDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to create composer.json: %v", err)
	}

	validator := NewValidator()

	tests := []struct {
		name            string
		manifest        *Manifest
		wantErrContains string
	}{
		{
			name: "empty root error message",
			manifest: &Manifest{
				Project: ProjectConfig{Root: ""},
				Scan:    ScanConfig{Targets: []string{"app"}, Globs: []string{"**/*.php"}},
			},
			wantErrContains: "cannot be empty",
		},
		{
			name: "nonexistent path error message",
			manifest: &Manifest{
				Project: ProjectConfig{Root: "/nonexistent/path", Composer: "composer.json"},
				Scan:    ScanConfig{Targets: []string{"app"}, Globs: []string{"**/*.php"}},
			},
			wantErrContains: "does not exist",
		},
		{
			name: "empty targets error message",
			manifest: &Manifest{
				Project: ProjectConfig{Root: validDir, Composer: "composer.json"},
				Scan:    ScanConfig{Targets: []string{}, Globs: []string{"**/*.php"}},
			},
			wantErrContains: "cannot be empty",
		},
		{
			name: "empty globs error message",
			manifest: &Manifest{
				Project: ProjectConfig{Root: validDir, Composer: "composer.json"},
				Scan:    ScanConfig{Targets: []string{"."}, Globs: []string{}},
			},
			wantErrContains: "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidatePaths(tt.manifest)
			if err == nil {
				t.Errorf("ValidatePaths() should return error for %s", tt.name)
				return
			}

			errMsg := err.Error()
			if !strings.Contains(errMsg, tt.wantErrContains) {
				t.Errorf("error message should contain %q, got %q", tt.wantErrContains, errMsg)
			}
		})
	}
}

