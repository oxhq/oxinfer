package psr4

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestDefaultClassMapper_MapClass(t *testing.T) {
	tests := []struct {
		name         string
		composerData *ComposerData
		includeDev   bool
		fqcn         string
		expected     []string
		expectError  bool
		errorType    error
	}{
		{
			name: "basic Laravel App namespace",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "App\\Http\\Controllers\\UserController",
			expected:    []string{"app/Http/Controllers/UserController.php"},
			expectError: false,
		},
		{
			name: "multiple path mappings",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": []string{"app/", "src/"},
					},
				},
			},
			includeDev:  false,
			fqcn:        "App\\Models\\User",
			expected:    []string{"app/Models/User.php", "src/Models/User.php"},
			expectError: false,
		},
		{
			name: "Laravel standard mappings",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\":               "app/",
						"Database\\Seeders\\": "database/seeders/",
					},
				},
				AutoloadDev: PSR4Config{
					PSR4: map[string]any{
						"Tests\\": "tests/",
					},
				},
			},
			includeDev:  true,
			fqcn:        "Tests\\Feature\\UserTest",
			expected:    []string{"tests/Feature/UserTest.php"},
			expectError: false,
		},
		{
			name: "exclude dev dependencies",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
				AutoloadDev: PSR4Config{
					PSR4: map[string]any{
						"Tests\\": "tests/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "Tests\\Feature\\UserTest",
			expectError: true,
			errorType:   ErrClassNotMappable,
		},
		{
			name: "longest namespace prefix wins",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\":                    "app/",
						"App\\Http\\":              "app/Http/",
						"App\\Http\\Controllers\\": "app/Http/Controllers/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "App\\Http\\Controllers\\Api\\UserController",
			expected:    []string{"app/Http/Controllers/Api/UserController.php"},
			expectError: false,
		},
		{
			name: "root namespace mapping",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"": "src/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "MyClass",
			expected:    []string{"src/MyClass.php"},
			expectError: false,
		},
		{
			name: "invalid FQCN with forward slashes",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "App/Http/Controllers/UserController",
			expectError: true,
			errorType:   ErrInvalidFQCN,
		},
		{
			name: "empty FQCN",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "",
			expectError: true,
			errorType:   ErrInvalidFQCN,
		},
		{
			name: "unmappable class",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "Vendor\\Package\\SomeClass",
			expectError: true,
			errorType:   ErrClassNotMappable,
		},
		{
			name: "single character namespace parts",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"A\\B\\": "src/",
					},
				},
			},
			includeDev:  false,
			fqcn:        "A\\B\\C",
			expected:    []string{"src/C.php"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper, err := NewClassMapper(tt.composerData, tt.includeDev)
			if err != nil {
				t.Fatalf("NewClassMapper() failed: %v", err)
			}

			result, err := mapper.MapClass(tt.fqcn)

			if tt.expectError {
				if err == nil {
					t.Errorf("MapClass() expected error, got nil")
					return
				}
				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("MapClass() expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("MapClass() unexpected error: %v", err)
				return
			}

			// Sort both slices for comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("MapClass() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDefaultClassMapper_GetNamespaces(t *testing.T) {
	tests := []struct {
		name         string
		composerData *ComposerData
		includeDev   bool
		expected     []string
	}{
		{
			name: "production namespaces only",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\":               "app/",
						"Database\\Seeders\\": "database/seeders/",
					},
				},
				AutoloadDev: PSR4Config{
					PSR4: map[string]any{
						"Tests\\": "tests/",
					},
				},
			},
			includeDev: false,
			expected:   []string{"App\\", "Database\\Seeders\\"},
		},
		{
			name: "include dev namespaces",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
				AutoloadDev: PSR4Config{
					PSR4: map[string]any{
						"Tests\\": "tests/",
					},
				},
			},
			includeDev: true,
			expected:   []string{"App\\", "Tests\\"},
		},
		{
			name: "empty configuration",
			composerData: &ComposerData{
				Autoload:    PSR4Config{},
				AutoloadDev: PSR4Config{},
			},
			includeDev: true,
			expected:   []string{},
		},
		{
			name: "root namespace",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"": "src/",
					},
				},
			},
			includeDev: false,
			expected:   []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper, err := NewClassMapper(tt.composerData, tt.includeDev)
			if err != nil {
				t.Fatalf("NewClassMapper() failed: %v", err)
			}

			result := mapper.GetNamespaces()

			// Sort both slices for comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GetNamespaces() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNewClassMapper(t *testing.T) {
	tests := []struct {
		name         string
		composerData *ComposerData
		includeDev   bool
		expectError  bool
	}{
		{
			name: "valid composer data",
			composerData: &ComposerData{
				Autoload: PSR4Config{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			includeDev:  false,
			expectError: false,
		},
		{
			name:         "nil composer data",
			composerData: nil,
			includeDev:   false,
			expectError:  true,
		},
		{
			name:         "empty composer data",
			composerData: &ComposerData{},
			includeDev:   false,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper, err := NewClassMapper(tt.composerData, tt.includeDev)

			if tt.expectError {
				if err == nil {
					t.Error("NewClassMapper() expected error, got nil")
				}
				if mapper != nil {
					t.Error("NewClassMapper() expected nil mapper on error")
				}
				return
			}

			if err != nil {
				t.Errorf("NewClassMapper() unexpected error: %v", err)
				return
			}

			if mapper == nil {
				t.Error("NewClassMapper() returned nil mapper")
			}
		})
	}
}

func TestStaticClassMapper_MapClassToFile(t *testing.T) {
	tests := []struct {
		name           string
		fqcn           string
		composerConfig *ComposerConfig
		expected       string
		expectError    bool
		errorType      error
	}{
		{
			name: "basic Laravel mapping",
			fqcn: "App\\Http\\Controllers\\UserController",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expected:    "app/Http/Controllers/UserController.php",
			expectError: false,
		},
		{
			name: "database seeder mapping",
			fqcn: "Database\\Seeders\\UserSeeder",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"Database\\Seeders\\": "database/seeders/",
					},
				},
			},
			expected:    "database/seeders/UserSeeder.php",
			expectError: false,
		},
		{
			name:           "nil composer config",
			fqcn:           "App\\Models\\User",
			composerConfig: nil,
			expectError:    true,
			errorType:      ErrInvalidFQCN,
		},
		{
			name: "empty FQCN",
			fqcn: "",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrInvalidFQCN,
		},
		{
			name: "unmappable class",
			fqcn: "Vendor\\Package\\SomeClass",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrClassNotMappable,
		},
	}

	staticMapper := StaticClassMapper{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := staticMapper.MapClassToFile(tt.fqcn, tt.composerConfig)

			if tt.expectError {
				if err == nil {
					t.Errorf("MapClassToFile() expected error, got nil")
					return
				}
				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("MapClassToFile() expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("MapClassToFile() unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("MapClassToFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStaticClassMapper_MapFileToClass(t *testing.T) {
	tests := []struct {
		name           string
		filePath       string
		composerConfig *ComposerConfig
		expected       string
		expectError    bool
		errorType      error
	}{
		{
			name:     "basic Laravel controller",
			filePath: "app/Http/Controllers/UserController.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expected:    "App\\Http\\Controllers\\UserController",
			expectError: false,
		},
		{
			name:     "model mapping",
			filePath: "app/Models/User.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expected:    "App\\Models\\User",
			expectError: false,
		},
		{
			name:     "database seeder",
			filePath: "database/seeders/UserSeeder.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"Database\\Seeders\\": "database/seeders/",
					},
				},
			},
			expected:    "Database\\Seeders\\UserSeeder",
			expectError: false,
		},
		{
			name:     "file path without extension",
			filePath: "app/Models/User",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expected:    "App\\Models\\User",
			expectError: false,
		},
		{
			name:           "nil composer config",
			filePath:       "app/Models/User.php",
			composerConfig: nil,
			expectError:    true,
			errorType:      ErrFileNotMappable,
		},
		{
			name:     "empty file path",
			filePath: "",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrFileNotMappable,
		},
		{
			name:     "unmappable file",
			filePath: "vendor/package/src/SomeClass.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrFileNotMappable,
		},
	}

	staticMapper := StaticClassMapper{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := staticMapper.MapFileToClass(tt.filePath, tt.composerConfig)

			if tt.expectError {
				if err == nil {
					t.Errorf("MapFileToClass() expected error, got nil")
					return
				}
				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("MapFileToClass() expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("MapFileToClass() unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("MapFileToClass() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStaticClassMapper_GetNamespaceForPath(t *testing.T) {
	tests := []struct {
		name           string
		filePath       string
		composerConfig *ComposerConfig
		expected       string
		expectError    bool
		errorType      error
	}{
		{
			name:     "controller namespace",
			filePath: "app/Http/Controllers/UserController.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expected:    "App\\Http\\Controllers",
			expectError: false,
		},
		{
			name:     "model namespace",
			filePath: "app/Models/User.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expected:    "App\\Models",
			expectError: false,
		},
		{
			name:     "root namespace file",
			filePath: "app/SomeClass.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expected:    "App",
			expectError: false,
		},
		{
			name:           "nil composer config",
			filePath:       "app/Models/User.php",
			composerConfig: nil,
			expectError:    true,
			errorType:      ErrFileNotMappable,
		},
		{
			name:     "empty file path",
			filePath: "",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrFileNotMappable,
		},
		{
			name:     "unmappable file path",
			filePath: "vendor/package/src/SomeClass.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrFileNotMappable,
		},
	}

	staticMapper := StaticClassMapper{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := staticMapper.GetNamespaceForPath(tt.filePath, tt.composerConfig)

			if tt.expectError {
				if err == nil {
					t.Errorf("GetNamespaceForPath() expected error, got nil")
					return
				}
				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("GetNamespaceForPath() expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("GetNamespaceForPath() unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("GetNamespaceForPath() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStaticClassMapper_ValidateClassMapping(t *testing.T) {
	tests := []struct {
		name           string
		fqcn           string
		filePath       string
		composerConfig *ComposerConfig
		expectError    bool
		errorType      error
	}{
		{
			name:     "valid mapping",
			fqcn:     "App\\Http\\Controllers\\UserController",
			filePath: "app/Http/Controllers/UserController.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: false,
		},
		{
			name:     "invalid mapping - wrong path",
			fqcn:     "App\\Http\\Controllers\\UserController",
			filePath: "app/Controllers/UserController.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrInvalidFQCN,
		},
		{
			name:           "nil composer config",
			fqcn:           "App\\Models\\User",
			filePath:       "app/Models/User.php",
			composerConfig: nil,
			expectError:    true,
			errorType:      ErrInvalidFQCN,
		},
		{
			name:     "invalid FQCN",
			fqcn:     "",
			filePath: "app/Models/User.php",
			composerConfig: &ComposerConfig{
				Autoload: AutoloadSection{
					PSR4: map[string]any{
						"App\\": "app/",
					},
				},
			},
			expectError: true,
			errorType:   ErrInvalidFQCN,
		},
	}

	staticMapper := StaticClassMapper{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := staticMapper.ValidateClassMapping(tt.fqcn, tt.filePath, tt.composerConfig)

			if tt.expectError {
				if err == nil {
					t.Errorf("ValidateClassMapping() expected error, got nil")
					return
				}
				if tt.errorType != nil && !isErrorType(err, tt.errorType) {
					t.Errorf("ValidateClassMapping() expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateClassMapping() unexpected error: %v", err)
			}
		})
	}
}

func TestBidirectionalMapping(t *testing.T) {
	// Test that class-to-file-to-class mapping is consistent
	composerConfig := &ComposerConfig{
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
	}

	testCases := []struct {
		originalFQCN string
		expectedFile string
	}{
		{"App\\Http\\Controllers\\UserController", "app/Http/Controllers/UserController.php"},
		{"App\\Models\\User", "app/Models/User.php"},
		{"Database\\Seeders\\UserSeeder", "database/seeders/UserSeeder.php"},
	}

	staticMapper := StaticClassMapper{}

	for _, tc := range testCases {
		t.Run("bidirectional_"+tc.originalFQCN, func(t *testing.T) {
			// Map class to file
			filePath, err := staticMapper.MapClassToFile(tc.originalFQCN, composerConfig)
			if err != nil {
				t.Fatalf("MapClassToFile() failed: %v", err)
			}

			if filePath != tc.expectedFile {
				t.Errorf("MapClassToFile() = %v, want %v", filePath, tc.expectedFile)
			}

			// Map file back to class
			fqcn, err := staticMapper.MapFileToClass(filePath, composerConfig)
			if err != nil {
				t.Fatalf("MapFileToClass() failed: %v", err)
			}

			if fqcn != tc.originalFQCN {
				t.Errorf("Bidirectional mapping failed: %v -> %v -> %v", tc.originalFQCN, filePath, fqcn)
			}
		})
	}
}

// Benchmark tests for performance requirements

func BenchmarkClassMapper_MapClass(b *testing.B) {
	composerData := &ComposerData{
		Autoload: PSR4Config{
			PSR4: map[string]any{
				"App\\":                 "app/",
				"Database\\Factories\\": "database/factories/",
				"Database\\Seeders\\":   "database/seeders/",
			},
		},
		AutoloadDev: PSR4Config{
			PSR4: map[string]any{
				"Tests\\": "tests/",
			},
		},
	}

	mapper, err := NewClassMapper(composerData, true)
	if err != nil {
		b.Fatalf("NewClassMapper() failed: %v", err)
	}

	testFQCNs := []string{
		"App\\Http\\Controllers\\UserController",
		"App\\Models\\User",
		"Database\\Seeders\\UserSeeder",
		"Tests\\Feature\\UserTest",
		"App\\Http\\Middleware\\AuthMiddleware",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fqcn := testFQCNs[i%len(testFQCNs)]
		_, err := mapper.MapClass(fqcn)
		if err != nil {
			b.Fatalf("MapClass() failed: %v", err)
		}
	}
}

func BenchmarkClassMapper_GetNamespaces(b *testing.B) {
	composerData := &ComposerData{
		Autoload: PSR4Config{
			PSR4: map[string]any{
				"App\\":                 "app/",
				"Database\\Factories\\": "database/factories/",
				"Database\\Seeders\\":   "database/seeders/",
			},
		},
		AutoloadDev: PSR4Config{
			PSR4: map[string]any{
				"Tests\\": "tests/",
			},
		},
	}

	mapper, err := NewClassMapper(composerData, true)
	if err != nil {
		b.Fatalf("NewClassMapper() failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mapper.GetNamespaces()
	}
}

func BenchmarkStaticClassMapper_MapClassToFile(b *testing.B) {
	composerConfig := &ComposerConfig{
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
	}

	staticMapper := StaticClassMapper{}
	testFQCNs := []string{
		"App\\Http\\Controllers\\UserController",
		"App\\Models\\User",
		"Database\\Seeders\\UserSeeder",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fqcn := testFQCNs[i%len(testFQCNs)]
		_, err := staticMapper.MapClassToFile(fqcn, composerConfig)
		if err != nil {
			b.Fatalf("MapClassToFile() failed: %v", err)
		}
	}
}

func BenchmarkStaticClassMapper_MapFileToClass(b *testing.B) {
	composerConfig := &ComposerConfig{
		Autoload: AutoloadSection{
			PSR4: map[string]any{
				"App\\":               "app/",
				"Database\\Seeders\\": "database/seeders/",
			},
		},
	}

	staticMapper := StaticClassMapper{}
	testFiles := []string{
		"app/Http/Controllers/UserController.php",
		"app/Models/User.php",
		"database/seeders/UserSeeder.php",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filePath := testFiles[i%len(testFiles)]
		_, err := staticMapper.MapFileToClass(filePath, composerConfig)
		if err != nil {
			b.Fatalf("MapFileToClass() failed: %v", err)
		}
	}
}

func BenchmarkFindLongestMatchingPrefix(b *testing.B) {
	namespaces := []string{
		"App\\",
		"App\\Http\\",
		"App\\Http\\Controllers\\",
		"Database\\Seeders\\",
		"Tests\\",
	}

	testFQCNs := []string{
		"App\\Http\\Controllers\\UserController",
		"App\\Models\\User",
		"Database\\Seeders\\UserSeeder",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fqcn := testFQCNs[i%len(testFQCNs)]
		_, _ = FindLongestMatchingPrefix(fqcn, namespaces)
	}
}

func BenchmarkValidateFQCNFormat(b *testing.B) {
	testFQCNs := []string{
		"App\\Http\\Controllers\\UserController",
		"App\\Models\\User",
		"Database\\Seeders\\UserSeeder",
		"Tests\\Feature\\UserTest",
		"Single",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fqcn := testFQCNs[i%len(testFQCNs)]
		_ = ValidateFQCNFormat(fqcn)
	}
}

// Test performance requirement: <100µs per mapping operation
func TestMappingPerformance(t *testing.T) {
	composerData := &ComposerData{
		Autoload: PSR4Config{
			PSR4: map[string]any{
				"App\\":               "app/",
				"Database\\Seeders\\": "database/seeders/",
			},
		},
	}

	mapper, err := NewClassMapper(composerData, false)
	if err != nil {
		t.Fatalf("NewClassMapper() failed: %v", err)
	}

	// Warm up
	_, _ = mapper.MapClass("App\\Http\\Controllers\\UserController")

	// Measure performance for multiple operations
	operations := []func(){
		func() { mapper.MapClass("App\\Http\\Controllers\\UserController") },
		func() { mapper.MapClass("App\\Models\\User") },
		func() { mapper.GetNamespaces() },
	}

	for i, op := range operations {
		start := time.Now()
		iterations := 1000
		for j := 0; j < iterations; j++ {
			op()
		}
		duration := time.Since(start)
		avgDuration := duration / time.Duration(iterations)
		maxDuration := 100 * time.Microsecond

		if avgDuration > maxDuration {
			t.Errorf("Operation %d average time %v exceeds requirement of %v", i, avgDuration, maxDuration)
		}

		t.Logf("Operation %d average time: %v (requirement: <%v)", i, avgDuration, maxDuration)
	}
}

// Test deterministic output behavior
func TestDeterministicMapping(t *testing.T) {
	composerData := &ComposerData{
		Autoload: PSR4Config{
			PSR4: map[string]any{
				"Multi\\": []string{"src/Multi", "lib/Multi", "app/Multi"},
			},
		},
	}

	mapper, err := NewClassMapper(composerData, false)
	if err != nil {
		t.Fatalf("NewClassMapper() failed: %v", err)
	}

	// Test that multiple calls return the same ordered results
	fqcn := "Multi\\Package\\SomeClass"

	var firstResult, secondResult []string

	firstResult, err = mapper.MapClass(fqcn)
	if err != nil {
		t.Fatalf("MapClass() failed: %v", err)
	}

	secondResult, err = mapper.MapClass(fqcn)
	if err != nil {
		t.Fatalf("MapClass() failed: %v", err)
	}

	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Errorf("Mapping is not deterministic: %v vs %v", firstResult, secondResult)
	}

	// Verify results are sorted
	expectedSorted := make([]string, len(firstResult))
	copy(expectedSorted, firstResult)
	sort.Strings(expectedSorted)

	if !reflect.DeepEqual(firstResult, expectedSorted) {
		t.Errorf("Results are not sorted: %v", firstResult)
	}
}
