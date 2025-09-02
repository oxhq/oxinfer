// Package pipeline provides orchestration for the complete oxinfer analysis pipeline.
package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/garaekz/oxinfer/internal/indexer"
	"github.com/garaekz/oxinfer/internal/infer"
	"github.com/garaekz/oxinfer/internal/manifest"
	"github.com/garaekz/oxinfer/internal/matchers"
	"github.com/garaekz/oxinfer/internal/parser"
)

func TestNewOrchestrator(t *testing.T) {
	tests := []struct {
		name        string
		config      *PipelineConfig
		expectError bool
		errorType   string
	}{
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "valid config",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    100,
				MaxWorkers:  2,
			},
			expectError: false,
		},
		{
			name: "invalid config - empty project root",
			config: &PipelineConfig{
				ProjectRoot: "",
				Targets:     []string{"app"},
				MaxFiles:    100,
				MaxWorkers:  2,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "invalid config - empty targets",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{},
				MaxFiles:    100,
				MaxWorkers:  2,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "invalid config - zero max files",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    0,
				MaxWorkers:  2,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "invalid config - zero max workers",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    100,
				MaxWorkers:  0,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orchestrator, err := NewOrchestrator(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}

				var pipelineErr *PipelineError
				if errors.As(err, &pipelineErr) {
					if pipelineErr.Type != tt.errorType {
						t.Errorf("expected error type %s, got %s", tt.errorType, pipelineErr.Type)
					}
				} else {
					t.Errorf("expected PipelineError but got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if orchestrator == nil {
				t.Error("orchestrator should not be nil")
				return
			}

			// Verify initial state
			if orchestrator.config != tt.config {
				t.Error("config not set correctly")
			}

			progress := orchestrator.GetProgress()
			if progress.Phase != PipelinePhaseInitializing {
				t.Errorf("expected initial phase %v, got %v", PipelinePhaseInitializing, progress.Phase)
			}

			stats := orchestrator.GetStats()
			if stats == nil {
				t.Error("stats should not be nil")
			}
		})
	}
}

func TestDefaultPipelineConfig(t *testing.T) {
	config := DefaultPipelineConfig()

	if config == nil {
		t.Fatal("config should not be nil")
	}

	// Verify default values
	if len(config.Targets) == 0 {
		t.Error("targets should not be empty")
	}

	if len(config.Globs) == 0 {
		t.Error("globs should not be empty")
	}

	if config.MaxFiles <= 0 {
		t.Error("max files should be positive")
	}

	if config.MaxWorkers <= 0 {
		t.Error("max workers should be positive")
	}

	if config.MatcherConfig == nil {
		t.Error("matcher config should not be nil")
	}

	if config.InferenceConfig == nil {
		t.Error("inference config should not be nil")
	}
}

func TestPipelineConfig_ConfigureFromManifest(t *testing.T) {
	tests := []struct {
		name         string
		config       *PipelineConfig
		manifest     *manifest.Manifest
		expectError  bool
		validateFunc func(*testing.T, *PipelineConfig)
	}{
		{
			name:        "nil manifest",
			config:      DefaultPipelineConfig(),
			manifest:    nil,
			expectError: true,
		},
		{
			name:   "basic configuration",
			config: DefaultPipelineConfig(),
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: "/custom/root",
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"custom", "targets"},
				},
				Limits: &manifest.LimitsConfig{
					MaxFiles:   intPtr(500),
					MaxWorkers: intPtr(8),
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, config *PipelineConfig) {
				if config.ProjectRoot != "/custom/root" {
					t.Errorf("expected project root /custom/root, got %s", config.ProjectRoot)
				}
				if len(config.Targets) != 2 || config.Targets[0] != "custom" || config.Targets[1] != "targets" {
					t.Errorf("expected targets [custom targets], got %v", config.Targets)
				}
				if config.MaxFiles != 500 {
					t.Errorf("expected max files 500, got %d", config.MaxFiles)
				}
				if config.MaxWorkers != 8 {
					t.Errorf("expected max workers 8, got %d", config.MaxWorkers)
				}
			},
		},
		{
			name:   "cache configuration",
			config: DefaultPipelineConfig(),
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{Root: "/test"},
				Cache: &manifest.CacheConfig{
					Enabled: boolPtr(false),
					Kind:    stringPtr("mtime"),
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, config *PipelineConfig) {
				if config.CacheConfig.CacheEnabled != false {
					t.Errorf("expected cache enabled false, got %v", config.CacheConfig.CacheEnabled)
				}
				if config.CacheConfig.CacheKind != "mtime" {
					t.Errorf("expected cache kind mtime, got %s", config.CacheConfig.CacheKind)
				}
			},
		},
		{
			name:   "feature flags configuration",
			config: DefaultPipelineConfig(),
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{Root: "/test"},
				Features: &manifest.FeatureConfig{
					HTTPStatus:   boolPtr(false),
					RequestUsage: boolPtr(true),
					WithPivot:    boolPtr(false),
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, config *PipelineConfig) {
				if config.MatcherConfig.EnableHTTPStatusMatching != false {
					t.Errorf("expected HTTP status matching false, got %v", config.MatcherConfig.EnableHTTPStatusMatching)
				}
				if config.MatcherConfig.EnableRequestMatching != true {
					t.Errorf("expected request matching true, got %v", config.MatcherConfig.EnableRequestMatching)
				}
				if config.MatcherConfig.EnablePivotMatching != false {
					t.Errorf("expected pivot matching false, got %v", config.MatcherConfig.EnablePivotMatching)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ConfigureFromManifest(tt.manifest)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, tt.config)
			}
		})
	}
}

func TestPipelineConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      *PipelineConfig
		expectError bool
		errorType   string
	}{
		{
			name: "valid config",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    100,
				MaxWorkers:  2,
			},
			expectError: false,
		},
		{
			name: "empty project root",
			config: &PipelineConfig{
				ProjectRoot: "",
				Targets:     []string{"app"},
				MaxFiles:    100,
				MaxWorkers:  2,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "empty targets",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{},
				MaxFiles:    100,
				MaxWorkers:  2,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "zero max files",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    0,
				MaxWorkers:  2,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "negative max files",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    -10,
				MaxWorkers:  2,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "zero max workers",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    100,
				MaxWorkers:  0,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
		{
			name: "negative max workers",
			config: &PipelineConfig{
				ProjectRoot: "/test",
				Targets:     []string{"app"},
				MaxFiles:    100,
				MaxWorkers:  -5,
			},
			expectError: true,
			errorType:   ErrorTypeConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}

				var pipelineErr *PipelineError
				if errors.As(err, &pipelineErr) {
					if pipelineErr.Type != tt.errorType {
						t.Errorf("expected error type %s, got %s", tt.errorType, pipelineErr.Type)
					}
				} else {
					t.Errorf("expected PipelineError but got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestPipelinePhase_String(t *testing.T) {
	tests := []struct {
		phase    PipelinePhase
		expected string
	}{
		{PipelinePhaseInitializing, "initializing"},
		{PipelinePhaseIndexing, "indexing"},
		{PipelinePhaseParsing, "parsing"},
		{PipelinePhaseMatching, "matching"},
		{PipelinePhaseInference, "inference"},
		{PipelinePhaseAssembly, "assembly"},
		{PipelinePhaseCompleted, "completed"},
		{PipelinePhaseFailed, "failed"},
		{PipelinePhase(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.phase.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestPipelineError(t *testing.T) {
	cause := errors.New("underlying cause")
	
	tests := []struct {
		name        string
		pipelineErr *PipelineError
		expectedMsg string
		expectedType string
	}{
		{
			name: "error without cause",
			pipelineErr: NewPipelineError(
				ErrorTypeIndexing,
				"indexing failed",
				PipelinePhaseIndexing,
				"test context",
				nil,
			),
			expectedMsg:  "indexing failed",
			expectedType: ErrorTypeIndexing,
		},
		{
			name: "error with cause",
			pipelineErr: NewPipelineError(
				ErrorTypeParsing,
				"parsing failed",
				PipelinePhaseParsing,
				"test context",
				cause,
			),
			expectedMsg:  "parsing failed: underlying cause",
			expectedType: ErrorTypeParsing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.pipelineErr.Error() != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, tt.pipelineErr.Error())
			}

			if tt.pipelineErr.Type != tt.expectedType {
				t.Errorf("expected type %s, got %s", tt.expectedType, tt.pipelineErr.Type)
			}

			if tt.pipelineErr.Cause != nil {
				if errors.Unwrap(tt.pipelineErr) != tt.pipelineErr.Cause {
					t.Error("Unwrap should return the cause")
				}
			}
		})
	}
}

func TestComponentRegistry_Close(t *testing.T) {
	registry := NewComponentRegistry()

	// Create mock components that implement Close
	registry.PSR4Resolver = &mockPSR4Resolver{}
	registry.PHPParser = &mockPHPParser{}
	registry.PatternMatcher = &mockPatternMatcher{}

	err := registry.Close()
	if err != nil {
		t.Errorf("unexpected error during close: %v", err)
	}
}

func TestOrchestrator_ConfigurationMethods(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ProjectRoot = "/test/project" // Add required field
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	defer orchestrator.Close()

	// Test GetConfiguration
	retrievedConfig := orchestrator.GetConfiguration()
	if retrievedConfig != config {
		t.Error("GetConfiguration should return the same config")
	}

	// Test SetConfiguration
	newConfig := DefaultPipelineConfig()
	newConfig.MaxFiles = 9999
	orchestrator.SetConfiguration(newConfig)

	retrievedConfig = orchestrator.GetConfiguration()
	if retrievedConfig.MaxFiles != 9999 {
		t.Error("SetConfiguration should update the config")
	}
}

func TestOrchestrator_ProgressCallback(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ProjectRoot = "/test/project" // Add required field
	orchestrator, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	defer orchestrator.Close()

	// Test progress callback
	var receivedProgress *PipelineProgress
	orchestrator.SetProgressCallback(func(progress *PipelineProgress) {
		receivedProgress = progress
	})

	// Trigger a progress update
	orchestrator.updateProgress(PipelinePhaseIndexing, "test status", 0.5)

	if receivedProgress == nil {
		t.Error("progress callback should have been called")
	}

	if receivedProgress.Phase != PipelinePhaseIndexing {
		t.Errorf("expected phase %v, got %v", PipelinePhaseIndexing, receivedProgress.Phase)
	}

	if receivedProgress.PhaseStatus != "test status" {
		t.Errorf("expected status 'test status', got %s", receivedProgress.PhaseStatus)
	}

	if receivedProgress.Progress != 0.5 {
		t.Errorf("expected progress 0.5, got %f", receivedProgress.Progress)
	}
}

// Mock implementations for testing

type mockPSR4Resolver struct{}

func (m *mockPSR4Resolver) ResolveClass(ctx context.Context, fqcn string) (string, error) {
	return "/path/to/class.php", nil
}

func (m *mockPSR4Resolver) GetAllClasses(ctx context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (m *mockPSR4Resolver) Refresh() error {
	return nil
}

type mockPHPParser struct{}

func (m *mockPHPParser) ProcessFile(ctx context.Context, file indexer.FileInfo) (*indexer.ProcessResult, error) {
	return &indexer.ProcessResult{}, nil
}

func (m *mockPHPParser) ParsePHPFile(ctx context.Context, filePath string) (*parser.PHPParseResult, error) {
	return &parser.PHPParseResult{}, nil
}

func (m *mockPHPParser) GetParserStats() parser.ParserStats {
	return parser.ParserStats{}
}

func (m *mockPHPParser) SetConfiguration(config parser.ParserConfig) error {
	return nil
}

func (m *mockPHPParser) Close() error {
	return nil
}

func (m *mockPHPParser) IsInitialized() bool {
	return true
}

func (m *mockPHPParser) ParseContent(content []byte) (*parser.SyntaxTree, error) {
	return &parser.SyntaxTree{}, nil
}

func (m *mockPHPParser) ParseFile(ctx context.Context, filePath string) (*parser.SyntaxTree, error) {
	return &parser.SyntaxTree{}, nil
}

type mockPatternMatcher struct{}

func (m *mockPatternMatcher) AddMatcher(matcher matchers.PatternMatcher) error {
	return nil
}

func (m *mockPatternMatcher) RemoveMatcher(patternType matchers.PatternType) error {
	return nil
}

func (m *mockPatternMatcher) MatchAll(ctx context.Context, tree *parser.SyntaxTree, filePath string) (*matchers.LaravelPatterns, error) {
	return &matchers.LaravelPatterns{}, nil
}

func (m *mockPatternMatcher) GetMatchers() map[matchers.PatternType]matchers.PatternMatcher {
	return make(map[matchers.PatternType]matchers.PatternMatcher)
}

func (m *mockPatternMatcher) IsInitialized() bool {
	return true
}

func (m *mockPatternMatcher) Close() error {
	return nil
}

type mockShapeInferencer struct{}

func (m *mockShapeInferencer) InferRequestShape(patterns []matchers.RequestUsageMatch) (*infer.RequestInfo, error) {
	return &infer.RequestInfo{}, nil
}

func (m *mockShapeInferencer) ConsolidatePatterns(patterns []matchers.RequestUsageMatch) (*infer.ConsolidatedRequest, error) {
	return &infer.ConsolidatedRequest{}, nil
}

// Helper functions for tests
func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}

func stringPtr(s string) *string {
	return &s
}