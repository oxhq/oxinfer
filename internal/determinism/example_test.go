package determinism

import (
	"fmt"
	"testing"

	"github.com/garaekz/oxinfer/internal/emitter"
)

// TestDeltaHasher_DemoTripleRun demonstrates the core functionality
// of the determinism validation system.
func TestDeltaHasher_DemoTripleRun(t *testing.T) {
	hasher := NewDeltaHasher()

	// Create three identical Delta structures (representing triple-run output)
	createExampleDelta := func() *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: 15,
					Skipped:     2,
					DurationMs:  1500, // This will vary between runs
				},
			},
			Controllers: []emitter.Controller{
				{
					FQCN:   "App\\Http\\Controllers\\UserController",
					Method: "index",
					HTTP: &emitter.HTTPInfo{
						Status:   &[]int{200}[0],
						Explicit: &[]bool{true}[0],
					},
					Resources: []emitter.Resource{
						{Class: "UserResource", Collection: true},
					},
				},
				{
					FQCN:   "App\\Http\\Controllers\\PostController",
					Method: "show",
					HTTP: &emitter.HTTPInfo{
						Status:   &[]int{200}[0],
						Explicit: &[]bool{false}[0],
					},
				},
			},
			Models: []emitter.Model{
				{
					FQCN: "App\\Models\\User",
					WithPivot: []emitter.PivotInfo{
						{
							Relation: "roles",
							Columns:  []string{"level", "granted_at"},
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
						},
					},
				},
			},
			Broadcast: []emitter.Broadcast{
				{
					Channel:    "user.{id}",
					Params:     []string{"id"},
					Visibility: "private",
				},
			},
		}
	}

	// Simulate three runs with different execution durations
	run1 := createExampleDelta()
	run1.Meta.Stats.DurationMs = 1450

	run2 := createExampleDelta()
	run2.Meta.Stats.DurationMs = 1523

	run3 := createExampleDelta()
	run3.Meta.Stats.DurationMs = 1601

	deltas := []*emitter.Delta{run1, run2, run3}

	// Validate determinism across all runs
	result, err := hasher.HashMultiple(deltas)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Results should show different full hashes but identical canonical hashes
	t.Logf("Triple-run validation results:")
	t.Logf("  All identical: %v", result.AllIdentical)
	t.Logf("  All canonical identical: %v", result.AllCanonical)
	t.Logf("  Unique hashes: %d", len(result.UniqueHashes))

	// Show that while execution times vary, core content is identical
	for i, hash := range result.Hashes {
		t.Logf("  Run %d: SHA256=%s, Canonical=%s",
			i+1, hash.SHA256[:16]+"...", hash.CanonicalSHA256[:16]+"...")
	}

	// Verify expected behavior
	if result.AllIdentical {
		t.Error("Expected different full hashes due to duration differences")
	}
	if !result.AllCanonical {
		t.Error("Expected identical canonical hashes")
	}
	if len(result.UniqueHashes) != 3 {
		t.Errorf("Expected 3 unique full hashes, got %d", len(result.UniqueHashes))
	}
}

// TestDeterminismReport_Demo demonstrates analyzing validation results.
func TestDeterminismReport_Demo(t *testing.T) {
	// Create a validation report representing a successful triple-run
	report := NewDeterminismReport("example_project", 3)
	report.AllIdentical = true
	report.AllCanonical = true
	report.UniqueHashCount = 1
	report.ExecutionTime = 2500
	report.FirstHash = &HashResult{
		SHA256:          "a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456",
		CanonicalSHA256: "f6e5d4c3b2a19876543210987654321098765432109876543210987654321098",
		Size:            4096,
	}

	// Analyze the report
	t.Logf("Validation Report Analysis:")
	t.Logf("  Test Name: %s", report.TestName)
	t.Logf("  Validation Passed: %v", report.IsValid())
	t.Logf("  Canonical Validation Passed: %v", report.IsCanonicalValid())
	t.Logf("  Execution Time: %d ms", report.ExecutionTime)
	t.Logf("  Output Size: %d bytes", report.FirstHash.Size)
	t.Logf("  Deterministic Hash: %s", report.FirstHash.SHA256[:32]+"...")

	if len(report.ValidationErrors) == 0 {
		t.Logf("  ✅ No validation errors - output is fully deterministic")
	} else {
		t.Logf("  ❌ %d validation errors detected", len(report.ValidationErrors))
	}

	// Verify expected report state
	if !report.IsValid() {
		t.Error("Expected successful validation report")
	}
	if !report.IsCanonicalValid() {
		t.Error("Expected successful canonical validation")
	}
}

// TestDeterminismDemo_RealWorldScenario demonstrates the complete validation
// workflow with realistic Laravel patterns.
func TestDeterminismDemo_RealWorldScenario(t *testing.T) {
	hasher := NewDeltaHasher()

	// Create a realistic Laravel application delta
	createLaravelDelta := func(variableDuration int64) *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: 42,
					Skipped:     3,
					DurationMs:  variableDuration, // This will vary between runs
				},
			},
			Controllers: []emitter.Controller{
				{
					FQCN:   "App\\Http\\Controllers\\Api\\UserController",
					Method: "index",
					HTTP: &emitter.HTTPInfo{
						Status:   &[]int{200}[0],
						Explicit: &[]bool{true}[0],
					},
					Request: &emitter.RequestInfo{
						ContentTypes: []string{"application/json"},
						Query:        emitter.NewOrderedObjectFromMap(map[string]interface{}{"page": map[string]interface{}{}}),
					},
					Resources: []emitter.Resource{
						{Class: "UserResource", Collection: true},
					},
					ScopesUsed: []emitter.ScopeUsed{
						{On: "User", Name: "active", Args: []string{"true"}},
						{On: "User", Name: "verified", Args: []string{}},
					},
				},
				{
					FQCN:   "App\\Http\\Controllers\\Api\\PostController",
					Method: "store",
					HTTP: &emitter.HTTPInfo{
						Status:   &[]int{201}[0],
						Explicit: &[]bool{true}[0],
					},
					Request: &emitter.RequestInfo{
						ContentTypes: []string{"application/json", "multipart/form-data"},
						Body:         emitter.NewOrderedObjectFromMap(map[string]interface{}{"title": map[string]interface{}{}, "content": map[string]interface{}{}}),
					},
				},
			},
			Models: []emitter.Model{
				{
					FQCN: "App\\Models\\User",
					WithPivot: []emitter.PivotInfo{
						{
							Relation:   "roles",
							Columns:    []string{"permission_level", "granted_at"},
							Alias:      &[]string{"user_role"}[0],
							Timestamps: &[]bool{true}[0],
						},
					},
					Attributes: []emitter.Attribute{
						{Name: "full_name", Via: "Attribute::make"},
						{Name: "avatar_url", Via: "Attribute::make"},
					},
				},
				{
					FQCN: "App\\Models\\Post",
					Attributes: []emitter.Attribute{
						{Name: "excerpt", Via: "Attribute::make"},
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
							"page": "App\\Models\\Page",
						},
					},
					DepthTruncated: &[]bool{false}[0],
				},
			},
			Broadcast: []emitter.Broadcast{
				{
					File:           &[]string{"routes/channels.php"}[0],
					Channel:        "user.{id}",
					Params:         []string{"id"},
					Visibility:     "private",
					PayloadLiteral: &[]bool{false}[0],
				},
				{
					Channel:    "notifications.{type}",
					Params:     []string{"type"},
					Visibility: "public",
				},
			},
		}
	}

	// Simulate three runs with different execution times
	deltas := []*emitter.Delta{
		createLaravelDelta(1450),
		createLaravelDelta(1523),
		createLaravelDelta(1601),
	}

	// Validate determinism
	result, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to validate Laravel app determinism: %v", err)
	}

	// Verify deterministic behavior
	if !result.AllCanonical {
		t.Error("Laravel application should produce deterministic canonical output")
		for i, hash := range result.Hashes {
			if hash != nil {
				t.Logf("Run %d canonical hash: %s", i+1, hash.CanonicalSHA256)
			}
		}
	}

	// The full hashes should be different due to duration differences
	if result.AllIdentical {
		t.Error("Expected different full hashes due to varying execution times")
	}

	if len(result.UniqueHashes) != 3 {
		t.Errorf("Expected 3 unique full hashes, got %d", len(result.UniqueHashes))
	}

	// But canonical hashes should be identical
	canonicalHashes := make(map[string]bool)
	for _, hash := range result.Hashes {
		if hash != nil {
			canonicalHashes[hash.CanonicalSHA256] = true
		}
	}

	if len(canonicalHashes) != 1 {
		t.Errorf("Expected 1 unique canonical hash, got %d", len(canonicalHashes))
	}

	t.Logf("Real-world Laravel determinism demo:")
	t.Logf("  Full hashes unique: %d (expected due to duration differences)", len(result.UniqueHashes))
	t.Logf("  Canonical hashes unique: %d (core determinism verified)", len(canonicalHashes))
	t.Logf("  First canonical hash: %s", result.Hashes[0].CanonicalSHA256)
	t.Logf("  Average output size: %d bytes", result.Hashes[0].Size)

	// This demonstrates the key determinism guarantee:
	// "same repository → same delta.json, byte-for-byte" (when excluding volatile fields)
}
