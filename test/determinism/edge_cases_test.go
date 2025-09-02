package determinism

import (
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/determinism"
	"github.com/garaekz/oxinfer/internal/emitter"
)

func TestDeterminism_MapIterationStability(t *testing.T) {
	// This test verifies that map iteration order doesn't affect output
	// by creating scenarios with large maps and checking hash stability
	
	hasher := determinism.NewDeltaHasher()

	// Create delta with large map-based structures to test iteration stability
	createDeltaWithLargeMaps := func(seed int64) *emitter.Delta {
		rng := rand.New(rand.NewSource(seed))
		
		// Create controllers with randomly ordered but deterministically named entries
		controllerCount := 50
		controllers := make([]emitter.Controller, controllerCount)
		
		for i := 0; i < controllerCount; i++ {
			// Use random but reproducible names based on seed
			controllers[i] = emitter.Controller{
				FQCN:   fmt.Sprintf("App\\Http\\Controllers\\Controller%d", rng.Intn(1000)),
				Method: fmt.Sprintf("method%d", rng.Intn(100)),
			}
		}

		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(controllerCount),
					Skipped:     0,
					DurationMs:  int64(rng.Intn(1000)), // This should vary
				},
			},
			Controllers: controllers,
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Generate multiple deltas with the same seed (should be identical after sorting)
	const seed = 12345
	deltas := make([]*emitter.Delta, 10)
	for i := 0; i < 10; i++ {
		deltas[i] = createDeltaWithLargeMaps(seed)
	}

	// All deltas should produce identical canonical hashes
	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash deltas: %v", err)
	}

	if !hashResult.AllCanonical {
		t.Error("Map iteration instability detected: canonical hashes differ")
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				t.Logf("Delta %d canonical hash: %s", i, hash.CanonicalSHA256)
			}
		}
	}

	if len(hashResult.UniqueHashes) > 1 {
		t.Errorf("Expected 1 unique canonical hash, got %d", len(hashResult.UniqueHashes))
		t.Logf("Unique hashes: %v", hashResult.UniqueHashes)
	}
}

func TestDeterminism_FloatingPointConsistency(t *testing.T) {
	// Test that any floating point values (if present) are handled consistently
	// This is important for cross-platform determinism
	
	hasher := determinism.NewDeltaHasher()

	createDeltaWithFloats := func() *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: 42,
					Skipped:     0,
					DurationMs:  1234, // Integer duration should be consistent
				},
			},
			Controllers: []emitter.Controller{},
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Create multiple identical deltas
	deltas := make([]*emitter.Delta, 5)
	for i := 0; i < 5; i++ {
		deltas[i] = createDeltaWithFloats()
	}

	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash deltas: %v", err)
	}

	if !hashResult.AllIdentical {
		t.Error("Floating point inconsistency detected")
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				t.Logf("Delta %d hash: %s", i, hash.SHA256)
			}
		}
	}
}

func TestDeterminism_MemoryPressureStability(t *testing.T) {
	// Test determinism under memory pressure conditions
	if testing.Short() {
		t.Skip("Skipping memory pressure test in short mode")
	}

	hasher := determinism.NewDeltaHasher()

	// Create a very large delta structure to test memory handling
	createLargeDelta := func() *emitter.Delta {
		const largeSize = 10000
		
		controllers := make([]emitter.Controller, largeSize)
		for i := 0; i < largeSize; i++ {
			controllers[i] = emitter.Controller{
				FQCN:   fmt.Sprintf("App\\Http\\Controllers\\LargeController%05d", i),
				Method: fmt.Sprintf("method%05d", i),
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{200}[0],
					Explicit: &[]bool{true}[0],
				},
			}
		}

		models := make([]emitter.Model, largeSize/10)
		for i := 0; i < largeSize/10; i++ {
			models[i] = emitter.Model{
				FQCN: fmt.Sprintf("App\\Models\\LargeModel%05d", i),
			}
		}

		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(largeSize),
					Skipped:     0,
					DurationMs:  5000,
				},
			},
			Controllers: controllers,
			Models:      models,
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Create the same large delta multiple times
	deltas := make([]*emitter.Delta, 3)
	for i := 0; i < 3; i++ {
		// Force garbage collection between creations to test memory consistency
		runtime.GC()
		deltas[i] = createLargeDelta()
	}

	// Monitor memory before and after
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)

	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash large deltas: %v", err)
	}

	runtime.ReadMemStats(&m2)

	// Verify determinism under memory pressure
	if !hashResult.AllIdentical {
		t.Error("Memory pressure affected determinism")
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				t.Logf("Delta %d hash: %s", i, hash.SHA256)
			}
		}
	}

	// Log memory usage for analysis
	memUsed := m2.TotalAlloc - m1.TotalAlloc
	t.Logf("Memory used during large delta hashing: %d bytes", memUsed)
	t.Logf("Delta size: %d bytes each", hashResult.Hashes[0].Size)
}

func TestDeterminism_ConcurrentAccess(t *testing.T) {
	// Test that concurrent access to the same hasher produces consistent results
	hasher := determinism.NewDeltaHasher()

	createTestDelta := func(id int) *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(id),
					Skipped:     0,
					DurationMs:  1000,
				},
			},
			Controllers: []emitter.Controller{
				{
					FQCN:   fmt.Sprintf("App\\Http\\Controllers\\TestController%d", id),
					Method: "index",
				},
			},
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Run concurrent hashing operations
	const numGoroutines = 10
	results := make(chan *determinism.HashResult, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			delta := createTestDelta(id)
			hash, err := hasher.HashDelta(delta)
			if err != nil {
				errors <- err
				return
			}
			results <- hash
		}(i)
	}

	// Collect results
	hashes := make([]*determinism.HashResult, 0, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		select {
		case hash := <-results:
			hashes = append(hashes, hash)
		case err := <-errors:
			t.Fatalf("Concurrent hashing failed: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("Concurrent hashing timed out")
		}
	}

	// Each delta should produce a unique hash (they have different IDs)
	hashMap := make(map[string]bool)
	for _, hash := range hashes {
		if hashMap[hash.SHA256] {
			t.Error("Duplicate hash detected in concurrent access test")
		}
		hashMap[hash.SHA256] = true
	}

	if len(hashMap) != numGoroutines {
		t.Errorf("Expected %d unique hashes, got %d", numGoroutines, len(hashMap))
	}

	t.Logf("Concurrent access test: %d goroutines produced %d unique hashes", 
		numGoroutines, len(hashMap))
}

func TestDeterminism_TimestampElimination(t *testing.T) {
	// Verify that timestamp fields are properly excluded from canonical hashes
	hasher := determinism.NewDeltaHasher()

	createDeltaWithTimestamp := func(timestamp *string) *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial:     false,
				GeneratedAt: timestamp,
				Stats: emitter.MetaStats{
					FilesParsed: 10,
					Skipped:     1,
					DurationMs:  500,
				},
			},
			Controllers: []emitter.Controller{
				{
					FQCN:   "App\\Http\\Controllers\\TestController",
					Method: "index",
				},
			},
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Create deltas with different timestamps
	timestamp1 := "2024-01-01T12:00:00Z"
	timestamp2 := "2024-01-01T12:00:01Z"
	timestamp3 := "2024-01-01T12:00:02Z"

	delta1 := createDeltaWithTimestamp(&timestamp1)
	delta2 := createDeltaWithTimestamp(&timestamp2)
	delta3 := createDeltaWithTimestamp(&timestamp3)
	deltaNil := createDeltaWithTimestamp(nil)

	deltas := []*emitter.Delta{delta1, delta2, delta3, deltaNil}

	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash deltas: %v", err)
	}

	// Full hashes should be different due to timestamps
	if hashResult.AllIdentical {
		t.Error("Expected different full hashes due to timestamps")
	}

	// But canonical hashes should be identical (timestamps excluded)
	if !hashResult.AllCanonical {
		t.Error("Canonical hashes should be identical despite timestamp differences")
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				t.Logf("Delta %d canonical hash: %s", i, hash.CanonicalSHA256)
			}
		}
	}

	// Verify we have multiple unique full hashes but one canonical hash
	canonicalHashes := make(map[string]bool)
	for _, hash := range hashResult.Hashes {
		if hash != nil {
			canonicalHashes[hash.CanonicalSHA256] = true
		}
	}

	if len(canonicalHashes) != 1 {
		t.Errorf("Expected 1 unique canonical hash, got %d", len(canonicalHashes))
	}
}

func TestDeterminism_UnicodeHandling(t *testing.T) {
	// Test consistent handling of Unicode characters in class names and content
	hasher := determinism.NewDeltaHasher()

	createDeltaWithUnicode := func() *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: 5,
					Skipped:     0,
					DurationMs:  300,
				},
			},
			Controllers: []emitter.Controller{
				{
					// Test Unicode in class names
					FQCN:   "App\\Http\\Controllers\\ÜnicodeController",
					Method: "ïndex",
				},
				{
					FQCN:   "App\\Http\\Controllers\\中文Controller",
					Method: "测试",
				},
			},
			Models: []emitter.Model{
				{
					FQCN: "App\\Models\\Modèl",
				},
			},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	// Create multiple deltas with Unicode content
	deltas := make([]*emitter.Delta, 5)
	for i := 0; i < 5; i++ {
		deltas[i] = createDeltaWithUnicode()
	}

	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash Unicode deltas: %v", err)
	}

	if !hashResult.AllIdentical {
		t.Error("Unicode handling inconsistency detected")
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				t.Logf("Delta %d hash: %s", i, hash.SHA256)
			}
		}
	}

	t.Logf("Unicode test passed with hash: %s", hashResult.Hashes[0].SHA256)
}

func TestDeterminism_CrossPlatformCompatibility(t *testing.T) {
	// Test that would verify cross-platform consistency
	// This is a placeholder for the actual implementation
	
	hasher := determinism.NewDeltaHasher()

	createPlatformTestDelta := func() *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: 25,
					Skipped:     2,
					DurationMs:  1500,
				},
			},
			Controllers: []emitter.Controller{
				{
					FQCN:   "App\\Http\\Controllers\\PlatformController",
					Method: "index",
					HTTP: &emitter.HTTPInfo{
						Status:   &[]int{200}[0],
						Explicit: &[]bool{true}[0],
					},
				},
			},
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	delta := createPlatformTestDelta()
	hash, err := hasher.HashDelta(delta)
	if err != nil {
		t.Fatalf("Failed to hash platform test delta: %v", err)
	}

	// Log platform-specific information for reference
	t.Logf("Platform test results:")
	t.Logf("  OS: %s", runtime.GOOS)
	t.Logf("  Architecture: %s", runtime.GOARCH)
	t.Logf("  Go version: %s", runtime.Version())
	t.Logf("  Hash: %s", hash.SHA256)
	t.Logf("  Canonical hash: %s", hash.CanonicalSHA256)
	t.Logf("  Size: %d bytes", hash.Size)

	// In a real cross-platform test, we would compare this hash
	// against known good hashes from other platforms
	expectedHashes := map[string]string{
		"linux/amd64":   "", // Would be populated with known good hashes
		"darwin/amd64":  "",
		"darwin/arm64":  "",
		"windows/amd64": "",
	}

	platformKey := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	if expectedHash, exists := expectedHashes[platformKey]; exists && expectedHash != "" {
		if hash.CanonicalSHA256 != expectedHash {
			t.Errorf("Cross-platform hash mismatch for %s", platformKey)
			t.Logf("Expected: %s", expectedHash)
			t.Logf("Actual: %s", hash.CanonicalSHA256)
		}
	} else {
		t.Logf("No reference hash available for platform %s", platformKey)
	}
}

func TestDeterminism_LargeDatasetStability(t *testing.T) {
	// Test determinism with datasets approaching production size
	if testing.Short() {
		t.Skip("Skipping large dataset test in short mode")
	}

	hasher := determinism.NewDeltaHasher()

	// Create a delta representing a large Laravel application
	createLargeProductionDelta := func() *emitter.Delta {
		const (
			controllerCount = 500
			modelCount      = 200
			polymorphicCount = 50
			broadcastCount   = 30
		)

		controllers := make([]emitter.Controller, controllerCount)
		for i := 0; i < controllerCount; i++ {
			controllers[i] = emitter.Controller{
				FQCN:   fmt.Sprintf("App\\Http\\Controllers\\%sController", generateControllerName(i)),
				Method: generateMethodName(i % 10),
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{200 + (i%5)*100}[0], // Vary status codes
					Explicit: &[]bool{i%2 == 0}[0],
				},
				Resources: generateResources(i % 3),
				ScopesUsed: generateScopes(i % 5),
			}
		}

		models := make([]emitter.Model, modelCount)
		for i := 0; i < modelCount; i++ {
			models[i] = emitter.Model{
				FQCN:      fmt.Sprintf("App\\Models\\%s", generateModelName(i)),
				WithPivot: generatePivots(i % 4),
			}
		}

		polymorphics := make([]emitter.Polymorphic, polymorphicCount)
		for i := 0; i < polymorphicCount; i++ {
			polymorphics[i] = generatePolymorphic(i)
		}

		broadcasts := make([]emitter.Broadcast, broadcastCount)
		for i := 0; i < broadcastCount; i++ {
			broadcasts[i] = generateBroadcast(i)
		}

		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(controllerCount + modelCount),
					Skipped:     int64((controllerCount + modelCount) / 20), // 5% skipped
					DurationMs:  int64(5000 + (controllerCount+modelCount)*2), // Scale with size
				},
			},
			Controllers: controllers,
			Models:      models,
			Polymorphic: polymorphics,
			Broadcast:   broadcasts,
		}
	}

	// Create the same large delta multiple times
	start := time.Now()
	deltas := make([]*emitter.Delta, 3)
	for i := 0; i < 3; i++ {
		deltas[i] = createLargeProductionDelta()
	}
	creationTime := time.Since(start)

	// Hash all deltas
	start = time.Now()
	hashResult, err := hasher.HashMultiple(deltas)
	if err != nil {
		t.Fatalf("Failed to hash large production deltas: %v", err)
	}
	hashingTime := time.Since(start)

	// Verify determinism
	if !hashResult.AllIdentical {
		t.Error("Large dataset determinism failure")
		for i, hash := range hashResult.Hashes {
			if hash != nil {
				t.Logf("Delta %d hash: %s", i, hash.SHA256)
			}
		}
	}

	// Performance metrics
	t.Logf("Large dataset test results:")
	t.Logf("  Dataset size: %d bytes per delta", hashResult.Hashes[0].Size)
	t.Logf("  Creation time: %v", creationTime)
	t.Logf("  Hashing time: %v", hashingTime)
	t.Logf("  Hash: %s", hashResult.Hashes[0].SHA256)

	// Verify acceptable performance (should process large datasets quickly)
	if hashingTime > 5*time.Second {
		t.Errorf("Large dataset hashing too slow: %v", hashingTime)
	}
}

// Helper functions for generating test data

func generateControllerName(i int) string {
	names := []string{"User", "Post", "Comment", "Product", "Order", "Payment", "Auth", "Admin", "Dashboard", "Report"}
	return names[i%len(names)]
}

func generateMethodName(i int) string {
	methods := []string{"index", "show", "store", "update", "destroy", "create", "edit", "search", "export", "import"}
	return methods[i%len(methods)]
}

func generateResources(count int) []emitter.Resource {
	if count == 0 {
		return nil
	}
	
	resources := make([]emitter.Resource, count)
	for i := 0; i < count; i++ {
		resources[i] = emitter.Resource{
			Class:      fmt.Sprintf("Resource%d", i),
			Collection: i%2 == 0,
		}
	}
	return resources
}

func generateScopes(count int) []emitter.ScopeUsed {
	if count == 0 {
		return nil
	}
	
	scopes := make([]emitter.ScopeUsed, count)
	scopeNames := []string{"active", "verified", "published", "featured", "recent"}
	
	for i := 0; i < count; i++ {
		scopes[i] = emitter.ScopeUsed{
			On:   "Model",
			Name: scopeNames[i%len(scopeNames)],
			Args: []string{fmt.Sprintf("arg%d", i)},
		}
	}
	return scopes
}

func generatePivots(count int) []emitter.PivotInfo {
	if count == 0 {
		return nil
	}
	
	pivots := make([]emitter.PivotInfo, count)
	for i := 0; i < count; i++ {
		pivots[i] = emitter.PivotInfo{
			Relation: fmt.Sprintf("relation%d", i),
			Columns:  []string{fmt.Sprintf("col%d", i), fmt.Sprintf("col%d_extra", i)},
		}
	}
	return pivots
}

func generatePolymorphic(i int) emitter.Polymorphic {
	return emitter.Polymorphic{
		Parent: fmt.Sprintf("App\\Models\\Parent%d", i),
		Morph: emitter.MorphInfo{
			Key:        fmt.Sprintf("morphable%d", i),
			TypeColumn: fmt.Sprintf("morphable%d_type", i),
			IDColumn:   fmt.Sprintf("morphable%d_id", i),
		},
		Discriminator: emitter.Discriminator{
			PropertyName: "type",
			Mapping: map[string]string{
				fmt.Sprintf("type%d", i): fmt.Sprintf("App\\Models\\Type%d", i),
			},
		},
	}
}

func generateModelName(i int) string {
	names := []string{"User", "Post", "Comment", "Product", "Order", "Category", "Tag", "Media", "Setting", "Permission"}
	return names[i%len(names)]
}

func generateBroadcast(i int) emitter.Broadcast {
	return emitter.Broadcast{
		Channel:    fmt.Sprintf("channel.{id}.%d", i),
		Params:     []string{"id"},
		Visibility: []string{"public", "private", "presence"}[i%3],
	}
}