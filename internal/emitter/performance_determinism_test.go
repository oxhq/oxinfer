// Package emitter provides performance and determinism validation tests.
// These tests ensure that deterministic JSON output is maintained even under
// high load and concurrent operations, validating both performance targets
// and byte-identical output consistency.
package emitter

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestTripleRunDeterministicConsistency validates the critical requirement
// that identical input always produces identical output across multiple runs.
// This is the core determinism guarantee for oxinfer's delta.json output.
func TestTripleRunDeterministicConsistency(t *testing.T) {
	emitter := NewJSONEmitter()

	// Create a complex delta that exercises all sorting paths
	delta := createComplexTestDelta()

	const tripleRunCount = 3
	hashes := make([]string, tripleRunCount)
	outputs := make([][]byte, tripleRunCount)

	// Perform triple-run test as specified in project requirements
	for i := 0; i < tripleRunCount; i++ {
		data, err := emitter.MarshalDeterministic(delta)
		if err != nil {
			t.Fatalf("Run %d: MarshalDeterministic() error = %v", i+1, err)
		}

		outputs[i] = data
		hash := sha256.Sum256(data)
		hashes[i] = fmt.Sprintf("%x", hash)

		t.Logf("Run %d: SHA256 = %s, Length = %d bytes", i+1, hashes[i], len(data))
	}

	// All three runs must produce identical SHA256 hashes
	for i := 1; i < tripleRunCount; i++ {
		if hashes[0] != hashes[i] {
			t.Errorf("Run %d hash mismatch: %s != %s", i+1, hashes[0], hashes[i])
		}
	}

	// Validate byte-for-byte identity
	for i := 1; i < tripleRunCount; i++ {
		if len(outputs[0]) != len(outputs[i]) {
			t.Errorf("Run %d length mismatch: %d != %d", i+1, len(outputs[0]), len(outputs[i]))
			continue
		}

		for j := 0; j < len(outputs[0]); j++ {
			if outputs[0][j] != outputs[i][j] {
				t.Errorf("Run %d byte mismatch at position %d: %d != %d", i+1, j, outputs[0][j], outputs[i][j])
				break
			}
		}
	}

	t.Logf("✅ Triple-run deterministic consistency validated: Hash = %s", hashes[0])
}

// TestConcurrentDeterministicConsistency validates that deterministic output
// is maintained even under concurrent access patterns.
func TestConcurrentDeterministicConsistency(t *testing.T) {
	emitter := NewJSONEmitter()
	delta := createComplexTestDelta()

	const goroutineCount = 10
	const iterationsPerGoroutine = 5

	var wg sync.WaitGroup
	results := make(chan string, goroutineCount*iterationsPerGoroutine)
	errors := make(chan error, goroutineCount*iterationsPerGoroutine)

	// Launch concurrent marshalers
	for i := 0; i < goroutineCount; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < iterationsPerGoroutine; j++ {
				data, err := emitter.MarshalDeterministic(delta)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d iteration %d: %w", goroutineID, j, err)
					return
				}

				hash := sha256.Sum256(data)
				hashStr := fmt.Sprintf("%x", hash)
				results <- hashStr
			}
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Error(err)
	}

	// Collect all hashes and verify they're identical
	var hashes []string
	for hash := range results {
		hashes = append(hashes, hash)
	}

	if len(hashes) != goroutineCount*iterationsPerGoroutine {
		t.Fatalf("Expected %d hashes, got %d", goroutineCount*iterationsPerGoroutine, len(hashes))
	}

	// All hashes must be identical
	expectedHash := hashes[0]
	for i, hash := range hashes {
		if hash != expectedHash {
			t.Errorf("Hash %d mismatch: %s != %s", i, expectedHash, hash)
		}
	}

	t.Logf("✅ Concurrent deterministic consistency validated: %d operations, Hash = %s", len(hashes), expectedHash)
}

// BenchmarkDeterministicMarshalingPerformance measures the performance
// impact of deterministic sorting operations.
func BenchmarkDeterministicMarshalingPerformance(b *testing.B) {
	emitter := NewJSONEmitter()

	benchmarks := []struct {
		name  string
		delta *Delta
	}{
		{
			name:  "empty_delta",
			delta: &Delta{Meta: MetaInfo{}, Controllers: []Controller{}, Models: []Model{}, Polymorphic: []Polymorphic{}, Broadcast: []Broadcast{}},
		},
		{
			name:  "small_delta",
			delta: createSmallTestDelta(),
		},
		{
			name:  "complex_delta",
			delta: createComplexTestDelta(),
		},
		{
			name:  "large_delta",
			delta: createLargeTestDelta(),
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := emitter.MarshalDeterministic(bm.delta)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMemoryUsageStability measures memory allocation patterns
// to ensure no memory leaks or excessive allocations.
func BenchmarkMemoryUsageStability(b *testing.B) {
	emitter := NewJSONEmitter()
	delta := createComplexTestDelta()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := emitter.MarshalDeterministic(delta)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestLargeDatasetPerformance validates that performance targets are met
// for datasets comparable to medium Laravel projects (200-600 files).
func TestLargeDatasetPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	emitter := NewJSONEmitter()

	// Simulate a medium project: ~300 controllers and ~200 models
	delta := createMediumProjectDelta(300, 200)

	const maxProcessingTime = 2 * time.Second // Should be well under 10s target

	start := time.Now()
	data, err := emitter.MarshalDeterministic(delta)
	processingTime := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to marshal medium project delta: %v", err)
	}

	if processingTime > maxProcessingTime {
		t.Errorf("Processing time %v exceeds target %v", processingTime, maxProcessingTime)
	}

	// Validate determinism is maintained even with large datasets
	start2 := time.Now()
	data2, err := emitter.MarshalDeterministic(delta)
	processingTime2 := time.Since(start2)

	if err != nil {
		t.Fatalf("Failed to marshal medium project delta (second run): %v", err)
	}

	hash1 := sha256.Sum256(data)
	hash2 := sha256.Sum256(data2)

	if fmt.Sprintf("%x", hash1) != fmt.Sprintf("%x", hash2) {
		t.Error("Large dataset determinism failed: different hashes between runs")
	}

	t.Logf("✅ Medium project performance validated:")
	t.Logf("   - Controllers: 300, Models: 200")
	t.Logf("   - Processing time 1: %v", processingTime)
	t.Logf("   - Processing time 2: %v", processingTime2)
	t.Logf("   - Output size: %d bytes", len(data))
	t.Logf("   - Hash consistency: %x", hash1)
}

// TestMemoryLeakDetection runs repeated marshaling operations to detect memory leaks.
func TestMemoryLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	emitter := NewJSONEmitter()
	delta := createComplexTestDelta()

	// Force GC to establish baseline
	runtime.GC()
	runtime.GC()

	var initialStats runtime.MemStats
	runtime.ReadMemStats(&initialStats)

	// Run many marshaling operations
	const iterations = 1000
	for i := 0; i < iterations; i++ {
		_, err := emitter.MarshalDeterministic(delta)
		if err != nil {
			t.Fatalf("Iteration %d failed: %v", i, err)
		}

		// Occasional GC to prevent normal buildup from affecting test
		if i%100 == 0 {
			runtime.GC()
		}
	}

	// Force final GC and measure
	runtime.GC()
	runtime.GC()

	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)

	// Check for significant memory increase (handle potential underflow)
	var memoryIncrease int64
	if finalStats.Alloc >= initialStats.Alloc {
		memoryIncrease = int64(finalStats.Alloc - initialStats.Alloc)
	} else {
		memoryIncrease = -int64(initialStats.Alloc - finalStats.Alloc)
	}
	const maxAcceptableIncrease int64 = 1024 * 1024 // 1MB

	if memoryIncrease > maxAcceptableIncrease {
		t.Errorf("Possible memory leak detected: memory increased by %d bytes (max acceptable: %d)",
			memoryIncrease, maxAcceptableIncrease)
	}

	t.Logf("✅ Memory leak test completed:")
	t.Logf("   - Iterations: %d", iterations)
	t.Logf("   - Initial memory: %d bytes", initialStats.Alloc)
	t.Logf("   - Final memory: %d bytes", finalStats.Alloc)
	t.Logf("   - Memory increase: %d bytes", memoryIncrease)
}

// Helper functions to create test data

func createComplexTestDelta() *Delta {
	return &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats: MetaStats{
				FilesParsed: 50,
				Skipped:     5,
				DurationMs:  1500,
			},
		},
		Controllers: []Controller{
			{
				FQCN:   "App\\Http\\Controllers\\UserController",
				Method: "store",
				HTTP:   &HTTPInfo{Status: func() *int { i := 201; return &i }(), Explicit: func() *bool { b := true; return &b }()},
				Request: &RequestInfo{
					ContentTypes: []string{"multipart/form-data", "application/json"},
					Body:         NewOrderedObjectFromMap(map[string]any{"name": map[string]any{}, "email": map[string]any{}}),
					Query:        NewOrderedObjectFromMap(map[string]any{"include": map[string]any{}}),
				},
				Resources: []Resource{
					{Class: "UserResource", Collection: false},
				},
				ScopesUsed: []ScopeUsed{
					{On: "User", Name: "verified", Args: []string{"true"}},
					{On: "User", Name: "active"},
				},
			},
			{
				FQCN:   "App\\Http\\Controllers\\AdminController",
				Method: "index",
				HTTP:   &HTTPInfo{Status: func() *int { i := 200; return &i }(), Explicit: func() *bool { b := false; return &b }()},
			},
		},
		Models: []Model{
			{
				FQCN: "App\\Models\\User",
				WithPivot: []PivotInfo{
					{Relation: "roles", Columns: []string{"level", "granted_at"}, Alias: func() *string { s := "user_roles"; return &s }(), Timestamps: func() *bool { b := true; return &b }()},
				},
				Attributes: []Attribute{
					{Name: "full_name", Via: "Attribute::make"},
					{Name: "display_name", Via: "Attribute::make"},
				},
			},
		},
		Polymorphic: []Polymorphic{
			{
				Parent: "App\\Models\\Comment",
				Morph: MorphInfo{
					Key:        "commentable",
					TypeColumn: "commentable_type",
					IdColumn:   "commentable_id",
				},
				Discriminator: Discriminator{
					PropertyName: "type",
					Mapping: map[string]string{
						"user":    "App\\Models\\User",
						"post":    "App\\Models\\Post",
						"product": "App\\Models\\Product",
					},
				},
				DepthTruncated: func() *bool { b := false; return &b }(),
			},
		},
		Broadcast: []Broadcast{
			{
				Channel:    "user.{id}.notifications",
				Params:     []string{"id"},
				Visibility: "private",
			},
			{
				Channel:    "public.announcements",
				Visibility: "public",
			},
		},
	}
}

func createSmallTestDelta() *Delta {
	return &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats:   MetaStats{FilesParsed: 5, Skipped: 0, DurationMs: 100},
		},
		Controllers: []Controller{
			{FQCN: "App\\Http\\Controllers\\HomeController", Method: "index"},
		},
		Models: []Model{
			{FQCN: "App\\Models\\User"},
		},
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}
}

func createLargeTestDelta() *Delta {
	controllers := make([]Controller, 50)
	for i := 0; i < 50; i++ {
		controllers[i] = Controller{
			FQCN:   fmt.Sprintf("App\\Http\\Controllers\\Controller%d", i),
			Method: "index",
			HTTP:   &HTTPInfo{Status: func() *int { s := 200; return &s }(), Explicit: func() *bool { b := false; return &b }()},
			Resources: []Resource{
				{Class: fmt.Sprintf("Resource%d", i), Collection: i%2 == 0},
			},
		}
	}

	models := make([]Model, 30)
	for i := 0; i < 30; i++ {
		models[i] = Model{
			FQCN: fmt.Sprintf("App\\Models\\Model%d", i),
			Attributes: []Attribute{
				{Name: fmt.Sprintf("attr_%d", i), Via: "Attribute::make"},
			},
		}
	}

	return &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats:   MetaStats{FilesParsed: 100, Skipped: 10, DurationMs: 5000},
		},
		Controllers: controllers,
		Models:      models,
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}
}

func createMediumProjectDelta(controllerCount, modelCount int) *Delta {
	controllers := make([]Controller, controllerCount)
	for i := 0; i < controllerCount; i++ {
		status := generateHTTPStatus(i)
		explicit := i%3 == 0
		controllers[i] = Controller{
			FQCN:   fmt.Sprintf("App\\Http\\Controllers\\%sController", generateControllerName(i)),
			Method: generateMethodName(i),
			HTTP:   &HTTPInfo{Status: &status, Explicit: &explicit},
			Resources: []Resource{
				{Class: fmt.Sprintf("%sResource", generateControllerName(i)), Collection: i%2 == 0},
			},
			ScopesUsed: []ScopeUsed{
				{On: generateModelName(i % 50), Name: generateScopeName(i), Args: generateScopeArgs(i)},
			},
		}
	}

	models := make([]Model, modelCount)
	for i := 0; i < modelCount; i++ {
		timestamps := i%2 == 0
		models[i] = Model{
			FQCN: fmt.Sprintf("App\\Models\\%s", generateModelName(i)),
			WithPivot: []PivotInfo{
				{Relation: generateRelationName(i), Columns: generateColumns(i), Timestamps: &timestamps},
			},
			Attributes: []Attribute{
				{Name: generateAttributeName(i), Via: "Attribute::make"},
			},
		}
	}

	return &Delta{
		Meta: MetaInfo{
			Partial: false,
			Stats:   MetaStats{FilesParsed: int64(controllerCount + modelCount), Skipped: 0, DurationMs: 8000},
		},
		Controllers: controllers,
		Models:      models,
		Polymorphic: []Polymorphic{},
		Broadcast:   []Broadcast{},
	}
}

// Helper generators for creating realistic test data
func generateControllerName(i int) string {
	names := []string{"User", "Post", "Product", "Order", "Payment", "Invoice", "Report", "Admin", "Dashboard", "Profile"}
	return names[i%len(names)]
}

func generateMethodName(i int) string {
	methods := []string{"index", "show", "store", "update", "destroy", "create", "edit"}
	return methods[i%len(methods)]
}

func generateHTTPStatus(i int) int {
	statuses := []int{200, 201, 204, 404, 422}
	return statuses[i%len(statuses)]
}

func generateModelName(i int) string {
	models := []string{"User", "Post", "Product", "Order", "Payment", "Category", "Tag", "Comment", "Review", "Rating"}
	return models[i%len(models)]
}

func generateScopeName(i int) string {
	scopes := []string{"active", "verified", "published", "recent", "popular", "featured"}
	return scopes[i%len(scopes)]
}

func generateScopeArgs(i int) []string {
	if i%3 == 0 {
		return []string{"true"}
	}
	if i%3 == 1 {
		return []string{"active", "verified"}
	}
	return []string{}
}

func generateRelationName(i int) string {
	relations := []string{"users", "posts", "products", "orders", "categories", "tags"}
	return relations[i%len(relations)]
}

func generateColumns(i int) []string {
	if i%4 == 0 {
		return []string{"created_at", "level"}
	}
	if i%4 == 1 {
		return []string{"priority", "status"}
	}
	if i%4 == 2 {
		return []string{"metadata"}
	}
	return []string{"assigned_at", "expires_at", "role"}
}

func generateAttributeName(i int) string {
	attrs := []string{"full_name", "display_name", "avatar_url", "status_text", "formatted_date", "computed_value"}
	return attrs[i%len(attrs)]
}
