package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		expected  *CLIConfig
		wantErr   bool
		errorType string
	}{
		{
			name: "default flags",
			args: []string{},
			expected: &CLIConfig{
				ManifestPath: "-",
				OutputPath:   "",
				NoColor:      false,
				Version:      false,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "manifest file flag",
			args: []string{"--manifest", "test.json"},
			expected: &CLIConfig{
				ManifestPath: "test.json",
				RequestPath:  "",
				OutputPath:   "",
				NoColor:      false,
				Version:      false,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "request file flag",
			args: []string{"--request", "analysis-request.json"},
			expected: &CLIConfig{
				ManifestPath: "",
				RequestPath:  "analysis-request.json",
				OutputPath:   "",
				NoColor:      false,
				Version:      false,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "output file flag",
			args: []string{"--out", "output.json"},
			expected: &CLIConfig{
				ManifestPath: "-",
				RequestPath:  "",
				OutputPath:   "output.json",
				NoColor:      false,
				Version:      false,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "no-color flag",
			args: []string{"--no-color"},
			expected: &CLIConfig{
				ManifestPath: "-",
				RequestPath:  "",
				OutputPath:   "",
				NoColor:      true,
				Version:      false,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "version flag",
			args: []string{"--version"},
			expected: &CLIConfig{
				ManifestPath: "-",
				RequestPath:  "",
				OutputPath:   "",
				NoColor:      false,
				Version:      true,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "help flag",
			args: []string{"--help"},
			expected: &CLIConfig{
				ManifestPath: "-",
				RequestPath:  "",
				OutputPath:   "",
				NoColor:      false,
				Version:      false,
				Help:         true,
			},
			wantErr: false,
		},
		{
			name: "help flag short form",
			args: []string{"-h"},
			expected: &CLIConfig{
				ManifestPath: "-",
				RequestPath:  "",
				OutputPath:   "",
				NoColor:      false,
				Version:      false,
				Help:         true,
			},
			wantErr: false,
		},
		{
			name: "all flags combined",
			args: []string{"--manifest", "test.json", "--out", "output.json", "--no-color", "--version"},
			expected: &CLIConfig{
				ManifestPath: "test.json",
				RequestPath:  "",
				OutputPath:   "output.json",
				NoColor:      true,
				Version:      true,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name:      "invalid flag",
			args:      []string{"--invalid"},
			expected:  nil,
			wantErr:   true,
			errorType: "input",
		},
		{
			name:      "flag without value",
			args:      []string{"--manifest"},
			expected:  nil,
			wantErr:   true,
			errorType: "input",
		},
		{
			name:      "mutually exclusive input flags",
			args:      []string{"--manifest", "manifest.json", "--request", "analysis-request.json"},
			expected:  nil,
			wantErr:   true,
			errorType: "input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseFlags(tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseFlags() error = nil, wantErr %v", tt.wantErr)
					return
				}

				// Check error type
				if cliErr, ok := err.(*CLIError); ok {
					if tt.errorType == "input" && cliErr.ExitCode != ExitInputError {
						t.Errorf("ParseFlags() error type = %v, want %v", cliErr.ExitCode, ExitInputError)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("ParseFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if config == nil {
				t.Errorf("ParseFlags() config = nil")
				return
			}

			// Compare all fields
			if config.ManifestPath != tt.expected.ManifestPath {
				t.Errorf("ParseFlags() ManifestPath = %v, want %v", config.ManifestPath, tt.expected.ManifestPath)
			}
			if config.OutputPath != tt.expected.OutputPath {
				t.Errorf("ParseFlags() OutputPath = %v, want %v", config.OutputPath, tt.expected.OutputPath)
			}
			if config.RequestPath != tt.expected.RequestPath {
				t.Errorf("ParseFlags() RequestPath = %v, want %v", config.RequestPath, tt.expected.RequestPath)
			}
			if config.NoColor != tt.expected.NoColor {
				t.Errorf("ParseFlags() NoColor = %v, want %v", config.NoColor, tt.expected.NoColor)
			}
			if config.Version != tt.expected.Version {
				t.Errorf("ParseFlags() Version = %v, want %v", config.Version, tt.expected.Version)
			}
			if config.Help != tt.expected.Help {
				t.Errorf("ParseFlags() Help = %v, want %v", config.Help, tt.expected.Help)
			}
		})
	}
}

func TestCLIConfig_BooleanMethods(t *testing.T) {
	tests := []struct {
		name   string
		config *CLIConfig
		method string
		want   bool
	}{
		{
			name:   "IsStdinInput with empty manifest path",
			config: &CLIConfig{ManifestPath: ""},
			method: "IsStdinInput",
			want:   true,
		},
		{
			name:   "IsStdinInput with manifest path",
			config: &CLIConfig{ManifestPath: "test.json"},
			method: "IsStdinInput",
			want:   false,
		},
		{
			name:   "IsStdoutOutput with empty output path",
			config: &CLIConfig{OutputPath: ""},
			method: "IsStdoutOutput",
			want:   true,
		},
		{
			name:   "IsStdoutOutput with output path",
			config: &CLIConfig{OutputPath: "output.json"},
			method: "IsStdoutOutput",
			want:   false,
		},
		{
			name:   "ShouldShowHelp true",
			config: &CLIConfig{Help: true},
			method: "ShouldShowHelp",
			want:   true,
		},
		{
			name:   "ShouldShowHelp false",
			config: &CLIConfig{Help: false},
			method: "ShouldShowHelp",
			want:   false,
		},
		{
			name:   "ShouldShowVersion true",
			config: &CLIConfig{Version: true},
			method: "ShouldShowVersion",
			want:   true,
		},
		{
			name:   "ShouldShowVersion false",
			config: &CLIConfig{Version: false},
			method: "ShouldShowVersion",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			switch tt.method {
			case "IsStdinInput":
				got = tt.config.IsStdinInput()
			case "IsStdoutOutput":
				got = tt.config.IsStdoutOutput()
			case "ShouldShowHelp":
				got = tt.config.ShouldShowHelp()
			case "ShouldShowVersion":
				got = tt.config.ShouldShowVersion()
			default:
				t.Fatalf("unknown method: %s", tt.method)
			}

			if got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestCLIConfig_GetManifestReader(t *testing.T) {
	// Create a temporary file for testing
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test.json")
	err := os.WriteFile(tempFile, []byte(`{"test": "data"}`), 0644)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	tests := []struct {
		name      string
		config    *CLIConfig
		wantStdin bool
		wantErr   bool
		errorType ExitCode
	}{
		{
			name:      "stdin input",
			config:    &CLIConfig{ManifestPath: ""},
			wantStdin: true,
			wantErr:   false,
		},
		{
			name:      "valid file",
			config:    &CLIConfig{ManifestPath: tempFile},
			wantStdin: false,
			wantErr:   false,
		},
		{
			name:      "nonexistent file",
			config:    &CLIConfig{ManifestPath: "/nonexistent/file.json"},
			wantStdin: false,
			wantErr:   true,
			errorType: ExitInputError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := tt.config.GetManifestReader()

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetManifestReader() error = nil, wantErr %v", tt.wantErr)
					return
				}

				if cliErr, ok := err.(*CLIError); ok {
					if cliErr.ExitCode != tt.errorType {
						t.Errorf("GetManifestReader() error type = %v, want %v", cliErr.ExitCode, tt.errorType)
					}
				} else {
					t.Errorf("GetManifestReader() error should be CLIError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("GetManifestReader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if reader == nil {
				t.Errorf("GetManifestReader() reader = nil")
				return
			}

			if tt.wantStdin {
				if reader != os.Stdin {
					t.Errorf("GetManifestReader() expected stdin, got different reader")
				}
			} else {
				if reader == os.Stdin {
					t.Errorf("GetManifestReader() expected file reader, got stdin")
				}
				// Close the file reader if it's not stdin
				defer reader.Close()
			}
		})
	}
}

func TestCLIConfig_GetOutputWriter(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "output.json")

	tests := []struct {
		name       string
		config     *CLIConfig
		wantStdout bool
		wantErr    bool
		errorType  ExitCode
	}{
		{
			name:       "stdout output",
			config:     &CLIConfig{OutputPath: ""},
			wantStdout: true,
			wantErr:    false,
		},
		{
			name:       "valid file output",
			config:     &CLIConfig{OutputPath: tempFile},
			wantStdout: false,
			wantErr:    false,
		},
		{
			name:       "invalid directory",
			config:     &CLIConfig{OutputPath: "/nonexistent/directory/output.json"},
			wantStdout: false,
			wantErr:    true,
			errorType:  ExitInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer, err := tt.config.GetOutputWriter()

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetOutputWriter() error = nil, wantErr %v", tt.wantErr)
					return
				}

				if cliErr, ok := err.(*CLIError); ok {
					if cliErr.ExitCode != tt.errorType {
						t.Errorf("GetOutputWriter() error type = %v, want %v", cliErr.ExitCode, tt.errorType)
					}
				} else {
					t.Errorf("GetOutputWriter() error should be CLIError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("GetOutputWriter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if writer == nil {
				t.Errorf("GetOutputWriter() writer = nil")
				return
			}

			if tt.wantStdout {
				if writer != os.Stdout {
					t.Errorf("GetOutputWriter() expected stdout, got different writer")
				}
			} else {
				if writer == os.Stdout {
					t.Errorf("GetOutputWriter() expected file writer, got stdout")
				}
				// Close the file writer if it's not stdout
				defer writer.Close()

				// Verify file was created
				if _, err := os.Stat(tempFile); os.IsNotExist(err) {
					t.Errorf("GetOutputWriter() should have created file %s", tempFile)
				}
			}
		})
	}
}

func TestCLIConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *CLIConfig
		wantErr bool
	}{
		{
			name: "valid config - default",
			config: &CLIConfig{
				ManifestPath: "-",
				OutputPath:   "",
				NoColor:      false,
				Version:      false,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "valid config - with files",
			config: &CLIConfig{
				ManifestPath: "test.json",
				OutputPath:   "output.json",
				NoColor:      true,
				Version:      false,
				Help:         false,
			},
			wantErr: false,
		},
		{
			name: "valid config - flags",
			config: &CLIConfig{
				ManifestPath: "",
				OutputPath:   "",
				NoColor:      false,
				Version:      true,
				Help:         true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("CLIConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseFlagsUsage tests that the usage function is properly set and callable
func TestParseFlagsUsage(t *testing.T) {
	// This test captures the usage output to ensure it contains expected content
	config, err := ParseFlags([]string{"--help"})
	if err != nil {
		t.Errorf("ParseFlags() with --help should not error, got: %v", err)
		return
	}

	if !config.ShouldShowHelp() {
		t.Error("ParseFlags() with --help should set Help to true")
	}
}

// TestParseFlagsArgValidation tests edge cases in argument parsing
func TestParseFlagsArgValidation(t *testing.T) {
	// Test empty arguments
	config, err := ParseFlags([]string{})
	if err != nil {
		t.Errorf("ParseFlags() with empty args should not error, got: %v", err)
	}
	if config == nil {
		t.Error("ParseFlags() with empty args should return valid config")
	}

	// Test nil arguments
	config, err = ParseFlags(nil)
	if err != nil {
		t.Errorf("ParseFlags() with nil args should not error, got: %v", err)
	}
	if config == nil {
		t.Error("ParseFlags() with nil args should return valid config")
	}
}

// TestParseFlagsErrorMessages tests that error messages are descriptive
func TestParseFlagsErrorMessages(t *testing.T) {
	_, err := ParseFlags([]string{"--invalid-flag"})
	if err == nil {
		t.Error("ParseFlags() with invalid flag should return error")
		return
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "flag parsing failed") {
		t.Errorf("ParseFlags() error message should mention flag parsing, got: %s", errStr)
	}
}

func TestCLIConfig_GetCacheDir(t *testing.T) {
	// Get current working directory for absolute path comparisons
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}

	tests := []struct {
		name        string
		config      *CLIConfig
		projectRoot string
		envVar      string
		want        string
		wantAbs     bool // whether result should be absolute path
	}{
		{
			name:        "flag takes priority over env and default",
			config:      &CLIConfig{CacheDir: "/flag/cache/dir"},
			projectRoot: "/project/root",
			envVar:      "/env/cache/dir",
			want:        "/flag/cache/dir",
			wantAbs:     true,
		},
		{
			name:        "flag with relative path converted to absolute",
			config:      &CLIConfig{CacheDir: "relative/cache"},
			projectRoot: "/project/root",
			envVar:      "",
			want:        filepath.Join(cwd, "relative/cache"),
			wantAbs:     true,
		},
		{
			name:        "env var takes priority over default when flag empty",
			config:      &CLIConfig{CacheDir: ""},
			projectRoot: "/project/root",
			envVar:      "/env/cache/dir",
			want:        "/env/cache/dir",
			wantAbs:     true,
		},
		{
			name:        "env var with relative path converted to absolute",
			config:      &CLIConfig{CacheDir: ""},
			projectRoot: "/project/root",
			envVar:      "relative/env/cache",
			want:        filepath.Join(cwd, "relative/env/cache"),
			wantAbs:     true,
		},
		{
			name:        "default path when flag and env empty",
			config:      &CLIConfig{CacheDir: ""},
			projectRoot: "/project/root",
			envVar:      "",
			want:        "/project/root/.oxinfer/cache/v1",
			wantAbs:     true,
		},
		{
			name:        "default path with relative project root",
			config:      &CLIConfig{CacheDir: ""},
			projectRoot: "relative/project",
			envVar:      "",
			want:        filepath.Join(cwd, "relative/project/.oxinfer/cache/v1"),
			wantAbs:     true,
		},
		{
			name:        "empty project root uses current dir",
			config:      &CLIConfig{CacheDir: ""},
			projectRoot: "",
			envVar:      "",
			want:        filepath.Join(cwd, ".oxinfer/cache/v1"),
			wantAbs:     true,
		},
		{
			name:        "flag priority with all three options set",
			config:      &CLIConfig{CacheDir: "/priority/flag"},
			projectRoot: "/some/project",
			envVar:      "/env/fallback",
			want:        "/priority/flag",
			wantAbs:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variable for this test
			oldEnv := os.Getenv("OXINFER_CACHE_DIR")
			if tt.envVar != "" {
				err := os.Setenv("OXINFER_CACHE_DIR", tt.envVar)
				if err != nil {
					t.Fatalf("failed to set environment variable: %v", err)
				}
			} else {
				err := os.Unsetenv("OXINFER_CACHE_DIR")
				if err != nil {
					t.Fatalf("failed to unset environment variable: %v", err)
				}
			}
			// Restore environment variable after test
			defer func() {
				if oldEnv != "" {
					os.Setenv("OXINFER_CACHE_DIR", oldEnv)
				} else {
					os.Unsetenv("OXINFER_CACHE_DIR")
				}
			}()

			got := tt.config.GetCacheDir(tt.projectRoot)

			if got != tt.want {
				t.Errorf("GetCacheDir() = %v, want %v", got, tt.want)
			}

			// Verify absolute path requirement
			if tt.wantAbs && !filepath.IsAbs(got) {
				t.Errorf("GetCacheDir() returned relative path %v, expected absolute", got)
			}
		})
	}
}

func TestCLIConfig_GetCacheDir_EnvironmentVariablePrecedence(t *testing.T) {
	tests := []struct {
		name     string
		flag     string
		envVar   string
		expected string
	}{
		{
			name:     "flag overrides environment",
			flag:     "/flag/path",
			envVar:   "/env/path",
			expected: "/flag/path",
		},
		{
			name:     "environment used when flag empty",
			flag:     "",
			envVar:   "/env/path",
			expected: "/env/path",
		},
		{
			name:     "default used when both empty",
			flag:     "",
			envVar:   "",
			expected: "", // will be tested separately since it's project-root dependent
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			oldEnv := os.Getenv("OXINFER_CACHE_DIR")
			defer func() {
				if oldEnv != "" {
					os.Setenv("OXINFER_CACHE_DIR", oldEnv)
				} else {
					os.Unsetenv("OXINFER_CACHE_DIR")
				}
			}()

			if tt.envVar != "" {
				os.Setenv("OXINFER_CACHE_DIR", tt.envVar)
			} else {
				os.Unsetenv("OXINFER_CACHE_DIR")
			}

			config := &CLIConfig{CacheDir: tt.flag}
			got := config.GetCacheDir("/test/project")

			if tt.expected != "" && got != tt.expected {
				t.Errorf("GetCacheDir() = %v, want %v", got, tt.expected)
			}

			// For default case, verify it follows the expected pattern
			if tt.expected == "" {
				expectedDefault := "/test/project/.oxinfer/cache/v1"
				if got != expectedDefault {
					t.Errorf("GetCacheDir() default = %v, want %v", got, expectedDefault)
				}
			}
		})
	}
}
