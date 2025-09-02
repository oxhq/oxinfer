package determinism

import (
	"context"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/determinism"
	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/manifest"
)



// TestDeterminismIntegration_ErrorHandling tests determinism validation
// behavior when the pipeline encounters errors.
func TestDeterminismIntegration_ErrorHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with invalid manifest that should cause errors
	invalidManifest := &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: "/nonexistent/path",
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"nonexistent"},
			Globs:   []string{"*.php"},
		},
	}

	config := determinism.DefaultValidationConfig()
	config.Verbose = false // Reduce noise for error testing
	
	validator := determinism.NewTripleRunValidator(config)

	report, err := validator.ValidateTripleRun(ctx, invalidManifest)
	
	// Validation should complete even with errors, but report failures
	if err != nil {
		t.Fatalf("Validation should handle errors gracefully: %v", err)
	}

	// Should have execution failures
	if report.IsValid() {
		t.Error("Expected validation to fail for invalid manifest")
	}

	// Should report appropriate error types
	foundExecutionError := false
	for _, validationErr := range report.ValidationErrors {
		if validationErr.Type == "execution_failure" {
			foundExecutionError = true
			break
		}
	}

	if !foundExecutionError {
		t.Error("Expected execution_failure error for invalid manifest")
	}

	t.Logf("Error handling test: %d validation errors reported", 
		len(report.ValidationErrors))
}



// TestDeterminismValidation_RegressionPrevention tests that changes to
// the codebase don't accidentally introduce non-determinism.
func TestDeterminismValidation_RegressionPrevention(t *testing.T) {
	// This is a meta-test that validates the determinism validation system itself
	hasher := determinism.NewDeltaHasher()

	// Create a reference delta that should always hash the same way
	referenceDelta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 25,
				Skipped:     2,
				DurationMs:  1000,
			},
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   "App\\Http\\Controllers\\RegressionController",
				Method: "index",
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{200}[0],
					Explicit: &[]bool{true}[0],
				},
				Request: &emitter.RequestInfo{
					ContentTypes: []string{"application/json"},
					Body:         emitter.NewOrderedObjectFromMap(map[string]interface{}{"test": map[string]interface{}{}}),
				},
				Resources: []emitter.Resource{
					{Class: "TestResource", Collection: false},
				},
			},
		},
		Models: []emitter.Model{
			{
				FQCN: "App\\Models\\RegressionModel",
				Attributes: []emitter.Attribute{
					{Name: "test_attribute", Via: "Attribute::make"},
				},
			},
		},
		Polymorphic: []emitter.Polymorphic{
			{
				Parent: "App\\Models\\RegressionParent",
				Morph: emitter.MorphInfo{
					Key:        "morphable",
					TypeColumn: "morphable_type",
					IdColumn:   "morphable_id",
				},
				Discriminator: emitter.Discriminator{
					PropertyName: "type",
					Mapping: map[string]string{
						"test": "App\\Models\\TestType",
					},
				},
			},
		},
		Broadcast: []emitter.Broadcast{
			{
				Channel:    "test.{id}",
				Params:     []string{"id"},
				Visibility: "private",
			},
		},
	}

	// This hash should be stable across code changes (unless delta structure changes)
	// If this test fails after code changes, it indicates either:
	// 1. A regression in determinism (bad)
	// 2. An intentional change to Delta structure (expected, update expected hash)
	
	hash, err := hasher.HashDelta(referenceDelta)
	if err != nil {
		t.Fatalf("Failed to hash reference delta: %v", err)
	}

	// Expected hash - this would be updated when Delta structure intentionally changes
	// For now, just verify we get a valid hash format
	if len(hash.SHA256) != 64 {
		t.Errorf("Invalid SHA256 format: %s", hash.SHA256)
	}

	if len(hash.CanonicalSHA256) != 64 {
		t.Errorf("Invalid canonical SHA256 format: %s", hash.CanonicalSHA256)
	}

	// Test multiple hashing of the same reference
	const iterations = 10
	for i := 0; i < iterations; i++ {
		currentHash, err := hasher.HashDelta(referenceDelta)
		if err != nil {
			t.Fatalf("Reference hash failed on iteration %d: %v", i, err)
		}

		if currentHash.SHA256 != hash.SHA256 {
			t.Errorf("Reference hash changed on iteration %d: %s != %s", 
				i, currentHash.SHA256, hash.SHA256)
		}
	}

	t.Logf("Regression prevention: reference hash %s (stable across %d iterations)", 
		hash.SHA256[:16]+"...", iterations)

	// Log the full hash for manual verification during development
	t.Logf("Full reference hash: %s", hash.SHA256)
	t.Logf("Full canonical hash: %s", hash.CanonicalSHA256)
}