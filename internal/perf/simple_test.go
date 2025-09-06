package perf

import (
	"context"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/bench"
)

// TestMVPTargetsValidation validates MVP performance targets are properly defined.
func TestMVPTargetsValidation(t *testing.T) {
	targets := MVPPerformanceTargets()

	// Verify MVP targets match plan.md requirements
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

	// Verify throughput targets are reasonable for medium projects (200-600 files)
	if targets.FilesPerSecond < 20 { // Minimum 20 files/second for 600 files in 10s
		t.Errorf("Files per second target too low: %.1f", targets.FilesPerSecond)
	}

	if targets.PatternsPerSec <= 0 {
		t.Error("Patterns per second target should be positive")
	}

	// Verify quality targets
	if targets.ErrorRate >= 0.05 { // Should be less than 5%
		t.Errorf("Error rate target too high: %.1f%%", targets.ErrorRate*100)
	}

	if targets.CacheHitRate <= 0.8 { // Should be at least 80% for incremental runs
		t.Errorf("Cache hit rate target too low: %.1f%%", targets.CacheHitRate*100)
	}
}

// TestPerformanceAnalyzerConfiguration validates analyzer configuration.
func TestPerformanceAnalyzerConfiguration(t *testing.T) {
	config := DefaultAnalyzerConfig()

	// Verify MVP-aligned configuration
	if config.TargetDuration != 10*time.Second {
		t.Errorf("Default target duration should be 10s, got %v", config.TargetDuration)
	}

	if config.TargetIncremental != 2*time.Second {
		t.Errorf("Default incremental target should be 2s, got %v", config.TargetIncremental)
	}

	if config.TargetMemoryPeak != 500 {
		t.Errorf("Default memory target should be 500MB, got %d", config.TargetMemoryPeak)
	}

	// Verify thresholds are reasonable
	if config.HotspotThreshold <= 0 || config.HotspotThreshold > 50 {
		t.Errorf("Hotspot threshold should be reasonable: got %.1f%%", config.HotspotThreshold)
	}

	if config.CPUUtilizationMin < 0.5 || config.CPUUtilizationMin > 1.0 {
		t.Errorf("CPU utilization minimum should be between 50-100%%: got %.1f%%", config.CPUUtilizationMin*100)
	}
}

// TestMemoryOptimizerConfiguration validates memory optimizer configuration.
func TestMemoryOptimizerConfiguration(t *testing.T) {
	config := DefaultMemoryConfig()

	// Verify MVP-aligned memory limits
	if config.MaxHeapSizeMB > 500 { // Should not exceed MVP target
		t.Errorf("Max heap size should not exceed MVP target: got %dMB", config.MaxHeapSizeMB)
	}

	if config.MaxHeapSizeMB <= 0 {
		t.Error("Max heap size should be positive")
	}

	// Verify pool sizes are reasonable
	if config.StringBuilderPoolSize <= 0 {
		t.Error("String builder pool size should be positive")
	}

	if config.SlicePoolSize <= 0 {
		t.Error("Slice pool size should be positive")
	}

	if config.ASTNodePoolSize <= 0 {
		t.Error("AST node pool size should be positive")
	}

	// Verify streaming threshold is reasonable for large files
	if config.StreamingThresholdMB <= 0 || config.StreamingThresholdMB > 100 {
		t.Errorf("Streaming threshold should be reasonable: got %dMB", config.StreamingThresholdMB)
	}
}

// TestWorkerPoolConfiguration validates worker pool configuration.
func TestWorkerPoolConfiguration(t *testing.T) {
	config := DefaultWorkerPoolConfig()

	// Verify worker configuration is reasonable
	if config.MinWorkers <= 0 {
		t.Error("Minimum workers should be positive")
	}

	if config.MaxWorkers <= config.MinWorkers {
		t.Error("Maximum workers should be greater than minimum workers")
	}

	// Verify scaling thresholds
	if config.ScaleUpThreshold <= 0 || config.ScaleUpThreshold >= 1 {
		t.Errorf("Scale up threshold should be between 0-1: got %.2f", config.ScaleUpThreshold)
	}

	if config.ScaleDownThreshold <= 0 || config.ScaleDownThreshold >= config.ScaleUpThreshold {
		t.Errorf("Scale down threshold should be positive and less than scale up: got %.2f", config.ScaleDownThreshold)
	}

	// Verify queue capacity is sufficient
	if config.QueueCapacity <= 0 {
		t.Error("Queue capacity should be positive")
	}

	if config.ResultCapacity <= 0 {
		t.Error("Result capacity should be positive")
	}

	// Verify memory limits are aligned with MVP targets
	if config.MemoryLimitMB > 400 { // Leave room for other components
		t.Errorf("Worker pool memory limit should not exceed 400MB: got %dMB", config.MemoryLimitMB)
	}
}

// TestPerformanceAnalyzer_BasicFunctionality tests basic analyzer functionality.
func TestPerformanceAnalyzer_BasicFunctionality(t *testing.T) {
	analyzer, err := NewPerformanceAnalyzer(nil) // Use default config
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	// Test with minimal dataset
	dataset := &ProfileDataset{
		Scenarios:      []*bench.BenchmarkResult{},
		SystemMetrics:  []SystemSnapshot{},
		ProfileFiles:   []string{},
		Timestamp:      time.Now(),
		CollectionTime: 100 * time.Millisecond,
	}

	ctx := context.Background()
	results, err := analyzer.AnalyzeProfiles(ctx, dataset)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Basic structure validation
	if results == nil {
		t.Fatal("Results should not be nil")
	}

	if results.Timestamp.IsZero() {
		t.Error("Results timestamp should be set")
	}

	if results.AnalysisDuration <= 0 {
		t.Error("Analysis duration should be positive")
	}

	// Empty dataset should produce empty results
	if len(results.Hotspots) != 0 {
		t.Errorf("Expected 0 hotspots for empty dataset, got %d", len(results.Hotspots))
	}

	if len(results.Recommendations) != 0 {
		t.Errorf("Expected 0 recommendations for empty dataset, got %d", len(results.Recommendations))
	}
}

// TestMemoryOptimizer_BasicFunctionality tests basic memory optimizer functionality.
func TestMemoryOptimizer_BasicFunctionality(t *testing.T) {
	optimizer, err := NewMemoryOptimizer(nil) // Use default config
	if err != nil {
		t.Fatalf("Failed to create memory optimizer: %v", err)
	}

	// Test initial state
	optimizations := optimizer.GetOptimizations()
	if len(optimizations) == 0 {
		t.Error("Expected some initial optimizations to be applied")
	}

	// Test metrics
	metrics := optimizer.GetMetrics()
	if metrics == nil {
		t.Error("Metrics should be available")
	}

	// Test estimated savings calculation
	savings := optimizer.EstimateMemorySavings()
	if savings < 0 {
		t.Error("Memory savings should not be negative")
	}
}

// TestOptimizedWorkerPool_BasicFunctionality tests basic worker pool functionality.
func TestOptimizedWorkerPool_BasicFunctionality(t *testing.T) {
	config := DefaultWorkerPoolConfig()
	config.MaxWorkers = 2 // Small pool for testing
	config.MinWorkers = 1

	pool, err := NewOptimizedWorkerPool(config)
	if err != nil {
		t.Fatalf("Failed to create worker pool: %v", err)
	}

	// Test initial metrics
	metrics := pool.GetMetrics()
	if metrics == nil {
		t.Error("Metrics should be available")
		return
	}

	if metrics.StartTime.IsZero() {
		t.Error("Start time should be set")
	}

	// Test shutdown
	err = pool.Shutdown(1 * time.Second)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

// BenchmarkPerformanceAnalyzer_CreateAndAnalyze benchmarks the core analysis workflow.
func BenchmarkPerformanceAnalyzer_CreateAndAnalyze(b *testing.B) {
	config := DefaultAnalyzerConfig()

	// Create realistic test scenario
	dataset := &ProfileDataset{
		Scenarios: []*bench.BenchmarkResult{
			{
				Success: true,
				Scenario: &bench.BenchmarkScenario{
					Name: "benchmark_scenario",
				},
				Metrics: &bench.PerformanceMetrics{
					TotalDuration: 8 * time.Second,
					PhaseDurations: bench.PhaseTimings{
						Indexing:  1 * time.Second,
						Parsing:   3 * time.Second,
						Matching:  2 * time.Second,
						Inference: 1 * time.Second,
						Assembly:  1 * time.Second,
					},
					MemoryStats: bench.MemoryProfile{
						PeakTotalMB: 400,
						GCCount:     10,
					},
				},
			},
		},
		Timestamp: time.Now(),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		analyzer, err := NewPerformanceAnalyzer(config)
		if err != nil {
			b.Fatalf("Failed to create analyzer: %v", err)
		}

		ctx := context.Background()
		_, err = analyzer.AnalyzeProfiles(ctx, dataset)
		if err != nil {
			b.Fatalf("Analysis failed: %v", err)
		}
	}
}
