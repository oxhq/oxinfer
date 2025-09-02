// Package bench provides performance benchmarking infrastructure for the Oxinfer pipeline.
// This file contains tests for the benchmark runner system.
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewBenchmarkRunner(t *testing.T) {
	tests := []struct {
		name        string
		config      *RunnerConfig
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil config",
			config:      nil,
			wantErr:     true,
			errContains: "cannot be nil",
		},
		{
			name: "invalid config - empty temp dir",
			config: &RunnerConfig{
				TempDir:       "",
				BenchmarkRuns: 1,
				TimeoutPerRun: time.Minute,
			},
			wantErr:     true,
			errContains: "temp directory cannot be empty",
		},
		{
			name: "invalid config - zero runs",
			config: &RunnerConfig{
				TempDir:       filepath.Join(os.TempDir(), "test"),
				BenchmarkRuns: 0,
				TimeoutPerRun: time.Minute,
			},
			wantErr:     true,
			errContains: "benchmark runs must be positive",
		},
		{
			name: "valid config",
			config: &RunnerConfig{
				TempDir:         filepath.Join(os.TempDir(), "test_bench"),
				BenchmarkRuns:   1,
				TimeoutPerRun:   time.Minute,
				CleanupAfterRun: true,
				OutputDir:       filepath.Join(os.TempDir(), "test_output"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, err := NewBenchmarkRunner(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewBenchmarkRunner() expected error but got none")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("NewBenchmarkRunner() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("NewBenchmarkRunner() unexpected error = %v", err)
				return
			}

			if runner == nil {
				t.Errorf("NewBenchmarkRunner() returned nil runner")
				return
			}

			// Cleanup
			if tt.config != nil && tt.config.TempDir != "" {
				os.RemoveAll(tt.config.TempDir)
			}
			if tt.config != nil && tt.config.OutputDir != "" {
				os.RemoveAll(tt.config.OutputDir)
			}
		})
	}
}

func TestBenchmarkRunner_AddScenario(t *testing.T) {
	config := &RunnerConfig{
		TempDir:         filepath.Join(os.TempDir(), "test_bench"),
		BenchmarkRuns:   1,
		TimeoutPerRun:   time.Minute,
		CleanupAfterRun: true,
	}

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	defer os.RemoveAll(config.TempDir)

	tests := []struct {
		name        string
		scenario    *BenchmarkScenario
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil scenario",
			scenario:    nil,
			wantErr:     true,
			errContains: "cannot be nil",
		},
		{
			name: "invalid scenario - empty name",
			scenario: &BenchmarkScenario{
				Name:        "",
				FileCount:   10,
				MaxDuration: time.Second,
				MaxMemoryMB: 100,
			},
			wantErr:     true,
			errContains: "name cannot be empty",
		},
		{
			name: "valid scenario",
			scenario: &BenchmarkScenario{
				Name:         "Test Scenario",
				Description:  "A test scenario",
				FileCount:    10,
				MaxDuration:  time.Second,
				MaxMemoryMB:  100,
				ScenarioType: ScenarioSmall,
				ProjectStructure: ProjectLayout{
					Controllers: []ControllerInfo{
						{Name: "TestController", Namespace: "App\\Http\\Controllers"},
					},
					Models: []ModelInfo{
						{Name: "TestModel", Namespace: "App\\Models"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate scenario name",
			scenario: &BenchmarkScenario{
				Name:         "Test Scenario", // Same name as previous
				Description:  "Another test scenario",
				FileCount:    20,
				MaxDuration:  time.Second,
				MaxMemoryMB:  200,
				ScenarioType: ScenarioSmall,
				ProjectStructure: ProjectLayout{
					Controllers: []ControllerInfo{
						{Name: "TestController2", Namespace: "App\\Http\\Controllers"},
					},
					Models: []ModelInfo{
						{Name: "TestModel2", Namespace: "App\\Models"},
					},
				},
			},
			wantErr:     true,
			errContains: "already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runner.AddScenario(tt.scenario)

			if tt.wantErr {
				if err == nil {
					t.Errorf("AddScenario() expected error but got none")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("AddScenario() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("AddScenario() unexpected error = %v", err)
			}
		})
	}
}

func TestBenchmarkRunner_AddAllMVPScenarios(t *testing.T) {
	config := &RunnerConfig{
		TempDir:         filepath.Join(os.TempDir(), "test_bench"),
		BenchmarkRuns:   1,
		TimeoutPerRun:   time.Minute,
		CleanupAfterRun: true,
	}

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	defer os.RemoveAll(config.TempDir)

	err = runner.AddAllMVPScenarios()
	if err != nil {
		t.Errorf("AddAllMVPScenarios() unexpected error = %v", err)
		return
	}

	// Check that scenarios were added
	runner.mu.RLock()
	scenarioCount := len(runner.scenarios)
	runner.mu.RUnlock()

	expectedCount := len(MVPBenchmarkScenarios())
	if scenarioCount != expectedCount {
		t.Errorf("AddAllMVPScenarios() added %d scenarios, expected %d", scenarioCount, expectedCount)
	}
}

func TestBenchmarkRunner_GetCurrentStatus(t *testing.T) {
	config := &RunnerConfig{
		TempDir:         filepath.Join(os.TempDir(), "test_bench"),
		BenchmarkRuns:   1,
		TimeoutPerRun:   time.Minute,
		CleanupAfterRun: true,
	}

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	defer os.RemoveAll(config.TempDir)

	// Initial status should be not running
	running, current, completed, total := runner.GetCurrentStatus()

	if running {
		t.Errorf("GetCurrentStatus() running = %v, expected false", running)
	}

	if current != "" {
		t.Errorf("GetCurrentStatus() current = %q, expected empty", current)
	}

	if completed != 0 {
		t.Errorf("GetCurrentStatus() completed = %d, expected 0", completed)
	}

	if total != 0 {
		t.Errorf("GetCurrentStatus() total = %d, expected 0", total)
	}
}

func TestDefaultBenchmarkConfig(t *testing.T) {
	config := DefaultBenchmarkConfig()

	if config == nil {
		t.Fatal("DefaultBenchmarkConfig() returned nil")
	}

	// Check that essential fields are set
	if config.TempDir == "" {
		t.Error("DefaultBenchmarkConfig() TempDir is empty")
	}

	if config.BenchmarkRuns <= 0 {
		t.Error("DefaultBenchmarkConfig() BenchmarkRuns is not positive")
	}

	if config.TimeoutPerRun <= 0 {
		t.Error("DefaultBenchmarkConfig() TimeoutPerRun is not positive")
	}

	// Validate using the validation function
	err := validateRunnerConfig(config)
	if err != nil {
		t.Errorf("DefaultBenchmarkConfig() produces invalid config: %v", err)
	}
}

// Benchmark tests

func BenchmarkBenchmarkRunner_AddScenario(b *testing.B) {
	config := &RunnerConfig{
		TempDir:         filepath.Join(os.TempDir(), "bench_test"),
		BenchmarkRuns:   1,
		TimeoutPerRun:   time.Minute,
		CleanupAfterRun: true,
	}

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		b.Fatalf("Failed to create runner: %v", err)
	}
	defer os.RemoveAll(config.TempDir)

	scenario := &BenchmarkScenario{
		Name:         "Benchmark Test Scenario",
		Description:  "A scenario for benchmark testing",
		FileCount:    100,
		MaxDuration:  5 * time.Second,
		MaxMemoryMB:  512,
		ScenarioType: ScenarioMedium,
		ProjectStructure: ProjectLayout{
			Controllers: []ControllerInfo{
				{Name: "TestController", Namespace: "App\\Http\\Controllers"},
			},
			Models: []ModelInfo{
				{Name: "TestModel", Namespace: "App\\Models"},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scenario.Name = fmt.Sprintf("Benchmark Test Scenario %d", i)
		err := runner.AddScenario(scenario)
		if err != nil {
			b.Errorf("AddScenario() error = %v", err)
		}
	}
}

func BenchmarkBenchmarkRunner_GetCurrentStatus(b *testing.B) {
	config := &RunnerConfig{
		TempDir:         filepath.Join(os.TempDir(), "bench_test"),
		BenchmarkRuns:   1,
		TimeoutPerRun:   time.Minute,
		CleanupAfterRun: true,
	}

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		b.Fatalf("Failed to create runner: %v", err)
	}
	defer os.RemoveAll(config.TempDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner.GetCurrentStatus()
	}
}

// Table-driven test for validateRunnerConfig
func TestValidateRunnerConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *RunnerConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: &RunnerConfig{
				TempDir:       filepath.Join(os.TempDir(), "test"),
				BenchmarkRuns: 3,
				TimeoutPerRun: time.Minute,
			},
			wantErr: false,
		},
		{
			name: "empty temp dir",
			config: &RunnerConfig{
				TempDir:       "",
				BenchmarkRuns: 3,
				TimeoutPerRun: time.Minute,
			},
			wantErr:     true,
			errContains: "temp directory cannot be empty",
		},
		{
			name: "zero benchmark runs",
			config: &RunnerConfig{
				TempDir:       filepath.Join(os.TempDir(), "test"),
				BenchmarkRuns: 0,
				TimeoutPerRun: time.Minute,
			},
			wantErr:     true,
			errContains: "benchmark runs must be positive",
		},
		{
			name: "negative benchmark runs",
			config: &RunnerConfig{
				TempDir:       filepath.Join(os.TempDir(), "test"),
				BenchmarkRuns: -1,
				TimeoutPerRun: time.Minute,
			},
			wantErr:     true,
			errContains: "benchmark runs must be positive",
		},
		{
			name: "zero timeout",
			config: &RunnerConfig{
				TempDir:       filepath.Join(os.TempDir(), "test"),
				BenchmarkRuns: 3,
				TimeoutPerRun: 0,
			},
			wantErr:     true,
			errContains: "timeout per run must be positive",
		},
		{
			name: "negative timeout",
			config: &RunnerConfig{
				TempDir:       filepath.Join(os.TempDir(), "test"),
				BenchmarkRuns: 3,
				TimeoutPerRun: -time.Minute,
			},
			wantErr:     true,
			errContains: "timeout per run must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunnerConfig(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateRunnerConfig() expected error but got none")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("validateRunnerConfig() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("validateRunnerConfig() unexpected error = %v", err)
			}
		})
	}
}

// Test helper functions

func TestGenerateRunID(t *testing.T) {
	scenarioName := "Test Scenario"

	// Generate multiple IDs to ensure uniqueness
	id1 := generateRunID(scenarioName)
	time.Sleep(time.Nanosecond) // Ensure different timestamps
	id2 := generateRunID(scenarioName)

	if id1 == id2 {
		t.Errorf("generateRunID() produced duplicate IDs: %s", id1)
	}

	if !containsString(id1, scenarioName) {
		t.Errorf("generateRunID() ID %s does not contain scenario name %s", id1, scenarioName)
	}
}

func TestEnsureDirectories(t *testing.T) {
	baseDir := filepath.Join(os.TempDir(), "test_ensure_dirs")
	defer os.RemoveAll(baseDir)

	config := &RunnerConfig{
		TempDir:    filepath.Join(baseDir, "temp"),
		OutputDir:  filepath.Join(baseDir, "output"),
		ProfileDir: filepath.Join(baseDir, "profiles"),
	}

	err := ensureDirectories(config)
	if err != nil {
		t.Errorf("ensureDirectories() unexpected error = %v", err)
		return
	}

	// Check that directories were created
	dirs := []string{config.TempDir, config.OutputDir, config.ProfileDir}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("ensureDirectories() failed to create directory: %s", dir)
		}
	}
}

// Helper function to check if a string contains another string (case-insensitive)
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(substr) == 0 ||
		strings.Contains(strings.ToLower(s), strings.ToLower(substr)))
}
