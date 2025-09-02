package determinism

import (
	"fmt"
	"testing"

	"github.com/garaekz/oxinfer/internal/determinism"
	"github.com/garaekz/oxinfer/internal/emitter"
)


// TestEmitterDeterminism_SortingStability validates that the emitter's
// sorting algorithms are stable and produce consistent results.
func TestEmitterDeterminism_SortingStability(t *testing.T) {
	hasher := determinism.NewDeltaHasher()

	// Create delta with elements that have identical sort keys to test stability
	createDeltaWithDuplicateKeys := func() *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: 20,
					Skipped:     0,
					DurationMs:  1000,
				},
			},
			Controllers: []emitter.Controller{
				// Multiple controllers with same FQCN but different methods
				{FQCN: "App\\Http\\Controllers\\TestController", Method: "zmethod"},
				{FQCN: "App\\Http\\Controllers\\TestController", Method: "amethod"},
				{FQCN: "App\\Http\\Controllers\\TestController", Method: "mmethod"},
				// Same FQCN and method should maintain creation order
				{FQCN: "App\\Http\\Controllers\\SameController", Method: "same"},
				{FQCN: "App\\Http\\Controllers\\SameController", Method: "same"},
			},
			Models: []emitter.Model{
				// Models with same FQCN should maintain stable order
				{FQCN: "App\\Models\\TestModel"},
				{FQCN: "App\\Models\\TestModel"},
			},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Create multiple instances
	deltas := make([]*emitter.Delta, 10)
	for i := 0; i < 10; i++ {
		deltas[i] = createDeltaWithDuplicateKeys()
	}

	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash deltas with duplicate keys: %v", err)
	}

	if !hashResult.AllIdentical {
		t.Error("Sorting stability failure: identical deltas produced different hashes")
		logHashDetails(t, hashResult)
	}

	t.Logf("Sorting stability test: %d deltas, 1 unique hash", len(deltas))
}

// TestMatcherDeterminism_OrderingInvariance tests that the order of
// processing files doesn't affect the final deterministic output.
func TestMatcherDeterminism_OrderingInvariance(t *testing.T) {
	hasher := determinism.NewDeltaHasher()

	// Create delta representing analysis results from files processed in different orders
	createDeltaFromFileOrder := func(fileOrder []string) *emitter.Delta {
		controllers := make([]emitter.Controller, len(fileOrder))
		
		for i, fileName := range fileOrder {
			controllers[i] = emitter.Controller{
				FQCN:   fmt.Sprintf("App\\Http\\Controllers\\%sController", fileName),
				Method: "index",
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{200}[0],
					Explicit: &[]bool{true}[0],
				},
			}
		}

		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(len(fileOrder)),
					Skipped:     0,
					DurationMs:  500,
				},
			},
			Controllers: controllers,
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Test different file processing orders
	baseFiles := []string{"User", "Post", "Comment", "Product", "Order"}
	
	// Forward order
	delta1 := createDeltaFromFileOrder(baseFiles)
	
	// Reverse order
	reverseFiles := make([]string, len(baseFiles))
	for i, file := range baseFiles {
		reverseFiles[len(baseFiles)-1-i] = file
	}
	delta2 := createDeltaFromFileOrder(reverseFiles)
	
	// Random order
	randomFiles := []string{"Comment", "Order", "User", "Product", "Post"}
	delta3 := createDeltaFromFileOrder(randomFiles)

	deltas := []*emitter.Delta{delta1, delta2, delta3}

	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash file order deltas: %v", err)
	}

	// Results should be identical regardless of input order
	// because the emitter sorts all collections
	if !hashResult.AllIdentical {
		t.Error("File processing order affects deterministic output")
		logHashDetails(t, hashResult)
		
		// Debug: show the actual structures to identify ordering issues
		for i, delta := range deltas {
			t.Logf("Delta %d controllers:", i)
			for j, controller := range delta.Controllers {
				t.Logf("  %d: %s.%s", j, controller.FQCN, controller.Method)
			}
		}
	}

	t.Logf("File order invariance test: %d different orders, 1 unique hash", len(deltas))
}

// Helper functions

func logHashDetails(t *testing.T, hashResult *determinism.MultiHashResult) {
	t.Helper()
	
	t.Logf("Hash comparison details:")
	t.Logf("  All identical: %v", hashResult.AllIdentical)
	t.Logf("  All canonical: %v", hashResult.AllCanonical)
	t.Logf("  Unique hashes: %d", len(hashResult.UniqueHashes))
	
	for i, hash := range hashResult.Hashes {
		if hash != nil {
			t.Logf("  Hash %d: %s (size: %d)", i, hash.SHA256[:16]+"...", hash.Size)
		}
	}
	
	if len(hashResult.Errors) > 0 {
		t.Logf("  Errors: %v", hashResult.Errors)
	}
}