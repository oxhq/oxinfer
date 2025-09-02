package perf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/indexer"
	"github.com/garaekz/oxinfer/internal/manifest"
	"github.com/garaekz/oxinfer/internal/pipeline"
)

func TestPerformanceIntegration_Initialize(t *testing.T) {
	tests := []struct {
		name        string
		config      *IntegrationConfig
		expectError bool
	}{
		{
			name:        "default_config",
			config:      DefaultIntegrationConfig(),
			expectError: false,
		},
		{
			name:        "nil_config_uses_default",
			config:      nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			integration, err := NewPerformanceIntegration(tt.config)
			if err != nil {
				if !tt.expectError {
					t.Fatalf("Unexpected error creating integration: %v", err)
				}
				return
			}

			if tt.expectError {
				t.Fatal("Expected error but got none")
			}

			ctx := context.Background()
			err = integration.Initialize(ctx)
			if err != nil {
				t.Fatalf("Initialization failed: %v", err)
			}

			// Verify components are initialized
			if integration.analyzer == nil {
				t.Error("Analyzer should be initialized")
			}

			if integration.optimizer == nil {
				t.Error("Optimizer should be initialized")
			}

			if integration.workerPool == nil {
				t.Error("Worker pool should be initialized")
			}

			if integration.targets == nil {
				t.Error("Performance targets should be set")
			}
		})
	}
}

func TestPerformanceIntegration_MVPTargets(t *testing.T) {
	targets := MVPPerformanceTargets()

	// Verify MVP targets match requirements
	if targets.ColdRun != 10*time.Second {
		t.Errorf("Expected cold run target of 10s, got %v", targets.ColdRun)
	}

	if targets.IncrementalRun != 2*time.Second {
		t.Errorf("Expected incremental run target of 2s, got %v", targets.IncrementalRun)
	}

	if targets.MemoryPeak != 500 {
		t.Errorf("Expected memory peak target of 500MB, got %d", targets.MemoryPeak)
	}

	if targets.CPUEfficiency != 0.8 {
		t.Errorf("Expected CPU efficiency target of 80%%, got %.1f", targets.CPUEfficiency*100)
	}
}

// createTestManifest creates a test manifest for performance testing.
func createTestManifest(t testing.TB, tempDir string) *manifest.Manifest {
	// Create required directories
	projectDir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("Failed to create project dir: %v", err)
	}

	composerFile := filepath.Join(projectDir, "composer.json")
	if err := os.WriteFile(composerFile, []byte(`{"name": "test/project"}`), 0644); err != nil {
		t.Fatalf("Failed to create composer.json: %v", err)
	}

	appDir := filepath.Join(projectDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	return &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     projectDir,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"**/*.php"},
		},
		Limits: &manifest.LimitsConfig{
			MaxWorkers: intPtr(8),
			MaxFiles:   intPtr(500),
			MaxDepth:   intPtr(3),
		},
		Cache: &manifest.CacheConfig{
			Enabled: boolPtr(true),
			Kind:    stringPtr("sha256+mtime"),
		},
	}
}

// mockPipelineOrchestrator implements pipeline.PipelineOrchestrator for testing.
type mockPipelineOrchestrator struct {
	processingTime time.Duration
	memoryUsage    int64
	filesProcessed int
	shouldFail     bool
}

func (m *mockPipelineOrchestrator) ProcessProject(ctx context.Context, manifest *manifest.Manifest) (*emitter.Delta, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("mock orchestrator failure")
	}

	// Simulate processing time
	time.Sleep(10 * time.Millisecond) // Minimal sleep for testing

	// Return mock delta
	return &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: int64(m.filesProcessed),
				DurationMs:  int64(m.processingTime / time.Millisecond),
			},
		},
		Controllers: []emitter.Controller{},
		Models:      []emitter.Model{},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}, nil
}

func (m *mockPipelineOrchestrator) RunIndexingPhase(ctx context.Context, manifest *manifest.Manifest) (*indexer.IndexResult, error) {
	return &indexer.IndexResult{TotalFiles: m.filesProcessed}, nil
}

func (m *mockPipelineOrchestrator) RunParsingPhase(ctx context.Context, files []indexer.FileInfo) (*pipeline.ParseResults, error) {
	return &pipeline.ParseResults{FilesProcessed: m.filesProcessed}, nil
}

func (m *mockPipelineOrchestrator) RunMatchingPhase(ctx context.Context, parseResults *pipeline.ParseResults) (*pipeline.MatchResults, error) {
	return &pipeline.MatchResults{TotalMatches: 10}, nil
}

func (m *mockPipelineOrchestrator) RunInferencePhase(ctx context.Context, matchResults *pipeline.MatchResults) (*pipeline.InferenceResults, error) {
	return &pipeline.InferenceResults{ShapesInferred: 5}, nil
}

func (m *mockPipelineOrchestrator) Close() error {
	return nil
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