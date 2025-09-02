// Package bench provides performance benchmarking infrastructure for the Oxinfer pipeline.
// It integrates with existing stats system to collect and export performance metrics.
package bench

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/garaekz/oxinfer/internal/stats"
)

// PerformanceMetrics aggregates comprehensive performance data for benchmark analysis.
type PerformanceMetrics struct {
	// Scenario information
	ScenarioName string    `json:"scenarioName"`
	ScenarioType string    `json:"scenarioType"`
	Timestamp    time.Time `json:"timestamp"`

	// Execution timing
	TotalDuration  time.Duration `json:"totalDurationMs"`
	PhaseDurations PhaseTimings  `json:"phaseDurations"`

	// Memory metrics
	MemoryStats MemoryProfile `json:"memoryStats"`

	// Processing metrics
	ProcessingStats *stats.ProcessingStats `json:"processingStats"`

	// Performance targets
	TargetsMet TargetResults `json:"targetsMet"`

	// System metrics
	SystemInfo SystemMetrics `json:"systemInfo"`

	// Regression detection
	BaselineComparison *RegressionAnalysis `json:"baselineComparison,omitempty"`
}

// PhaseTimings tracks duration of each pipeline phase for performance analysis.
type PhaseTimings struct {
	Initialization time.Duration `json:"initializationMs"`
	Indexing       time.Duration `json:"indexingMs"`
	Parsing        time.Duration `json:"parsingMs"`
	Matching       time.Duration `json:"matchingMs"`
	Inference      time.Duration `json:"inferenceMs"`
	Assembly       time.Duration `json:"assemblyMs"`

	// Breakdown of major components
	FileDiscovery  time.Duration `json:"fileDiscoveryMs,omitempty"`
	CacheOps       time.Duration `json:"cacheOpsMs,omitempty"`
	TreeSitterOps  time.Duration `json:"treeSitterMs,omitempty"`
	PatternMatches time.Duration `json:"patternMatchesMs,omitempty"`
	ShapeInference time.Duration `json:"shapeInferenceMs,omitempty"`
}

// MemoryProfile captures memory usage patterns throughout benchmark execution.
type MemoryProfile struct {
	// Peak memory usage
	PeakHeapMB  int64 `json:"peakHeapMB"`
	PeakStackMB int64 `json:"peakStackMB"`
	PeakTotalMB int64 `json:"peakTotalMB"`

	// Memory efficiency metrics
	AllocationsCount int64 `json:"allocationsCount"`
	GCCount          int64 `json:"gcCount"`
	GCPauseTotal     int64 `json:"gcPauseTotalMs"`

	// Memory usage by phase
	PhaseMemoryPeaks map[string]int64 `json:"phaseMemoryPeaks"`

	// Memory growth patterns
	MemoryGrowthRate float64 `json:"memoryGrowthRate"` // MB/second
}

// TargetResults indicates whether performance targets were met.
type TargetResults struct {
	DurationTarget   bool `json:"durationTarget"`
	MemoryTarget     bool `json:"memoryTarget"`
	ThroughputTarget bool `json:"throughputTarget"`
	ErrorRateTarget  bool `json:"errorRateTarget"`

	// Detailed results
	ActualDuration time.Duration `json:"actualDurationMs"`
	TargetDuration time.Duration `json:"targetDurationMs"`
	ActualMemoryMB int64         `json:"actualMemoryMB"`
	TargetMemoryMB int64         `json:"targetMemoryMB"`
	FilesPerSecond float64       `json:"filesPerSecond"`
	ErrorRate      float64       `json:"errorRate"`
}

// SystemMetrics captures system-level performance context.
type SystemMetrics struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	GoVersion     string `json:"goVersion"`
	NumCPU        int    `json:"numCPU"`
	NumGoroutines int    `json:"numGoroutines"`
	CGOCalls      int64  `json:"cgoCallsCount"`

	// Resource constraints
	MaxOpenFiles   int64 `json:"maxOpenFiles,omitempty"`
	SystemMemoryMB int64 `json:"systemMemoryMB,omitempty"`
}

// RegressionAnalysis compares current performance against baseline metrics.
type RegressionAnalysis struct {
	BaselineVersion  string  `json:"baselineVersion"`
	DurationChange   float64 `json:"durationChangePercent"`   // Positive = slower
	MemoryChange     float64 `json:"memoryChangePercent"`     // Positive = more memory
	ThroughputChange float64 `json:"throughputChangePercent"` // Positive = faster

	IsRegression        bool    `json:"isRegression"`
	RegressionSeverity  string  `json:"regressionSeverity"` // "minor", "major", "critical"
	RegressionThreshold float64 `json:"regressionThreshold"`

	PhaseRegressions map[string]float64 `json:"phaseRegressions,omitempty"`
}

// MetricsCollector provides thread-safe collection of performance metrics.
type MetricsCollector struct {
	mu              sync.RWMutex
	scenario        *BenchmarkScenario
	startTime       time.Time
	phaseStartTimes map[string]time.Time
	phaseDurations  PhaseTimings
	memorySnapshots []MemorySnapshot
	memoryPeaks     map[string]int64
	baselineMetrics *PerformanceMetrics

	// System monitoring
	systemMetrics SystemMetrics
	runtimeStats  runtime.MemStats

	// Collection state
	collecting     bool
	collectionDone chan struct{}
}

// MemorySnapshot represents memory usage at a specific point in time.
type MemorySnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	HeapMB    int64     `json:"heapMB"`
	StackMB   int64     `json:"stackMB"`
	TotalMB   int64     `json:"totalMB"`
	Phase     string    `json:"phase"`
	GCCount   int64     `json:"gcCount"`
}

// NewMetricsCollector creates a new metrics collector for a benchmark scenario.
func NewMetricsCollector(scenario *BenchmarkScenario) (*MetricsCollector, error) {
	if scenario == nil {
		return nil, fmt.Errorf("scenario cannot be nil")
	}

	collector := &MetricsCollector{
		scenario:        scenario,
		phaseStartTimes: make(map[string]time.Time),
		memorySnapshots: make([]MemorySnapshot, 0, 100),
		memoryPeaks:     make(map[string]int64),
		collectionDone:  make(chan struct{}),
		systemMetrics:   captureSystemMetrics(),
	}

	return collector, nil
}

// StartCollection begins performance metrics collection.
func (mc *MetricsCollector) StartCollection() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.collecting {
		return fmt.Errorf("collection already started")
	}

	mc.startTime = time.Now()
	mc.collecting = true

	// Start memory monitoring goroutine
	go mc.monitorMemory()

	return nil
}

// StartPhase records the start of a pipeline phase.
func (mc *MetricsCollector) StartPhase(phase string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.phaseStartTimes[phase] = time.Now()
	mc.recordMemorySnapshot(phase + "_start")
}

// EndPhase records the end of a pipeline phase.
func (mc *MetricsCollector) EndPhase(phase string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if startTime, exists := mc.phaseStartTimes[phase]; exists {
		duration := time.Since(startTime)
		mc.setPhaseDuration(phase, duration)
		mc.recordMemorySnapshot(phase + "_end")
		delete(mc.phaseStartTimes, phase)
	}
}

// StopCollection ends metrics collection and returns final metrics.
func (mc *MetricsCollector) StopCollection(processingStats *stats.ProcessingStats) (*PerformanceMetrics, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if !mc.collecting {
		return nil, fmt.Errorf("collection not started")
	}

	mc.collecting = false
	close(mc.collectionDone)

	totalDuration := time.Since(mc.startTime)

	// Calculate memory profile
	memProfile := mc.calculateMemoryProfile()

	// Calculate target results
	targetResults := mc.calculateTargetResults(totalDuration, memProfile)

	// Build final metrics
	metrics := &PerformanceMetrics{
		ScenarioName:    mc.scenario.Name,
		ScenarioType:    string(mc.scenario.ScenarioType),
		Timestamp:       mc.startTime,
		TotalDuration:   totalDuration,
		PhaseDurations:  mc.phaseDurations,
		MemoryStats:     memProfile,
		ProcessingStats: processingStats,
		TargetsMet:      targetResults,
		SystemInfo:      mc.systemMetrics,
	}

	// Add regression analysis if baseline is available
	if mc.baselineMetrics != nil {
		regression := mc.calculateRegressionAnalysis(metrics)
		metrics.BaselineComparison = regression
	}

	return metrics, nil
}

// SetBaseline sets baseline metrics for regression analysis.
func (mc *MetricsCollector) SetBaseline(baseline *PerformanceMetrics) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.baselineMetrics = baseline
}

// ExportMetrics exports metrics to JSON format for analysis and reporting.
func (mc *MetricsCollector) ExportMetrics(metrics *PerformanceMetrics) ([]byte, error) {
	if metrics == nil {
		return nil, fmt.Errorf("metrics cannot be nil")
	}

	// Create a copy for JSON marshaling with deterministic ordering
	type exportMetrics struct {
		*PerformanceMetrics
		ExportedAt time.Time `json:"exportedAt"`
	}

	export := &exportMetrics{
		PerformanceMetrics: metrics,
		ExportedAt:         time.Now(),
	}

	return json.MarshalIndent(export, "", "  ")
}

// Helper methods

// monitorMemory continuously monitors memory usage during collection.
func (mc *MetricsCollector) monitorMemory() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mc.recordMemorySnapshot("monitoring")
		case <-mc.collectionDone:
			return
		}
	}
}

// recordMemorySnapshot captures current memory usage.
func (mc *MetricsCollector) recordMemorySnapshot(phase string) {
	runtime.ReadMemStats(&mc.runtimeStats)

	heapMB := int64(mc.runtimeStats.HeapAlloc / 1024 / 1024)
	stackMB := int64(mc.runtimeStats.StackInuse / 1024 / 1024)
	totalMB := int64(mc.runtimeStats.Sys / 1024 / 1024)

	snapshot := MemorySnapshot{
		Timestamp: time.Now(),
		HeapMB:    heapMB,
		StackMB:   stackMB,
		TotalMB:   totalMB,
		Phase:     phase,
		GCCount:   int64(mc.runtimeStats.NumGC),
	}

	mc.memorySnapshots = append(mc.memorySnapshots, snapshot)

	// Track phase memory peaks
	if existing, exists := mc.memoryPeaks[phase]; !exists || totalMB > existing {
		mc.memoryPeaks[phase] = totalMB
	}
}

// setPhaseDuration sets the duration for a specific phase.
func (mc *MetricsCollector) setPhaseDuration(phase string, duration time.Duration) {
	switch phase {
	case "initialization":
		mc.phaseDurations.Initialization = duration
	case "indexing":
		mc.phaseDurations.Indexing = duration
	case "parsing":
		mc.phaseDurations.Parsing = duration
	case "matching":
		mc.phaseDurations.Matching = duration
	case "inference":
		mc.phaseDurations.Inference = duration
	case "assembly":
		mc.phaseDurations.Assembly = duration
	case "file_discovery":
		mc.phaseDurations.FileDiscovery = duration
	case "cache_ops":
		mc.phaseDurations.CacheOps = duration
	case "tree_sitter_ops":
		mc.phaseDurations.TreeSitterOps = duration
	case "pattern_matches":
		mc.phaseDurations.PatternMatches = duration
	case "shape_inference":
		mc.phaseDurations.ShapeInference = duration
	}
}

// calculateMemoryProfile analyzes memory usage patterns.
func (mc *MetricsCollector) calculateMemoryProfile() MemoryProfile {
	if len(mc.memorySnapshots) == 0 {
		return MemoryProfile{}
	}

	var peakHeap, peakStack, peakTotal int64
	var maxGC int64

	for _, snapshot := range mc.memorySnapshots {
		if snapshot.HeapMB > peakHeap {
			peakHeap = snapshot.HeapMB
		}
		if snapshot.StackMB > peakStack {
			peakStack = snapshot.StackMB
		}
		if snapshot.TotalMB > peakTotal {
			peakTotal = snapshot.TotalMB
		}
		if snapshot.GCCount > maxGC {
			maxGC = snapshot.GCCount
		}
	}

	// Calculate memory growth rate
	growthRate := 0.0
	if len(mc.memorySnapshots) > 1 {
		first := mc.memorySnapshots[0]
		last := mc.memorySnapshots[len(mc.memorySnapshots)-1]
		timeDiff := last.Timestamp.Sub(first.Timestamp).Seconds()
		memDiff := float64(last.TotalMB - first.TotalMB)
		if timeDiff > 0 {
			growthRate = memDiff / timeDiff
		}
	}

	// Sort phase memory peaks for deterministic output
	phaseNames := make([]string, 0, len(mc.memoryPeaks))
	for phase := range mc.memoryPeaks {
		phaseNames = append(phaseNames, phase)
	}
	sort.Strings(phaseNames)

	sortedPhasePeaks := make(map[string]int64, len(mc.memoryPeaks))
	for _, phase := range phaseNames {
		sortedPhasePeaks[phase] = mc.memoryPeaks[phase]
	}

	return MemoryProfile{
		PeakHeapMB:       peakHeap,
		PeakStackMB:      peakStack,
		PeakTotalMB:      peakTotal,
		AllocationsCount: int64(mc.runtimeStats.TotalAlloc / 1024 / 1024),
		GCCount:          maxGC,
		GCPauseTotal:     int64(mc.runtimeStats.PauseTotalNs / 1000000), // Convert to ms
		PhaseMemoryPeaks: sortedPhasePeaks,
		MemoryGrowthRate: growthRate,
	}
}

// calculateTargetResults determines if performance targets were met.
func (mc *MetricsCollector) calculateTargetResults(totalDuration time.Duration, memProfile MemoryProfile) TargetResults {
	durationMet := totalDuration <= mc.scenario.MaxDuration
	memoryMet := memProfile.PeakTotalMB <= mc.scenario.MaxMemoryMB

	// Calculate throughput (files per second)
	filesPerSec := 0.0
	if totalDuration.Seconds() > 0 {
		filesPerSec = float64(mc.scenario.FileCount) / totalDuration.Seconds()
	}

	// For MVP targets, we expect at least 20 files/sec for medium projects
	throughputTarget := filesPerSec >= 20.0
	if mc.scenario.FileCount < 100 {
		// Smaller projects should process faster
		throughputTarget = filesPerSec >= 50.0
	}

	// Error rate calculation would need processing stats
	errorRate := 0.0
	errorRateMet := errorRate <= 0.05 // 5% max error rate

	return TargetResults{
		DurationTarget:   durationMet,
		MemoryTarget:     memoryMet,
		ThroughputTarget: throughputTarget,
		ErrorRateTarget:  errorRateMet,
		ActualDuration:   totalDuration,
		TargetDuration:   mc.scenario.MaxDuration,
		ActualMemoryMB:   memProfile.PeakTotalMB,
		TargetMemoryMB:   mc.scenario.MaxMemoryMB,
		FilesPerSecond:   filesPerSec,
		ErrorRate:        errorRate,
	}
}

// calculateRegressionAnalysis compares current metrics against baseline.
func (mc *MetricsCollector) calculateRegressionAnalysis(current *PerformanceMetrics) *RegressionAnalysis {
	if mc.baselineMetrics == nil {
		return nil
	}

	baseline := mc.baselineMetrics

	// Calculate percentage changes
	durationChange := calculatePercentChange(
		baseline.TotalDuration.Seconds(),
		current.TotalDuration.Seconds(),
	)

	memoryChange := calculatePercentChange(
		float64(baseline.MemoryStats.PeakTotalMB),
		float64(current.MemoryStats.PeakTotalMB),
	)

	throughputChange := calculatePercentChange(
		baseline.TargetsMet.FilesPerSecond,
		current.TargetsMet.FilesPerSecond,
	) * -1 // Invert because higher throughput is better

	// Determine if this is a regression
	regressionThreshold := 10.0 // 10% threshold
	isRegression := durationChange > regressionThreshold || memoryChange > regressionThreshold

	severity := "none"
	if isRegression {
		if durationChange > 25.0 || memoryChange > 25.0 {
			severity = "critical"
		} else if durationChange > 15.0 || memoryChange > 15.0 {
			severity = "major"
		} else {
			severity = "minor"
		}
	}

	return &RegressionAnalysis{
		BaselineVersion:     "baseline", // TODO: Add version tracking
		DurationChange:      durationChange,
		MemoryChange:        memoryChange,
		ThroughputChange:    throughputChange,
		IsRegression:        isRegression,
		RegressionSeverity:  severity,
		RegressionThreshold: regressionThreshold,
	}
}

// calculatePercentChange calculates the percentage change between two values.
func calculatePercentChange(baseline, current float64) float64 {
	if baseline == 0 {
		if current == 0 {
			return 0
		}
		return 100.0 // 100% increase from zero
	}

	return ((current - baseline) / baseline) * 100.0
}

// captureSystemMetrics captures current system information.
func captureSystemMetrics() SystemMetrics {
	return SystemMetrics{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		NumCPU:        runtime.NumCPU(),
		NumGoroutines: runtime.NumGoroutine(),
		CGOCalls:      runtime.NumCgoCall(),
	}
}
