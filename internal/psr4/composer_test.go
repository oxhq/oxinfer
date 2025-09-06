package psr4

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultComposerLoader_LoadComposer(t *testing.T) {
	tests := []struct {
		name           string
		composerJSON   string
		expectError    bool
		errorType      error
		expectedConfig *ComposerConfig
	}{
		{
			name: "valid Laravel composer.json",
			composerJSON: `{
				"name": "laravel/laravel",
				"type": "project",
				"description": "The Laravel Framework.",
				"autoload": {
					"psr-4": {
						"App\\": "app/",
						"Database\\Seeders\\": "database/seeders/"
					}
				},
				"autoload-dev": {
					"psr-4": {
						"Tests\\": "tests/"
					}
				}
			}`,
			expectError: false,
			expectedConfig: &ComposerConfig{
				Name: "laravel/laravel",
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\":               "app/",
						"Database\\Seeders\\": "database/seeders/",
					},
				},
				AutoloadDev: AutoloadSection{
					PSR4: map[string]any{
						"Tests\\": "tests/",
					},
				},
			},
		},
		{
			name: "composer with multiple path formats",
			composerJSON: `{
				"name": "test/package",
				"autoload": {
					"psr-4": {
						"App\\": "src/",
						"Multi\\": ["src/Multi", "lib/Multi"],
						"Single\\": "src/Single/"
					}
				}
			}`,
			expectError: false,
			expectedConfig: &ComposerConfig{
				Name: "test/package",
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\":    "src/",
						"Multi\\":  []any{"src/Multi", "lib/Multi"},
						"Single\\": "src/Single/",
					},
				},
			},
		},
		{
			name: "minimal composer with only psr-4",
			composerJSON: `{
				"autoload": {
					"psr-4": {
						"App\\": "app/"
					}
				}
			}`,
			expectError: false,
			expectedConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
		},
		{
			name: "composer with classmap and files",
			composerJSON: `{
				"name": "test/package",
				"autoload": {
					"psr-4": {
						"App\\": "src/"
					},
					"classmap": ["legacy/"],
					"files": ["helpers.php"]
				}
			}`,
			expectError: false,
			expectedConfig: &ComposerConfig{
				Name: "test/package",
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "src/",
					},
					Classmap: []string{"legacy/"},
					Files:    []string{"helpers.php"},
				},
			},
		},
		{
			name:         "malformed JSON",
			composerJSON: `{"name": "test/package", "invalid": json}`,
			expectError:  true,
			errorType:    ErrComposerMalformed,
		},
		{
			name:           "empty JSON",
			composerJSON:   `{}`,
			expectError:    false,
			expectedConfig: &ComposerConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpFile := createTempComposerFile(t, tt.composerJSON)
			defer os.Remove(tmpFile)

			loader := NewComposerLoader()
			config, err := loader.LoadComposer(tmpFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("LoadComposer() expected error, got nil")
					return
				}
				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("LoadComposer() expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("LoadComposer() unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(config, tt.expectedConfig) {
				t.Errorf("LoadComposer() config mismatch.\nExpected: %+v\nGot: %+v", tt.expectedConfig, config)
			}
		})
	}
}

func TestDefaultComposerLoader_LoadComposer_FileNotFound(t *testing.T) {
	loader := NewComposerLoader()
	_, err := loader.LoadComposer("/nonexistent/composer.json")

	if err == nil {
		t.Error("LoadComposer() expected error for nonexistent file, got nil")
		return
	}

	if !isErrorType(err, ErrComposerNotFound) {
		t.Errorf("LoadComposer() expected ErrComposerNotFound, got %v", err)
	}
}

func TestDefaultComposerLoader_ValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *ComposerConfig
		expectError bool
		errorType   error
	}{
		{
			name: "valid config with PSR-4",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid config with dev PSR-4",
			config: &ComposerConfig{
				AutoloadDev: AutoloadSection{
					PSR4: map[string]any{
						"Tests\\": "tests/",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid config with multiple namespaces",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\":               "app/",
						"Database\\Seeders\\": "database/seeders/",
					},
				},
				AutoloadDev: AutoloadSection{
					PSR4: map[string]any{
						"Tests\\": "tests/",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid config with array paths",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"Multi\\": []string{"src/Multi", "lib/Multi"},
					},
				},
			},
			expectError: false,
		},
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
		},
		{
			name: "config without PSR-4",
			config: &ComposerConfig{
				Name: "test/package",
				Autoload: AutoloadSection{
					Classmap: []string{"legacy/"},
				},
			},
			expectError: true,
			errorType:   ErrMissingPSR4,
		},
		{
			name: "invalid namespace without backslash",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App": "app/", // Missing trailing backslash
					},
				},
			},
			expectError: true,
			errorType:   ErrInvalidNamespace,
		},
		{
			name: "empty namespace",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"": "app/", // Empty namespace (valid for PSR-4 root)
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid path type",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": 123, // Invalid type
					},
				},
			},
			expectError: true,
			errorType:   ErrInvalidNamespace,
		},
		{
			name: "empty path string",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "", // Empty path
					},
				},
			},
			expectError: true,
			errorType:   ErrInvalidNamespace,
		},
		{
			name: "empty paths array",
			config: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": []string{}, // Empty paths array
					},
				},
			},
			expectError: true,
			errorType:   ErrInvalidNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewComposerLoader()
			err := loader.ValidateConfig(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("ValidateConfig() expected error, got nil")
					return
				}
				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("ValidateConfig() expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateConfig() unexpected error: %v", err)
			}
		})
	}
}

func TestLoadComposerFromReader(t *testing.T) {
	tests := []struct {
		name           string
		composerJSON   string
		expectError    bool
		expectedConfig *ComposerConfig
	}{
		{
			name: "valid composer from reader",
			composerJSON: `{
				"name": "test/package",
				"autoload": {
					"psr-4": {
						"App\\": "src/"
					}
				}
			}`,
			expectError: false,
			expectedConfig: &ComposerConfig{
				Name: "test/package",
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "src/",
					},
				},
			},
		},
		{
			name:         "invalid JSON from reader",
			composerJSON: `{"invalid": json}`,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.composerJSON)
			config, err := LoadComposerFromReader(reader)

			if tt.expectError {
				if err == nil {
					t.Error("LoadComposerFromReader() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("LoadComposerFromReader() unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(config, tt.expectedConfig) {
				t.Errorf("LoadComposerFromReader() config mismatch.\nExpected: %+v\nGot: %+v", tt.expectedConfig, config)
			}
		})
	}
}

func TestFindComposerFile(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create nested directories
	subDir := filepath.Join(tmpDir, "app", "Http", "Controllers")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Create composer.json in root
	composerPath := filepath.Join(tmpDir, "composer.json")
	err = os.WriteFile(composerPath, []byte(`{"name": "test/app"}`), 0644)
	if err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	tests := []struct {
		name        string
		startDir    string
		expectError bool
	}{
		{
			name:        "find from root directory",
			startDir:    tmpDir,
			expectError: false,
		},
		{
			name:        "find from nested directory",
			startDir:    subDir,
			expectError: false,
		},
		{
			name:        "not found in non-existent directory",
			startDir:    "/this/path/does/not/exist",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			foundPath, err := FindComposerFile(tt.startDir)

			if tt.expectError {
				if err == nil {
					t.Error("FindComposerFile() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("FindComposerFile() unexpected error: %v", err)
				return
			}

			expectedPath, _ := filepath.Abs(composerPath)
			foundPathAbs, _ := filepath.Abs(foundPath)

			if foundPathAbs != expectedPath {
				t.Errorf("FindComposerFile() expected %s, got %s", expectedPath, foundPathAbs)
			}
		})
	}
}

func TestMustLoadComposer(t *testing.T) {
	// Test successful load
	t.Run("successful load", func(t *testing.T) {
		tmpFile := createTempComposerFile(t, `{
			"name": "test/package",
			"autoload": {
				"psr-4": {
					"App\\": "src/"
				}
			}
		}`)
		defer os.Remove(tmpFile)

		config := MustLoadComposer(tmpFile)
		if config == nil {
			t.Error("MustLoadComposer() returned nil")
			return
		}
		if config.Name != "test/package" {
			t.Errorf("MustLoadComposer() expected name 'test/package', got '%s'", config.Name)
		}
	})

	// Test panic on invalid file (this would normally panic, so we test the logic instead)
	t.Run("would panic on invalid file", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustLoadComposer() expected panic for nonexistent file")
			}
		}()

		MustLoadComposer("/nonexistent/composer.json")
	})
}

// Helper functions

func createTempComposerFile(t *testing.T, content string) string {
	tmpFile, err := os.CreateTemp("", "composer-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	return tmpFile.Name()
}

func isErrorType(err error, target error) bool {
	if composerErr, ok := err.(*ComposerError); ok {
		return composerErr.Cause != nil &&
			(composerErr.Cause == target || strings.Contains(composerErr.Cause.Error(), target.Error()))
	}
	if mappingErr, ok := err.(*MappingError); ok {
		return mappingErr.Cause != nil &&
			(mappingErr.Cause == target || strings.Contains(mappingErr.Cause.Error(), target.Error()))
	}
	return err == target || strings.Contains(err.Error(), target.Error())
}

// Benchmark tests for performance requirements

func BenchmarkLoadComposer(b *testing.B) {
	// Create a realistic Laravel composer.json
	composerJSON := `{
		"name": "laravel/laravel",
		"type": "project",
		"description": "The Laravel Framework.",
		"keywords": ["framework", "laravel"],
		"license": "MIT",
		"require": {
			"php": "^8.0.2",
			"guzzlehttp/guzzle": "^7.2",
			"laravel/framework": "^9.0",
			"laravel/sanctum": "^3.0",
			"laravel/tinker": "^2.7"
		},
		"require-dev": {
			"fakerphp/faker": "^1.9.1",
			"laravel/pint": "^1.0",
			"laravel/sail": "^1.0.1",
			"mockery/mockery": "^1.4.4",
			"nunomaduro/collision": "^6.0",
			"phpunit/phpunit": "^9.5.10",
			"spatie/laravel-ignition": "^1.0"
		},
		"autoload": {
			"psr-4": {
				"App\\": "app/",
				"Database\\Factories\\": "database/factories/",
				"Database\\Seeders\\": "database/seeders/"
			}
		},
		"autoload-dev": {
			"psr-4": {
				"Tests\\": "tests/"
			}
		},
		"scripts": {
			"post-autoload-dump": [
				"Illuminate\\Foundation\\ComposerScripts::postAutoloadDump",
				"@php artisan package:discover --ansi"
			],
			"post-update-cmd": [
				"@php artisan vendor:publish --tag=laravel-assets --ansi --force"
			]
		},
		"config": {
			"optimize-autoloader": true,
			"preferred-install": "dist",
			"sort-packages": true,
			"allow-plugins": {
				"pestphp/pest-plugin": true
			}
		},
		"minimum-stability": "stable",
		"prefer-stable": true
	}`

	tmpFile, err := os.CreateTemp("", "composer-bench-*.json")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(composerJSON); err != nil {
		b.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	loader := NewComposerLoader()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config, err := loader.LoadComposer(tmpFile.Name())
		if err != nil {
			b.Fatalf("LoadComposer() failed: %v", err)
		}
		if err := loader.ValidateConfig(config); err != nil {
			b.Fatalf("ValidateConfig() failed: %v", err)
		}
	}
}

func BenchmarkValidateConfig(b *testing.B) {
	config := &ComposerConfig{
		Name: "laravel/laravel",
		Autoload: AutoloadSection{
			PSR4: map[string]any{
				"App\\":                 "app/",
				"Database\\Factories\\": "database/factories/",
				"Database\\Seeders\\":   "database/seeders/",
			},
		},
		AutoloadDev: AutoloadSection{
			PSR4: map[string]any{
				"Tests\\": "tests/",
			},
		},
	}

	loader := NewComposerLoader()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := loader.ValidateConfig(config)
		if err != nil {
			b.Fatalf("ValidateConfig() failed: %v", err)
		}
	}
}

// Test performance requirement: <1ms parse time
func TestComposerParsePerformance(t *testing.T) {
	composerJSON := `{
		"name": "laravel/laravel",
		"autoload": {
			"psr-4": {
				"App\\": "app/",
				"Database\\Seeders\\": "database/seeders/"
			}
		},
		"autoload-dev": {
			"psr-4": {
				"Tests\\": "tests/"
			}
		}
	}`

	tmpFile := createTempComposerFile(t, composerJSON)
	defer os.Remove(tmpFile)

	loader := NewComposerLoader()

	// Warm up
	_, _ = loader.LoadComposer(tmpFile)

	// Measure performance
	start := time.Now()
	for i := 0; i < 100; i++ {
		_, err := loader.LoadComposer(tmpFile)
		if err != nil {
			t.Fatalf("LoadComposer() failed: %v", err)
		}
	}
	duration := time.Since(start)

	avgDuration := duration / 100
	maxDuration := 1 * time.Millisecond

	if avgDuration > maxDuration {
		t.Errorf("Average parse time %v exceeds requirement of %v", avgDuration, maxDuration)
	}

	t.Logf("Average parse time: %v (requirement: <%v)", avgDuration, maxDuration)
}
