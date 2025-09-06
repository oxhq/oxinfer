// Package determinism provides triple-run validation for deterministic output
// ensuring that oxinfer produces identical delta.json files across multiple runs.
//go:build goexperiment.jsonv2

package determinism

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/manifest"
	"github.com/garaekz/oxinfer/internal/pipeline"
)

// TripleRunValidator validates deterministic output by running analysis three times.
type TripleRunValidator struct {
	hasher *DeltaHasher
	config *ValidationConfig
}

// ValidationConfig configures the behavior of determinism validation.
type ValidationConfig struct {
	// Timeout for each individual run
	RunTimeout time.Duration

	// Total timeout for all validation
	TotalTimeout time.Duration

	// Whether to validate using CLI binary or direct API calls
	UseCLI bool

	// Path to CLI binary (required if UseCLI is true)
	CLIBinaryPath string

	// Whether to enable verbose logging during validation
	Verbose bool

	// Whether to run validation concurrently (may affect determinism testing)
	Concurrent bool

	// Whether to validate cross-platform determinism
	CrossPlatform bool
}

// DefaultValidationConfig returns a default validation configuration.
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		RunTimeout:    30 * time.Second,
		TotalTimeout:  2 * time.Minute,
		UseCLI:        false,
		Verbose:       false,
		Concurrent:    false,
		CrossPlatform: false,
	}
}

// NewTripleRunValidator creates a new validator instance.
func NewTripleRunValidator(config *ValidationConfig) *TripleRunValidator {
	if config == nil {
		config = DefaultValidationConfig()
	}

	return &TripleRunValidator{
		hasher: NewDeltaHasher(),
		config: config,
	}
}

// ValidateTripleRun executes the core triple-run validation.
// This runs analysis 3 times and verifies all outputs produce identical SHA256 hashes.
func (v *TripleRunValidator) ValidateTripleRun(ctx context.Context, manifest *manifest.Manifest) (*DeterminismReport, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest cannot be nil")
	}

	start := time.Now()
	report := NewDeterminismReport("triple_run", 3)

	// Apply total timeout
	if v.config.TotalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, v.config.TotalTimeout)
		defer cancel()
	}

	if v.config.Verbose {
		fmt.Printf("Starting triple-run validation for manifest root: %s\n", manifest.Project.Root)
	}

	// Execute three runs
	var deltas []*emitter.Delta
	var runErrors []string

	if v.config.Concurrent {
		// Concurrent execution (use with caution - may mask race conditions)
		deltas, runErrors = v.runConcurrent(ctx, manifest)
	} else {
		// Sequential execution (recommended for determinism testing)
		deltas, runErrors = v.runSequential(ctx, manifest)
	}

	report.ExecutionTime = time.Since(start).Milliseconds()

	// Report any run failures
	for i, err := range runErrors {
		if err != "" {
			report.AddValidationError("execution_failure",
				fmt.Sprintf("Run %d failed", i+1),
				map[string]string{"error": err})
		}
	}

	// If we don't have 3 successful runs, validation fails
	validDeltas := make([]*emitter.Delta, 0, 3)
	for _, delta := range deltas {
		if delta != nil {
			validDeltas = append(validDeltas, delta)
		}
	}

	if len(validDeltas) < 3 {
		report.AddValidationError("insufficient_runs",
			fmt.Sprintf("Only %d out of 3 runs completed successfully", len(validDeltas)),
			map[string]string{
				"expected": "3",
				"actual":   fmt.Sprintf("%d", len(validDeltas)),
			})
		return report, nil
	}

	// Calculate and compare hashes
	hashResult, err := v.hasher.HashMultiple(validDeltas)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate hashes: %w", err)
	}

	// Update report with results
	report.AllIdentical = hashResult.AllIdentical
	report.AllCanonical = hashResult.AllCanonical
	report.UniqueHashCount = len(hashResult.UniqueHashes)

	if len(hashResult.Hashes) > 0 {
		report.FirstHash = hashResult.Hashes[0]
	}

	// Add validation errors if hashes don't match
	if !hashResult.AllIdentical {
		details := make(map[string]string)
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				details[fmt.Sprintf("run_%d_hash", i+1)] = hash.SHA256
			}
		}

		report.AddValidationError("hash_mismatch",
			"Triple-run validation failed: outputs are not identical",
			details)
	}

	if !hashResult.AllCanonical {
		details := make(map[string]string)
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				details[fmt.Sprintf("run_%d_canonical", i+1)] = hash.CanonicalSHA256
			}
		}

		report.AddValidationError("canonical_mismatch",
			"Canonical hash validation failed: non-volatile fields differ",
			details)
	}

	// Include any hash calculation errors
	for _, hashError := range hashResult.Errors {
		report.AddValidationError("hash_calculation", hashError, nil)
	}

	if v.config.Verbose {
		if report.IsValid() {
			fmt.Printf("✅ Triple-run validation passed: all outputs identical\n")
			fmt.Printf("   SHA256: %s\n", report.FirstHash.SHA256)
		} else {
			fmt.Printf("❌ Triple-run validation failed: %d errors\n", len(report.ValidationErrors))
			for _, err := range report.ValidationErrors {
				fmt.Printf("   - %s: %s\n", err.Type, err.Description)
			}
		}
	}

	return report, nil
}

// ValidateAgainstGolden validates output against a golden file.
func (v *TripleRunValidator) ValidateAgainstGolden(ctx context.Context, manifest *manifest.Manifest, goldenPath string) (*DeterminismReport, error) {
	report := NewDeterminismReport("golden_validation", 1)
	start := time.Now()

	// Execute single run
	delta, err := v.runSingle(ctx, manifest)
	if err != nil {
		report.AddValidationError("execution_failure", "Failed to run analysis",
			map[string]string{"error": err.Error()})
		return report, nil
	}

	// Load golden file
	goldenData, err := loadJSONFile(goldenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load golden file: %w", err)
	}

	// Calculate hashes
	actualHash, err := v.hasher.HashDelta(delta)
	if err != nil {
		return nil, fmt.Errorf("failed to hash actual output: %w", err)
	}

	goldenHash, err := v.hasher.HashBytes(goldenData)
	if err != nil {
		return nil, fmt.Errorf("failed to hash golden output: %w", err)
	}

	// Compare results
	comparison := v.hasher.CompareHashes(actualHash, goldenHash)

	report.AllIdentical = comparison.Identical
	report.AllCanonical = comparison.CanonicalIdentical
	report.FirstHash = actualHash
	report.ExecutionTime = time.Since(start).Milliseconds()

	if !comparison.Identical {
		report.AddValidationError("golden_mismatch",
			"Output does not match golden file",
			map[string]string{
				"actual":   actualHash.SHA256,
				"expected": goldenHash.SHA256,
				"error":    comparison.Error,
			})
	}

	return report, nil
}

// ValidateCrossPlatform validates determinism across different platforms.
// This is useful for ensuring output consistency across different environments.
func (v *TripleRunValidator) ValidateCrossPlatform(ctx context.Context, manifest *manifest.Manifest) (*DeterminismReport, error) {
	if !v.config.CrossPlatform {
		return nil, fmt.Errorf("cross-platform validation not enabled")
	}

	report := NewDeterminismReport("cross_platform", 1)
	start := time.Now()

	// For now, this validates on the current platform but includes platform info
	// In a real implementation, this might coordinate with remote runners
	delta, err := v.runSingle(ctx, manifest)
	if err != nil {
		report.AddValidationError("execution_failure", "Failed to run analysis",
			map[string]string{"error": err.Error()})
		return report, nil
	}

	hash, err := v.hasher.HashDelta(delta)
	if err != nil {
		return nil, fmt.Errorf("failed to hash output: %w", err)
	}

	report.FirstHash = hash
	report.AllIdentical = true // Single platform run always identical to itself
	report.AllCanonical = true
	report.ExecutionTime = time.Since(start).Milliseconds()

	// Add platform information for reference
	report.AddValidationError("platform_info", "Platform information",
		map[string]string{
			"os":   runtime.GOOS,
			"arch": runtime.GOARCH,
			"go":   runtime.Version(),
		})

	return report, nil
}

// runSequential executes three runs sequentially.
func (v *TripleRunValidator) runSequential(ctx context.Context, manifest *manifest.Manifest) ([]*emitter.Delta, []string) {
	deltas := make([]*emitter.Delta, 3)
	errors := make([]string, 3)

	for i := 0; i < 3; i++ {
		if v.config.Verbose {
			fmt.Printf("Executing run %d/3...\n", i+1)
		}

		delta, err := v.runSingle(ctx, manifest)
		if err != nil {
			errors[i] = err.Error()
			if v.config.Verbose {
				fmt.Printf("Run %d failed: %v\n", i+1, err)
			}
		} else {
			deltas[i] = delta
			if v.config.Verbose {
				fmt.Printf("Run %d completed successfully\n", i+1)
			}
		}

		// Check for context cancellation between runs
		select {
		case <-ctx.Done():
			return deltas, errors
		default:
		}
	}

	return deltas, errors
}

// runConcurrent executes three runs concurrently.
// Note: This may mask race conditions and should be used carefully.
func (v *TripleRunValidator) runConcurrent(ctx context.Context, manifest *manifest.Manifest) ([]*emitter.Delta, []string) {
	type runResult struct {
		index int
		delta *emitter.Delta
		error string
	}

	results := make(chan runResult, 3)

	// Start three concurrent runs
	for i := 0; i < 3; i++ {
		go func(index int) {
			delta, err := v.runSingle(ctx, manifest)
			result := runResult{index: index}
			if err != nil {
				result.error = err.Error()
			} else {
				result.delta = delta
			}
			results <- result
		}(i)
	}

	// Collect results
	deltas := make([]*emitter.Delta, 3)
	errors := make([]string, 3)

	for i := 0; i < 3; i++ {
		select {
		case result := <-results:
			deltas[result.index] = result.delta
			errors[result.index] = result.error
		case <-ctx.Done():
			return deltas, errors
		}
	}

	return deltas, errors
}

// runSingle executes a single analysis run.
func (v *TripleRunValidator) runSingle(ctx context.Context, manifest *manifest.Manifest) (*emitter.Delta, error) {
	// Apply run timeout if configured
	if v.config.RunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, v.config.RunTimeout)
		defer cancel()
	}

	if v.config.UseCLI {
		return v.runSingleCLI(ctx, manifest)
	} else {
		return v.runSingleDirect(ctx, manifest)
	}
}

// runSingleCLI executes analysis using the CLI binary.
func (v *TripleRunValidator) runSingleCLI(ctx context.Context, manifest *manifest.Manifest) (*emitter.Delta, error) {
	if v.config.CLIBinaryPath == "" {
		return nil, fmt.Errorf("CLI binary path not configured")
	}

	// Create temporary manifest file
	manifestPath, cleanup, err := writeTempManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary manifest: %w", err)
	}
	defer cleanup()

	// Execute CLI
	cmd := exec.CommandContext(ctx, v.config.CLIBinaryPath, "--manifest", manifestPath)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("CLI execution failed with exit code %d: %s",
				exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("CLI execution failed: %w", err)
	}

	// Parse output as Delta
	delta, err := parseDeltaFromJSON(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CLI output: %w", err)
	}

	return delta, nil
}

// runSingleDirect executes analysis using direct API calls.
func (v *TripleRunValidator) runSingleDirect(ctx context.Context, manifest *manifest.Manifest) (*emitter.Delta, error) {
	// Create pipeline orchestrator
	config := pipeline.DefaultPipelineConfig()
	config.ProjectRoot = manifest.Project.Root

	orchestrator, err := pipeline.NewOrchestrator(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}
	defer orchestrator.Close()

	// Process project
	delta, err := orchestrator.ProcessProject(ctx, manifest)
	if err != nil {
		return nil, fmt.Errorf("pipeline processing failed: %w", err)
	}

	return delta, nil
}

// StressTestValidation runs validation under stress conditions.
func (v *TripleRunValidator) StressTestValidation(ctx context.Context, manifest *manifest.Manifest, iterations int) ([]*DeterminismReport, error) {
	if iterations <= 0 {
		return nil, fmt.Errorf("iterations must be positive")
	}

	reports := make([]*DeterminismReport, iterations)

	for i := 0; i < iterations; i++ {
		if v.config.Verbose {
			fmt.Printf("Stress test iteration %d/%d\n", i+1, iterations)
		}

		report, err := v.ValidateTripleRun(ctx, manifest)
		if err != nil {
			return reports, fmt.Errorf("stress test iteration %d failed: %w", i+1, err)
		}

		reports[i] = report

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return reports[:i+1], ctx.Err()
		default:
		}
	}

	return reports, nil
}

// Helper functions

func loadJSONFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	// Validate it's proper JSON
	var temp any
	if err := json.Unmarshal(data, &temp); err != nil {
		return nil, fmt.Errorf("file %s contains invalid JSON: %w", path, err)
	}

	return data, nil
}

func writeTempManifest(manifest *manifest.Manifest) (string, func(), error) {
	// Create temporary file
	tmpFile, err := os.CreateTemp("", "oxinfer-manifest-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()

	// Marshal manifest to JSON
	data, err := json.Marshal(manifest, json.Deterministic(true), json.Indent("", "  "))
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Write to file
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	cleanup := func() {
		os.Remove(tmpPath)
	}

	return tmpPath, cleanup, nil
}

func parseDeltaFromJSON(data []byte) (*emitter.Delta, error) {
	var delta emitter.Delta
	if err := json.Unmarshal(data, &delta); err != nil {
		return nil, fmt.Errorf("failed to parse Delta JSON: %w", err)
	}

	return &delta, nil
}

// DetectNonDeterministicElements analyzes delta structures to identify
// potential sources of non-determinism.
func (v *TripleRunValidator) DetectNonDeterministicElements(deltas []*emitter.Delta) []string {
	if len(deltas) < 2 {
		return nil
	}

	issues := make([]string, 0)

	// Check for varying timestamps
	timestamps := make(map[string]bool)
	for _, delta := range deltas {
		if delta.Meta.GeneratedAt != nil {
			timestamps[*delta.Meta.GeneratedAt] = true
		}
	}
	if len(timestamps) > 1 {
		issues = append(issues, "varying meta.generatedAt timestamps detected")
	}

	// Check for varying execution durations
	durations := make(map[int64]bool)
	for _, delta := range deltas {
		durations[delta.Meta.Stats.DurationMs] = true
	}
	if len(durations) > 1 {
		issues = append(issues, "varying meta.stats.durationMs values detected")
	}

	// Check for unsorted collections
	for i, delta := range deltas {
		if !v.isCollectionsSorted(delta) {
			issues = append(issues, fmt.Sprintf("unsorted collections detected in delta %d", i))
		}
	}

	return issues
}

// isCollectionsSorted checks if all collections in a Delta are properly sorted.
func (v *TripleRunValidator) isCollectionsSorted(delta *emitter.Delta) bool {
	// Check controllers are sorted by FQCN then method
	for i := 1; i < len(delta.Controllers); i++ {
		curr := delta.Controllers[i]
		prev := delta.Controllers[i-1]

		if curr.FQCN < prev.FQCN {
			return false
		}
		if curr.FQCN == prev.FQCN && curr.Method < prev.Method {
			return false
		}
	}

	// Check models are sorted by FQCN
	for i := 1; i < len(delta.Models); i++ {
		if delta.Models[i].FQCN < delta.Models[i-1].FQCN {
			return false
		}
	}

	// Check polymorphic are sorted by parent
	for i := 1; i < len(delta.Polymorphic); i++ {
		if delta.Polymorphic[i].Parent < delta.Polymorphic[i-1].Parent {
			return false
		}
	}

	// Check broadcast are sorted by channel
	for i := 1; i < len(delta.Broadcast); i++ {
		if delta.Broadcast[i].Channel < delta.Broadcast[i-1].Channel {
			return false
		}
	}

	return true
}
