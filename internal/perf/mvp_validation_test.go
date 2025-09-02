package perf

import (
	"testing"
	"time"
)

// TestMVPPerformanceRequirements validates that the performance optimization system
// correctly defines and implements the MVP requirements from plan.md.
func TestMVPPerformanceRequirements(t *testing.T) {
	// Test 1: Verify performance targets match MVP requirements
	t.Run("performance_targets", func(t *testing.T) {
		targets := MVPPerformanceTargets()

		// Cold run target: <10s for 200-600 file projects
		if targets.ColdRun != 10*time.Second {
			t.Errorf("Cold run target should be 10s, got %v", targets.ColdRun)
		}

		// Incremental run target: <2s with warm cache
		if targets.IncrementalRun != 2*time.Second {
			t.Errorf("Incremental run target should be 2s, got %v", targets.IncrementalRun)
		}

		// Memory peak target: <500MB for medium projects
		if targets.MemoryPeak != 500 {
			t.Errorf("Memory peak target should be 500MB, got %d", targets.MemoryPeak)
		}

		// CPU efficiency target: >80% useful CPU time
		if targets.CPUEfficiency != 0.8 {
			t.Errorf("CPU efficiency target should be 80%%, got %.1f%%", targets.CPUEfficiency*100)
		}

		// Verify throughput targets are achievable
		// For medium project (400 files), need >40 files/second to meet 10s target
		expectedMinThroughput := 40.0 // 400 files / 10 seconds
		if targets.FilesPerSecond < expectedMinThroughput {
			t.Errorf("Files per second target too low: %.1f (minimum needed: %.1f)", 
				targets.FilesPerSecond, expectedMinThroughput)
		}

		// Verify cache hit rate is suitable for incremental performance
		if targets.CacheHitRate < 0.9 {
			t.Errorf("Cache hit rate should be at least 90%% for 2s incremental target, got %.1f%%", 
				targets.CacheHitRate*100)
		}

		// Verify error rate is acceptable
		if targets.ErrorRate > 0.01 {
			t.Errorf("Error rate should be at most 1%%, got %.1f%%", targets.ErrorRate*100)
		}
	})

	// Test 2: Verify analyzer configuration supports MVP targets
	t.Run("analyzer_configuration", func(t *testing.T) {
		config := DefaultAnalyzerConfig()

		// Should use MVP targets
		if config.TargetDuration != 10*time.Second {
			t.Errorf("Analyzer target duration should match MVP: 10s, got %v", config.TargetDuration)
		}

		if config.TargetIncremental != 2*time.Second {
			t.Errorf("Analyzer incremental target should match MVP: 2s, got %v", config.TargetIncremental)
		}

		if config.TargetMemoryPeak != 500 {
			t.Errorf("Analyzer memory target should match MVP: 500MB, got %d", config.TargetMemoryPeak)
		}

		// Verify thresholds will identify MVP-blocking issues
		if config.HotspotThreshold > 10.0 {
			t.Errorf("Hotspot threshold too high for MVP detection: %.1f%% (should be ≤10%%)", config.HotspotThreshold)
		}

		if config.MemoryThreshold > 500 {
			t.Errorf("Memory threshold should not exceed MVP target: %dMB", config.MemoryThreshold)
		}

		if config.CPUUtilizationMin < 0.8 {
			t.Errorf("CPU utilization minimum should support MVP efficiency target: %.1f%% (should be ≥80%%)", 
				config.CPUUtilizationMin*100)
		}

		// Verify regression detection is sensitive enough for MVP
		if config.RegressionThreshold > 1.5 {
			t.Errorf("Regression threshold too permissive for MVP: %.1fx (should be ≤1.5x)", config.RegressionThreshold)
		}
	})

	// Test 3: Verify memory configuration supports MVP targets
	t.Run("memory_configuration", func(t *testing.T) {
		config := DefaultMemoryConfig()

		// Memory limit should not exceed MVP target
		if config.MaxHeapSizeMB > 500 {
			t.Errorf("Max heap size exceeds MVP target: %dMB (should be ≤500MB)", config.MaxHeapSizeMB)
		}

		// Should be configured for reasonable pool sizes
		if config.StringBuilderPoolSize <= 0 {
			t.Error("String builder pool should be enabled for performance")
		}

		if config.SlicePoolSize <= 0 {
			t.Error("Slice pool should be enabled for performance")
		}

		if config.ASTNodePoolSize <= 0 {
			t.Error("AST node pool should be enabled for tree-sitter performance")
		}

		// Streaming should be enabled for reasonable file sizes
		if config.StreamingThresholdMB <= 0 || config.StreamingThresholdMB > 50 {
			t.Errorf("Streaming threshold should be reasonable: %dMB", config.StreamingThresholdMB)
		}

		// GC tuning should be enabled for MVP performance
		if !config.EnableGCTuning {
			t.Error("GC tuning should be enabled for MVP performance targets")
		}
	})

	// Test 4: Verify worker pool configuration supports MVP targets
	t.Run("worker_pool_configuration", func(t *testing.T) {
		config := DefaultWorkerPoolConfig()

		// Should scale to utilize available CPU cores
		if config.MaxWorkers < 2 {
			t.Errorf("Max workers too low for parallel processing: %d", config.MaxWorkers)
		}

		// Should have reasonable scaling thresholds
		if config.ScaleUpThreshold <= 0.5 || config.ScaleUpThreshold >= 1.0 {
			t.Errorf("Scale up threshold should be between 50-99%%: %.1f%%", config.ScaleUpThreshold*100)
		}

		if config.ScaleDownThreshold >= config.ScaleUpThreshold {
			t.Error("Scale down threshold should be less than scale up threshold")
		}

		// Queue capacity should handle burst processing
		if config.QueueCapacity < 100 {
			t.Errorf("Queue capacity too small for burst processing: %d", config.QueueCapacity)
		}

		// Memory limit should leave room for other components
		if config.MemoryLimitMB > 400 {
			t.Errorf("Worker pool memory limit too high: %dMB (should be ≤400MB)", config.MemoryLimitMB)
		}
	})
}

// TestPerformanceSystemIntegration validates that all performance components can be created.
func TestPerformanceSystemIntegration(t *testing.T) {
	// Test 1: Performance analyzer creation
	t.Run("analyzer_creation", func(t *testing.T) {
		analyzer, err := NewPerformanceAnalyzer(nil)
		if err != nil {
			t.Fatalf("Failed to create performance analyzer: %v", err)
		}

		if analyzer == nil {
			t.Fatal("Analyzer should not be nil")
		}

		// Test basic functionality
		hotspots := analyzer.GetHotspots()
		if hotspots == nil {
			t.Error("Hotspots slice should not be nil")
		}

		recommendations := analyzer.GetRecommendations()
		if recommendations == nil {
			t.Error("Recommendations slice should not be nil")
		}
	})

	// Test 2: Performance integration creation  
	t.Run("integration_creation", func(t *testing.T) {
		integration, err := NewPerformanceIntegration(nil)
		if err != nil {
			t.Fatalf("Failed to create performance integration: %v", err)
		}

		if integration == nil {
			t.Fatal("Integration should not be nil")
		}

		// Verify all components are initialized
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

// TestPerformanceTargetsCalculation validates performance target calculations.
func TestPerformanceTargetsCalculation(t *testing.T) {
	targets := MVPPerformanceTargets()

	// Test medium project scenario (400 files)
	projectFiles := 400
	
	// Calculate required throughput for cold run target
	requiredThroughput := float64(projectFiles) / targets.ColdRun.Seconds()
	if targets.FilesPerSecond < requiredThroughput {
		t.Errorf("Files per second target insufficient for medium project: %.1f < %.1f", 
			targets.FilesPerSecond, requiredThroughput)
	}

	// Incremental run should be much faster (assuming 90% cache hit rate)
	uncachedFiles := float64(projectFiles) * (1.0 - targets.CacheHitRate)
	requiredIncrementalThroughput := uncachedFiles / targets.IncrementalRun.Seconds()
	
	// Should be achievable with good caching
	if requiredIncrementalThroughput > targets.FilesPerSecond*2 {
		t.Errorf("Incremental throughput requirement too high: %.1f files/sec", requiredIncrementalThroughput)
	}

	// Memory per file should be reasonable
	memoryPerFile := float64(targets.MemoryPeak) / float64(projectFiles)
	if memoryPerFile > 2.0 { // More than 2MB per file seems excessive
		t.Errorf("Memory per file too high: %.2fMB", memoryPerFile)
	}
}

// TestPerformanceOptimizationTypes validates optimization type definitions.
func TestPerformanceOptimizationTypes(t *testing.T) {
	// Test recommendation types are defined
	types := []RecommendationType{
		RecommendationMemoryOptimization,
		RecommendationConcurrencyImprovement,
		RecommendationCacheOptimization,
		RecommendationAlgorithmOptimization,
		RecommendationWorkerPoolTuning,
	}

	for _, recType := range types {
		if string(recType) == "" {
			t.Error("Recommendation type should not be empty")
		}
	}

	// Test severity levels are defined
	severities := []SeverityLevel{
		SeverityLow,
		SeverityMedium,
		SeverityHigh,
		SeverityCritical,
	}

	for _, severity := range severities {
		if string(severity) == "" {
			t.Error("Severity level should not be empty")
		}
	}

	// Test priorities are defined
	priorities := []Priority{
		PriorityLow,
		PriorityMedium,
		PriorityHigh,
		PriorityCritical,
	}

	for _, priority := range priorities {
		if string(priority) == "" {
			t.Error("Priority should not be empty")
		}
	}
}

// BenchmarkMVPTargetsCalculation benchmarks the performance targets calculation.
func BenchmarkMVPTargetsCalculation(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		targets := MVPPerformanceTargets()
		
		// Basic validation to ensure the function works
		if targets.ColdRun == 0 {
			b.Fatal("Cold run target should not be zero")
		}
	}
}

// TestDeterministicPerformanceAnalysis validates that performance analysis is deterministic.
func TestDeterministicPerformanceAnalysis(t *testing.T) {
	config := DefaultAnalyzerConfig()

	// Create two identical analyzers
	analyzer1, err := NewPerformanceAnalyzer(config)
	if err != nil {
		t.Fatalf("Failed to create first analyzer: %v", err)
	}

	analyzer2, err := NewPerformanceAnalyzer(config)
	if err != nil {
		t.Fatalf("Failed to create second analyzer: %v", err)
	}

	// Both should have identical configurations
	if analyzer1.config.TargetDuration != analyzer2.config.TargetDuration {
		t.Error("Analyzers should have identical target durations")
	}

	if analyzer1.config.TargetMemoryPeak != analyzer2.config.TargetMemoryPeak {
		t.Error("Analyzers should have identical memory targets")
	}

	if analyzer1.config.HotspotThreshold != analyzer2.config.HotspotThreshold {
		t.Error("Analyzers should have identical hotspot thresholds")
	}
}

// TestPerformanceSystemResourceUsage validates the performance system itself is efficient.
func TestPerformanceSystemResourceUsage(t *testing.T) {
	start := time.Now()

	// Create performance system components
	analyzer, err := NewPerformanceAnalyzer(DefaultAnalyzerConfig())
	if err != nil {
		t.Fatalf("Failed to create analyzer: %v", err)
	}

	integration, err := NewPerformanceIntegration(DefaultIntegrationConfig())
	if err != nil {
		t.Fatalf("Failed to create integration: %v", err)
	}

	creationTime := time.Since(start)

	// Performance system creation should be fast
	if creationTime > 100*time.Millisecond {
		t.Errorf("Performance system creation too slow: %v", creationTime)
	}

	// Verify components are functional
	if analyzer == nil {
		t.Error("Analyzer should be created")
	}

	if integration == nil {
		t.Error("Integration should be created")
	}

	// Verify targets are set correctly
	if integration.targets == nil {
		t.Error("Performance targets should be initialized")
	}

	if integration.targets.ColdRun != 10*time.Second {
		t.Error("Integration should use correct MVP targets")
	}
}