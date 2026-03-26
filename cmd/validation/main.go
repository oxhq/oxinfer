package main

import (
	"context"
	"fmt"
	"os"

	"github.com/oxhq/oxinfer/internal/indexer"
)

func init() {
	// Ensure temp directories exist for validation
	dirs := []string{
		"/tmp/oxinfer_validation_small",
		"/tmp/oxinfer_validation_medium",
		"/tmp/oxinfer_validation_large",
		"/tmp/oxinfer_validation_cache",
		"/tmp/oxinfer_validation_determinism",
		"/tmp/oxinfer_validation_discoverer",
		"/tmp/oxinfer_validation_workers",
		"/tmp/oxinfer_validation_indexer",
	}

	for _, dir := range dirs {
		_ = os.MkdirAll(dir, 0755)
	}
}

func main() {
	ctx := context.Background()
	validator := indexer.NewPerformanceValidator()

	fmt.Println("OXINFER PERFORMANCE VALIDATION")
	fmt.Println("=" + fmt.Sprintf("%50s", "="))
	fmt.Println("Validating indexer performance targets and components...")

	result := validator.ValidateAllPerformanceTargets(ctx)

	report := validator.GenerateReport()
	fmt.Println(report)

	// Summary output
	fmt.Printf("VALIDATION SUMMARY:\n")
	fmt.Printf("Overall Result: %s\n", result.OverallResult)
	fmt.Printf("Passed Targets: %d\n", len(result.PassedTargets))
	fmt.Printf("Failed Targets: %d\n", len(result.FailedTargets))

	if len(result.PassedTargets) > 0 {
		fmt.Println("\nPASSED TARGETS:")
		for _, target := range result.PassedTargets {
			fmt.Printf("  • %s\n", target)
		}
	}

	if len(result.FailedTargets) > 0 {
		fmt.Println("\nFAILED TARGETS:")
		for _, target := range result.FailedTargets {
			fmt.Printf("  • %s\n", target)
		}
		os.Exit(1)
	}

	fmt.Println("\nPerformance validation completed successfully!")
}
