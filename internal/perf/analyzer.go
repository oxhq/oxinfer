// Package perf provides performance analysis and optimization capabilities for the Oxinfer pipeline.
// It integrates with the benchmarking infrastructure to identify hotspots and recommend optimizations.
package perf

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/garaekz/oxinfer/internal/bench"
)

// PerformanceAnalyzer identifies performance bottlenecks and provides optimization recommendations.
type PerformanceAnalyzer struct {
	config          *AnalyzerConfig
	profileData     *ProfileDataset
	regressionTests map[string]*bench.PerformanceMetrics

	// Analysis state
	mu              sync.RWMutex
	hotspots        []Hotspot
	recommendations []Recommendation
	baseline        *PerformanceBaseline
}

// AnalyzerConfig contains configuration for performance analysis.
type AnalyzerConfig struct {
	// Analysis settings
	HotspotThreshold  float64 `json:"hotspotThreshold"`  // Percentage of total time to qualify as hotspot
	MemoryThreshold   int64   `json:"memoryThreshold"`   // Memory usage in MB to flag as concern
	CPUUtilizationMin float64 `json:"cpuUtilizationMin"` // Minimum CPU utilization target

	// Target performance requirements
	TargetDuration    time.Duration `json:"targetDurationMs"`    // MVP: <10s for cold runs
	TargetIncremental time.Duration `json:"targetIncrementalMs"` // MVP: <2s for incremental runs
	TargetMemoryPeak  int64         `json:"targetMemoryPeakMB"`  // MVP: <500MB for medium projects

	// Regression detection
	RegressionThreshold float64 `json:"regressionThreshold"` // Threshold for performance regression detection
	BaselineComparisons bool    `json:"baselineComparisons"` // Enable baseline comparison analysis
}

// DefaultAnalyzerConfig returns configuration with MVP performance targets.
func DefaultAnalyzerConfig() *AnalyzerConfig {
	return &AnalyzerConfig{
		HotspotThreshold:    5.0, // 5% of total time
		MemoryThreshold:     500, // 500MB
		CPUUtilizationMin:   0.8, // 80% useful CPU time
		TargetDuration:      10 * time.Second,
		TargetIncremental:   2 * time.Second,
		TargetMemoryPeak:    500, // 500MB
		RegressionThreshold: 1.2, // 20% regression threshold
		BaselineComparisons: true,
	}
}

// ProfileDataset aggregates performance data from multiple sources.
type ProfileDataset struct {
	Scenarios      []*bench.BenchmarkResult `json:"scenarios"`
	SystemMetrics  []SystemSnapshot         `json:"systemMetrics"`
	ProfileFiles   []string                 `json:"profileFiles"`
	Timestamp      time.Time                `json:"timestamp"`
	CollectionTime time.Duration            `json:"collectionTimeMs"`
}

// SystemSnapshot captures system-level metrics at a point in time.
type SystemSnapshot struct {
	Timestamp      time.Time `json:"timestamp"`
	CPUPercent     float64   `json:"cpuPercent"`
	MemoryUsedMB   int64     `json:"memoryUsedMB"`
	GoroutineCount int       `json:"goroutineCount"`
	GCCount        int64     `json:"gcCount"`
	HeapSizeMB     int64     `json:"heapSizeMB"`
}

// Hotspot represents a performance bottleneck identified in the pipeline.
type Hotspot struct {
	Component      string        `json:"component"`      // Pipeline component (indexer, parser, etc.)
	Phase          string        `json:"phase"`          // Pipeline phase (indexing, parsing, etc.)
	Metric         string        `json:"metric"`         // Performance metric (time, memory, etc.)
	Value          float64       `json:"value"`          // Measured value
	PercentOfTotal float64       `json:"percentOfTotal"` // Percentage of total resource consumption
	Severity       SeverityLevel `json:"severity"`       // Hotspot severity level

	// Context
	FileCount      int           `json:"fileCount"`      // Number of files processed
	AveragePerFile time.Duration `json:"averagePerFile"` // Average time per file
	Description    string        `json:"description"`    // Human-readable description
}

// SeverityLevel indicates the severity of a performance hotspot.
type SeverityLevel string

const (
	SeverityLow      SeverityLevel = "low"
	SeverityMedium   SeverityLevel = "medium"
	SeverityHigh     SeverityLevel = "high"
	SeverityCritical SeverityLevel = "critical"
)

// Recommendation provides optimization advice based on analysis.
type Recommendation struct {
	ID          string             `json:"id"`
	Component   string             `json:"component"`
	Type        RecommendationType `json:"type"`
	Priority    Priority           `json:"priority"`
	Title       string             `json:"title"`
	Description string             `json:"description"`

	// Implementation details
	EstimatedImpact    float64  `json:"estimatedImpact"`    // Expected performance improvement (percentage)
	ImplementationCost string   `json:"implementationCost"` // Development effort estimate
	RequiredChanges    []string `json:"requiredChanges"`    // List of changes needed

	// Supporting data
	RelatedHotspots []string       `json:"relatedHotspots"` // Related hotspot IDs
	Metrics         map[string]any `json:"metrics,omitempty"`
}

// RecommendationType categorizes optimization recommendations.
type RecommendationType string

const (
	RecommendationMemoryOptimization     RecommendationType = "memory_optimization"
	RecommendationConcurrencyImprovement RecommendationType = "concurrency_improvement"
	RecommendationCacheOptimization      RecommendationType = "cache_optimization"
	RecommendationAlgorithmOptimization  RecommendationType = "algorithm_optimization"
	RecommendationWorkerPoolTuning       RecommendationType = "worker_pool_tuning"
)

// Priority indicates the priority level of a recommendation.
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

// PerformanceBaseline stores baseline performance metrics for regression detection.
type PerformanceBaseline struct {
	Version           string                               `json:"version"`
	Timestamp         time.Time                            `json:"timestamp"`
	ScenarioBaselines map[string]*bench.PerformanceMetrics `json:"scenarioBaselines"`
	SystemInfo        SystemInfo                           `json:"systemInfo"`
}

// SystemInfo captures system configuration for baseline comparison.
type SystemInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
	NumCPU    int    `json:"numCPU"`
	MemoryMB  int64  `json:"memoryMB"`
}

// NewPerformanceAnalyzer creates a new performance analyzer with the given configuration.
func NewPerformanceAnalyzer(config *AnalyzerConfig) (*PerformanceAnalyzer, error) {
	if config == nil {
		config = DefaultAnalyzerConfig()
	}

	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid analyzer config: %w", err)
	}

	return &PerformanceAnalyzer{
		config:          config,
		regressionTests: make(map[string]*bench.PerformanceMetrics),
		hotspots:        make([]Hotspot, 0),
		recommendations: make([]Recommendation, 0),
	}, nil
}

// AnalyzeProfiles performs comprehensive analysis on the provided performance data.
func (pa *PerformanceAnalyzer) AnalyzeProfiles(ctx context.Context, dataset *ProfileDataset) (*AnalysisResults, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	pa.profileData = dataset
	pa.hotspots = pa.hotspots[:0] // Clear previous results
	pa.recommendations = pa.recommendations[:0]

	start := time.Now()

	// Step 1: Identify performance hotspots
	if err := pa.identifyHotspots(ctx, dataset); err != nil {
		return nil, fmt.Errorf("hotspot identification failed: %w", err)
	}

	// Step 2: Generate optimization recommendations
	if err := pa.generateRecommendations(ctx, dataset); err != nil {
		return nil, fmt.Errorf("recommendation generation failed: %w", err)
	}

	// Step 3: Perform regression analysis if baseline available
	regressions := make([]RegressionResult, 0)
	if pa.baseline != nil && pa.config.BaselineComparisons {
		regressions = pa.detectRegressions(ctx, dataset)
	}

	// Step 4: Calculate performance scores
	targetsMet := pa.evaluateTargets(dataset)

	analysisTime := time.Since(start)

	return &AnalysisResults{
		Timestamp:        time.Now(),
		AnalysisDuration: analysisTime,
		Hotspots:         pa.hotspots,
		Recommendations:  pa.recommendations,
		Regressions:      regressions,
		TargetsMet:       targetsMet,
		SystemInfo:       pa.captureSystemInfo(),
		Summary:          pa.generateSummary(dataset, targetsMet),
	}, nil
}

// identifyHotspots analyzes performance data to identify bottlenecks.
func (pa *PerformanceAnalyzer) identifyHotspots(ctx context.Context, dataset *ProfileDataset) error {
	for _, scenario := range dataset.Scenarios {
		if !scenario.Success {
			continue // Skip failed scenarios
		}

		metrics := scenario.Metrics
		if metrics == nil {
			continue
		}

		// Analyze timing hotspots
		pa.analyzeTimingHotspots(scenario, metrics)

		// Analyze memory hotspots
		pa.analyzeMemoryHotspots(scenario, metrics)

		// Analyze concurrency hotspots
		pa.analyzeConcurrencyHotspots(scenario, metrics)
	}

	// Sort hotspots by severity and impact
	sort.Slice(pa.hotspots, func(i, j int) bool {
		return pa.hotspots[i].PercentOfTotal > pa.hotspots[j].PercentOfTotal
	})

	return nil
}

// analyzeTimingHotspots identifies time-based performance bottlenecks.
func (pa *PerformanceAnalyzer) analyzeTimingHotspots(scenario *bench.BenchmarkResult, metrics *bench.PerformanceMetrics) {
	total := metrics.TotalDuration
	phases := metrics.PhaseDurations

	// Define phase analysis
	phaseAnalysis := []struct {
		name      string
		duration  time.Duration
		component string
	}{
		{"indexing", phases.Indexing, "indexer"},
		{"parsing", phases.Parsing, "parser"},
		{"matching", phases.Matching, "matchers"},
		{"inference", phases.Inference, "infer"},
		{"assembly", phases.Assembly, "pipeline"},
	}

	for _, phase := range phaseAnalysis {
		if phase.duration == 0 {
			continue
		}

		percentage := (float64(phase.duration) / float64(total)) * 100
		if percentage >= pa.config.HotspotThreshold {
			hotspot := Hotspot{
				Component:      phase.component,
				Phase:          phase.name,
				Metric:         "duration",
				Value:          float64(phase.duration / time.Millisecond),
				PercentOfTotal: percentage,
				Severity:       pa.calculateSeverity(percentage),
				Description:    fmt.Sprintf("%s phase consuming %.1f%% of total execution time", phase.name, percentage),
			}

			if metrics.ProcessingStats != nil {
				hotspot.FileCount = int(metrics.ProcessingStats.FilesParsed)
				if hotspot.FileCount > 0 {
					hotspot.AveragePerFile = time.Duration(int64(phase.duration) / int64(hotspot.FileCount))
				}
			}

			pa.hotspots = append(pa.hotspots, hotspot)
		}
	}
}

// analyzeMemoryHotspots identifies memory-based performance bottlenecks.
func (pa *PerformanceAnalyzer) analyzeMemoryHotspots(scenario *bench.BenchmarkResult, metrics *bench.PerformanceMetrics) {
	memProfile := metrics.MemoryStats

	// Check peak memory usage
	if memProfile.PeakTotalMB > pa.config.MemoryThreshold {
		pa.hotspots = append(pa.hotspots, Hotspot{
			Component:      "memory",
			Phase:          "overall",
			Metric:         "peak_memory",
			Value:          float64(memProfile.PeakTotalMB),
			PercentOfTotal: (float64(memProfile.PeakTotalMB) / float64(pa.config.TargetMemoryPeak)) * 100,
			Severity:       pa.calculateMemorySeverity(memProfile.PeakTotalMB),
			Description:    fmt.Sprintf("Peak memory usage of %d MB exceeds threshold", memProfile.PeakTotalMB),
		})
	}

	// Analyze memory growth patterns
	if memProfile.MemoryGrowthRate > 0 {
		pa.hotspots = append(pa.hotspots, Hotspot{
			Component:      "memory",
			Phase:          "allocation",
			Metric:         "growth_rate",
			Value:          memProfile.MemoryGrowthRate,
			PercentOfTotal: 0, // Not time-based
			Severity:       pa.calculateGrowthSeverity(memProfile.MemoryGrowthRate),
			Description:    fmt.Sprintf("Memory growth rate of %.2f MB/s indicates potential leak", memProfile.MemoryGrowthRate),
		})
	}

	// Check GC pressure
	if memProfile.GCCount > 0 && metrics.TotalDuration > 0 {
		gcFrequency := float64(memProfile.GCCount) / metrics.TotalDuration.Seconds()
		if gcFrequency > 2.0 { // More than 2 GCs per second
			pa.hotspots = append(pa.hotspots, Hotspot{
				Component:      "memory",
				Phase:          "gc",
				Metric:         "gc_frequency",
				Value:          gcFrequency,
				PercentOfTotal: (float64(memProfile.GCPauseTotal) / float64(metrics.TotalDuration.Nanoseconds()/1e6)) * 100,
				Severity:       SeverityMedium,
				Description:    fmt.Sprintf("High GC frequency of %.1f GCs/second causing performance impact", gcFrequency),
			})
		}
	}
}

// analyzeConcurrencyHotspots identifies concurrency-related performance issues.
func (pa *PerformanceAnalyzer) analyzeConcurrencyHotspots(scenario *bench.BenchmarkResult, metrics *bench.PerformanceMetrics) {
	// For now, skip concurrency analysis until stats package provides the required fields
	// This would be implemented once worker pool metrics are available in the stats package
}

// generateRecommendations creates optimization recommendations based on identified hotspots.
func (pa *PerformanceAnalyzer) generateRecommendations(ctx context.Context, dataset *ProfileDataset) error {
	// Group hotspots by component and type
	hotspotsByComponent := make(map[string][]Hotspot)
	for _, hotspot := range pa.hotspots {
		hotspotsByComponent[hotspot.Component] = append(hotspotsByComponent[hotspot.Component], hotspot)
	}

	// Generate component-specific recommendations
	for component, hotspots := range hotspotsByComponent {
		pa.generateComponentRecommendations(component, hotspots)
	}

	// Sort recommendations by priority and estimated impact
	sort.Slice(pa.recommendations, func(i, j int) bool {
		priorityOrder := map[Priority]int{
			PriorityCritical: 4,
			PriorityHigh:     3,
			PriorityMedium:   2,
			PriorityLow:      1,
		}

		iPriority := priorityOrder[pa.recommendations[i].Priority]
		jPriority := priorityOrder[pa.recommendations[j].Priority]

		if iPriority != jPriority {
			return iPriority > jPriority
		}

		return pa.recommendations[i].EstimatedImpact > pa.recommendations[j].EstimatedImpact
	})

	return nil
}

// generateComponentRecommendations creates recommendations for a specific component.
func (pa *PerformanceAnalyzer) generateComponentRecommendations(component string, hotspots []Hotspot) {
	switch component {
	case "indexer":
		pa.generateIndexerRecommendations(hotspots)
	case "parser":
		pa.generateParserRecommendations(hotspots)
	case "matchers":
		pa.generateMatcherRecommendations(hotspots)
	case "memory":
		pa.generateMemoryRecommendations(hotspots)
	case "concurrency":
		pa.generateConcurrencyRecommendations(hotspots)
	}
}

// generateIndexerRecommendations creates optimization recommendations for the indexer.
func (pa *PerformanceAnalyzer) generateIndexerRecommendations(hotspots []Hotspot) {
	for _, hotspot := range hotspots {
		if hotspot.Severity >= SeverityMedium {
			pa.recommendations = append(pa.recommendations, Recommendation{
				ID:                 fmt.Sprintf("indexer_%s_%d", hotspot.Phase, len(pa.recommendations)),
				Component:          "indexer",
				Type:               RecommendationConcurrencyImprovement,
				Priority:           pa.hotspotToPriority(hotspot.Severity),
				Title:              "Optimize File Discovery Performance",
				Description:        "Implement concurrent directory scanning with worker pool to reduce indexing time",
				EstimatedImpact:    30.0, // 30% improvement estimate
				ImplementationCost: "Medium",
				RequiredChanges: []string{
					"Add concurrent directory traversal",
					"Implement file filtering pipeline",
					"Add progress reporting for large directories",
				},
				RelatedHotspots: []string{hotspot.Component + "_" + hotspot.Phase},
			})
		}
	}
}

// generateParserRecommendations creates optimization recommendations for the parser.
func (pa *PerformanceAnalyzer) generateParserRecommendations(hotspots []Hotspot) {
	for _, hotspot := range hotspots {
		if hotspot.Severity >= SeverityMedium {
			pa.recommendations = append(pa.recommendations, Recommendation{
				ID:                 fmt.Sprintf("parser_%s_%d", hotspot.Phase, len(pa.recommendations)),
				Component:          "parser",
				Type:               RecommendationAlgorithmOptimization,
				Priority:           pa.hotspotToPriority(hotspot.Severity),
				Title:              "Optimize Tree-sitter Query Performance",
				Description:        "Cache compiled queries and reuse AST nodes to reduce parsing overhead",
				EstimatedImpact:    25.0, // 25% improvement estimate
				ImplementationCost: "High",
				RequiredChanges: []string{
					"Implement query compilation caching",
					"Add AST node pooling",
					"Optimize tree-sitter query patterns",
				},
				RelatedHotspots: []string{hotspot.Component + "_" + hotspot.Phase},
			})
		}
	}
}

// generateMatcherRecommendations creates optimization recommendations for pattern matchers.
func (pa *PerformanceAnalyzer) generateMatcherRecommendations(hotspots []Hotspot) {
	for _, hotspot := range hotspots {
		if hotspot.Severity >= SeverityMedium {
			pa.recommendations = append(pa.recommendations, Recommendation{
				ID:                 fmt.Sprintf("matcher_%s_%d", hotspot.Phase, len(pa.recommendations)),
				Component:          "matchers",
				Type:               RecommendationCacheOptimization,
				Priority:           pa.hotspotToPriority(hotspot.Severity),
				Title:              "Implement Pattern Match Caching",
				Description:        "Cache pattern match results and implement parallel pattern matching",
				EstimatedImpact:    40.0, // 40% improvement estimate
				ImplementationCost: "Medium",
				RequiredChanges: []string{
					"Add pattern match result caching",
					"Implement parallel pattern processing",
					"Optimize regex compilation",
				},
				RelatedHotspots: []string{hotspot.Component + "_" + hotspot.Phase},
			})
		}
	}
}

// generateMemoryRecommendations creates memory optimization recommendations.
func (pa *PerformanceAnalyzer) generateMemoryRecommendations(hotspots []Hotspot) {
	for _, hotspot := range hotspots {
		switch hotspot.Metric {
		case "peak_memory":
			pa.recommendations = append(pa.recommendations, Recommendation{
				ID:                 fmt.Sprintf("memory_peak_%d", len(pa.recommendations)),
				Component:          "memory",
				Type:               RecommendationMemoryOptimization,
				Priority:           pa.hotspotToPriority(hotspot.Severity),
				Title:              "Reduce Peak Memory Usage",
				Description:        "Implement streaming processing and object pooling to reduce memory footprint",
				EstimatedImpact:    50.0, // 50% memory reduction estimate
				ImplementationCost: "High",
				RequiredChanges: []string{
					"Add streaming file processing",
					"Implement object pools for frequently allocated structures",
					"Add memory-mapped cache files",
				},
				RelatedHotspots: []string{"memory_peak"},
			})
		case "gc_frequency":
			pa.recommendations = append(pa.recommendations, Recommendation{
				ID:                 fmt.Sprintf("memory_gc_%d", len(pa.recommendations)),
				Component:          "memory",
				Type:               RecommendationMemoryOptimization,
				Priority:           PriorityHigh,
				Title:              "Reduce GC Pressure",
				Description:        "Optimize allocation patterns and reduce temporary object creation",
				EstimatedImpact:    20.0, // 20% GC reduction estimate
				ImplementationCost: "Medium",
				RequiredChanges: []string{
					"Reduce temporary allocations in hot paths",
					"Pre-allocate slices with known capacity",
					"Implement string builders for concatenation",
				},
				RelatedHotspots: []string{"memory_gc"},
			})
		}
	}
}

// generateConcurrencyRecommendations creates concurrency optimization recommendations.
func (pa *PerformanceAnalyzer) generateConcurrencyRecommendations(hotspots []Hotspot) {
	for _, hotspot := range hotspots {
		if hotspot.Metric == "cpu_efficiency" {
			pa.recommendations = append(pa.recommendations, Recommendation{
				ID:                 fmt.Sprintf("concurrency_%d", len(pa.recommendations)),
				Component:          "concurrency",
				Type:               RecommendationWorkerPoolTuning,
				Priority:           pa.hotspotToPriority(hotspot.Severity),
				Title:              "Optimize Worker Pool Configuration",
				Description:        "Tune worker pool size and implement better load balancing",
				EstimatedImpact:    35.0, // 35% efficiency improvement estimate
				ImplementationCost: "Medium",
				RequiredChanges: []string{
					"Implement dynamic worker pool sizing",
					"Add work-stealing queue for better load balancing",
					"Optimize task granularity",
				},
				RelatedHotspots: []string{"concurrency_cpu_efficiency"},
			})
		}
	}
}

// Helper methods for severity calculation and priority mapping
func (pa *PerformanceAnalyzer) calculateSeverity(percentage float64) SeverityLevel {
	switch {
	case percentage >= 30:
		return SeverityCritical
	case percentage >= 20:
		return SeverityHigh
	case percentage >= 10:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func (pa *PerformanceAnalyzer) calculateMemorySeverity(memoryMB int64) SeverityLevel {
	ratio := float64(memoryMB) / float64(pa.config.TargetMemoryPeak)
	switch {
	case ratio >= 2.0:
		return SeverityCritical
	case ratio >= 1.5:
		return SeverityHigh
	case ratio >= 1.2:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func (pa *PerformanceAnalyzer) calculateGrowthSeverity(growthRate float64) SeverityLevel {
	switch {
	case growthRate >= 100: // 100 MB/s
		return SeverityCritical
	case growthRate >= 50: // 50 MB/s
		return SeverityHigh
	case growthRate >= 10: // 10 MB/s
		return SeverityMedium
	default:
		return SeverityLow
	}
}


func (pa *PerformanceAnalyzer) hotspotToPriority(severity SeverityLevel) Priority {
	switch severity {
	case SeverityCritical:
		return PriorityCritical
	case SeverityHigh:
		return PriorityHigh
	case SeverityMedium:
		return PriorityMedium
	default:
		return PriorityLow
	}
}

// detectRegressions compares current performance against baseline.
func (pa *PerformanceAnalyzer) detectRegressions(ctx context.Context, dataset *ProfileDataset) []RegressionResult {
	regressions := make([]RegressionResult, 0)

	for _, scenario := range dataset.Scenarios {
		if !scenario.Success || scenario.Metrics == nil {
			continue
		}

		scenarioName := scenario.Scenario.Name
		baseline, exists := pa.baseline.ScenarioBaselines[scenarioName]
		if !exists {
			continue
		}

		current := scenario.Metrics

		// Check duration regression
		durationRatio := float64(current.TotalDuration) / float64(baseline.TotalDuration)
		if durationRatio > pa.config.RegressionThreshold {
			regressions = append(regressions, RegressionResult{
				Scenario:        scenarioName,
				Metric:          "duration",
				BaselineValue:   float64(baseline.TotalDuration / time.Millisecond),
				CurrentValue:    float64(current.TotalDuration / time.Millisecond),
				RegressionRatio: durationRatio,
				Severity:        pa.calculateRegressionSeverity(durationRatio),
			})
		}

		// Check memory regression
		memoryRatio := float64(current.MemoryStats.PeakTotalMB) / float64(baseline.MemoryStats.PeakTotalMB)
		if memoryRatio > pa.config.RegressionThreshold {
			regressions = append(regressions, RegressionResult{
				Scenario:        scenarioName,
				Metric:          "peak_memory",
				BaselineValue:   float64(baseline.MemoryStats.PeakTotalMB),
				CurrentValue:    float64(current.MemoryStats.PeakTotalMB),
				RegressionRatio: memoryRatio,
				Severity:        pa.calculateRegressionSeverity(memoryRatio),
			})
		}
	}

	return regressions
}

func (pa *PerformanceAnalyzer) calculateRegressionSeverity(ratio float64) SeverityLevel {
	switch {
	case ratio >= 2.0:
		return SeverityCritical
	case ratio >= 1.5:
		return SeverityHigh
	case ratio >= 1.3:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// evaluateTargets checks if performance targets are met.
func (pa *PerformanceAnalyzer) evaluateTargets(dataset *ProfileDataset) bench.TargetResults {
	targets := bench.TargetResults{}

	// Find the best performing scenario for each metric
	var bestDuration time.Duration = time.Hour // Start with a high value
	var bestMemory int64 = 1000000             // Start with a high value

	for _, scenario := range dataset.Scenarios {
		if !scenario.Success || scenario.Metrics == nil {
			continue
		}

		if scenario.Metrics.TotalDuration < bestDuration {
			bestDuration = scenario.Metrics.TotalDuration
		}

		if scenario.Metrics.MemoryStats.PeakTotalMB < bestMemory {
			bestMemory = scenario.Metrics.MemoryStats.PeakTotalMB
		}
	}

	// Evaluate targets
	targets.DurationTarget = bestDuration <= pa.config.TargetDuration
	targets.MemoryTarget = bestMemory <= pa.config.TargetMemoryPeak
	targets.ActualDuration = bestDuration
	targets.TargetDuration = pa.config.TargetDuration
	targets.ActualMemoryMB = bestMemory
	targets.TargetMemoryMB = pa.config.TargetMemoryPeak

	// Calculate throughput if we have file processing stats
	if len(dataset.Scenarios) > 0 && dataset.Scenarios[0].Success && dataset.Scenarios[0].Metrics != nil {
		if stats := dataset.Scenarios[0].Metrics.ProcessingStats; stats != nil {
			targets.FilesPerSecond = float64(stats.FilesParsed) / bestDuration.Seconds()
		}
	}

	return targets
}

// captureSystemInfo captures current system information.
func (pa *PerformanceAnalyzer) captureSystemInfo() SystemInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return SystemInfo{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		NumCPU:    runtime.NumCPU(),
		MemoryMB:  int64(memStats.Sys / 1024 / 1024),
	}
}

// generateSummary creates a human-readable summary of the analysis.
func (pa *PerformanceAnalyzer) generateSummary(dataset *ProfileDataset, targets bench.TargetResults) string {
	summary := "Performance Analysis Summary:\n"
	summary += fmt.Sprintf("- Scenarios analyzed: %d\n", len(dataset.Scenarios))
	summary += fmt.Sprintf("- Hotspots identified: %d\n", len(pa.hotspots))
	summary += fmt.Sprintf("- Recommendations generated: %d\n", len(pa.recommendations))
	summary += fmt.Sprintf("- Duration target met: %v (%.2fs vs %.2fs target)\n",
		targets.DurationTarget, targets.ActualDuration.Seconds(), pa.config.TargetDuration.Seconds())
	summary += fmt.Sprintf("- Memory target met: %v (%d MB vs %d MB target)\n",
		targets.MemoryTarget, targets.ActualMemoryMB, pa.config.TargetMemoryPeak)

	if len(pa.hotspots) > 0 {
		summary += fmt.Sprintf("- Top hotspot: %s (%s phase, %.1f%% of total time)\n",
			pa.hotspots[0].Component, pa.hotspots[0].Phase, pa.hotspots[0].PercentOfTotal)
	}

	if len(pa.recommendations) > 0 {
		summary += fmt.Sprintf("- Top recommendation: %s (%.0f%% estimated improvement)\n",
			pa.recommendations[0].Title, pa.recommendations[0].EstimatedImpact)
	}

	return summary
}

// SetBaseline sets the performance baseline for regression detection.
func (pa *PerformanceAnalyzer) SetBaseline(baseline *PerformanceBaseline) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	pa.baseline = baseline
}

// GetHotspots returns the identified performance hotspots.
func (pa *PerformanceAnalyzer) GetHotspots() []Hotspot {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	// Return a copy to prevent external modification
	hotspots := make([]Hotspot, len(pa.hotspots))
	copy(hotspots, pa.hotspots)
	return hotspots
}

// GetRecommendations returns the generated optimization recommendations.
func (pa *PerformanceAnalyzer) GetRecommendations() []Recommendation {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	// Return a copy to prevent external modification
	recommendations := make([]Recommendation, len(pa.recommendations))
	copy(recommendations, pa.recommendations)
	return recommendations
}

// validateConfig validates the analyzer configuration.
func validateConfig(config *AnalyzerConfig) error {
	if config.HotspotThreshold <= 0 || config.HotspotThreshold > 100 {
		return fmt.Errorf("hotspot threshold must be between 0 and 100")
	}

	if config.MemoryThreshold <= 0 {
		return fmt.Errorf("memory threshold must be positive")
	}

	if config.CPUUtilizationMin < 0 || config.CPUUtilizationMin > 1 {
		return fmt.Errorf("CPU utilization minimum must be between 0 and 1")
	}

	if config.TargetDuration <= 0 {
		return fmt.Errorf("target duration must be positive")
	}

	if config.TargetIncremental <= 0 {
		return fmt.Errorf("target incremental duration must be positive")
	}

	if config.RegressionThreshold <= 1.0 {
		return fmt.Errorf("regression threshold must be greater than 1.0")
	}

	return nil
}
