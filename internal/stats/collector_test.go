package stats

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewStatsCollector(t *testing.T) {
	collector := NewStatsCollector()

	if collector == nil {
		t.Fatal("NewStatsCollector() returned nil")
	}

	stats := collector.GetStats()
	if stats == nil {
		t.Fatal("GetStats() returned nil")
	}

	// Check initial values
	if stats.GetFilesProcessed() != 0 {
		t.Errorf("Initial files processed should be 0, got %d", stats.GetFilesProcessed())
	}

	if stats.GetFilesSkipped() != 0 {
		t.Errorf("Initial files skipped should be 0, got %d", stats.GetFilesSkipped())
	}

	if stats.IsPartial() {
		t.Error("Initial partial flag should be false")
	}
}

func TestRecordFilesProcessed(t *testing.T) {
	tests := []struct {
		name     string
		counts   []int
		expected int64
	}{
		{
			name:     "single positive count",
			counts:   []int{5},
			expected: 5,
		},
		{
			name:     "multiple positive counts",
			counts:   []int{3, 7, 2},
			expected: 12,
		},
		{
			name:     "with zero count",
			counts:   []int{5, 0, 3},
			expected: 8,
		},
		{
			name:     "with negative count",
			counts:   []int{5, -2, 3},
			expected: 8, // negative counts should be ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewStatsCollector()

			for _, count := range tt.counts {
				collector.RecordFilesProcessed(count)
			}

			stats := collector.GetStats()
			if got := stats.GetFilesProcessed(); got != tt.expected {
				t.Errorf("GetFilesProcessed() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestRecordFilesSkipped(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordFilesSkipped(3)
	collector.RecordFilesSkipped(2)

	stats := collector.GetStats()
	if got := stats.GetFilesSkipped(); got != 5 {
		t.Errorf("GetFilesSkipped() = %d, want 5", got)
	}
}

func TestRecordProcessingTime(t *testing.T) {
	collector := NewStatsCollector()

	// Test different phases
	collector.RecordProcessingTime(string(PhaseIndexing), 100*time.Millisecond)
	collector.RecordProcessingTime(string(PhaseParsing), 200*time.Millisecond)
	collector.RecordProcessingTime(string(PhaseIndexing), 50*time.Millisecond)

	stats := collector.GetStats()

	// Check total duration
	if got := stats.GetProcessingTime(); got != 350 {
		t.Errorf("GetProcessingTime() = %d, want 350", got)
	}

	// Check phase stats
	phaseStats := stats.GetPhaseStatsTyped()
	if got := phaseStats[string(PhaseIndexing)]; got != 150 {
		t.Errorf("PhaseStats[indexing] = %d, want 150", got)
	}
	if got := phaseStats[string(PhaseParsing)]; got != 200 {
		t.Errorf("PhaseStats[parsing] = %d, want 200", got)
	}
}

func TestRecordError(t *testing.T) {
	collector := NewStatsCollector()

	err1 := &ParseError{message: "parse error"}
	err2 := &ParseError{message: "another error"}

	collector.RecordError(string(PhaseParsing), err1)
	collector.RecordError(string(PhaseMatching), err2)
	collector.RecordError("", err1) // empty phase

	stats := collector.GetStats()
	if got := stats.GetErrorCount(); got != 3 {
		t.Errorf("GetErrorCount() = %d, want 3", got)
	}

	// Check phase-specific error tracking
	phaseStats := stats.GetPhaseStatsTyped()
	if got := phaseStats["parsing_errors"]; got != 1 {
		t.Errorf("PhaseStats[parsing_errors] = %d, want 1", got)
	}
	if got := phaseStats["matching_errors"]; got != 1 {
		t.Errorf("PhaseStats[matching_errors] = %d, want 1", got)
	}
}

func TestRecordMatch(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordMatch(string(MatchTypeHTTPStatus), 5)
	collector.RecordMatch(string(MatchTypeResource), 3)
	collector.RecordMatch(string(MatchTypeHTTPStatus), 2)

	stats := collector.GetStats()
	matchStats := stats.GetMatchStatsTyped()

	if got := matchStats[string(MatchTypeHTTPStatus)]; got != 7 {
		t.Errorf("MatchStats[http_status] = %d, want 7", got)
	}
	if got := matchStats[string(MatchTypeResource)]; got != 3 {
		t.Errorf("MatchStats[resource] = %d, want 3", got)
	}
}

func TestRecordInferenceOperation(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordInferenceOperation(string(InferenceOpConsolidation), 3)
	collector.RecordInferenceOperation(string(InferenceOpMerging), 5)

	stats := collector.GetStats()
	if got := stats.InferenceOps; got != 8 {
		t.Errorf("InferenceOps = %d, want 8", got)
	}

	phaseStats := stats.GetPhaseStatsTyped()
	if got := phaseStats["inference_consolidation"]; got != 3 {
		t.Errorf("PhaseStats[inference_consolidation] = %d, want 3", got)
	}
	if got := phaseStats["inference_merging"]; got != 5 {
		t.Errorf("PhaseStats[inference_merging] = %d, want 5", got)
	}
}

func TestRecordPropertiesInferred(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordPropertiesInferred(10)
	collector.RecordPropertiesInferred(5)

	stats := collector.GetStats()
	if got := stats.PropertiesInferred; got != 15 {
		t.Errorf("PropertiesInferred = %d, want 15", got)
	}
}

func TestCacheOperations(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordCacheHit()
	collector.RecordCacheHit()
	collector.RecordCacheMiss()

	stats := collector.GetStats()
	if got := stats.CacheHits; got != 2 {
		t.Errorf("CacheHits = %d, want 2", got)
	}
	if got := stats.CacheMisses; got != 1 {
		t.Errorf("CacheMisses = %d, want 1", got)
	}

	// Test cache hit rate calculation
	if collector, ok := collector.(*DefaultStatsCollector); ok {
		hitRate := collector.GetCacheHitRate()
		expected := 66.66666666666666 // 2/3 * 100
		if hitRate != expected {
			t.Errorf("GetCacheHitRate() = %f, want %f", hitRate, expected)
		}
	}
}

func TestSetPartialFlag(t *testing.T) {
	collector := NewStatsCollector()

	// Initially false
	stats := collector.GetStats()
	if stats.IsPartial() {
		t.Error("Initial partial flag should be false")
	}

	// Set to true
	collector.SetPartialFlag(true)
	stats = collector.GetStats()
	if !stats.IsPartial() {
		t.Error("Partial flag should be true after SetPartialFlag(true)")
	}

	// Set back to false
	collector.SetPartialFlag(false)
	stats = collector.GetStats()
	if stats.IsPartial() {
		t.Error("Partial flag should be false after SetPartialFlag(false)")
	}
}

func TestMarkProcessingTimes(t *testing.T) {
	collector := NewStatsCollector()

	start := time.Now().UnixMilli()
	collector.MarkProcessingStart()

	// Small delay to ensure different timestamps
	time.Sleep(1 * time.Millisecond)

	collector.MarkProcessingEnd()

	stats := collector.GetStats()

	if stats.StartTime < start {
		t.Errorf("StartTime %d should be >= %d", stats.StartTime, start)
	}

	if stats.EndTime <= stats.StartTime {
		t.Errorf("EndTime %d should be > StartTime %d", stats.EndTime, stats.StartTime)
	}
}

func TestReset(t *testing.T) {
	collector := NewStatsCollector()

	// Add some data
	collector.RecordFilesProcessed(5)
	collector.RecordFilesSkipped(2)
	collector.SetPartialFlag(true)
	collector.RecordMatch(string(MatchTypeHTTPStatus), 3)

	// Verify data exists
	stats := collector.GetStats()
	if stats.GetFilesProcessed() == 0 {
		t.Error("Expected non-zero files processed before reset")
	}

	// Reset
	collector.Reset()

	// Verify reset worked
	stats = collector.GetStats()
	if stats.GetFilesProcessed() != 0 {
		t.Errorf("Files processed should be 0 after reset, got %d", stats.GetFilesProcessed())
	}
	if stats.GetFilesSkipped() != 0 {
		t.Errorf("Files skipped should be 0 after reset, got %d", stats.GetFilesSkipped())
	}
	if stats.IsPartial() {
		t.Error("Partial flag should be false after reset")
	}

	matchStats := stats.GetMatchStatsTyped()
	if len(matchStats) != 0 {
		t.Errorf("Match stats should be empty after reset, got %v", matchStats)
	}
}

func TestConcurrentAccess(t *testing.T) {
	collector := NewStatsCollector()

	const numWorkers = 10
	const numOperationsPerWorker = 100

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Start multiple workers that perform concurrent operations
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < numOperationsPerWorker; j++ {
				collector.RecordFilesProcessed(1)
				collector.RecordFilesSkipped(1)
				collector.RecordMatch(string(MatchTypeHTTPStatus), 1)
				collector.RecordProcessingTime(string(PhaseParsing), time.Millisecond)
				collector.RecordInferenceOperation(string(InferenceOpMerging), 1)
				collector.RecordPropertiesInferred(1)
				collector.RecordCacheHit()
				collector.RecordCacheMiss()

				if j%10 == 0 {
					collector.RecordError(string(PhaseParsing), &ParseError{message: "test error"})
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final counts
	stats := collector.GetStats()
	expectedFiles := int64(numWorkers * numOperationsPerWorker)

	if got := stats.GetFilesProcessed(); got != expectedFiles {
		t.Errorf("Files processed = %d, want %d", got, expectedFiles)
	}

	if got := stats.GetFilesSkipped(); got != expectedFiles {
		t.Errorf("Files skipped = %d, want %d", got, expectedFiles)
	}

	matchStats := stats.GetMatchStatsTyped()
	if got := matchStats[string(MatchTypeHTTPStatus)]; got != int(expectedFiles) {
		t.Errorf("HTTP status matches = %d, want %d", got, expectedFiles)
	}

	expectedErrors := int64(numWorkers * (numOperationsPerWorker / 10))
	if got := stats.GetErrorCount(); got != expectedErrors {
		t.Errorf("Error count = %d, want %d", got, expectedErrors)
	}

	if got := stats.CacheHits; got != expectedFiles {
		t.Errorf("Cache hits = %d, want %d", got, expectedFiles)
	}

	if got := stats.CacheMisses; got != expectedFiles {
		t.Errorf("Cache misses = %d, want %d", got, expectedFiles)
	}
}

func TestJSONMarshalingDeterministic(t *testing.T) {
	collector := NewStatsCollector()

	// Add data in different orders to test deterministic output
	collector.RecordMatch("zeta", 1)
	collector.RecordMatch("alpha", 2)
	collector.RecordMatch("beta", 3)

	collector.RecordProcessingTime("zebra_phase", 100*time.Millisecond)
	collector.RecordProcessingTime("alpha_phase", 200*time.Millisecond)
	collector.RecordProcessingTime("beta_phase", 150*time.Millisecond)

	stats := collector.GetStats()

	// Marshal multiple times and verify consistent output
	var results [][]byte
	for i := 0; i < 5; i++ {
		data, err := json.Marshal(stats)
		if err != nil {
			t.Fatalf("JSON marshaling failed: %v", err)
		}
		results = append(results, data)
	}

	// Compare all results
	baseline := string(results[0])
	for i, result := range results[1:] {
		if string(result) != baseline {
			t.Errorf("JSON output %d differs from baseline:\nBaseline: %s\nResult:   %s",
				i+1, baseline, string(result))
		}
	}

	// Verify that keys are sorted in JSON output string (not after unmarshaling)
	jsonStr := string(results[0])

	// Check that match stats keys appear in alphabetical order in the JSON string
	alphaPos := strings.Index(jsonStr, `"alpha"`)
	betaPos := strings.Index(jsonStr, `"beta"`)
	zetaPos := strings.Index(jsonStr, `"zeta"`)

	// If all keys are present, verify they appear in sorted order
	if alphaPos != -1 && betaPos != -1 && zetaPos != -1 {
		if !(alphaPos < betaPos && betaPos < zetaPos) {
			t.Errorf("Match stats keys are not sorted in JSON string: alpha=%d, beta=%d, zeta=%d", alphaPos, betaPos, zetaPos)
		}
	}
}

func TestBatchOperations(t *testing.T) {
	if collector, ok := NewStatsCollector().(*DefaultStatsCollector); ok {
		matches := map[string]int{
			"http_status": 5,
			"resource":    3,
			"polymorphic": 2,
		}

		collector.BatchRecordMatches(matches)

		stats := collector.GetStats()
		matchStats := stats.GetMatchStatsTyped()

		for matchType, expectedCount := range matches {
			if got := matchStats[matchType]; got != expectedCount {
				t.Errorf("Match stats[%s] = %d, want %d", matchType, got, expectedCount)
			}
		}

		// Test indexing stats recording
		collector.RecordIndexingStats(100, 5, 105, 2*time.Second, true)

		stats = collector.GetStats()
		if got := stats.GetFilesProcessed(); got != 100 {
			t.Errorf("Files processed = %d, want 100", got)
		}
		if got := stats.GetFilesSkipped(); got != 5 {
			t.Errorf("Files skipped = %d, want 5", got)
		}
		if !stats.IsPartial() {
			t.Error("Partial flag should be true")
		}

		phaseStats := stats.GetPhaseStatsTyped()
		if got := phaseStats[string(PhaseIndexing)]; got != 2000 { // 2 seconds = 2000ms
			t.Errorf("Indexing phase time = %d, want 2000", got)
		}
	} else {
		t.Skip("Batch operations test requires DefaultStatsCollector")
	}
}

func TestCalculatedMetrics(t *testing.T) {
	if collector, ok := NewStatsCollector().(*DefaultStatsCollector); ok {
		collector.RecordFilesProcessed(100)
		collector.RecordProcessingTime("total", 5*time.Second)
		collector.RecordTotalFiles(120)
		collector.RecordError("parsing", &ParseError{message: "test"})

		// Test processing throughput
		throughput := collector.GetProcessingThroughput()
		expected := 20.0 // 100 files / 5 seconds
		if throughput != expected {
			t.Errorf("Processing throughput = %f, want %f", throughput, expected)
		}

		// Test error rate
		errorRate := collector.GetErrorRate()
		expectedRate := 1.0 / 120.0 * 100.0 // 1 error out of 120 total files
		if errorRate != expectedRate {
			t.Errorf("Error rate = %f, want %f", errorRate, expectedRate)
		}

		// Test total processing time
		totalTime := collector.GetTotalProcessingTime()
		if totalTime != 5*time.Second {
			t.Errorf("Total processing time = %v, want %v", totalTime, 5*time.Second)
		}

		// Test cache hit rate with no cache operations
		hitRate := collector.GetCacheHitRate()
		if hitRate != 0.0 {
			t.Errorf("Cache hit rate with no operations = %f, want 0.0", hitRate)
		}

	} else {
		t.Skip("Calculated metrics test requires DefaultStatsCollector")
	}
}

func TestRecordPhaseStart(t *testing.T) {
	if collector, ok := NewStatsCollector().(*DefaultStatsCollector); ok {
		// Test the phase timing helper function
		endPhase := collector.RecordPhaseStart("test_phase")

		// Simulate some work
		time.Sleep(10 * time.Millisecond)

		endPhase()

		stats := collector.GetStats()
		phaseStats := stats.GetPhaseStatsTyped()

		if phaseTime := phaseStats["test_phase"]; phaseTime < 10 {
			t.Errorf("Phase time = %d, want >= 10ms", phaseTime)
		}

		if totalTime := stats.GetProcessingTime(); totalTime < 10 {
			t.Errorf("Total time = %d, want >= 10ms", totalTime)
		}
	} else {
		t.Skip("RecordPhaseStart test requires DefaultStatsCollector")
	}
}

func TestEdgeCases(t *testing.T) {
	collector := NewStatsCollector()

	// Test with zero and negative values
	collector.RecordFilesProcessed(0)
	collector.RecordFilesProcessed(-5)
	collector.RecordFilesSkipped(0)
	collector.RecordMatch("", 5)                                  // empty match type
	collector.RecordMatch("test", 0)                              // zero count
	collector.RecordMatch("test", -3)                             // negative count
	collector.RecordProcessingTime("", 100*time.Millisecond)      // empty phase
	collector.RecordProcessingTime("test", 0)                     // zero duration
	collector.RecordProcessingTime("test", -100*time.Millisecond) // negative duration
	collector.RecordError("test", nil)                            // nil error

	stats := collector.GetStats()

	// All should remain zero due to edge case handling
	if stats.GetFilesProcessed() != 0 {
		t.Errorf("Files processed = %d, want 0", stats.GetFilesProcessed())
	}

	if stats.GetFilesSkipped() != 0 {
		t.Errorf("Files skipped = %d, want 0", stats.GetFilesSkipped())
	}

	if stats.GetErrorCount() != 0 {
		t.Errorf("Error count = %d, want 0", stats.GetErrorCount())
	}

	matchStats := stats.GetMatchStatsTyped()
	if len(matchStats) != 0 {
		t.Errorf("Match stats should be empty, got %v", matchStats)
	}
}

// Benchmark concurrent access to ensure performance scales reasonably
func BenchmarkConcurrentStatsCollection(b *testing.B) {
	collector := NewStatsCollector()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.RecordFilesProcessed(1)
			collector.RecordMatch(string(MatchTypeHTTPStatus), 1)
			collector.RecordProcessingTime(string(PhaseParsing), time.Microsecond)
		}
	})
}

// Benchmark JSON marshaling performance
func BenchmarkJSONMarshaling(b *testing.B) {
	collector := NewStatsCollector()

	// Setup some realistic data
	collector.RecordFilesProcessed(1000)
	collector.RecordFilesSkipped(50)
	collector.RecordMatch(string(MatchTypeHTTPStatus), 200)
	collector.RecordMatch(string(MatchTypeResource), 150)
	collector.RecordMatch(string(MatchTypePolymorphic), 75)
	collector.RecordProcessingTime(string(PhaseIndexing), 500*time.Millisecond)
	collector.RecordProcessingTime(string(PhaseParsing), 2*time.Second)
	collector.RecordProcessingTime(string(PhaseMatching), 800*time.Millisecond)

	stats := collector.GetStats()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(stats)
		if err != nil {
			b.Fatal(err)
		}
	}
}
