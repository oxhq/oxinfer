// Package bench provides performance benchmarking infrastructure for the Oxinfer pipeline.
// The runner orchestrates benchmark execution with proper cache management and metrics collection.
//go:build goexperiment.jsonv2

package bench

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/oxhq/oxinfer/internal/manifest"
	"github.com/oxhq/oxinfer/internal/pipeline"
	"github.com/oxhq/oxinfer/internal/stats"
)

// BenchmarkRunner orchestrates performance testing across realistic scenarios.
type BenchmarkRunner struct {
	config          *RunnerConfig
	scenarios       []*BenchmarkScenario
	generator       ScenarioGenerator
	orchestrator    pipeline.PipelineOrchestrator
	baselineMetrics map[string]*PerformanceMetrics

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
	// Create unique directory for this test run
	testDir := filepath.Join(br.config.TempDir, fmt.Sprintf("scenario_%s_%d", scenario.Name, time.Now().UnixNano()))
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create test directory: %w", err)
	}

	// Generate test files based on scenario
	ctx := context.Background()
	if err := br.generator.GenerateScenario(ctx, scenario, testDir); err != nil {
		return "", fmt.Errorf("failed to generate scenario files: %w", err)
	}

	return testDir, nil
}

func (br *BenchmarkRunner) cleanupTestEnvironment(testDir string) error {
	return os.RemoveAll(testDir)
}

func (br *BenchmarkRunner) clearCache(testDir string) error {
	// Clear pipeline cache directory
	cacheDir := filepath.Join(testDir, ".oxinfer_cache")
	if _, err := os.Stat(cacheDir); err == nil {
		if err := os.RemoveAll(cacheDir); err != nil {
			return fmt.Errorf("failed to remove cache directory: %w", err)
		}
	}

	// Reset orchestrator's internal caches
	if br.orchestrator != nil {
		br.orchestrator.ClearCaches()
	}

	return nil
}

func (br *BenchmarkRunner) createTestManifest(scenario *BenchmarkScenario, testDir, manifestPath string) (*manifest.Manifest, error) {
	// Create manifest based on scenario configuration
	trueVal := true
	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root:     testDir,
			Composer: "composer.json",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app", "routes"},
		},
		Features: &manifest.FeatureConfig{
			HTTPStatus:        &trueVal,
			ResourceUsage:     &trueVal,
			WithPivot:         &trueVal,
			AttributeMake:     &trueVal,
			ScopesUsed:        &trueVal,
			Polymorphic:       &trueVal,
			BroadcastChannels: &trueVal,
		},
	}

	// Features are configured above based on scenario type
	// All features enabled by default for comprehensive testing

	// Write manifest to file
	manifestData, err := manifest.ToJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write manifest file: %w", err)
	}

	return manifest, nil
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
	if br.config.BaselineFile == "" {
		return nil // No baseline file configured
	}

	if _, err := os.Stat(br.config.BaselineFile); os.IsNotExist(err) {
		return fmt.Errorf("baseline file not found: %s", br.config.BaselineFile)
	}

	data, err := os.ReadFile(br.config.BaselineFile)
	if err != nil {
		return fmt.Errorf("failed to read baseline file: %w", err)
	}

	// Parse baseline data (simplified JSON format)
	var baselineData map[string]*PerformanceMetrics
	if err := json.Unmarshal(data, &baselineData); err != nil {
		return fmt.Errorf("failed to parse baseline file: %w", err)
	}

	br.baselineMetrics = baselineData
	return nil
}

func (br *BenchmarkRunner) saveBaseline(summary *RunSummary) error {
	if br.config.BaselineFile == "" {
		return nil // No baseline file configured
	}

	// Collect metrics from successful runs
	baselineData := make(map[string]*PerformanceMetrics)
	for scenarioName, result := range summary.ScenarioResults {
		if result.Success && result.Metrics != nil {
			baselineData[scenarioName] = result.Metrics
		}
	}

	// Serialize to JSON
	data, err := json.Marshal(baselineData, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("failed to serialize baseline data: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(br.config.BaselineFile), 0755); err != nil {
		return fmt.Errorf("failed to create baseline directory: %w", err)
	}

	// Write to file
	if err := os.WriteFile(br.config.BaselineFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write baseline file: %w", err)
	}

	return nil
}

func (br *BenchmarkRunner) generateReports(summary *RunSummary) error {
	if br.config.OutputDir == "" {
		return nil // No output directory configured
	}

	// Generate requested report formats
	for _, format := range br.config.ReportFormats {
		switch format {
		case "json":
			if err := br.generateJSONReport(summary); err != nil {
				return fmt.Errorf("JSON report generation failed: %w", err)
			}
		case "csv":
			if err := br.generateCSVReport(summary); err != nil {
				return fmt.Errorf("CSV report generation failed: %w", err)
			}
		case "html":
			if err := br.generateHTMLReport(summary); err != nil {
				return fmt.Errorf("HTML report generation failed: %w", err)
			}
		default:
			return fmt.Errorf("unsupported report format: %s", format)
		}
	}

	return nil
}

func (br *BenchmarkRunner) generateJSONReport(summary *RunSummary) error {
	data, err := json.Marshal(summary, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("failed to serialize summary: %w", err)
	}

	reportPath := filepath.Join(br.config.OutputDir, "benchmark_report.json")
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write JSON report: %w", err)
	}

	return nil
}

func (br *BenchmarkRunner) generateCSVReport(summary *RunSummary) error {
	reportPath := filepath.Join(br.config.OutputDir, "benchmark_report.csv")
	file, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	// CSV header
	fmt.Fprintln(file, "Scenario,Success,Duration(ms),Memory(MB),FilesProcessed")

	// CSV data rows
	for _, result := range summary.ScenarioResults {
		fmt.Fprintf(file, "%s,%t,%.2f,%.2f,%d\n",
			result.Scenario.Name,
			result.Success,
			float64(result.Metrics.TotalDuration.Nanoseconds())/1e6,
			float64(result.Metrics.MemoryStats.PeakTotalMB),
			result.Metrics.ProcessingStats.FilesParsed,
		)
	}

	return nil
}

func (br *BenchmarkRunner) generateHTMLReport(summary *RunSummary) error {
	// Simple HTML report template
	htmlTemplate := `<!DOCTYPE html>
<html>
<head><title>Oxinfer Benchmark Report</title></head>
<body>
<h1>Benchmark Report</h1>
<p>Generated: %s</p>
<p>Duration: %s</p>
<p>Scenarios: %d total, %d succeeded, %d failed</p>
<h2>Results</h2>
<table border="1">
<tr><th>Scenario</th><th>Success</th><th>Duration</th><th>Memory</th></tr>
%s
</table>
</body>
</html>`

	var tableRows string
	for _, result := range summary.ScenarioResults {
		tableRows += fmt.Sprintf("<tr><td>%s</td><td>%t</td><td>%s</td><td>%.2f MB</td></tr>\n",
			result.Scenario.Name,
			result.Success,
			result.Metrics.TotalDuration,
			float64(result.Metrics.MemoryStats.PeakTotalMB),
		)
	}

	htmlContent := fmt.Sprintf(htmlTemplate,
		time.Now().Format("2006-01-02 15:04:05"),
		summary.TotalDuration,
		summary.ScenariosRun,
		summary.ScenariosSucceeded,
		summary.ScenariosFailed,
		tableRows,
	)

	reportPath := filepath.Join(br.config.OutputDir, "benchmark_report.html")
	if err := os.WriteFile(reportPath, []byte(htmlContent), 0644); err != nil {
		return fmt.Errorf("failed to write HTML report: %w", err)
	}

	return nil
}

func (br *BenchmarkRunner) startProfiling(scenarioName string, runNumber int) ([]string, error) {
	profileFiles := make([]string, 0, len(br.config.ProfileTypes))

	for _, profileType := range br.config.ProfileTypes {
		fileName := fmt.Sprintf("%s_%s_run%d.prof", scenarioName, profileType, runNumber)
		profilePath := filepath.Join(br.config.ProfileDir, fileName)

		// Create the profile file (actual profiling would be implemented here)
		file, err := os.Create(profilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create profile file %s: %w", profilePath, err)
		}
		file.Close() // Just create empty file as placeholder

		profileFiles = append(profileFiles, profilePath)
	}

	return profileFiles, nil
}

func (br *BenchmarkRunner) stopProfiling() error {
	// In a full implementation, this would stop active profilers
	// For now, just return success as we're creating placeholder profiles
	return nil
}

func (br *BenchmarkRunner) calculateRunSummary(summary *RunSummary) {
	if len(summary.ScenarioResults) == 0 {
		return
	}

	var totalDuration time.Duration
	var fastest, slowest *BenchmarkResult

	targetFailures := make([]string, 0)
	regressionDetails := make([]string, 0)

	for _, result := range summary.ScenarioResults {
		// Track fastest and slowest scenarios
		if fastest == nil || result.Metrics.TotalDuration < fastest.Metrics.TotalDuration {
			fastest = result
		}
		if slowest == nil || result.Metrics.TotalDuration > slowest.Metrics.TotalDuration {
			slowest = result
		}

		totalDuration += result.Metrics.TotalDuration

		// Track target failures
		if !result.TargetsMet {
			targetFailures = append(targetFailures, fmt.Sprintf("%s: targets not met", result.Scenario.Name))
		}

		// Track regressions
		if result.Regression {
			summary.RegressionsFound++
			regressionDetails = append(regressionDetails, fmt.Sprintf("%s: performance regression detected", result.Scenario.Name))
		}
	}

	// Set summary fields
	if fastest != nil {
		summary.FastestScenario = fastest.Scenario.Name
	}
	if slowest != nil {
		summary.SlowestScenario = slowest.Scenario.Name
	}

	summary.AverageDuration = totalDuration / time.Duration(len(summary.ScenarioResults))
	summary.AllTargetsMet = len(targetFailures) == 0
	summary.TargetFailures = targetFailures
	summary.RegressionDetails = regressionDetails
}

func convertPipelineStatsToProcessingStats(pipelineStats *pipeline.PipelineStats) *stats.ProcessingStats {
	if pipelineStats == nil {
		return stats.NewProcessingStats()
	}

	processingStats := stats.NewProcessingStats()

	// Convert pipeline stats to processing stats
	if pipelineStats.FilesProcessed > 0 {
		processingStats.FilesParsed = int64(pipelineStats.FilesProcessed)
	}
	if pipelineStats.TotalDuration > 0 {
		processingStats.DurationMs = int64(pipelineStats.TotalDuration.Nanoseconds() / 1e6)
	}
	if pipelineStats.PatternsDetected > 0 {
		// PatternsDetected field doesn't exist in ProcessingStats, use inference ops instead
		processingStats.InferenceOps = int64(pipelineStats.PatternsDetected)
	}

	return processingStats
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
	return &DefaultScenarioGenerator{}
}
