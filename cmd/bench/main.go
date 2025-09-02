// Command bench is a standalone CLI tool for running Oxinfer performance benchmarks.
// It provides comprehensive performance testing capabilities including profiling and regression detection.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/garaekz/oxinfer/internal/bench"
)

// CliConfig holds configuration for the benchmark CLI.
type CliConfig struct {
	// Scenarios
	ScenarioNames []string `json:"scenarioNames"`
	ListScenarios bool     `json:"listScenarios"`
	AllScenarios  bool     `json:"allScenarios"`

	// Execution
	WarmupRuns     int  `json:"warmupRuns"`
	BenchmarkRuns  int  `json:"benchmarkRuns"`
	Parallel       bool `json:"parallel"`
	TimeoutMinutes int  `json:"timeoutMinutes"`

	// Environment
	TempDir         string `json:"tempDir"`
	OutputDir       string `json:"outputDir"`
	CleanupAfterRun bool   `json:"cleanupAfterRun"`
	KeepFailures    bool   `json:"keepFailures"`

	// Profiling
	EnableProfiling bool     `json:"enableProfiling"`
	ProfileTypes    []string `json:"profileTypes"`
	ProfileDir      string   `json:"profileDir"`

	// Regression testing
	BaselineFile        string  `json:"baselineFile"`
	SaveBaseline        bool    `json:"saveBaseline"`
	RegressionThreshold float64 `json:"regressionThreshold"`

	// Output
	Format    string `json:"format"` // "json", "table", "summary"
	Verbose   bool   `json:"verbose"`
	QuietMode bool   `json:"quietMode"`

	// Cache management
	ClearCache bool `json:"clearCache"`
	WarmCache  bool `json:"warmCache"`
}

// RunResult contains the aggregated results from a benchmark run.
type RunResult struct {
	Summary       *bench.RunSummary        `json:"summary"`
	Results       []*bench.BenchmarkResult `json:"results"`
	Config        *CliConfig               `json:"config"`
	ExecutionTime time.Duration            `json:"executionTimeMs"`
	Success       bool                     `json:"success"`
	Errors        []string                 `json:"errors,omitempty"`
}

func main() {
	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, shutting down gracefully...\n")
		cancel()
	}()

	// Parse command line flags
	config, err := parseFlags()
	if err != nil {
		log.Fatalf("Flag parsing failed: %v", err)
	}

	// Handle list scenarios request
	if config.ListScenarios {
		listScenarios()
		return
	}

	// Execute benchmarks
	startTime := time.Now()
	result, err := runBenchmarks(ctx, config)
	executionTime := time.Since(startTime)

	if result == nil {
		result = &RunResult{
			Config:        config,
			ExecutionTime: executionTime,
			Success:       false,
		}
	} else {
		result.ExecutionTime = executionTime
	}

	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err.Error())
	}

	// Output results
	if err := outputResults(result, config); err != nil {
		log.Fatalf("Failed to output results: %v", err)
	}

	// Exit with appropriate code
	if !result.Success {
		os.Exit(1)
	}

	// Check performance targets
	if result.Summary != nil && !result.Summary.AllTargetsMet {
		fmt.Fprintf(os.Stderr, "Warning: Not all performance targets were met\n")
		os.Exit(2)
	}

	// Check for regressions
	if result.Summary != nil && result.Summary.RegressionsFound > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d performance regressions detected\n", result.Summary.RegressionsFound)
		os.Exit(3)
	}
}

// parseFlags parses command line arguments and returns configuration.
func parseFlags() (*CliConfig, error) {
	config := &CliConfig{
		WarmupRuns:          1,
		BenchmarkRuns:       3,
		TimeoutMinutes:      30,
		TempDir:             filepath.Join(os.TempDir(), "oxinfer_bench"),
		OutputDir:           "benchmark_results",
		CleanupAfterRun:     true,
		Format:              "summary",
		RegressionThreshold: 10.0,
	}

	var scenarioNamesStr string
	var profileTypesStr string

	// Scenario selection
	flag.StringVar(&scenarioNamesStr, "scenarios", "", "Comma-separated list of scenario names to run")
	flag.BoolVar(&config.ListScenarios, "list", false, "List all available scenarios and exit")
	flag.BoolVar(&config.AllScenarios, "all", false, "Run all available scenarios")

	// Execution parameters
	flag.IntVar(&config.WarmupRuns, "warmup", config.WarmupRuns, "Number of warmup runs per scenario")
	flag.IntVar(&config.BenchmarkRuns, "runs", config.BenchmarkRuns, "Number of benchmark runs per scenario")
	flag.BoolVar(&config.Parallel, "parallel", false, "Run scenarios in parallel")
	flag.IntVar(&config.TimeoutMinutes, "timeout", config.TimeoutMinutes, "Timeout per scenario in minutes")

	// Environment
	flag.StringVar(&config.TempDir, "temp-dir", config.TempDir, "Temporary directory for test files")
	flag.StringVar(&config.OutputDir, "output-dir", config.OutputDir, "Output directory for results")
	flag.BoolVar(&config.CleanupAfterRun, "cleanup", config.CleanupAfterRun, "Cleanup test files after run")
	flag.BoolVar(&config.KeepFailures, "keep-failures", false, "Keep test files for failed scenarios")

	// Profiling
	flag.BoolVar(&config.EnableProfiling, "profile", false, "Enable profiling during benchmarks")
	flag.StringVar(&profileTypesStr, "profile-types", "cpu,memory", "Comma-separated list of profile types (cpu,memory,goroutine,block,mutex,trace)")
	flag.StringVar(&config.ProfileDir, "profile-dir", "profiles", "Directory for profile outputs")

	// Regression testing
	flag.StringVar(&config.BaselineFile, "baseline", "", "Baseline metrics file for regression testing")
	flag.BoolVar(&config.SaveBaseline, "save-baseline", false, "Save current results as baseline")
	flag.Float64Var(&config.RegressionThreshold, "regression-threshold", config.RegressionThreshold, "Regression threshold percentage")

	// Output formatting
	flag.StringVar(&config.Format, "format", config.Format, "Output format: json, table, summary")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&config.QuietMode, "quiet", false, "Suppress non-essential output")

	// Cache management
	flag.BoolVar(&config.ClearCache, "clear-cache", false, "Clear cache before each run")
	flag.BoolVar(&config.WarmCache, "warm-cache", false, "Warm cache before benchmark runs")

	// Parse flags
	flag.Parse()

	// Process string lists
	if scenarioNamesStr != "" {
		config.ScenarioNames = strings.Split(scenarioNamesStr, ",")
		for i := range config.ScenarioNames {
			config.ScenarioNames[i] = strings.TrimSpace(config.ScenarioNames[i])
		}
	}

	if profileTypesStr != "" {
		config.ProfileTypes = strings.Split(profileTypesStr, ",")
		for i := range config.ProfileTypes {
			config.ProfileTypes[i] = strings.TrimSpace(config.ProfileTypes[i])
		}
	}

	// Validate configuration
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

// validateConfig validates the CLI configuration.
func validateConfig(config *CliConfig) error {
	if config.BenchmarkRuns <= 0 {
		return fmt.Errorf("benchmark runs must be positive")
	}

	if config.TimeoutMinutes <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	if config.RegressionThreshold < 0 {
		return fmt.Errorf("regression threshold cannot be negative")
	}

	validFormats := map[string]bool{"json": true, "table": true, "summary": true}
	if !validFormats[config.Format] {
		return fmt.Errorf("invalid format '%s', must be one of: json, table, summary", config.Format)
	}

	if !config.AllScenarios && len(config.ScenarioNames) == 0 && !config.ListScenarios {
		return fmt.Errorf("must specify either -all, -scenarios, or -list")
	}

	return nil
}

// listScenarios prints all available benchmark scenarios.
func listScenarios() {
	scenarios := bench.MVPBenchmarkScenarios()

	fmt.Printf("Available Benchmark Scenarios:\n\n")

	// Group by type for better organization
	byType := make(map[bench.ScenarioType][]*bench.BenchmarkScenario)
	for _, scenario := range scenarios {
		byType[scenario.ScenarioType] = append(byType[scenario.ScenarioType], scenario)
	}

	// Sort types for consistent output
	types := make([]bench.ScenarioType, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return string(types[i]) < string(types[j]) })

	for _, scenarioType := range types {
		fmt.Printf("%s Scenarios:\n", strings.Title(string(scenarioType)))

		scenarios := byType[scenarioType]
		sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Name < scenarios[j].Name })

		for _, scenario := range scenarios {
			complexity := bench.EstimateScenarioComplexity(scenario)
			fmt.Printf("  %-30s %s (%.1f complexity, %d files, max %v)\n",
				scenario.Name,
				scenario.Description,
				complexity,
				scenario.FileCount,
				scenario.MaxDuration,
			)
		}
		fmt.Println()
	}
}

// runBenchmarks executes the configured benchmark scenarios.
func runBenchmarks(ctx context.Context, config *CliConfig) (*RunResult, error) {
	// Create runner configuration
	runnerConfig := &bench.RunnerConfig{
		TempDir:             config.TempDir,
		CleanupAfterRun:     config.CleanupAfterRun,
		KeepFailures:        config.KeepFailures,
		WarmupRuns:          config.WarmupRuns,
		BenchmarkRuns:       config.BenchmarkRuns,
		TimeoutPerRun:       time.Duration(config.TimeoutMinutes) * time.Minute,
		ParallelScenarios:   config.Parallel,
		ClearCacheBetween:   config.ClearCache,
		WarmCacheRuns:       0,
		EnableProfiling:     config.EnableProfiling,
		ProfileDir:          config.ProfileDir,
		ProfileTypes:        config.ProfileTypes,
		BaselineFile:        config.BaselineFile,
		SaveBaseline:        config.SaveBaseline,
		RegressionThreshold: config.RegressionThreshold,
		OutputDir:           config.OutputDir,
		GenerateReports:     true,
		ReportFormats:       []string{"json"},
	}

	if config.WarmCache {
		runnerConfig.WarmCacheRuns = 1
	}

	// Create benchmark runner
	runner, err := bench.NewBenchmarkRunner(runnerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create benchmark runner: %w", err)
	}

	// Add scenarios based on configuration
	if config.AllScenarios {
		if err := runner.AddAllMVPScenarios(); err != nil {
			return nil, fmt.Errorf("failed to add MVP scenarios: %w", err)
		}
	} else {
		// Add specific scenarios
		for _, scenarioName := range config.ScenarioNames {
			scenario, err := bench.GetScenarioByName(scenarioName)
			if err != nil {
				return nil, fmt.Errorf("scenario '%s' not found: %w", scenarioName, err)
			}

			if err := runner.AddScenario(scenario); err != nil {
				return nil, fmt.Errorf("failed to add scenario '%s': %w", scenarioName, err)
			}
		}
	}

	// Progress reporting
	if !config.QuietMode {
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					running, current, completed, total := runner.GetCurrentStatus()
					if running {
						fmt.Fprintf(os.Stderr, "Progress: %d/%d scenarios completed, currently running: %s\n",
							completed, total, current)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Run benchmarks
	if !config.QuietMode {
		fmt.Printf("Starting benchmark execution...\n")
	}

	summary, err := runner.RunAllScenarios(ctx)
	if err != nil {
		return &RunResult{
			Config:  config,
			Success: false,
			Errors:  []string{err.Error()},
		}, fmt.Errorf("benchmark execution failed: %w", err)
	}

	// Collect individual results
	var results []*bench.BenchmarkResult
	for _, result := range summary.ScenarioResults {
		results = append(results, result)
	}

	// Sort results for consistent output
	sort.Slice(results, func(i, j int) bool {
		return results[i].Scenario.Name < results[j].Scenario.Name
	})

	return &RunResult{
		Summary: summary,
		Results: results,
		Config:  config,
		Success: summary.ScenariosFailed == 0,
	}, nil
}

// outputResults formats and outputs the benchmark results.
func outputResults(result *RunResult, config *CliConfig) error {
	switch config.Format {
	case "json":
		return outputJSON(result)
	case "table":
		return outputTable(result, config.Verbose)
	case "summary":
		return outputSummary(result, config.Verbose)
	default:
		return fmt.Errorf("unsupported output format: %s", config.Format)
	}
}

// outputJSON outputs results in JSON format.
func outputJSON(result *RunResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// outputTable outputs results in a formatted table.
func outputTable(result *RunResult, verbose bool) error {
	fmt.Printf("\nBenchmark Results:\n")
	fmt.Printf("==================\n\n")

	if result.Summary == nil {
		fmt.Printf("No results available\n")
		return nil
	}

	// Summary table
	fmt.Printf("Overall Summary:\n")
	fmt.Printf("  Total Duration: %v\n", result.Summary.TotalDuration)
	fmt.Printf("  Scenarios Run: %d\n", result.Summary.ScenariosRun)
	fmt.Printf("  Succeeded: %d\n", result.Summary.ScenariosSucceeded)
	fmt.Printf("  Failed: %d\n", result.Summary.ScenariosFailed)
	fmt.Printf("  All Targets Met: %t\n", result.Summary.AllTargetsMet)
	fmt.Printf("  Regressions Found: %d\n", result.Summary.RegressionsFound)
	fmt.Printf("\n")

	// Individual scenario results
	if len(result.Results) > 0 {
		fmt.Printf("Individual Scenarios:\n")
		fmt.Printf("%-30s %-10s %-12s %-12s %-8s %-8s\n",
			"Scenario", "Success", "Duration", "Memory", "Targets", "Regress")
		fmt.Printf("%s\n", strings.Repeat("-", 90))

		for _, res := range result.Results {
			success := "PASS"
			if !res.Success {
				success = "FAIL"
			}

			targets := "YES"
			if !res.TargetsMet {
				targets = "NO"
			}

			regression := "NO"
			if res.Regression {
				regression = "YES"
			}

			memoryMB := ""
			duration := ""
			if res.Metrics != nil {
				duration = res.Metrics.TotalDuration.String()
				memoryMB = fmt.Sprintf("%dMB", res.Metrics.MemoryStats.PeakTotalMB)
			}

			fmt.Printf("%-30s %-10s %-12s %-12s %-8s %-8s\n",
				res.Scenario.Name, success, duration, memoryMB, targets, regression)

			if verbose && res.Error != "" {
				fmt.Printf("  Error: %s\n", res.Error)
			}
		}
	}

	return nil
}

// outputSummary outputs a concise summary of results.
func outputSummary(result *RunResult, verbose bool) error {
	if result.Summary == nil {
		fmt.Printf("Benchmark execution failed\n")
		if len(result.Errors) > 0 {
			fmt.Printf("Errors:\n")
			for _, err := range result.Errors {
				fmt.Printf("  - %s\n", err)
			}
		}
		return nil
	}

	summary := result.Summary

	fmt.Printf("Benchmark Summary:\n")
	fmt.Printf("==================\n")
	fmt.Printf("Execution Time: %v\n", result.ExecutionTime)
	fmt.Printf("Scenarios: %d total, %d succeeded, %d failed\n",
		summary.ScenariosRun, summary.ScenariosSucceeded, summary.ScenariosFailed)

	if summary.AllTargetsMet {
		fmt.Printf("✓ All performance targets met\n")
	} else {
		fmt.Printf("✗ Some performance targets not met\n")
		if verbose && len(summary.TargetFailures) > 0 {
			fmt.Printf("  Failed targets:\n")
			for _, failure := range summary.TargetFailures {
				fmt.Printf("    - %s\n", failure)
			}
		}
	}

	if summary.RegressionsFound == 0 {
		fmt.Printf("✓ No performance regressions detected\n")
	} else {
		fmt.Printf("✗ %d performance regressions detected\n", summary.RegressionsFound)
		if verbose && len(summary.RegressionDetails) > 0 {
			fmt.Printf("  Regressions:\n")
			for _, detail := range summary.RegressionDetails {
				fmt.Printf("    - %s\n", detail)
			}
		}
	}

	if summary.FastestScenario != "" {
		fmt.Printf("Fastest Scenario: %s\n", summary.FastestScenario)
	}
	if summary.SlowestScenario != "" {
		fmt.Printf("Slowest Scenario: %s\n", summary.SlowestScenario)
	}
	if summary.AverageDuration > 0 {
		fmt.Printf("Average Duration: %v\n", summary.AverageDuration)
	}

	if verbose && len(result.Results) > 0 {
		fmt.Printf("\nDetailed Results:\n")
		for _, res := range result.Results {
			status := "PASS"
			if !res.Success {
				status = "FAIL"
			}

			fmt.Printf("  %s: %s", res.Scenario.Name, status)
			if res.Metrics != nil {
				fmt.Printf(" (%v, %dMB)", res.Metrics.TotalDuration, res.Metrics.MemoryStats.PeakTotalMB)
			}
			if res.Regression {
				fmt.Printf(" [REGRESSION]")
			}
			fmt.Printf("\n")

			if res.Error != "" {
				fmt.Printf("    Error: %s\n", res.Error)
			}
		}
	}

	return nil
}
