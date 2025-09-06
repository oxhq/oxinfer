package perf

import (
	"context"
	"testing"
	"time"
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