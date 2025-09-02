// validate-determinism is a standalone CLI tool for validating deterministic
// behavior of oxinfer across multiple runs. This tool is designed for CI/CD
// integration and automated testing pipelines.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/garaekz/oxinfer/internal/determinism"
	"github.com/garaekz/oxinfer/internal/manifest"
)

// ValidationConfig holds configuration for the validation tool.
type ValidationConfig struct {
	ManifestPath  string
	GoldenFile    string
	Iterations    int
	Timeout       time.Duration
	UseCLI        bool
	CLIBinaryPath string
	OutputFormat  string
	Verbose       bool
	ExitOnFailure bool
	ReportFile    string
	Concurrent    bool
	StressTest    bool
	CrossPlatform bool
}

// ValidationResults aggregates results from multiple validation runs.
type ValidationResults struct {
	Config               *ValidationConfig                `json:"config"`
	TripleRunResults     []*determinism.DeterminismReport `json:"tripleRunResults,omitempty"`
	GoldenResults        *determinism.DeterminismReport   `json:"goldenResults,omitempty"`
	StressResults        []*determinism.DeterminismReport `json:"stressResults,omitempty"`
	CrossPlatformResults *determinism.DeterminismReport   `json:"crossPlatformResults,omitempty"`
	OverallSuccess       bool                             `json:"overallSuccess"`
	TotalDuration        int64                            `json:"totalDurationMs"`
	Summary              *ValidationSummary               `json:"summary"`
}

// ValidationSummary provides a high-level overview of validation results.
type ValidationSummary struct {
	TotalTests        int      `json:"totalTests"`
	PassedTests       int      `json:"passedTests"`
	FailedTests       int      `json:"failedTests"`
	SuccessRate       float64  `json:"successRate"`
	AverageExecTime   int64    `json:"averageExecTimeMs"`
	UniqueHashCount   int      `json:"uniqueHashCount"`
	DeterminismIssues []string `json:"determinismIssues,omitempty"`
}

func main() {
	config := parseFlags()

	if config.Verbose {
		log.Printf("Starting determinism validation with config: %+v\n", config)
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	validator := createValidator(config)
	results := &ValidationResults{
		Config: config,
		Summary: &ValidationSummary{
			DeterminismIssues: make([]string, 0),
		},
	}

	start := time.Now()

	// Load manifest
	manifest, err := loadManifest(config.ManifestPath)
	if err != nil {
		log.Fatalf("Failed to load manifest: %v", err)
	}

	if config.Verbose {
		log.Printf("Loaded manifest for project root: %s", manifest.Project.Root)
	}

	// Run triple-run validation (always performed)
	if config.Verbose {
		log.Println("Running triple-run validation...")
	}

	tripleRunResult, err := validator.ValidateTripleRun(ctx, manifest)
	if err != nil {
		log.Fatalf("Triple-run validation failed: %v", err)
	}

	results.TripleRunResults = []*determinism.DeterminismReport{tripleRunResult}
	results.Summary.TotalTests++
	if tripleRunResult.IsValid() {
		results.Summary.PassedTests++
	} else {
		results.Summary.FailedTests++
		addDeterminismIssues(results.Summary, tripleRunResult.ValidationErrors)
	}

	// Run golden file validation if specified
	if config.GoldenFile != "" {
		if config.Verbose {
			log.Printf("Running golden file validation against: %s", config.GoldenFile)
		}

		goldenResult, err := validator.ValidateAgainstGolden(ctx, manifest, config.GoldenFile)
		if err != nil {
			log.Printf("Golden file validation failed: %v", err)
		} else {
			results.GoldenResults = goldenResult
			results.Summary.TotalTests++
			if goldenResult.IsValid() {
				results.Summary.PassedTests++
			} else {
				results.Summary.FailedTests++
				addDeterminismIssues(results.Summary, goldenResult.ValidationErrors)
			}
		}
	}

	// Run stress testing if enabled
	if config.StressTest && config.Iterations > 1 {
		if config.Verbose {
			log.Printf("Running stress test with %d iterations...", config.Iterations)
		}

		stressResults, err := validator.StressTestValidation(ctx, manifest, config.Iterations)
		if err != nil {
			log.Printf("Stress test failed: %v", err)
		} else {
			results.StressResults = stressResults
			for _, result := range stressResults {
				results.Summary.TotalTests++
				if result.IsValid() {
					results.Summary.PassedTests++
				} else {
					results.Summary.FailedTests++
					addDeterminismIssues(results.Summary, result.ValidationErrors)
				}
			}
		}
	}

	// Run cross-platform validation if enabled
	if config.CrossPlatform {
		if config.Verbose {
			log.Println("Running cross-platform validation...")
		}

		crossPlatformResult, err := validator.ValidateCrossPlatform(ctx, manifest)
		if err != nil {
			log.Printf("Cross-platform validation failed: %v", err)
		} else {
			results.CrossPlatformResults = crossPlatformResult
			results.Summary.TotalTests++
			if crossPlatformResult.IsValid() {
				results.Summary.PassedTests++
			} else {
				results.Summary.FailedTests++
				addDeterminismIssues(results.Summary, crossPlatformResult.ValidationErrors)
			}
		}
	}

	// Calculate final results
	results.TotalDuration = time.Since(start).Milliseconds()
	results.OverallSuccess = results.Summary.FailedTests == 0

	if results.Summary.TotalTests > 0 {
		results.Summary.SuccessRate = float64(results.Summary.PassedTests) / float64(results.Summary.TotalTests)
	}

	// Calculate average execution time
	totalExecTime := int64(0)
	execCount := 0

	for _, result := range results.TripleRunResults {
		if result != nil {
			totalExecTime += result.ExecutionTime
			execCount++
		}
	}
	if results.GoldenResults != nil {
		totalExecTime += results.GoldenResults.ExecutionTime
		execCount++
	}
	for _, result := range results.StressResults {
		if result != nil {
			totalExecTime += result.ExecutionTime
			execCount++
		}
	}
	if results.CrossPlatformResults != nil {
		totalExecTime += results.CrossPlatformResults.ExecutionTime
		execCount++
	}

	if execCount > 0 {
		results.Summary.AverageExecTime = totalExecTime / int64(execCount)
	}

	// Count unique hashes
	uniqueHashes := make(map[string]bool)
	for _, result := range results.TripleRunResults {
		if result != nil && result.FirstHash != nil {
			uniqueHashes[result.FirstHash.SHA256] = true
		}
	}
	if results.GoldenResults != nil && results.GoldenResults.FirstHash != nil {
		uniqueHashes[results.GoldenResults.FirstHash.SHA256] = true
	}
	results.Summary.UniqueHashCount = len(uniqueHashes)

	// Output results
	outputResults(results, config)

	// Write report file if specified
	if config.ReportFile != "" {
		if err := writeReportFile(results, config.ReportFile); err != nil {
			log.Printf("Failed to write report file: %v", err)
		}
	}

	// Exit with appropriate code
	if config.ExitOnFailure && !results.OverallSuccess {
		os.Exit(1)
	}

	if config.Verbose {
		log.Println("Determinism validation completed successfully")
	}
}

func parseFlags() *ValidationConfig {
	config := &ValidationConfig{}

	flag.StringVar(&config.ManifestPath, "manifest", "", "Path to manifest.json file (required)")
	flag.StringVar(&config.GoldenFile, "golden", "", "Path to golden file for comparison (optional)")
	flag.IntVar(&config.Iterations, "iterations", 1, "Number of iterations for stress testing")
	flag.DurationVar(&config.Timeout, "timeout", 10*time.Minute, "Timeout for entire validation process")
	flag.BoolVar(&config.UseCLI, "use-cli", false, "Use CLI binary instead of direct API calls")
	flag.StringVar(&config.CLIBinaryPath, "cli-path", "./oxinfer", "Path to CLI binary (when use-cli is true)")
	flag.StringVar(&config.OutputFormat, "format", "human", "Output format (human|json|junit)")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose output")
	flag.BoolVar(&config.ExitOnFailure, "exit-on-failure", true, "Exit with code 1 on validation failure")
	flag.StringVar(&config.ReportFile, "report", "", "Write detailed report to file")
	flag.BoolVar(&config.Concurrent, "concurrent", false, "Run validations concurrently (may mask race conditions)")
	flag.BoolVar(&config.StressTest, "stress", false, "Enable stress testing mode")
	flag.BoolVar(&config.CrossPlatform, "cross-platform", false, "Enable cross-platform validation")

	flag.Parse()

	// Validate required flags
	if config.ManifestPath == "" {
		fmt.Fprintf(os.Stderr, "Error: -manifest flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	// Validate flag combinations
	if config.UseCLI && config.CLIBinaryPath == "" {
		fmt.Fprintf(os.Stderr, "Error: -cli-path is required when -use-cli is true\n")
		os.Exit(1)
	}

	if config.StressTest && config.Iterations <= 1 {
		config.Iterations = 10 // Default stress test iterations
	}

	return config
}

func createValidator(config *ValidationConfig) *determinism.TripleRunValidator {
	validationConfig := &determinism.ValidationConfig{
		RunTimeout:    30 * time.Second,
		TotalTimeout:  config.Timeout,
		UseCLI:        config.UseCLI,
		CLIBinaryPath: config.CLIBinaryPath,
		Verbose:       config.Verbose,
		Concurrent:    config.Concurrent,
		CrossPlatform: config.CrossPlatform,
	}

	return determinism.NewTripleRunValidator(validationConfig)
}

func loadManifest(path string) (*manifest.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	return &m, nil
}

func outputResults(results *ValidationResults, config *ValidationConfig) {
	switch config.OutputFormat {
	case "json":
		outputJSON(results)
	case "junit":
		outputJUnit(results)
	default:
		outputHuman(results, config.Verbose)
	}
}

func outputHuman(results *ValidationResults, verbose bool) {
	fmt.Printf("Determinism Validation Results\n")
	fmt.Printf("==============================\n\n")

	// Overall summary
	fmt.Printf("Overall Success: %s\n", formatBoolean(results.OverallSuccess))
	fmt.Printf("Total Duration: %d ms\n", results.TotalDuration)
	fmt.Printf("Tests: %d total, %d passed, %d failed (%.1f%% success rate)\n",
		results.Summary.TotalTests,
		results.Summary.PassedTests,
		results.Summary.FailedTests,
		results.Summary.SuccessRate*100)
	fmt.Printf("Average Execution Time: %d ms\n", results.Summary.AverageExecTime)
	fmt.Printf("Unique Hashes: %d\n\n", results.Summary.UniqueHashCount)

	// Triple-run results
	if len(results.TripleRunResults) > 0 {
		fmt.Printf("Triple-Run Validation:\n")
		for i, result := range results.TripleRunResults {
			fmt.Printf("  Run %d: %s", i+1, formatBoolean(result.IsValid()))
			if result.FirstHash != nil {
				fmt.Printf(" (Hash: %s)", result.FirstHash.SHA256[:16]+"...")
			}
			fmt.Printf("\n")

			if verbose && len(result.ValidationErrors) > 0 {
				for _, err := range result.ValidationErrors {
					fmt.Printf("    Error: %s - %s\n", err.Type, err.Description)
				}
			}
		}
		fmt.Printf("\n")
	}

	// Golden file results
	if results.GoldenResults != nil {
		fmt.Printf("Golden File Validation: %s\n", formatBoolean(results.GoldenResults.IsValid()))
		if verbose && len(results.GoldenResults.ValidationErrors) > 0 {
			for _, err := range results.GoldenResults.ValidationErrors {
				fmt.Printf("  Error: %s - %s\n", err.Type, err.Description)
			}
		}
		fmt.Printf("\n")
	}

	// Stress test results
	if len(results.StressResults) > 0 {
		successCount := 0
		for _, result := range results.StressResults {
			if result.IsValid() {
				successCount++
			}
		}

		fmt.Printf("Stress Test Results:\n")
		fmt.Printf("  Iterations: %d\n", len(results.StressResults))
		fmt.Printf("  Success Rate: %.1f%% (%d/%d)\n",
			float64(successCount)/float64(len(results.StressResults))*100,
			successCount, len(results.StressResults))
		fmt.Printf("\n")
	}

	// Cross-platform results
	if results.CrossPlatformResults != nil {
		fmt.Printf("Cross-Platform Validation: %s\n", formatBoolean(results.CrossPlatformResults.IsValid()))
		fmt.Printf("\n")
	}

	// Determinism issues
	if len(results.Summary.DeterminismIssues) > 0 {
		fmt.Printf("Determinism Issues Detected:\n")
		for _, issue := range results.Summary.DeterminismIssues {
			fmt.Printf("  - %s\n", issue)
		}
		fmt.Printf("\n")
	}

	// Final result
	if results.OverallSuccess {
		fmt.Printf("✅ All validations passed - oxinfer produces deterministic output\n")
	} else {
		fmt.Printf("❌ Validation failures detected - oxinfer output is not deterministic\n")
	}
}

func outputJSON(results *ValidationResults) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		log.Fatalf("Failed to encode JSON results: %v", err)
	}
}

func outputJUnit(results *ValidationResults) {
	// JUnit XML output for CI integration
	fmt.Printf(`<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="Determinism Validation" tests="%d" failures="%d" time="%.3f">
`, results.Summary.TotalTests, results.Summary.FailedTests, float64(results.TotalDuration)/1000.0)

	// Triple-run tests
	for i, result := range results.TripleRunResults {
		fmt.Printf(`  <testcase name="triple-run-%d" classname="determinism" time="%.3f">
`, i+1, float64(result.ExecutionTime)/1000.0)

		if !result.IsValid() {
			fmt.Printf(`    <failure message="Triple-run validation failed">
`)
			for _, err := range result.ValidationErrors {
				fmt.Printf("      %s: %s\n", err.Type, err.Description)
			}
			fmt.Printf(`    </failure>
`)
		}
		fmt.Printf(`  </testcase>
`)
	}

	// Golden file test
	if results.GoldenResults != nil {
		fmt.Printf(`  <testcase name="golden-file" classname="determinism" time="%.3f">
`, float64(results.GoldenResults.ExecutionTime)/1000.0)

		if !results.GoldenResults.IsValid() {
			fmt.Printf(`    <failure message="Golden file validation failed">
`)
			for _, err := range results.GoldenResults.ValidationErrors {
				fmt.Printf("      %s: %s\n", err.Type, err.Description)
			}
			fmt.Printf(`    </failure>
`)
		}
		fmt.Printf(`  </testcase>
`)
	}

	// Stress tests
	for i, result := range results.StressResults {
		fmt.Printf(`  <testcase name="stress-test-%d" classname="determinism" time="%.3f">
`, i+1, float64(result.ExecutionTime)/1000.0)

		if !result.IsValid() {
			fmt.Printf(`    <failure message="Stress test iteration failed">
`)
			for _, err := range result.ValidationErrors {
				fmt.Printf("      %s: %s\n", err.Type, err.Description)
			}
			fmt.Printf(`    </failure>
`)
		}
		fmt.Printf(`  </testcase>
`)
	}

	// Cross-platform test
	if results.CrossPlatformResults != nil {
		fmt.Printf(`  <testcase name="cross-platform" classname="determinism" time="%.3f">
`, float64(results.CrossPlatformResults.ExecutionTime)/1000.0)

		if !results.CrossPlatformResults.IsValid() {
			fmt.Printf(`    <failure message="Cross-platform validation failed">
`)
			for _, err := range results.CrossPlatformResults.ValidationErrors {
				fmt.Printf("      %s: %s\n", err.Type, err.Description)
			}
			fmt.Printf(`    </failure>
`)
		}
		fmt.Printf(`  </testcase>
`)
	}

	fmt.Printf("</testsuite>\n")
}

func writeReportFile(results *ValidationResults, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create report file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		return fmt.Errorf("failed to encode report: %w", err)
	}

	return nil
}

func addDeterminismIssues(summary *ValidationSummary, errors []*determinism.ValidationError) {
	for _, err := range errors {
		issue := fmt.Sprintf("%s: %s", err.Type, err.Description)
		// Avoid duplicates
		found := false
		for _, existing := range summary.DeterminismIssues {
			if existing == issue {
				found = true
				break
			}
		}
		if !found {
			summary.DeterminismIssues = append(summary.DeterminismIssues, issue)
		}
	}
}

func formatBoolean(b bool) string {
	if b {
		return "✅ PASS"
	}
	return "❌ FAIL"
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `validate-determinism - Determinism validation tool for oxinfer

This tool validates that oxinfer produces deterministic output by running
analysis multiple times and comparing SHA256 hashes of the results.

Usage:
  validate-determinism [flags]

Required Flags:
  -manifest string      Path to manifest.json file

Optional Flags:
  -golden string        Path to golden file for comparison
  -iterations int       Number of iterations for stress testing (default 1)
  -timeout duration     Timeout for entire validation process (default 10m0s)
  -use-cli             Use CLI binary instead of direct API calls
  -cli-path string     Path to CLI binary (default "./oxinfer")
  -format string       Output format: human|json|junit (default "human")
  -verbose             Enable verbose output
  -exit-on-failure     Exit with code 1 on validation failure (default true)
  -report string       Write detailed report to file
  -concurrent          Run validations concurrently (may mask race conditions)
  -stress              Enable stress testing mode
  -cross-platform      Enable cross-platform validation

Examples:
  # Basic triple-run validation
  validate-determinism -manifest manifest.json

  # Validate against golden file with verbose output
  validate-determinism -manifest manifest.json -golden expected.json -verbose

  # Run stress test with 50 iterations
  validate-determinism -manifest manifest.json -stress -iterations 50

  # Generate JSON report for CI integration
  validate-determinism -manifest manifest.json -format json -report results.json

  # Use CLI binary for validation
  validate-determinism -manifest manifest.json -use-cli -cli-path ./bin/oxinfer

Exit Codes:
  0 - All validations passed
  1 - Validation failures detected (when -exit-on-failure is true)

`)
	}
}
