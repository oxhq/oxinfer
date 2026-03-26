package optimizations

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oxhq/oxinfer/internal/matchers"
	"github.com/oxhq/oxinfer/internal/parser"
)

// mockCompositePatternMatcher simulates pattern matching with realistic delay
type mockCompositePatternMatcher struct {
	processingDelay time.Duration
}

func (m *mockCompositePatternMatcher) MatchAll(ctx context.Context, tree *parser.SyntaxTree, filePath string) (*matchers.LaravelPatterns, error) {
	// Simulate realistic pattern matching processing time
	time.Sleep(m.processingDelay)

	return &matchers.LaravelPatterns{
		FilePath:     filePath,
		HTTPStatus:   []*matchers.HTTPStatusMatch{{Status: 200, Explicit: true}},
		RequestUsage: []*matchers.RequestUsageMatch{{Methods: []string{"GET"}}},
		ProcessedAt:  time.Now().Unix(),
		ProcessingMs: m.processingDelay.Milliseconds(),
	}, nil
}

func (m *mockCompositePatternMatcher) AddMatcher(matcher matchers.PatternMatcher) error { return nil }
func (m *mockCompositePatternMatcher) RemoveMatcher(patternType matchers.PatternType) error {
	return nil
}
func (m *mockCompositePatternMatcher) GetMatchers() map[matchers.PatternType]matchers.PatternMatcher {
	return nil
}
func (m *mockCompositePatternMatcher) IsInitialized() bool { return true }
func (m *mockCompositePatternMatcher) Close() error        { return nil }

// BenchmarkSequentialPatternMatching benchmarks the current sequential approach
func BenchmarkSequentialPatternMatching(b *testing.B) {
	// Simulate realistic pattern matching delay (2ms per file)
	matcher := &mockCompositePatternMatcher{processingDelay: 2 * time.Millisecond}
	ctx := context.Background()

	// Create test files
	files := make([]*ParsedFile, 100)
	for i := range files {
		files[i] = &ParsedFile{
			FilePath:   fmt.Sprintf("app/Models/Model%d.php", i),
			SyntaxTree: &parser.SyntaxTree{},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Sequential processing (current approach)
		filePatterns := make(map[string]*matchers.LaravelPatterns)

		start := time.Now()
		for _, file := range files {
			patterns, err := matcher.MatchAll(ctx, file.SyntaxTree, file.FilePath)
			if err == nil && patterns != nil {
				filePatterns[file.FilePath] = patterns
			}
		}
		duration := time.Since(start)

		// Verify we processed all files
		if len(filePatterns) != len(files) {
			b.Fatalf("Expected %d files processed, got %d", len(files), len(filePatterns))
		}

		// Record timing (sequential should take ~200ms for 100 files * 2ms each)
		b.Logf("Sequential processing: %d files in %v", len(files), duration)
	}
}

// BenchmarkParallelPatternMatching benchmarks the optimized parallel approach
func BenchmarkParallelPatternMatching(b *testing.B) {
	// Simulate realistic pattern matching delay (2ms per file)
	matcher := &mockCompositePatternMatcher{processingDelay: 2 * time.Millisecond}

	// Test different worker configurations
	workerCounts := []int{4, 8, 16, 32}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers%d", workers), func(b *testing.B) {
			parallelMatcher := NewParallelPatternMatcher(matcher, workers)
			ctx := context.Background()

			// Create test files
			files := make([]*ParsedFile, 100)
			for i := range files {
				files[i] = &ParsedFile{
					FilePath:   fmt.Sprintf("app/Models/Model%d.php", i),
					SyntaxTree: &parser.SyntaxTree{},
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				start := time.Now()
				filePatterns, err := parallelMatcher.MatchAllFiles(ctx, files)
				duration := time.Since(start)

				if err != nil {
					b.Fatalf("Parallel processing failed: %v", err)
				}

				// Verify we processed all files
				if len(filePatterns) != len(files) {
					b.Fatalf("Expected %d files processed, got %d", len(files), len(filePatterns))
				}

				// With parallel processing, this should be much faster
				// Expected: ~25ms with 8 workers (200ms / 8), plus some overhead
				b.Logf("Parallel processing (%d workers): %d files in %v", workers, len(files), duration)
			}
		})
	}
}

// TestParallelPatternMatchingSpeedup tests that parallel processing is actually faster
func TestParallelPatternMatchingSpeedup(t *testing.T) {
	// Simulate realistic pattern matching delay
	matcher := &mockCompositePatternMatcher{processingDelay: 5 * time.Millisecond}
	ctx := context.Background()

	// Create test files
	fileCount := 50
	files := make([]*ParsedFile, fileCount)
	for i := range files {
		files[i] = &ParsedFile{
			FilePath:   fmt.Sprintf("app/Models/Model%d.php", i),
			SyntaxTree: &parser.SyntaxTree{},
		}
	}

	// Benchmark sequential processing
	start := time.Now()
	sequentialResults := make(map[string]*matchers.LaravelPatterns)
	for _, file := range files {
		patterns, err := matcher.MatchAll(ctx, file.SyntaxTree, file.FilePath)
		if err == nil && patterns != nil {
			sequentialResults[file.FilePath] = patterns
		}
	}
	sequentialTime := time.Since(start)

	// Benchmark parallel processing
	parallelMatcher := NewParallelPatternMatcher(matcher, 8)
	start = time.Now()
	parallelResults, err := parallelMatcher.MatchAllFiles(ctx, files)
	parallelTime := time.Since(start)

	if err != nil {
		t.Fatalf("Parallel processing failed: %v", err)
	}

	// Verify same number of files processed
	if len(sequentialResults) != len(parallelResults) {
		t.Errorf("Result count mismatch: sequential=%d, parallel=%d",
			len(sequentialResults), len(parallelResults))
	}

	// Calculate speedup
	speedup := float64(sequentialTime) / float64(parallelTime)

	t.Logf("Sequential processing: %d files in %v", fileCount, sequentialTime)
	t.Logf("Parallel processing (8 workers): %d files in %v", fileCount, parallelTime)
	t.Logf("Speedup: %.2fx", speedup)

	// We should see significant speedup (at least 3x with 8 workers)
	if speedup < 3.0 {
		t.Errorf("Expected speedup >= 3.0x, got %.2fx", speedup)
	}

	// Parallel should be much faster than sequential
	if parallelTime >= sequentialTime {
		t.Errorf("Parallel processing should be faster than sequential: %v >= %v",
			parallelTime, sequentialTime)
	}
}

// TestAggregateResults tests the result aggregation functionality
func TestAggregateResults(t *testing.T) {
	matcher := &mockCompositePatternMatcher{processingDelay: 1 * time.Millisecond}
	parallelMatcher := NewParallelPatternMatcher(matcher, 4)

	// Create test data
	filePatterns := map[string]*matchers.LaravelPatterns{
		"file1.php": {
			FilePath:   "file1.php",
			HTTPStatus: []*matchers.HTTPStatusMatch{{Status: 200}, {Status: 404}},
			Resources:  []*matchers.ResourceMatch{{Class: "UserResource"}},
		},
		"file2.php": {
			FilePath:   "file2.php",
			HTTPStatus: []*matchers.HTTPStatusMatch{{Status: 500}},
			Pivots:     []*matchers.PivotMatch{{Relation: "tags"}},
		},
	}

	// Test aggregation
	results := parallelMatcher.AggregateResults(filePatterns)

	// Verify file patterns preserved
	if len(results.FilePatterns) != 2 {
		t.Errorf("Expected 2 file patterns, got %d", len(results.FilePatterns))
	}

	// Verify aggregated matches
	if len(results.HTTPStatusMatches) != 3 { // 2 from file1 + 1 from file2
		t.Errorf("Expected 3 HTTP status matches, got %d", len(results.HTTPStatusMatches))
	}

	if len(results.ResourceMatches) != 1 {
		t.Errorf("Expected 1 resource match, got %d", len(results.ResourceMatches))
	}

	if len(results.PivotMatches) != 1 {
		t.Errorf("Expected 1 pivot match, got %d", len(results.PivotMatches))
	}

	// Verify metrics
	if results.FilesMatched != 2 {
		t.Errorf("Expected 2 files matched, got %d", results.FilesMatched)
	}

	if results.TotalMatches != 5 { // 3 HTTP + 1 Resource + 1 Pivot
		t.Errorf("Expected 5 total matches, got %d", results.TotalMatches)
	}
}
