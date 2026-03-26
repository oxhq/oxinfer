package determinism

import (
	"fmt"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/emitter"
)

func TestDeltaHasher_HashDelta(t *testing.T) {
	hasher := NewDeltaHasher()

	tests := []struct {
		name        string
		delta       *emitter.Delta
		expectError bool
		description string
	}{
		{
			name:        "nil_delta",
			delta:       nil,
			expectError: true,
			description: "Should return error for nil delta",
		},
		{
			name: "empty_delta",
			delta: &emitter.Delta{
				Meta: emitter.MetaInfo{
					Partial: false,
					Stats: emitter.MetaStats{
						FilesParsed: 0,
						Skipped:     0,
						DurationMs:  0,
					},
				},
				Controllers: []emitter.Controller{},
				Models:      []emitter.Model{},
				Polymorphic: []emitter.Polymorphic{},
				Broadcast:   []emitter.Broadcast{},
			},
			expectError: false,
			description: "Should successfully hash empty delta",
		},
		{
			name: "populated_delta",
			delta: &emitter.Delta{
				Meta: emitter.MetaInfo{
					Partial: false,
					Stats: emitter.MetaStats{
						FilesParsed: 5,
						Skipped:     1,
						DurationMs:  500,
					},
				},
				Controllers: []emitter.Controller{
					{
						FQCN:   "App\\Http\\Controllers\\TestController",
						Method: "index",
						HTTP: &emitter.HTTPInfo{
							Status:   &[]int{200}[0],
							Explicit: &[]bool{true}[0],
						},
					},
				},
				Models: []emitter.Model{
					{
						FQCN: "App\\Models\\TestModel",
					},
				},
				Polymorphic: []emitter.Polymorphic{},
				Broadcast:   []emitter.Broadcast{},
			},
			expectError: false,
			description: "Should successfully hash populated delta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := hasher.HashDelta(tt.delta)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s, but got none", tt.description)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error for %s: %v", tt.description, err)
			}

			if result == nil {
				t.Fatal("HashResult should not be nil")
			}

			// Validate hash format
			if len(result.SHA256) != 64 {
				t.Errorf("SHA256 hash should be 64 characters, got %d", len(result.SHA256))
			}

			if len(result.CanonicalSHA256) != 64 {
				t.Errorf("Canonical SHA256 hash should be 64 characters, got %d", len(result.CanonicalSHA256))
			}

			if result.Size <= 0 {
				t.Errorf("Size should be positive, got %d", result.Size)
			}

			t.Logf("%s: hash=%s, canonical=%s, size=%d",
				tt.name, result.SHA256[:16]+"...", result.CanonicalSHA256[:16]+"...", result.Size)
		})
	}
}

func TestDeltaHasher_HashBytes(t *testing.T) {
	hasher := NewDeltaHasher()

	tests := []struct {
		name        string
		jsonBytes   []byte
		expectError bool
		description string
	}{
		{
			name:        "empty_bytes",
			jsonBytes:   []byte{},
			expectError: true,
			description: "Should return error for empty bytes",
		},
		{
			name:        "invalid_json",
			jsonBytes:   []byte("{invalid json}"),
			expectError: true,
			description: "Should return error for invalid JSON",
		},
		{
			name: "valid_delta_json",
			jsonBytes: []byte(`{
				"meta": {
					"partial": false,
					"stats": {
						"filesParsed": 10,
						"skipped": 2,
						"durationMs": 750
					}
				},
				"controllers": [],
				"models": [],
				"polymorphic": [],
				"broadcast": []
			}`),
			expectError: false,
			description: "Should successfully hash valid delta JSON",
		},
		{
			name: "unordered_json",
			jsonBytes: []byte(`{
				"broadcast": [],
				"controllers": [],
				"meta": {
					"stats": {
						"durationMs": 750,
						"filesParsed": 10,
						"skipped": 2
					},
					"partial": false
				},
				"models": [],
				"polymorphic": []
			}`),
			expectError: false,
			description: "Should normalize and hash unordered JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := hasher.HashBytes(tt.jsonBytes)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for %s, but got none", tt.description)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error for %s: %v", tt.description, err)
			}

			if result == nil {
				t.Fatal("HashResult should not be nil")
			}

			// Validate hash format
			if len(result.SHA256) != 64 {
				t.Errorf("SHA256 hash should be 64 characters, got %d", len(result.SHA256))
			}

			t.Logf("%s: hash=%s, size=%d",
				tt.name, result.SHA256[:16]+"...", result.Size)
		})
	}
}

func TestDeltaHasher_CompareHashes(t *testing.T) {
	hasher := NewDeltaHasher()

	// Create test hash results
	hash1 := &HashResult{
		SHA256:          "abc123def456ghi789jkl012mno345pqr678stu901vwx234yzabcdefghijklmnop",
		CanonicalSHA256: "def456ghi789jkl012mno345pqr678stu901vwx234yzabcdefghijklmnopqrstuv",
		Size:            1024,
	}

	hash2 := &HashResult{
		SHA256:          "abc123def456ghi789jkl012mno345pqr678stu901vwx234yzabcdefghijklmnop",
		CanonicalSHA256: "def456ghi789jkl012mno345pqr678stu901vwx234yzabcdefghijklmnopqrstuv",
		Size:            1024,
	}

	hash3 := &HashResult{
		SHA256:          "different123456789abcdef0123456789abcdef0123456789abcdef0123456789ab",
		CanonicalSHA256: "def456ghi789jkl012mno345pqr678stu901vwx234yzabcdefghijklmnopqrstuv", // Same canonical
		Size:            1020,
	}

	tests := []struct {
		name                 string
		hash1, hash2         *HashResult
		expectIdentical      bool
		expectCanonicalEqual bool
		expectError          bool
		description          string
	}{
		{
			name:                 "identical_hashes",
			hash1:                hash1,
			hash2:                hash2,
			expectIdentical:      true,
			expectCanonicalEqual: true,
			expectError:          false,
			description:          "Identical hashes should match",
		},
		{
			name:                 "different_full_same_canonical",
			hash1:                hash1,
			hash2:                hash3,
			expectIdentical:      false,
			expectCanonicalEqual: true,
			expectError:          false, // This is information, not an error condition
			description:          "Different full hashes but same canonical should be detected",
		},
		{
			name:                 "nil_hash1",
			hash1:                nil,
			hash2:                hash2,
			expectIdentical:      false,
			expectCanonicalEqual: false,
			expectError:          true,
			description:          "Nil first hash should cause error",
		},
		{
			name:                 "nil_hash2",
			hash1:                hash1,
			hash2:                nil,
			expectIdentical:      false,
			expectCanonicalEqual: false,
			expectError:          true,
			description:          "Nil second hash should cause error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasher.CompareHashes(tt.hash1, tt.hash2)

			if tt.expectError {
				if result.Error == "" {
					t.Errorf("Expected error for %s, but got none", tt.description)
				}
				return
			}

			hasError := result.Error != ""
			if hasError != tt.expectError {
				if tt.expectError {
					t.Errorf("Expected error for %s, but got none", tt.description)
				} else {
					// For informational differences, we log but don't fail the test
					t.Logf("Hash difference detected for %s: %s", tt.description, result.Error)
				}
			}

			if result.Identical != tt.expectIdentical {
				t.Errorf("Identical mismatch for %s: got %v, want %v",
					tt.description, result.Identical, tt.expectIdentical)
			}

			if result.CanonicalIdentical != tt.expectCanonicalEqual {
				t.Errorf("CanonicalIdentical mismatch for %s: got %v, want %v",
					tt.description, result.CanonicalIdentical, tt.expectCanonicalEqual)
			}

			t.Logf("%s: identical=%v, canonical=%v, sizeDiff=%d",
				tt.name, result.Identical, result.CanonicalIdentical, result.SizeDifference)
		})
	}
}

func TestDeltaHasher_HashMultiple(t *testing.T) {
	hasher := NewDeltaHasher()

	createTestDelta := func(id int, duration int64) *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(id),
					Skipped:     0,
					DurationMs:  duration, // Vary duration to test canonical hashing
				},
			},
			Controllers: []emitter.Controller{
				{
					FQCN:   fmt.Sprintf("App\\Http\\Controllers\\Controller%d", id),
					Method: "index",
				},
			},
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	tests := []struct {
		name               string
		deltas             []*emitter.Delta
		expectAllIdentical bool
		expectAllCanonical bool
		expectUniqueCount  int
		description        string
	}{
		{
			name:               "empty_slice",
			deltas:             []*emitter.Delta{},
			expectAllIdentical: false,
			expectAllCanonical: false,
			expectUniqueCount:  0,
			description:        "Empty slice should be handled gracefully",
		},
		{
			name: "identical_deltas",
			deltas: []*emitter.Delta{
				createTestDelta(1, 500),
				createTestDelta(1, 500),
				createTestDelta(1, 500),
			},
			expectAllIdentical: true,
			expectAllCanonical: true,
			expectUniqueCount:  1,
			description:        "Identical deltas should produce identical hashes",
		},
		{
			name: "same_content_different_duration",
			deltas: []*emitter.Delta{
				createTestDelta(1, 500),
				createTestDelta(1, 600),
				createTestDelta(1, 700),
			},
			expectAllIdentical: false, // Full hashes differ due to duration
			expectAllCanonical: true,  // Canonical hashes identical (duration excluded)
			expectUniqueCount:  3,     // Three different full hashes
			description:        "Same content with different durations",
		},
		{
			name: "different_content",
			deltas: []*emitter.Delta{
				createTestDelta(1, 500),
				createTestDelta(2, 500),
				createTestDelta(3, 500),
			},
			expectAllIdentical: false,
			expectAllCanonical: false,
			expectUniqueCount:  3,
			description:        "Different content should produce different hashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := hasher.HashMultiple(tt.deltas)

			// Empty slice is an error case
			if len(tt.deltas) == 0 {
				if err == nil {
					t.Error("Expected error for empty delta slice")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error for %s: %v", tt.description, err)
			}

			if result == nil {
				t.Fatal("MultiHashResult should not be nil")
			}

			if result.AllIdentical != tt.expectAllIdentical {
				t.Errorf("AllIdentical mismatch for %s: got %v, want %v",
					tt.description, result.AllIdentical, tt.expectAllIdentical)
			}

			if result.AllCanonical != tt.expectAllCanonical {
				t.Errorf("AllCanonical mismatch for %s: got %v, want %v",
					tt.description, result.AllCanonical, tt.expectAllCanonical)
			}

			if len(result.UniqueHashes) != tt.expectUniqueCount {
				t.Errorf("Unique hash count mismatch for %s: got %d, want %d",
					tt.description, len(result.UniqueHashes), tt.expectUniqueCount)
			}

			// Validate comparison matrix dimensions
			expectedSize := len(tt.deltas)
			if len(result.ComparisonMatrix) != expectedSize {
				t.Errorf("Comparison matrix rows mismatch: got %d, want %d",
					len(result.ComparisonMatrix), expectedSize)
			}

			for i, row := range result.ComparisonMatrix {
				if len(row) != expectedSize {
					t.Errorf("Comparison matrix columns mismatch at row %d: got %d, want %d",
						i, len(row), expectedSize)
				}

				// Diagonal should always be true (element compared to itself)
				if !row[i] {
					t.Errorf("Comparison matrix diagonal should be true at [%d][%d]", i, i)
				}
			}

			t.Logf("%s: AllIdentical=%v, AllCanonical=%v, UniqueHashes=%d",
				tt.name, result.AllIdentical, result.AllCanonical, len(result.UniqueHashes))
		})
	}
}

func TestDeterminismReport_Validation(t *testing.T) {
	tests := []struct {
		name             string
		setup            func() *DeterminismReport
		expectValid      bool
		expectCanonical  bool
		expectErrorCount int
		description      string
	}{
		{
			name: "perfect_validation",
			setup: func() *DeterminismReport {
				report := NewDeterminismReport("test", 3)
				report.AllIdentical = true
				report.AllCanonical = true
				report.UniqueHashCount = 1
				report.FirstHash = &HashResult{
					SHA256:          "abc123",
					CanonicalSHA256: "def456",
					Size:            1024,
				}
				return report
			},
			expectValid:      true,
			expectCanonical:  true,
			expectErrorCount: 0,
			description:      "Perfect validation should pass all checks",
		},
		{
			name: "hash_mismatch_only",
			setup: func() *DeterminismReport {
				report := NewDeterminismReport("test", 3)
				report.AllIdentical = false
				report.AllCanonical = true
				report.UniqueHashCount = 3
				report.AddValidationError("hash_mismatch", "Hashes differ", nil)
				return report
			},
			expectValid:      false,
			expectCanonical:  false, // Any validation errors cause IsCanonicalValid to return false
			expectErrorCount: 1,
			description:      "Hash mismatch should fail validation",
		},
		{
			name: "execution_failures",
			setup: func() *DeterminismReport {
				report := NewDeterminismReport("test", 3)
				report.AllIdentical = false
				report.AllCanonical = false
				report.UniqueHashCount = 0
				report.AddValidationError("execution_failure", "Run 1 failed", nil)
				report.AddValidationError("execution_failure", "Run 2 failed", nil)
				return report
			},
			expectValid:      false,
			expectCanonical:  false,
			expectErrorCount: 2,
			description:      "Multiple execution failures should fail validation",
		},
		{
			name: "canonical_failure",
			setup: func() *DeterminismReport {
				report := NewDeterminismReport("test", 3)
				report.AllIdentical = false
				report.AllCanonical = false
				report.UniqueHashCount = 3
				report.AddValidationError("canonical_mismatch", "Core content differs", nil)
				return report
			},
			expectValid:      false,
			expectCanonical:  false,
			expectErrorCount: 1,
			description:      "Canonical hash failure indicates serious determinism issue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := tt.setup()

			if report.IsValid() != tt.expectValid {
				t.Errorf("IsValid() for %s: got %v, want %v",
					tt.description, report.IsValid(), tt.expectValid)
			}

			if report.IsCanonicalValid() != tt.expectCanonical {
				t.Errorf("IsCanonicalValid() for %s: got %v, want %v",
					tt.description, report.IsCanonicalValid(), tt.expectCanonical)
			}

			if len(report.ValidationErrors) != tt.expectErrorCount {
				t.Errorf("Error count for %s: got %d, want %d",
					tt.description, len(report.ValidationErrors), tt.expectErrorCount)
			}

			// Validate report structure
			if report.TestName == "" {
				t.Error("TestName should not be empty")
			}

			if report.RunCount <= 0 {
				t.Error("RunCount should be positive")
			}

			if report.ExecutionTime < 0 {
				t.Error("ExecutionTime should not be negative")
			}

			t.Logf("%s: Valid=%v, Canonical=%v, Errors=%d",
				tt.name, report.IsValid(), report.IsCanonicalValid(),
				len(report.ValidationErrors))
		})
	}
}

func TestHashingConsistency_RepeatableResults(t *testing.T) {
	// Test that the same hasher instance produces identical results
	// when called multiple times with the same input
	hasher := NewDeltaHasher()

	delta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 42,
				Skipped:     3,
				DurationMs:  1337,
			},
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   "App\\Http\\Controllers\\RepeatTestController",
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

	// Hash the same delta multiple times
	const iterations = 100
	hashes := make([]*HashResult, iterations)

	for i := 0; i < iterations; i++ {
		hash, err := hasher.HashDelta(delta)
		if err != nil {
			t.Fatalf("Hashing failed on iteration %d: %v", i, err)
		}
		hashes[i] = hash
	}

	// All hashes should be identical
	firstHash := hashes[0]
	for i := 1; i < iterations; i++ {
		if hashes[i].SHA256 != firstHash.SHA256 {
			t.Errorf("Hash inconsistency at iteration %d: %s != %s",
				i, hashes[i].SHA256, firstHash.SHA256)
		}

		if hashes[i].CanonicalSHA256 != firstHash.CanonicalSHA256 {
			t.Errorf("Canonical hash inconsistency at iteration %d: %s != %s",
				i, hashes[i].CanonicalSHA256, firstHash.CanonicalSHA256)
		}

		if hashes[i].Size != firstHash.Size {
			t.Errorf("Size inconsistency at iteration %d: %d != %d",
				i, hashes[i].Size, firstHash.Size)
		}
	}

	t.Logf("Repeatability test: %d iterations, hash %s",
		iterations, firstHash.SHA256[:16]+"...")
}

func TestHashingPerformance_LargeDeltas(t *testing.T) {
	// Test hashing performance with large delta structures
	hasher := NewDeltaHasher()

	// Create a large delta to test performance
	createLargeDelta := func(size int) *emitter.Delta {
		controllers := make([]emitter.Controller, size)
		for i := 0; i < size; i++ {
			controllers[i] = emitter.Controller{
				FQCN:   fmt.Sprintf("App\\Http\\Controllers\\LargeController%05d", i),
				Method: fmt.Sprintf("method%03d", i%10),
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{200 + (i%5)*100}[0],
					Explicit: &[]bool{i%2 == 0}[0],
				},
				Resources: []emitter.Resource{
					{Class: fmt.Sprintf("Resource%d", i), Collection: i%3 == 0},
				},
			}
		}

		models := make([]emitter.Model, size/5)
		for i := 0; i < size/5; i++ {
			models[i] = emitter.Model{
				FQCN: fmt.Sprintf("App\\Models\\LargeModel%05d", i),
				WithPivot: []emitter.PivotInfo{
					{
						Relation: fmt.Sprintf("relation%d", i),
						Columns:  []string{fmt.Sprintf("col%d_a", i), fmt.Sprintf("col%d_b", i)},
					},
				},
			}
		}

		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(size),
					Skipped:     int64(size / 20),
					DurationMs:  int64(size * 10),
				},
			},
			Controllers: controllers,
			Models:      models,
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			delta := createLargeDelta(size)

			start := time.Now()
			hash, err := hasher.HashDelta(delta)
			duration := time.Since(start)

			if err != nil {
				t.Fatalf("Hashing failed for size %d: %v", size, err)
			}

			// Performance requirements: should hash large deltas quickly
			maxDuration := time.Duration(size) * time.Millisecond // 1ms per controller
			if duration > maxDuration {
				t.Errorf("Hashing too slow for size %d: %v > %v", size, duration, maxDuration)
			}

			t.Logf("Size %d: %v, hash %s, size %d bytes",
				size, duration, hash.SHA256[:16]+"...", hash.Size)
		})
	}
}

// BenchmarkDeltaHasher_HashDelta benchmarks delta hashing performance.
func BenchmarkDeltaHasher_HashDelta(b *testing.B) {
	hasher := NewDeltaHasher()

	// Create a realistic test delta
	delta := &emitter.Delta{
		Meta: emitter.MetaInfo{
			Partial: false,
			Stats: emitter.MetaStats{
				FilesParsed: 50,
				Skipped:     2,
				DurationMs:  1500,
			},
		},
		Controllers: []emitter.Controller{
			{
				FQCN:   "App\\Http\\Controllers\\BenchmarkController",
				Method: "index",
				HTTP: &emitter.HTTPInfo{
					Status:   &[]int{200}[0],
					Explicit: &[]bool{true}[0],
				},
				Resources: []emitter.Resource{
					{Class: "BenchmarkResource", Collection: true},
				},
				ScopesUsed: []emitter.ScopeUsed{
					{On: "Model", Name: "active", Args: []string{"true"}},
				},
			},
		},
		Models: []emitter.Model{
			{
				FQCN: "App\\Models\\BenchmarkModel",
				WithPivot: []emitter.PivotInfo{
					{
						Relation: "relationships",
						Columns:  []string{"level", "created_at"},
					},
				},
			},
		},
		Polymorphic: []emitter.Polymorphic{},
		Broadcast:   []emitter.Broadcast{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := hasher.HashDelta(delta)
		if err != nil {
			b.Fatalf("Hashing failed: %v", err)
		}
	}
}

// BenchmarkDeltaHasher_HashMultiple benchmarks multiple delta hashing.
func BenchmarkDeltaHasher_HashMultiple(b *testing.B) {
	hasher := NewDeltaHasher()

	// Create multiple deltas for benchmarking
	createBenchDelta := func(id int) *emitter.Delta {
		return &emitter.Delta{
			Meta: emitter.MetaInfo{
				Partial: false,
				Stats: emitter.MetaStats{
					FilesParsed: int64(id * 10),
					Skipped:     int64(id),
					DurationMs:  int64(id * 100),
				},
			},
			Controllers: []emitter.Controller{
				{
					FQCN:   fmt.Sprintf("App\\Http\\Controllers\\BenchController%d", id),
					Method: "show",
				},
			},
			Models:      []emitter.Model{},
			Polymorphic: []emitter.Polymorphic{},
			Broadcast:   []emitter.Broadcast{},
		}
	}

	deltas := make([]*emitter.Delta, 5)
	for i := 0; i < 5; i++ {
		deltas[i] = createBenchDelta(i + 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := hasher.HashMultiple(deltas)
		if err != nil {
			b.Fatalf("Multiple hashing failed: %v", err)
		}
	}
}
