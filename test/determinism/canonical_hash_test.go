package determinism

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/determinism"
	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/manifest"
)

// fixtureExists checks if a fixture directory exists
func fixtureExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestDeterminismWithCanonicalHash tests that canonical hashes are identical across runs
// even when volatile fields (like durationMs) differ.
func TestDeterminismWithCanonicalHash(t *testing.T) {
	tests := []struct {
		name           string
		manifest       *manifest.Manifest
		expectValid    bool
		expectCanonical bool
		skipIfNoFixture bool
	}{
		{
			name: "minimal_project_canonical",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: "../../test/fixtures/integration/minimal-laravel",
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app", "routes"},
					Globs:   []string{"**/*.php"},
				},
			},
			expectValid:     false, // Full hashes will differ due to durationMs
			expectCanonical: true,  // Canonical hashes should be identical
			skipIfNoFixture: true,
		},
		{
			name: "complex_app_canonical",
			manifest: &manifest.Manifest{
				Project: manifest.ProjectConfig{
					Root: "../../test/fixtures/integration/complex-app",
				},
				Scan: manifest.ScanConfig{
					Targets: []string{"app", "routes"},
					Globs:   []string{"**/*.php"},
				},
			},
			expectValid:     false, // Full hashes will differ
			expectCanonical: true,  // Canonical should be identical
			skipIfNoFixture: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipIfNoFixture && !fixtureExists(tt.manifest.Project.Root) {
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

			// Check full hash validation (should fail due to volatile fields)
			if report.IsValid() != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected identical full hashes but got different ones")
				}
				// This is expected - full hashes differ
			}

			// Check canonical hash validation (should pass)
			if report.AllCanonical != tt.expectCanonical {
				t.Errorf("Expected canonical validation %v, got %v", 
					tt.expectCanonical, report.AllCanonical)
				
				if !report.AllCanonical {
					t.Error("CRITICAL: Canonical hashes should be identical but are not!")
					t.Log("This indicates non-deterministic behavior in core logic")
				}
			}

			// Log results
			t.Logf("Execution time: %d ms", report.ExecutionTime)
			t.Logf("Full hash identical: %v", report.AllIdentical)
			t.Logf("Canonical hash identical: %v", report.AllCanonical)
			t.Logf("Unique full hashes: %d", report.UniqueHashCount)
			
			if report.FirstHash != nil {
				t.Logf("First full hash: %s", report.FirstHash.SHA256[:16]+"...")
				t.Logf("Canonical hash: %s", report.FirstHash.CanonicalSHA256[:16]+"...")
			}
		})
	}
}

// TestCanonicalHashExcludesVolatileFields verifies that canonical hash calculation
// correctly excludes volatile fields like generatedAt and durationMs.
func TestCanonicalHashExcludesVolatileFields(t *testing.T) {
	hasher := determinism.NewDeltaHasher()

	// Create two identical deltas except for volatile fields
	delta1 := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 100,
				Skipped:     5,
				DurationMs:  1500, // Different duration
			},
			GeneratedAt: func() *string { s := "2024-01-01T10:00:00Z"; return &s }(),
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   "App\\Http\\Controllers\\UserController",
				Method: "index",
			},
		},
		Models:      []emitter.Model{},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}

	delta2 := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 100,
				Skipped:     5,
				DurationMs:  2300, // Different duration
			},
			GeneratedAt: func() *string { s := "2024-01-01T10:05:00Z"; return &s }(),
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   "App\\Http\\Controllers\\UserController",
				Method: "index",
			},
		},
		Models:      []emitter.Model{},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}

	// Calculate hashes
	hash1, err := hasher.HashDelta(delta1)
	if err != nil {
		t.Fatalf("Failed to hash delta1: %v", err)
	}

	hash2, err := hasher.HashDelta(delta2)
	if err != nil {
		t.Fatalf("Failed to hash delta2: %v", err)
	}

	// Full hashes should differ
	if hash1.SHA256 == hash2.SHA256 {
		t.Error("Full hashes should differ due to volatile fields")
	}

	// Canonical hashes should be identical
	if hash1.CanonicalSHA256 != hash2.CanonicalSHA256 {
		t.Error("Canonical hashes should be identical")
		t.Logf("Canonical 1: %s", hash1.CanonicalSHA256)
		t.Logf("Canonical 2: %s", hash2.CanonicalSHA256)
	}

	t.Logf("✓ Full hashes differ: %s != %s", 
		hash1.SHA256[:16]+"...", hash2.SHA256[:16]+"...")
	t.Logf("✓ Canonical hashes match: %s", 
		hash1.CanonicalSHA256[:16]+"...")
}

// TestTripleRunWithStampFlag simulates the --stamp flag behavior
func TestTripleRunWithStampFlag(t *testing.T) {
	// This test verifies that when --stamp is used, generatedAt is included
	// but canonical hash still excludes it for determinism checking
	
	deltas := make([]*emitter.Delta, 3)
	for i := 0; i < 3; i++ {
		timestamp := time.Now().Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
		deltas[i] = &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: 50,
					DurationMs:  int64(100 + i*50),
				},
				GeneratedAt: &timestamp, // Simulating --stamp flag
			},
			Controllers: []emitter.Controller{},
			Models:      []emitter.Model{},
		}
	}

	hasher := determinism.NewDeltaHasher()
	result, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash multiple deltas: %v", err)
	}

	// Full hashes should all be different
	if result.AllIdentical {
		t.Error("Full hashes should differ due to timestamps")
	}

	// Canonical hashes should be identical
	if !result.AllCanonical {
		t.Error("Canonical hashes should be identical despite timestamps")
	}

	t.Logf("✓ Unique full hashes: %d", len(result.UniqueHashes))
	t.Logf("✓ Canonical validation: %v", result.AllCanonical)
}
