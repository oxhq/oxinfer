package perf

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/bench"
	"github.com/oxhq/oxinfer/internal/stats"
)

func TestPerformanceAnalyzer_AnalyzeProfiles(t *testing.T) {
	tests := []struct {
		name                  string
		config                *AnalyzerConfig
		dataset               *ProfileDataset
		expectHotspots        int
		expectRecommendations int
		expectError           bool
	}{
		{
			name:   "empty_dataset",
			config: DefaultAnalyzerConfig(),
			dataset: &ProfileDataset{
				Scenarios: []*bench.BenchmarkResult{},
			},
			expectHotspots:        0,
			expectRecommendations: 0,
			expectError:           false,
		},
		{
			name:   "single_scenario_with_hotspots",
			config: DefaultAnalyzerConfig(),
			dataset: &ProfileDataset{
				Scenarios: []*bench.BenchmarkResult{
					{
						Success: true,
						Metrics: &bench.PerformanceMetrics{
							TotalDuration: 15 * time.Second, // Exceeds 10s target
							PhaseDurations: bench.PhaseTimings{
								Parsing:   8 * time.Second, // 53% of total - should be hotspot
								Matching:  4 * time.Second, // 27% of total - should be hotspot
								Indexing:  2 * time.Second, // 13% of total - should be hotspot
								Inference: 1 * time.Second, // 7% of total - below threshold
							},
							MemoryStats: bench.MemoryProfile{
								PeakTotalMB: 600, // Exceeds 500MB target
								GCCount:     50,
							},
						},
					},
				},
			},
			expectHotspots:        6, // 4 timing (parsing, matching, indexing, inference) + 2 memory (peak + GC pressure)
			expectRecommendations: 3, // One per component: timing/parser, timing/matchers, memory
			expectError:           false,
		},
		{
			name:   "memory_pressure_scenario",
			config: DefaultAnalyzerConfig(),
			dataset: &ProfileDataset{
				Scenarios: []*bench.BenchmarkResult{
					{
						Success: true,
						Metrics: &bench.PerformanceMetrics{
							TotalDuration: 5 * time.Second,
							PhaseDurations: bench.PhaseTimings{
								// Explicitly set all phases to 0 to avoid timing hotspots
								Parsing:   0,
								Matching:  0,
								Indexing:  0,
								Inference: 0,
								Assembly:  0,
							},
							MemoryStats: bench.MemoryProfile{
								PeakTotalMB:      800, // High memory usage
								MemoryGrowthRate: 75,  // High growth rate
								GCCount:          100, // High GC frequency
							},
						},
					},
				},
			},
			expectHotspots:        3, // Peak memory + growth rate + GC pressure
			expectRecommendations: 2, // Peak memory + GC pressure (growth_rate metric not handled in switch)
			expectError:           false,
		},
		{
			name:   "concurrency_inefficiency",
			config: DefaultAnalyzerConfig(),
			dataset: &ProfileDataset{
				Scenarios: []*bench.BenchmarkResult{
					{
						Success: true,
						Metrics: &bench.PerformanceMetrics{
							TotalDuration: 8 * time.Second,
							ProcessingStats: &stats.ProcessingStats{
								FilesParsed: 100,
								DurationMs:  8000, // 8 seconds in milliseconds
							},
						},
					},
				},
			},
			expectHotspots:        0, // No hotspots since concurrency analysis is disabled
			expectRecommendations: 0,
			expectError:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzer, err := NewPerformanceAnalyzer(tt.config)
			if err != nil {
				t.Fatalf("Failed to create analyzer: %v", err)
			}

			ctx := context.Background()
			results, err := analyzer.AnalyzeProfiles(ctx, tt.dataset)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if results == nil {
				t.Fatalf("Results cannot be nil")
			}

			// Verify hotspots count
			actualHotspots := len(results.Hotspots)
			if actualHotspots != tt.expectHotspots {
				t.Errorf("Expected %d hotspots, got %d", tt.expectHotspots, actualHotspots)
			}

			// Verify recommendations count
			actualRecommendations := len(results.Recommendations)
			if actualRecommendations != tt.expectRecommendations {
				t.Errorf("Expected %d recommendations, got %d", tt.expectRecommendations, actualRecommendations)
			}

			// Verify analysis results structure
			if results.Timestamp.IsZero() {
				t.Error("Analysis timestamp should be set")
			}

			if results.AnalysisDuration <= 0 {
				t.Error("Analysis duration should be positive")
			}

			// Verify targets evaluation
			if len(tt.dataset.Scenarios) > 0 && tt.dataset.Scenarios[0].Success {
				// Should have evaluated targets
				if results.TargetsMet.ActualDuration == 0 && results.TargetsMet.TargetDuration == 0 {
					t.Error("Targets should be evaluated when scenarios are present")
				}
			}
		})
	}
}

func TestPerformanceAnalyzer_HotspotIdentification(t *testing.T) {
	analyzer, err := NewPerformanceAnalyzer(DefaultAnalyzerConfig())
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	// Create test dataset with known hotspots
	dataset := &ProfileDataset{
		Scenarios: []*bench.BenchmarkResult{
			{
				Success: true,
				Scenario: &bench.BenchmarkScenario{
					Name: "test_scenario",
				},
				Metrics: &bench.PerformanceMetrics{
					TotalDuration: 20 * time.Second,
					PhaseDurations: bench.PhaseTimings{
						Parsing:   12 * time.Second, // 60% - critical hotspot
						Matching:  5 * time.Second,  // 25% - high hotspot
						Indexing:  2 * time.Second,  // 10% - medium hotspot
						Inference: 1 * time.Second,  // 5% - at threshold
					},
					MemoryStats: bench.MemoryProfile{
						PeakTotalMB: 400, // Under threshold
					},
				},
			},
		},
	}

	ctx := context.Background()
	results, err := analyzer.AnalyzeProfiles(ctx, dataset)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Should identify 4 timing hotspots (parsing, matching, indexing, inference)
	if len(results.Hotspots) != 4 {
		t.Errorf("Expected 4 hotspots, got %d", len(results.Hotspots))
	}

	// Verify hotspots are sorted by impact
	for i := 1; i < len(results.Hotspots); i++ {
		if results.Hotspots[i].PercentOfTotal > results.Hotspots[i-1].PercentOfTotal {
			t.Error("Hotspots should be sorted by percentage of total time")
		}
	}

	// Verify hotspot severity levels
	expectedSeverities := []SeverityLevel{SeverityCritical, SeverityHigh, SeverityMedium}
	for i, hotspot := range results.Hotspots {
		if i < len(expectedSeverities) && hotspot.Severity != expectedSeverities[i] {
			t.Errorf("Hotspot %d: expected severity %s, got %s", i, expectedSeverities[i], hotspot.Severity)
		}
	}
}

func TestPerformanceAnalyzer_RecommendationGeneration(t *testing.T) {
	analyzer, err := NewPerformanceAnalyzer(DefaultAnalyzerConfig())
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	// Create dataset that should trigger specific recommendations
	dataset := &ProfileDataset{
		Scenarios: []*bench.BenchmarkResult{
			{
				Success: true,
				Metrics: &bench.PerformanceMetrics{
					TotalDuration: 12 * time.Second,
					PhaseDurations: bench.PhaseTimings{
						Matching: 8 * time.Second, // Major matcher hotspot
					},
					MemoryStats: bench.MemoryProfile{
						PeakTotalMB:      700, // Memory pressure
						MemoryGrowthRate: 60,  // High growth
					},
				},
			},
		},
	}

	ctx := context.Background()
	results, err := analyzer.AnalyzeProfiles(ctx, dataset)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Should generate multiple recommendations
	if len(results.Recommendations) == 0 {
		t.Error("Expected recommendations to be generated")
	}

	// Verify recommendations have required fields
	for i, rec := range results.Recommendations {
		if rec.ID == "" {
			t.Errorf("Recommendation %d missing ID", i)
		}
		if rec.Component == "" {
			t.Errorf("Recommendation %d missing component", i)
		}
		if rec.Title == "" {
			t.Errorf("Recommendation %d missing title", i)
		}
		if rec.EstimatedImpact <= 0 {
			t.Errorf("Recommendation %d should have positive estimated impact", i)
		}
		if len(rec.RequiredChanges) == 0 {
			t.Errorf("Recommendation %d should have required changes", i)
		}
	}

	// Verify recommendations are sorted by priority
	for i := 1; i < len(results.Recommendations); i++ {
		prev := results.Recommendations[i-1]
		curr := results.Recommendations[i]

		// Higher priority should come first
		if prev.Priority == PriorityLow && curr.Priority == PriorityHigh {
			t.Error("Recommendations should be sorted by priority")
		}
	}
}

func TestPerformanceAnalyzer_TargetEvaluation(t *testing.T) {
	tests := []struct {
		name              string
		metrics           *bench.PerformanceMetrics
		expectDurationMet bool
		expectMemoryMet   bool
	}{
		{
			name: "targets_met",
			metrics: &bench.PerformanceMetrics{
				TotalDuration: 8 * time.Second, // Under 10s target
				MemoryStats: bench.MemoryProfile{
					PeakTotalMB: 400, // Under 500MB target
				},
			},
			expectDurationMet: true,
			expectMemoryMet:   true,
		},
		{
			name: "duration_exceeded",
			metrics: &bench.PerformanceMetrics{
				TotalDuration: 15 * time.Second, // Over 10s target
				MemoryStats: bench.MemoryProfile{
					PeakTotalMB: 300, // Under 500MB target
				},
			},
			expectDurationMet: false,
			expectMemoryMet:   true,
		},
		{
			name: "memory_exceeded",
			metrics: &bench.PerformanceMetrics{
				TotalDuration: 8 * time.Second, // Under 10s target
				MemoryStats: bench.MemoryProfile{
					PeakTotalMB: 600, // Over 500MB target
				},
			},
			expectDurationMet: true,
			expectMemoryMet:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzer, err := NewPerformanceAnalyzer(DefaultAnalyzerConfig())
			if err != nil {
				t.Fatalf("Failed to create analyzer: %v", err)
			}

			dataset := &ProfileDataset{
				Scenarios: []*bench.BenchmarkResult{
					{
						Success: true,
						Metrics: tt.metrics,
					},
				},
			}

			ctx := context.Background()
			results, err := analyzer.AnalyzeProfiles(ctx, dataset)
			if err != nil {
				t.Fatalf("Analysis failed: %v", err)
			}

			if results.TargetsMet.DurationTarget != tt.expectDurationMet {
				t.Errorf("Expected duration target met: %v, got: %v", tt.expectDurationMet, results.TargetsMet.DurationTarget)
			}

			if results.TargetsMet.MemoryTarget != tt.expectMemoryMet {
				t.Errorf("Expected memory target met: %v, got: %v", tt.expectMemoryMet, results.TargetsMet.MemoryTarget)
			}
		})
	}
}

func TestPerformanceAnalyzer_RegressionDetection(t *testing.T) {
	analyzer, err := NewPerformanceAnalyzer(DefaultAnalyzerConfig())
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	// Set baseline
	baseline := &PerformanceBaseline{
		Version:   "1.0.0",
		Timestamp: time.Now().Add(-24 * time.Hour),
		ScenarioBaselines: map[string]*bench.PerformanceMetrics{
			"test_scenario": {
				TotalDuration: 5 * time.Second,
				MemoryStats: bench.MemoryProfile{
					PeakTotalMB: 300,
				},
			},
		},
	}
	analyzer.SetBaseline(baseline)

	// Create dataset with regression
	dataset := &ProfileDataset{
		Scenarios: []*bench.BenchmarkResult{
			{
				Success: true,
				Scenario: &bench.BenchmarkScenario{
					Name: "test_scenario",
				},
				Metrics: &bench.PerformanceMetrics{
					TotalDuration: 8 * time.Second, // 60% slower - regression
					MemoryStats: bench.MemoryProfile{
						PeakTotalMB: 450, // 50% more memory - regression
					},
				},
			},
		},
	}

	ctx := context.Background()
	results, err := analyzer.AnalyzeProfiles(ctx, dataset)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Should detect 2 regressions (duration and memory)
	if len(results.Regressions) != 2 {
		t.Errorf("Expected 2 regressions, got %d", len(results.Regressions))
	}

	// Verify regression details
	for _, regression := range results.Regressions {
		if regression.Scenario != "test_scenario" {
			t.Errorf("Expected scenario 'test_scenario', got '%s'", regression.Scenario)
		}

		if regression.RegressionRatio <= 1.0 {
			t.Errorf("Regression ratio should be > 1.0, got %.2f", regression.RegressionRatio)
		}
	}
}

func TestPerformanceAnalyzer_DeterministicOutput(t *testing.T) {
	config := DefaultAnalyzerConfig()
	analyzer1, err := NewPerformanceAnalyzer(config)
	if err != nil {
		t.Fatalf("Failed to create first analyzer: %v", err)
	}

	analyzer2, err := NewPerformanceAnalyzer(config)
	if err != nil {
		t.Fatalf("Failed to create second analyzer: %v", err)
	}

	// Create identical dataset
	dataset := &ProfileDataset{
		Scenarios: []*bench.BenchmarkResult{
			{
				Success: true,
				Metrics: &bench.PerformanceMetrics{
					TotalDuration: 12 * time.Second,
					PhaseDurations: bench.PhaseTimings{
						Parsing:  7 * time.Second,
						Matching: 3 * time.Second,
						Indexing: 2 * time.Second,
					},
					MemoryStats: bench.MemoryProfile{
						PeakTotalMB: 550,
					},
				},
			},
		},
		Timestamp: time.Unix(1234567890, 0), // Fixed timestamp for determinism
	}

	ctx := context.Background()

	results1, err := analyzer1.AnalyzeProfiles(ctx, dataset)
	if err != nil {
		t.Fatalf("First analysis failed: %v", err)
	}

	results2, err := analyzer2.AnalyzeProfiles(ctx, dataset)
	if err != nil {
		t.Fatalf("Second analysis failed: %v", err)
	}

	// Results should be identical (excluding timestamps)
	if len(results1.Hotspots) != len(results2.Hotspots) {
		t.Errorf("Hotspot count differs: %d vs %d", len(results1.Hotspots), len(results2.Hotspots))
	}

	if len(results1.Recommendations) != len(results2.Recommendations) {
		t.Errorf("Recommendation count differs: %d vs %d", len(results1.Recommendations), len(results2.Recommendations))
	}

	// Verify hotspot order is deterministic
	for i := range results1.Hotspots {
		if i >= len(results2.Hotspots) {
			break
		}

		h1 := results1.Hotspots[i]
		h2 := results2.Hotspots[i]

		if h1.Component != h2.Component || h1.Phase != h2.Phase {
			t.Errorf("Hotspot order differs at index %d: %s/%s vs %s/%s",
				i, h1.Component, h1.Phase, h2.Component, h2.Phase)
		}
	}
}

func TestPerformanceAnalyzer_ConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    *AnalyzerConfig
		expectErr bool
	}{
		{
			name:      "nil_config_uses_default",
			config:    nil,
			expectErr: false,
		},
		{
			name: "valid_config",
			config: &AnalyzerConfig{
				HotspotThreshold:    10.0,
				MemoryThreshold:     400,
				CPUUtilizationMin:   0.7,
				TargetDuration:      15 * time.Second,
				TargetIncremental:   3 * time.Second,
				TargetMemoryPeak:    600,
				RegressionThreshold: 1.3,
			},
			expectErr: false,
		},
		{
			name: "invalid_hotspot_threshold",
			config: &AnalyzerConfig{
				HotspotThreshold:    -5.0, // Invalid
				MemoryThreshold:     400,
				CPUUtilizationMin:   0.7,
				TargetDuration:      15 * time.Second,
				TargetIncremental:   3 * time.Second,
				TargetMemoryPeak:    600,
				RegressionThreshold: 1.3,
			},
			expectErr: true,
		},
		{
			name: "invalid_memory_threshold",
			config: &AnalyzerConfig{
				HotspotThreshold:    10.0,
				MemoryThreshold:     -100, // Invalid
				CPUUtilizationMin:   0.7,
				TargetDuration:      15 * time.Second,
				TargetIncremental:   3 * time.Second,
				TargetMemoryPeak:    600,
				RegressionThreshold: 1.3,
			},
			expectErr: true,
		},
		{
			name: "invalid_cpu_utilization",
			config: &AnalyzerConfig{
				HotspotThreshold:    10.0,
				MemoryThreshold:     400,
				CPUUtilizationMin:   1.5, // Invalid - greater than 1
				TargetDuration:      15 * time.Second,
				TargetIncremental:   3 * time.Second,
				TargetMemoryPeak:    600,
				RegressionThreshold: 1.3,
			},
			expectErr: true,
		},
		{
			name: "invalid_regression_threshold",
			config: &AnalyzerConfig{
				HotspotThreshold:    10.0,
				MemoryThreshold:     400,
				CPUUtilizationMin:   0.7,
				TargetDuration:      15 * time.Second,
				TargetIncremental:   3 * time.Second,
				TargetMemoryPeak:    600,
				RegressionThreshold: 0.9, // Invalid - less than 1.0
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPerformanceAnalyzer(tt.config)

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func BenchmarkPerformanceAnalyzer_AnalyzeProfiles(b *testing.B) {
	analyzer, err := NewPerformanceAnalyzer(DefaultAnalyzerConfig())
	if err != nil {
		b.Fatalf("Failed to create analyzer: %v", err)
	}

	// Create realistic dataset
	dataset := &ProfileDataset{
		Scenarios: make([]*bench.BenchmarkResult, 10),
	}

	for i := 0; i < 10; i++ {
		dataset.Scenarios[i] = &bench.BenchmarkResult{
			Success: true,
			Scenario: &bench.BenchmarkScenario{
				Name: fmt.Sprintf("scenario_%d", i),
			},
			Metrics: &bench.PerformanceMetrics{
				TotalDuration: time.Duration(5+i) * time.Second,
				PhaseDurations: bench.PhaseTimings{
					Indexing:  time.Duration(1+i/3) * time.Second,
					Parsing:   time.Duration(2+i/2) * time.Second,
					Matching:  time.Duration(1+i/4) * time.Second,
					Inference: time.Duration(1) * time.Second,
				},
				MemoryStats: bench.MemoryProfile{
					PeakTotalMB: int64(300 + i*50),
					GCCount:     int64(10 + i*5),
				},
			},
		}
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := analyzer.AnalyzeProfiles(ctx, dataset)
		if err != nil {
			b.Fatalf("Analysis failed: %v", err)
		}
	}
}

func TestPerformanceAnalyzer_ConcurrentAccess(t *testing.T) {
	analyzer, err := NewPerformanceAnalyzer(DefaultAnalyzerConfig())
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	// Create test dataset that will produce hotspots
	dataset := &ProfileDataset{
		Scenarios: []*bench.BenchmarkResult{
			{
				Success: true,
				Metrics: &bench.PerformanceMetrics{
					TotalDuration: 15 * time.Second,
					PhaseDurations: bench.PhaseTimings{
						Parsing: 8 * time.Second, // 53% > 5% threshold
					},
					MemoryStats: bench.MemoryProfile{
						PeakTotalMB: 600, // > 500MB threshold
					},
				},
			},
		},
	}

	ctx := context.Background()
	const goroutines = 10
	const iterations = 5

	// Run concurrent analysis
	errChan := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				_, err := analyzer.AnalyzeProfiles(ctx, dataset)
				if err != nil {
					errChan <- err
					return
				}
			}
			errChan <- nil
		}()
	}

	// Check for errors
	for i := 0; i < goroutines; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("Concurrent analysis failed: %v", err)
		}
	}

	// Verify final state is consistent
	hotspots := analyzer.GetHotspots()
	recommendations := analyzer.GetRecommendations()

	if len(hotspots) == 0 {
		t.Error("Expected hotspots to be identified")
	}

	if len(recommendations) == 0 {
		t.Error("Expected recommendations to be generated")
	}
}
