// Package bench provides performance benchmarking infrastructure for the Oxinfer pipeline.
// The runner orchestrates benchmark execution with proper cache management and metrics collection.
package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
	"github.com/garaekz/oxinfer/internal/pipeline"
	"github.com/garaekz/oxinfer/internal/stats"
)

// BenchmarkRunner orchestrates performance testing across realistic scenarios.
type BenchmarkRunner struct {
	config           *RunnerConfig
	scenarios        []*BenchmarkScenario
	generator        ScenarioGenerator
	orchestrator     pipeline.PipelineOrchestrator
	metricsCollector *MetricsCollector
	baselineMetrics  map[string]*PerformanceMetrics

	// State management
	mu              sync.RWMutex
	running         bool
	results         []*BenchmarkResult
	currentScenario string
}

// RunnerConfig contains configuration for benchmark execution.
type RunnerConfig struct {
	// Test environment
	TempDir         string `json:"tempDir"`
	CleanupAfterRun bool   `json:"cleanupAfterRun"`
	KeepFailures    bool   `json:"keepFailures"`

	// Execution settings
	WarmupRuns        int           `json:"warmupRuns"`
	BenchmarkRuns     int           `json:"benchmarkRuns"`
	TimeoutPerRun     time.Duration `json:"timeoutPerRunMs"`
	ParallelScenarios bool          `json:"parallelScenarios"`

	// Cache management
	ClearCacheBetween bool `json:"clearCacheBetween"`
	WarmCacheRuns     int  `json:"warmCacheRuns"`

	// Profiling
	EnableProfiling bool     `json:"enableProfiling"`
	ProfileDir      string   `json:"profileDir"`
	ProfileTypes    []string `json:"profileTypes"` // "cpu", "mem", "goroutine", "block"

	// Regression testing
	BaselineFile        string  `json:"baselineFile,omitempty"`
	SaveBaseline        bool    `json:"saveBaseline"`
	RegressionThreshold float64 `json:"regressionThreshold"`

	// Output configuration
	OutputDir       string   `json:"outputDir"`
	GenerateReports bool     `json:"generateReports"`
	ReportFormats   []string `json:"reportFormats"` // "json", "html", "csv"
}

// BenchmarkResult contains the results of executing a single benchmark scenario.
type BenchmarkResult struct {
	Scenario *BenchmarkScenario  `json:"scenario"`
	Metrics  *PerformanceMetrics `json:"metrics"`
	Success  bool                `json:"success"`
	Error    string              `json:"error,omitempty"`

	// Run details
	RunID     string        `json:"runId"`
	StartTime time.Time     `json:"startTime"`
	EndTime   time.Time     `json:"endTime"`
	Duration  time.Duration `json:"durationMs"`

	// Context
	CacheWarm bool `json:"cacheWarm"`
	RunNumber int  `json:"runNumber"`
	WarmupRun bool `json:"warmupRun"`

	// Validation
	TargetsMet   bool     `json:"targetsMet"`
	Regression   bool     `json:"regression"`
	ProfileFiles []string `json:"profileFiles,omitempty"`
}

// RunSummary provides an aggregate view of benchmark execution results.
type RunSummary struct {
	StartTime          time.Time     `json:"startTime"`
	EndTime            time.Time     `json:"endTime"`
	TotalDuration      time.Duration `json:"totalDurationMs"`
	ScenariosRun       int           `json:"scenariosRun"`
	ScenariosSucceeded int           `json:"scenariosSucceeded"`
	ScenariosFailed    int           `json:"scenariosFailed"`

	// Target analysis
	AllTargetsMet  bool     `json:"allTargetsMet"`
	TargetFailures []string `json:"targetFailures,omitempty"`

	// Regression analysis
	RegressionsFound  int      `json:"regressionsFound"`
	RegressionDetails []string `json:"regressionDetails,omitempty"`

	// Performance summary
	FastestScenario string        `json:"fastestScenario"`
	SlowestScenario string        `json:"slowestScenario"`
	AverageDuration time.Duration `json:"averageDurationMs"`

	// Results by scenario
	ScenarioResults map[string]*BenchmarkResult `json:"scenarioResults"`
}

// NewBenchmarkRunner creates a new benchmark runner with the specified configuration.
func NewBenchmarkRunner(config *RunnerConfig) (*BenchmarkRunner, error) {
	if config == nil {
		return nil, fmt.Errorf("runner config cannot be nil")
	}

	if err := validateRunnerConfig(config); err != nil {
		return nil, fmt.Errorf("invalid runner configuration: %w", err)
	}

	// Ensure directories exist
	if err := ensureDirectories(config); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	// Create pipeline orchestrator
	pipelineConfig := pipeline.DefaultPipelineConfig()
	pipelineConfig.ProjectRoot = config.TempDir // Use temp dir as default project root
	orchestrator, err := pipeline.NewOrchestrator(pipelineConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline orchestrator: %w", err)
	}

	runner := &BenchmarkRunner{
		config:          config,
		scenarios:       make([]*BenchmarkScenario, 0),
		generator:       NewDefaultScenarioGenerator(),
		orchestrator:    orchestrator,
		baselineMetrics: make(map[string]*PerformanceMetrics),
		results:         make([]*BenchmarkResult, 0),
	}

	// Load baseline metrics if specified
	if config.BaselineFile != "" {
		if err := runner.loadBaseline(); err != nil {
			// Warn but don't fail - baseline is optional
			fmt.Printf("Warning: failed to load baseline metrics: %v\n", err)
		}
	}

	return runner, nil
}

// AddScenario adds a benchmark scenario to the runner.
func (br *BenchmarkRunner) AddScenario(scenario *BenchmarkScenario) error {
	if scenario == nil {
		return fmt.Errorf("scenario cannot be nil")
	}

	if err := ValidateScenario(scenario); err != nil {
		return fmt.Errorf("invalid scenario '%s': %w", scenario.Name, err)
	}

	br.mu.Lock()
	defer br.mu.Unlock()

	// Check for duplicate scenario names
	for _, existing := range br.scenarios {
		if existing.Name == scenario.Name {
			return fmt.Errorf("scenario '%s' already exists", scenario.Name)
		}
	}

	br.scenarios = append(br.scenarios, scenario)
	return nil
}

// AddAllMVPScenarios adds all standard MVP benchmark scenarios.
func (br *BenchmarkRunner) AddAllMVPScenarios() error {
	scenarios := MVPBenchmarkScenarios()

	for _, scenario := range scenarios {
		if err := br.AddScenario(scenario); err != nil {
			return fmt.Errorf("failed to add MVP scenario '%s': %w", scenario.Name, err)
		}
	}

	return nil
}

// RunAllScenarios executes all configured benchmark scenarios.
func (br *BenchmarkRunner) RunAllScenarios(ctx context.Context) (*RunSummary, error) {
	br.mu.Lock()
	if br.running {
		br.mu.Unlock()
		return nil, fmt.Errorf("benchmark runner is already running")
	}
	br.running = true
	defer func() {
		br.mu.Lock()
		br.running = false
		br.mu.Unlock()
	}()
	br.mu.Unlock()

	if len(br.scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios configured")
	}

	summary := &RunSummary{
		StartTime:       time.Now(),
		ScenarioResults: make(map[string]*BenchmarkResult),
	}

	// Sort scenarios for deterministic execution order
	scenarios := make([]*BenchmarkScenario, len(br.scenarios))
	copy(scenarios, br.scenarios)
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Name < scenarios[j].Name
	})

	// Execute scenarios
	if br.config.ParallelScenarios {
		if err := br.runScenariosParallel(ctx, scenarios, summary); err != nil {
			return nil, err
		}
	} else {
		if err := br.runScenariosSequential(ctx, scenarios, summary); err != nil {
			return nil, err
		}
	}

	// Finalize summary
	summary.EndTime = time.Now()
	summary.TotalDuration = summary.EndTime.Sub(summary.StartTime)
	br.calculateRunSummary(summary)

	// Generate reports if requested
	if br.config.GenerateReports {
		if err := br.generateReports(summary); err != nil {
			return summary, fmt.Errorf("report generation failed: %w", err)
		}
	}

	// Save baseline if requested
	if br.config.SaveBaseline {
		if err := br.saveBaseline(summary); err != nil {
			return summary, fmt.Errorf("baseline save failed: %w", err)
		}
	}

	return summary, nil
}

// RunScenario executes a single benchmark scenario.
func (br *BenchmarkRunner) RunScenario(ctx context.Context, scenario *BenchmarkScenario) (*BenchmarkResult, error) {
	if scenario == nil {
		return nil, fmt.Errorf("scenario cannot be nil")
	}

	br.mu.Lock()
	br.currentScenario = scenario.Name
	br.mu.Unlock()

	defer func() {
		br.mu.Lock()
		br.currentScenario = ""
		br.mu.Unlock()
	}()

	result := &BenchmarkResult{
		Scenario:  scenario,
		RunID:     generateRunID(scenario.Name),
		StartTime: time.Now(),
	}

	// Create scenario-specific context with timeout
	scenarioCtx, cancel := context.WithTimeout(ctx, br.config.TimeoutPerRun)
	defer cancel()

	// Setup test environment
	testDir, err := br.setupTestEnvironment(scenario)
	if err != nil {
		result.Error = fmt.Sprintf("environment setup failed: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	// Cleanup on completion
	defer func() {
		if br.config.CleanupAfterRun && (result.Success || !br.config.KeepFailures) {
			if cleanupErr := br.cleanupTestEnvironment(testDir); cleanupErr != nil {
				fmt.Printf("Warning: cleanup failed for %s: %v\n", scenario.Name, cleanupErr)
			}
		}
	}()

	// Execute benchmark runs
	var bestResult *BenchmarkResult
	var bestMetrics *PerformanceMetrics

	totalRuns := br.config.WarmupRuns + br.config.BenchmarkRuns

	for i := 0; i < totalRuns; i++ {
		isWarmup := i < br.config.WarmupRuns

		// Clear cache between runs if configured
		if br.config.ClearCacheBetween && i > 0 {
			if err := br.clearCache(testDir); err != nil {
				fmt.Printf("Warning: cache clear failed: %v\n", err)
			}
		}

		runResult, err := br.executeSingleRun(scenarioCtx, scenario, testDir, i+1, isWarmup)
		if err != nil {
			result.Error = fmt.Sprintf("run %d failed: %v", i+1, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil
		}

		// Keep the best non-warmup run
		if !isWarmup {
			if bestResult == nil || runResult.Metrics.TotalDuration < bestMetrics.TotalDuration {
				bestResult = runResult
				bestMetrics = runResult.Metrics
			}
		}
	}

	// Use the best run result
	if bestResult != nil {
		result = bestResult
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Success = result.Error == ""

	// Validate targets
	result.TargetsMet = result.Success && br.validateTargets(scenario, result.Metrics)

	// Check for regression
	if baseline, exists := br.baselineMetrics[scenario.Name]; exists && result.Success {
		result.Regression = br.detectRegression(baseline, result.Metrics)
	}

	return result, nil
}

// GetCurrentStatus returns the current status of the benchmark runner.
func (br *BenchmarkRunner) GetCurrentStatus() (bool, string, int, int) {
	br.mu.RLock()
	defer br.mu.RUnlock()

	return br.running, br.currentScenario, len(br.results), len(br.scenarios)
}

// Helper methods

// runScenariosSequential executes scenarios one after another.
func (br *BenchmarkRunner) runScenariosSequential(ctx context.Context, scenarios []*BenchmarkScenario, summary *RunSummary) error {
	for _, scenario := range scenarios {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result, err := br.RunScenario(ctx, scenario)
		if err != nil {
			return fmt.Errorf("scenario '%s' execution failed: %w", scenario.Name, err)
		}

		br.mu.Lock()
		br.results = append(br.results, result)
		br.mu.Unlock()

		summary.ScenarioResults[scenario.Name] = result
		summary.ScenariosRun++

		if result.Success {
			summary.ScenariosSucceeded++
		} else {
			summary.ScenariosFailed++
		}
	}

	return nil
}

// runScenariosParallel executes scenarios concurrently.
func (br *BenchmarkRunner) runScenariosParallel(ctx context.Context, scenarios []*BenchmarkScenario, summary *RunSummary) error {
	results := make(chan *BenchmarkResult, len(scenarios))
	errors := make(chan error, len(scenarios))

	// Create a semaphore to limit concurrent scenarios
	maxConcurrent := 2 // Limit to avoid resource exhaustion
	if len(scenarios) < maxConcurrent {
		maxConcurrent = len(scenarios)
	}
	semaphore := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup

	for _, scenario := range scenarios {
		wg.Add(1)
		go func(s *BenchmarkScenario) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			result, err := br.RunScenario(ctx, s)
			if err != nil {
				errors <- fmt.Errorf("scenario '%s': %w", s.Name, err)
				return
			}

			results <- result
		}(scenario)
	}

	// Wait for all scenarios to complete
	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	// Collect results
	for result := range results {
		br.mu.Lock()
		br.results = append(br.results, result)
		br.mu.Unlock()

		summary.ScenarioResults[result.Scenario.Name] = result
		summary.ScenariosRun++

		if result.Success {
			summary.ScenariosSucceeded++
		} else {
			summary.ScenariosFailed++
		}
	}

	// Check for errors
	for err := range errors {
		return err
	}

	return nil
}

// executeSingleRun executes a single benchmark run for a scenario.
func (br *BenchmarkRunner) executeSingleRun(ctx context.Context, scenario *BenchmarkScenario, testDir string, runNumber int, warmup bool) (*BenchmarkResult, error) {
	// Create metrics collector
	collector, err := NewMetricsCollector(scenario)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics collector: %w", err)
	}

	// Set baseline for regression analysis
	if baseline, exists := br.baselineMetrics[scenario.Name]; exists {
		collector.SetBaseline(baseline)
	}

	// Create manifest for the test scenario
	manifestPath := filepath.Join(testDir, "manifest.json")
	manifest, err := br.createTestManifest(scenario, testDir, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create test manifest: %w", err)
	}

	// Start profiling if enabled
	profileFiles := make([]string, 0)
	if br.config.EnableProfiling {
		profiles, err := br.startProfiling(scenario.Name, runNumber)
		if err != nil {
			fmt.Printf("Warning: profiling start failed: %v\n", err)
		} else {
			profileFiles = profiles
		}
	}

	// Start metrics collection
	if err := collector.StartCollection(); err != nil {
		return nil, fmt.Errorf("failed to start metrics collection: %w", err)
	}

	// Configure pipeline for the test
	br.orchestrator.GetConfiguration().ProjectRoot = testDir
	br.orchestrator.GetConfiguration().CacheConfig.CacheEnabled = scenario.CacheEnabled

	// Execute pipeline phases with timing
	collector.StartPhase("initialization")
	// Pipeline already initialized
	collector.EndPhase("initialization")

	collector.StartPhase("total")
	delta, pipelineErr := br.orchestrator.ProcessProject(ctx, manifest)
	collector.EndPhase("total")

	// Stop profiling if enabled
	if br.config.EnableProfiling && len(profileFiles) > 0 {
		if err := br.stopProfiling(); err != nil {
			fmt.Printf("Warning: profiling stop failed: %v\n", err)
		}
	}

	// Collect pipeline statistics
	pipelineStats := br.orchestrator.GetStats()
	processingStats := convertPipelineStatsToProcessingStats(pipelineStats)

	// Stop metrics collection
	metrics, err := collector.StopCollection(processingStats)
	if err != nil {
		return nil, fmt.Errorf("failed to stop metrics collection: %w", err)
	}

	// Build result
	result := &BenchmarkResult{
		Scenario:     scenario,
		Metrics:      metrics,
		Success:      pipelineErr == nil && delta != nil,
		RunID:        generateRunID(scenario.Name),
		StartTime:    metrics.Timestamp,
		EndTime:      time.Now(),
		Duration:     metrics.TotalDuration,
		CacheWarm:    scenario.CacheEnabled,
		RunNumber:    runNumber,
		WarmupRun:    warmup,
		ProfileFiles: profileFiles,
	}

	if pipelineErr != nil {
		result.Error = pipelineErr.Error()
	}

	return result, nil
}

// Additional helper methods would go here...
// (setupTestEnvironment, cleanupTestEnvironment, createTestManifest,
//  validateTargets, detectRegression, etc.)

// validateRunnerConfig validates the runner configuration.
func validateRunnerConfig(config *RunnerConfig) error {
	if config.TempDir == "" {
		return fmt.Errorf("temp directory cannot be empty")
	}

	if config.BenchmarkRuns <= 0 {
		return fmt.Errorf("benchmark runs must be positive")
	}

	if config.TimeoutPerRun <= 0 {
		return fmt.Errorf("timeout per run must be positive")
	}

	return nil
}

// ensureDirectories creates required directories.
func ensureDirectories(config *RunnerConfig) error {
	dirs := []string{config.TempDir}

	if config.OutputDir != "" {
		dirs = append(dirs, config.OutputDir)
	}

	if config.ProfileDir != "" {
		dirs = append(dirs, config.ProfileDir)
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// generateRunID creates a unique run identifier.
func generateRunID(scenarioName string) string {
	return fmt.Sprintf("%s_%d", scenarioName, time.Now().UnixNano())
}

// Placeholder implementations for remaining methods
func (br *BenchmarkRunner) setupTestEnvironment(scenario *BenchmarkScenario) (string, error) {
	// TODO: Implement test environment setup
	return "", fmt.Errorf("not implemented")
}

func (br *BenchmarkRunner) cleanupTestEnvironment(testDir string) error {
	return os.RemoveAll(testDir)
}

func (br *BenchmarkRunner) clearCache(testDir string) error {
	// TODO: Implement cache clearing
	return nil
}

func (br *BenchmarkRunner) createTestManifest(scenario *BenchmarkScenario, testDir, manifestPath string) (*manifest.Manifest, error) {
	// TODO: Implement manifest creation
	return nil, fmt.Errorf("not implemented")
}

func (br *BenchmarkRunner) validateTargets(scenario *BenchmarkScenario, metrics *PerformanceMetrics) bool {
	return metrics.TargetsMet.DurationTarget && metrics.TargetsMet.MemoryTarget
}

func (br *BenchmarkRunner) detectRegression(baseline, current *PerformanceMetrics) bool {
	if current.BaselineComparison == nil {
		return false
	}
	return current.BaselineComparison.IsRegression
}

func (br *BenchmarkRunner) loadBaseline() error {
	// TODO: Implement baseline loading
	return nil
}

func (br *BenchmarkRunner) saveBaseline(summary *RunSummary) error {
	// TODO: Implement baseline saving
	return nil
}

func (br *BenchmarkRunner) generateReports(summary *RunSummary) error {
	// TODO: Implement report generation
	return nil
}

func (br *BenchmarkRunner) startProfiling(scenarioName string, runNumber int) ([]string, error) {
	// TODO: Implement profiling start
	return nil, nil
}

func (br *BenchmarkRunner) stopProfiling() error {
	// TODO: Implement profiling stop
	return nil
}

func (br *BenchmarkRunner) calculateRunSummary(summary *RunSummary) {
	// TODO: Implement summary calculation
}

func convertPipelineStatsToProcessingStats(pipelineStats *pipeline.PipelineStats) *stats.ProcessingStats {
	// TODO: Implement conversion
	return stats.NewProcessingStats()
}

// DefaultBenchmarkConfig returns a sensible default configuration for benchmark runs.
func DefaultBenchmarkConfig() *RunnerConfig {
	return &RunnerConfig{
		TempDir:             filepath.Join(os.TempDir(), "oxinfer_bench"),
		CleanupAfterRun:     true,
		KeepFailures:        false,
		WarmupRuns:          1,
		BenchmarkRuns:       3,
		TimeoutPerRun:       5 * time.Minute,
		ParallelScenarios:   false,
		ClearCacheBetween:   true,
		WarmCacheRuns:       1,
		EnableProfiling:     false,
		ProfileDir:          filepath.Join(os.TempDir(), "oxinfer_profiles"),
		ProfileTypes:        []string{"cpu", "mem"},
		RegressionThreshold: 10.0,
		OutputDir:           filepath.Join(".", "benchmark_results"),
		GenerateReports:     true,
		ReportFormats:       []string{"json"},
	}
}

// NewDefaultScenarioGenerator creates a default scenario generator.
func NewDefaultScenarioGenerator() ScenarioGenerator {
	// TODO: Implement default scenario generator
	return nil
}
