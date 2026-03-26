package determinism

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/determinism"
	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/manifest"
)

func TestTripleRunValidation_BasicScenarios(t *testing.T) {
	tests := []struct {
		name        string
		manifest    *manifest.Manifest
		expectValid bool
		description string
	}{
		{
			name: "minimal_deterministic_output",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: "../../test/fixtures/integration/minimal-laravel",
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app"},
					Globs:   []string{"*.php"},
				},
			},
			expectValid: true,
			description: "Basic Laravel project should produce deterministic output",
		},
		{
			name: "complex_patterns_determinism",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: "../../test/fixtures/integration/complex-app",
				},
				Scan: manifest.ScanConfig{
					Targets:         []string{"app", "routes"},
					Globs:           []string{"*.php"},
					VendorWhitelist: []string{"laravel"},
				},
			},
			expectValid: true,
			description: "Complex patterns with polymorphic relationships should be deterministic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip tests that don't have fixtures yet - this is infrastructure-only
			if _, err := os.Stat(tt.manifest.Project.Root); os.IsNotExist(err) {
				t.Skipf("Fixture not found: %s", tt.manifest.Project.Root)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			config := determinism.DefaultValidationConfig()
			config.Verbose = true

			validator := determinism.NewTripleRunValidator(config)

			report, err := validator.ValidateTripleRun(ctx, tt.manifest)
			if err != nil {
				t.Fatalf("Validation failed: %v", err)
			}

			if report.IsValid() != tt.expectValid {
				t.Errorf("Expected validation result %v, got %v", tt.expectValid, report.IsValid())
				t.Logf("Validation errors: %d", len(report.ValidationErrors))
				for _, validationErr := range report.ValidationErrors {
					t.Logf("  - %s: %s", validationErr.Type, validationErr.Description)
				}
			}

			// Log performance metrics
			t.Logf("Execution time: %d ms", report.ExecutionTime)
			if report.FirstHash != nil {
				t.Logf("Output hash: %s", report.FirstHash.SHA256)
				t.Logf("Output size: %d bytes", report.FirstHash.Size)
			}

			// Verify triple run requirements
			if tt.expectValid {
				if report.RunCount != 3 {
					t.Errorf("Expected 3 runs, got %d", report.RunCount)
				}
				if !report.AllIdentical {
					t.Error("Expected all runs to produce identical output")
				}
				if report.UniqueHashCount != 1 {
					t.Errorf("Expected 1 unique hash, got %d", report.UniqueHashCount)
				}
			}
		})
	}
}

func TestHasherDeterminism_EdgeCases(t *testing.T) {
	hasher := determinism.NewDeltaHasher()

	tests := []struct {
		name        string
		createDelta func() *emitter.Delta
		description string
	}{
		{
			name: "empty_delta_consistency",
			createDelta: func() *emitter.Delta {
				return &emitter.Delta{
					Meta: emitter.MetaInfo{
						Partial: false,
						Stats: emitter.MetaStats{
							FilesParsed: 0,
							Skipped:     0,
							DurationMs:  100, // This should vary but canonical hash should be identical
						},
					},
					Controllers: []emitter.Controller{},
					Models:      []emitter.Model{},
					Polymorphic: []emitter.Polymorphic{},
					Broadcast:   []emitter.Broadcast{},
				}
			},
			description: "Empty delta with only duration differences",
		},
		{
			name: "large_collections_ordering",
			createDelta: func() *emitter.Delta {
				// Create delta with many controllers to test sorting stability
				controllers := make([]emitter.Controller, 100)
				for i := 0; i < 100; i++ {
					controllers[i] = emitter.Controller{
						FQCN:   fmt.Sprintf("App\\Http\\Controllers\\Controller%03d", 99-i), // Reverse order
						Method: fmt.Sprintf("method%03d", i),
					}
				}

				return &emitter.Delta{
					Meta: emitter.MetaInfo{
						Partial: false,
						Stats: emitter.MetaStats{
							FilesParsed: 100,
							Skipped:     0,
							DurationMs:  500,
						},
					},
					Controllers: controllers,
					Models:      []emitter.Model{},
					Polymorphic: []emitter.Polymorphic{},
					Broadcast:   []emitter.Broadcast{},
				}
			},
			description: "Large collections with complex sorting requirements",
		},
		{
			name: "complex_nested_structures",
			createDelta: func() *emitter.Delta {
				return &emitter.Delta{
					Meta: emitter.MetaInfo{
						Partial: false,
						Stats: emitter.MetaStats{
							FilesParsed: 10,
							Skipped:     2,
							DurationMs:  750,
						},
					},
					Controllers: []emitter.Controller{
						{
							FQCN:   "App\\Http\\Controllers\\UserController",
							Method: "index",
							Resources: []emitter.Resource{
								{Class: "UserResource", Collection: true},
								{Class: "AdminResource", Collection: false}, // Test sorting
							},
							ScopesUsed: []emitter.ScopeUsed{
								{On: "User", Name: "verified", Args: []string{"true", "false"}}, // Test arg sorting
								{On: "User", Name: "active", Args: []string{"1"}},
							},
						},
					},
					Models: []emitter.Model{
						{
							FQCN: "App\\Models\\User",
							WithPivot: []emitter.PivotInfo{
								{
									Relation: "roles",
									Columns:  []string{"permission", "created_at", "updated_at"}, // Test column sorting
								},
							},
						},
					},
					Polymorphic: []emitter.Polymorphic{
						{
							Parent: "App\\Models\\Comment",
							Morph: emitter.MorphInfo{
								Key:        "commentable",
								TypeColumn: "commentable_type",
								IdColumn:   "commentable_id",
							},
							Discriminator: emitter.Discriminator{
								PropertyName: "type",
								Mapping: map[string]string{
									"post": "App\\Models\\Post",
									"user": "App\\Models\\User",
									"page": "App\\Models\\Page", // Test map key sorting
								},
							},
						},
					},
					Broadcast: []emitter.Broadcast{
						{
							Channel:    "user.{id}",
							Params:     []string{"id", "type", "action"}, // Test param sorting
							Visibility: "private",
						},
					},
				}
			},
			description: "Complex nested structures with multiple sorting requirements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create multiple instances of the same logical delta
			deltas := make([]*emitter.Delta, 5)
			for i := 0; i < 5; i++ {
				deltas[i] = tt.createDelta()
				// Vary the duration to ensure canonical hashing works
				deltas[i].Meta.Stats.DurationMs = int64(100 + i*50)
			}

			// Calculate hashes for all deltas
			hashResult, err := hasher.HashMultiple(deltas)
			if err != nil {
				t.Fatalf("Failed to calculate hashes: %v", err)
			}

			// All canonical hashes should be identical even if full hashes differ
			if !hashResult.AllCanonical {
				t.Errorf("Canonical hashes should be identical for %s", tt.description)
				for i, hash := range hashResult.Hashes {
					if hash != nil {
						t.Logf("Delta %d canonical hash: %s", i, hash.CanonicalSHA256)
					}
				}
			}

			// Verify we have exactly one unique canonical hash
			canonicalHashes := make(map[string]bool)
			for _, hash := range hashResult.Hashes {
				if hash != nil {
					canonicalHashes[hash.CanonicalSHA256] = true
				}
			}

			if len(canonicalHashes) != 1 {
				t.Errorf("Expected 1 unique canonical hash, got %d", len(canonicalHashes))
			}

			t.Logf("Test case: %s - %d deltas processed", tt.name, len(deltas))
			if len(hashResult.Hashes) > 0 && hashResult.Hashes[0] != nil {
				t.Logf("Canonical hash: %s", hashResult.Hashes[0].CanonicalSHA256)
			}
		})
	}
}

func TestValidationReport_Accuracy(t *testing.T) {
	tests := []struct {
		name               string
		setup              func() *determinism.DeterminismReport
		wantValid          bool
		wantCanonicalValid bool
		description        string
	}{
		{
			name: "all_successful_runs",
			setup: func() *determinism.DeterminismReport {
				report := determinism.NewDeterminismReport("test", 3)
				report.AllIdentical = true
				report.AllCanonical = true
				report.UniqueHashCount = 1
				report.FirstHash = &determinism.HashResult{
					SHA256:          "abc123",
					CanonicalSHA256: "def456",
					Size:            1024,
				}
				return report
			},
			wantValid:          true,
			wantCanonicalValid: true,
			description:        "Perfect triple-run validation",
		},
		{
			name: "hash_mismatch",
			setup: func() *determinism.DeterminismReport {
				report := determinism.NewDeterminismReport("test", 3)
				report.AllIdentical = false
				report.AllCanonical = true // Canonical still matches
				report.UniqueHashCount = 3 // All different
				report.AddValidationError("hash_mismatch", "Hashes don't match", nil)
				return report
			},
			wantValid:          false,
			wantCanonicalValid: false, // Any validation errors cause IsCanonicalValid to return false
			description:        "Hashes differ but canonical content identical",
		},
		{
			name: "execution_failure",
			setup: func() *determinism.DeterminismReport {
				report := determinism.NewDeterminismReport("test", 3)
				report.AllIdentical = false
				report.AllCanonical = false
				report.UniqueHashCount = 0
				report.AddValidationError("execution_failure", "Run 2 failed",
					map[string]string{"error": "timeout"})
				return report
			},
			wantValid:          false,
			wantCanonicalValid: false,
			description:        "Execution failures prevent validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := tt.setup()

			if report.IsValid() != tt.wantValid {
				t.Errorf("IsValid() = %v, want %v", report.IsValid(), tt.wantValid)
			}

			if report.IsCanonicalValid() != tt.wantCanonicalValid {
				t.Errorf("IsCanonicalValid() = %v, want %v",
					report.IsCanonicalValid(), tt.wantCanonicalValid)
			}

			t.Logf("Report: %s - Valid: %v, Canonical: %v, Errors: %d",
				tt.description, report.IsValid(), report.IsCanonicalValid(),
				len(report.ValidationErrors))
		})
	}
}

func TestConcurrentVsSequentialValidation(t *testing.T) {
	// This test verifies that concurrent and sequential validation
	// produce the same results (when they both succeed)

	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: "/tmp/test-project",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"*.php"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Run sequential validation
	seqConfig := determinism.DefaultValidationConfig()
	seqConfig.Concurrent = false
	seqConfig.Verbose = false
	seqValidator := determinism.NewTripleRunValidator(seqConfig)

	// Run concurrent validation
	concConfig := determinism.DefaultValidationConfig()
	concConfig.Concurrent = true
	concConfig.Verbose = false
	concValidator := determinism.NewTripleRunValidator(concConfig)

	// Skip if fixtures don't exist yet
	if _, err := os.Stat(manifest.Project.Root); os.IsNotExist(err) {
		t.Skip("Fixtures not available for concurrent validation test")
		return
	}

	// Execute the actual validation:
	seqReport, err := seqValidator.ValidateTripleRun(ctx, manifest)
	if err != nil {
		t.Fatalf("Sequential validation failed: %v", err)
	}

	concReport, err := concValidator.ValidateTripleRun(ctx, manifest)
	if err != nil {
		t.Fatalf("Concurrent validation failed: %v", err)
	}

	// Both should produce the same validity results
	if seqReport.IsValid() != concReport.IsValid() {
		t.Errorf("Sequential and concurrent validation produced different results")
		t.Logf("Sequential valid: %v, Concurrent valid: %v",
			seqReport.IsValid(), concReport.IsValid())
	}

	// If both are valid, their hashes should match
	if seqReport.IsValid() && concReport.IsValid() {
		if seqReport.FirstHash.SHA256 != concReport.FirstHash.SHA256 {
			t.Errorf("Sequential and concurrent validation produced different hashes")
			t.Logf("Sequential: %s, Concurrent: %s",
				seqReport.FirstHash.SHA256, concReport.FirstHash.SHA256)
		}
	}
}

func TestStressValidation_MemoryStability(t *testing.T) {
	// This test runs many iterations to check for memory leaks
	// and ensure consistent performance

	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	manifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: "/tmp/stress-project",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app"},
			Globs:   []string{"*.php"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config := determinism.DefaultValidationConfig()
	config.Verbose = false
	validator := determinism.NewTripleRunValidator(config)

	// Skip if fixtures don't exist yet
	if _, err := os.Stat(manifest.Project.Root); os.IsNotExist(err) {
		t.Skip("Fixtures not available for stress test")
		return
	}

	const iterations = 50
	reports, err := validator.StressTestValidation(ctx, manifest, iterations)
	if err != nil {
		t.Fatalf("Stress validation failed: %v", err)
	}

	// Analyze results
	successCount := 0
	totalExecutionTime := int64(0)
	var executionTimes []int64

	for i, report := range reports {
		if report.IsValid() {
			successCount++
		}
		totalExecutionTime += report.ExecutionTime
		executionTimes = append(executionTimes, report.ExecutionTime)

		t.Logf("Iteration %d: Valid=%v, Time=%dms", i+1, report.IsValid(), report.ExecutionTime)
	}

	successRate := float64(successCount) / float64(iterations)
	avgExecutionTime := totalExecutionTime / int64(iterations)

	t.Logf("Stress test results:")
	t.Logf("  Success rate: %.2f%% (%d/%d)", successRate*100, successCount, iterations)
	t.Logf("  Average execution time: %dms", avgExecutionTime)
	t.Logf("  Total execution time: %dms", totalExecutionTime)

	// Verify acceptable success rate (should be 100% for deterministic system)
	if successRate < 0.95 {
		t.Errorf("Success rate too low: %.2f%%, expected >= 95%%", successRate*100)
	}

	// Check for performance degradation
	firstQuarter := executionTimes[:iterations/4]
	lastQuarter := executionTimes[iterations*3/4:]

	avgFirst := calculateAverage(firstQuarter)
	avgLast := calculateAverage(lastQuarter)

	degradation := float64(avgLast-avgFirst) / float64(avgFirst) * 100
	if degradation > 50 { // More than 50% slower
		t.Errorf("Performance degradation detected: %.1f%% slower in final quarter", degradation)
	}
}

// Helper function for tests
func calculateAverage(times []int64) int64 {
	if len(times) == 0 {
		return 0
	}

	sum := int64(0)
	for _, time := range times {
		sum += time
	}
	return sum / int64(len(times))
}
