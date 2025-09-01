package main

import (
	"context"
	"fmt"
	"os"

	"github.com/garaekz/oxinfer/internal/indexer"
)

func init() {
	// Ensure temp directories exist for validation
	dirs := []string{
		"/tmp/sprint3_validation_small",
		"/tmp/sprint3_validation_medium", 
		"/tmp/sprint3_validation_large",
		"/tmp/sprint3_validation_cache",
		"/tmp/sprint3_validation_determinism",
		"/tmp/sprint3_validation_discoverer",
		"/tmp/sprint3_validation_workers", 
		"/tmp/sprint3_validation_indexer",
	}
	
	for _, dir := range dirs {
		os.MkdirAll(dir, 0755)
	}
}

func main() {
	ctx := context.Background()
	validator := indexer.NewSprint3Validator()
	
	fmt.Println("🚀 OXINFER SPRINT 3 COMPREHENSIVE VALIDATION")
	fmt.Println("=" + fmt.Sprintf("%50s", "="))
	fmt.Println("Validating all Sprint 3 performance targets and components...")
	
	result := validator.ValidateAllSprint3Targets(ctx)
	
	report := validator.GenerateReport()
	fmt.Println(report)
	
	// Summary output
	fmt.Printf("📋 VALIDATION SUMMARY:\n")
	fmt.Printf("Overall Result: %s\n", result.OverallResult)
	fmt.Printf("Passed Targets: %d\n", len(result.PassedTargets))
	fmt.Printf("Failed Targets: %d\n", len(result.FailedTargets))
	
	if len(result.PassedTargets) > 0 {
		fmt.Println("\n✅ PASSED TARGETS:")
		for _, target := range result.PassedTargets {
			fmt.Printf("  • %s\n", target)
		}
	}
	
	if len(result.FailedTargets) > 0 {
		fmt.Println("\n❌ FAILED TARGETS:")
		for _, target := range result.FailedTargets {
			fmt.Printf("  • %s\n", target)
		}
		os.Exit(1)
	}
	
	fmt.Println("\n🎉 Sprint 3 validation completed successfully!")
}