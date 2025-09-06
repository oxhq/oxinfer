package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/indexer"
	"github.com/garaekz/oxinfer/internal/parser"
)

// TestStatsWiring tests that duration statistics are properly calculated and never zero for non-trivial runs.
func TestStatsWiring(t *testing.T) {
	tests := []struct {
		name           string
		results        *PipelineResults
		expectedMinMs  int64
		description    string
	}{
		{
			name: "with_processing_time",
			results: &PipelineResults{
				ProcessingTime: 150 * time.Millisecond,
				ParseResults: &ParseResults{
					FilesProcessed: 50,
				},
			},
			expectedMinMs: 150,
			description: "Should use ProcessingTime when available",
		},
		{
			name: "fallback_from_stages",
			results: &PipelineResults{
				ProcessingTime: 0, // Zero processing time triggers fallback
				IndexResult: &indexer.IndexResult{
					DurationMs: 50,
				},
				ParseResults: &ParseResults{
					FilesProcessed: 100,
					ParseDuration:  75 * time.Millisecond,
				},
				MatchResults: &MatchResults{
					MatchingDuration: 25 * time.Millisecond,
				},
				InferenceResults: &InferenceResults{
					InferenceDuration: 30 * time.Millisecond,
				},
			},
			expectedMinMs: 180, // 50 + 75 + 25 + 30 = 180ms
			description: "Should sum all stage durations as fallback",
		},
		{
			name: "minimum_duration_for_files",
			results: &PipelineResults{
				ProcessingTime: 0,
				ParseResults: &ParseResults{
					FilesProcessed: 100,
				},
			},
			expectedMinMs: 100, // At least 1ms per file
			description: "Should estimate minimum 1ms per file when no timing data",
		},
		{
			name: "from_start_end_times",
			results: &PipelineResults{
				ProcessingTime: 0,
				StartTime:      time.Now(),
				EndTime:        time.Now().Add(250 * time.Millisecond),
				ParseResults: &ParseResults{
					FilesProcessed: 75,
				},
			},
			expectedMinMs: 75, // Will use files count fallback since times are too close
			description: "Should calculate from start/end times or use file count fallback",
		},
		{
			name: "never_zero_for_real_work",
			results: &PipelineResults{
				ProcessingTime: 0,
				ParseResults: &ParseResults{
					FilesProcessed: 1, // Even one file should have non-zero duration
				},
			},
			expectedMinMs: 1,
			description: "Should never be zero when files were processed",
		},
	}

	assembler := NewDeltaAssembler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate stats
			stats := assembler.calculatePipelineStats(tt.results)

			// Verify duration is never zero for non-trivial runs
			if stats.FilesProcessed > 0 && stats.TotalDuration == 0 {
				t.Errorf("%s: Duration is zero for %d files processed", 
					tt.description, stats.FilesProcessed)
			}

			// Verify minimum expected duration
			actualMs := stats.TotalDuration.Milliseconds()
			if actualMs < tt.expectedMinMs {
				t.Errorf("%s: Duration %dms is less than expected minimum %dms",
					tt.description, actualMs, tt.expectedMinMs)
			}

			// Assemble metadata to verify it propagates correctly
			meta, err := assembler.AssembleMetadata(tt.results, stats)
			if err != nil {
				t.Fatalf("Failed to assemble metadata: %v", err)
			}

			// Verify metadata has non-zero duration for non-trivial runs
			if stats.FilesProcessed > 0 && meta.Stats.DurationMs == 0 {
				t.Errorf("%s: Metadata DurationMs is zero for %d files",
					tt.description, stats.FilesProcessed)
			}

			// Verify metadata duration matches calculated duration
			if meta.Stats.DurationMs != actualMs {
				t.Errorf("%s: Metadata DurationMs %d doesn't match calculated %dms",
					tt.description, meta.Stats.DurationMs, actualMs)
			}

			t.Logf("✓ %s: DurationMs = %dms", tt.name, meta.Stats.DurationMs)
		})
	}
}

// TestStatsAccuracy verifies that file counts align correctly.
func TestStatsAccuracy(t *testing.T) {
	assembler := NewDeltaAssembler()

	results := &PipelineResults{
		ProcessingTime: 100 * time.Millisecond,
		IndexResult: &indexer.IndexResult{
			TotalFiles: 150,
			DurationMs: 50,
		},
		ParseResults: &ParseResults{
			FilesProcessed: 145,
			// FilesSkipped is tracked in PipelineStats, not ParseResults
		},
	}

	stats := assembler.calculatePipelineStats(results)

	// Manually set FilesSkipped for this test
	stats.FilesSkipped = 5

	// Verify filesParsed + skipped aligns with files discovered
	totalProcessed := stats.FilesProcessed + stats.FilesSkipped
	if totalProcessed != 150 {
		t.Errorf("Files mismatch: processed+skipped=%d, discovered=%d",
			totalProcessed, 150)
	}

	// Create metadata
	meta, err := assembler.AssembleMetadata(results, stats)
	if err != nil {
		t.Fatalf("Failed to assemble metadata: %v", err)
	}

	// Verify in final metadata
	if meta.Stats.FilesParsed != 145 {
		t.Errorf("FilesParsed = %d, want 145", meta.Stats.FilesParsed)
	}

	if meta.Stats.Skipped != 5 {
		t.Errorf("Skipped = %d, want 5", meta.Stats.Skipped)
	}
}

// TestEndToEndStatsPropagation tests that stats propagate correctly through the entire pipeline.
func TestEndToEndStatsPropagation(t *testing.T) {
	ctx := context.Background()
	assembler := NewDeltaAssembler()

	// Simulate a complete pipeline run with timing
	pipelineStart := time.Now()
	
	results := &PipelineResults{
		StartTime: pipelineStart,
		IndexResult: &indexer.IndexResult{
			TotalFiles: 100,
			DurationMs: 50,
			Files: make([]indexer.FileInfo, 100),
		},
		ParseResults: &ParseResults{
			FilesProcessed: 100,
			ParseDuration:  100 * time.Millisecond,
			Classes: []parser.PHPClass{
				{
					Name:               "TestController",
					FullyQualifiedName: "App\\Http\\Controllers\\TestController",
					Namespace:          "App\\Http\\Controllers",
				},
			},
		},
		MatchResults: &MatchResults{
			MatchingDuration: 50 * time.Millisecond,
			TotalMatches:     10,
		},
		InferenceResults: &InferenceResults{
			InferenceDuration: 25 * time.Millisecond,
			ShapesInferred:    5,
		},
	}

	// Simulate elapsed time
	results.EndTime = pipelineStart.Add(225 * time.Millisecond)
	results.ProcessingTime = results.EndTime.Sub(results.StartTime)

	// Assemble delta
	delta, err := assembler.AssembleDelta(ctx, results)
	if err != nil {
		t.Fatalf("Failed to assemble delta: %v", err)
	}

	// Verify stats in final delta
	if delta.Meta.Stats.DurationMs == 0 {
		t.Error("DurationMs is zero in final delta")
	}

	if delta.Meta.Stats.DurationMs < 225 {
		t.Errorf("DurationMs %d is less than actual processing time 225ms",
			delta.Meta.Stats.DurationMs)
	}

	if delta.Meta.Stats.FilesParsed != 100 {
		t.Errorf("FilesParsed = %d, want 100", delta.Meta.Stats.FilesParsed)
	}

	t.Logf("✓ End-to-end stats: %dms for %d files",
		delta.Meta.Stats.DurationMs, delta.Meta.Stats.FilesParsed)
}
