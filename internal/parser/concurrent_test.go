// Package parser provides comprehensive tests for concurrent PHP parsing functionality.
// Tests thread safety, resource management, error handling, and integration with manifest configuration.
package parser

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/indexer"
)

// TestConcurrentParsing_Basic tests basic concurrent parsing functionality.
func TestConcurrentParsing_Basic(t *testing.T) {
	parser, err := NewConcurrentPHPParser(4, nil)
	if err != nil {
		t.Fatalf("Failed to create concurrent parser: %v", err)
	}
	defer parser.Shutdown(context.Background())

	jobs := []ParseJob{
		{
			ID:      "job1",
			Content: []byte("<?php class TestClass {}"),
		},
		{
			ID:      "job2",  
			Content: []byte("<?php function testFunction() { return 'hello'; }"),
		},
		{
			ID:      "job3",
			Content: []byte("<?php namespace Test; class AnotherClass {}"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := parser.ParseConcurrently(ctx, jobs)
	if err != nil {
		t.Fatalf("ParseConcurrently failed: %v", err)
	}

	// Collect all results
	var collectedResults []ParseJobResult
	for result := range results {
		collectedResults = append(collectedResults, result)
	}

	// Verify all jobs were processed
	if len(collectedResults) != 3 {
		t.Errorf("Expected 3 results, got %d", len(collectedResults))
	}

	// Verify all jobs succeeded
	for _, result := range collectedResults {
		if result.Error != nil {
			t.Errorf("Job %s failed: %v", result.JobID, result.Error)
		}
		if result.Result == nil {
			t.Errorf("Job %s has nil result", result.JobID)
		}
		if result.WorkerID == "" {
			t.Errorf("Job %s missing worker ID", result.JobID)
		}
	}

	// Verify statistics
	stats := parser.GetStats()
	if stats.ParsedFiles != 3 {
		t.Errorf("Expected 3 parsed files, got %d", stats.ParsedFiles)
	}
	if stats.FailedFiles != 0 {
		t.Errorf("Expected 0 failed files, got %d", stats.FailedFiles)
	}
}

// TestConcurrentParsing_ErrorResilience tests error handling and worker resilience.
func TestConcurrentParsing_ErrorResilience(t *testing.T) {
	parser, err := NewConcurrentPHPParser(4, nil)
	if err != nil {
		t.Fatalf("Failed to create concurrent parser: %v", err)
	}
	defer parser.Shutdown(context.Background())

	jobs := []ParseJob{
		{ID: "valid1", Content: []byte("<?php class ValidClass {}")},
		{ID: "invalid", Content: []byte("<?php class BrokenClass { function test() { /* missing closing brace */")}, // Malformed PHP
		{ID: "empty", Content: []byte("")},                            // Empty content
		{ID: "valid2", Content: []byte("<?php function validFunc() {}")},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := parser.ParseConcurrently(ctx, jobs)
	if err != nil {
		t.Fatalf("ParseConcurrently failed: %v", err)
	}

	var successful, failed int
	resultsByID := make(map[string]ParseJobResult)
	
	for result := range results {
		resultsByID[result.JobID] = result
		if result.Error != nil {
			failed++
		} else {
			successful++
		}
	}

	// Verify results
	if len(resultsByID) != 4 {
		t.Errorf("Expected 4 results, got %d", len(resultsByID))
	}

	// Valid jobs should succeed
	if result, ok := resultsByID["valid1"]; !ok || result.Error != nil {
		t.Errorf("valid1 should succeed, got error: %v", result.Error)
	}
	if result, ok := resultsByID["valid2"]; !ok || result.Error != nil {
		t.Errorf("valid2 should succeed, got error: %v", result.Error)
	}

	// Invalid and empty jobs should fail
	if result, ok := resultsByID["invalid"]; !ok || result.Error == nil {
		t.Errorf("invalid job should fail")
	}
	if result, ok := resultsByID["empty"]; !ok || result.Error == nil {
		t.Errorf("empty job should fail")
	}

	// Verify statistics
	stats := parser.GetStats()
	if stats.ParsedFiles != 2 {
		t.Errorf("Expected 2 parsed files, got %d", stats.ParsedFiles)
	}
	if stats.FailedFiles != 2 {
		t.Errorf("Expected 2 failed files, got %d", stats.FailedFiles)
	}
}

// TestConcurrentParsing_ContextCancellation tests context cancellation handling.
func TestConcurrentParsing_ContextCancellation(t *testing.T) {
	parser, err := NewConcurrentPHPParser(4, nil)
	if err != nil {
		t.Fatalf("Failed to create concurrent parser: %v", err)
	}
	defer parser.Shutdown(context.Background())

	// Create many jobs to ensure some will be cancelled
	jobs := make([]ParseJob, 20)
	for i := 0; i < 20; i++ {
		jobs[i] = ParseJob{
			ID:      fmt.Sprintf("job%d", i),
			Content: []byte("<?php class TestClass { public function test() { sleep(1); } }"),
		}
	}

	// Create context and cancel it immediately after starting
	ctx, cancel := context.WithCancel(context.Background())
	
	results, err := parser.ParseConcurrently(ctx, jobs)
	
	// Cancel context immediately after starting to test cancellation
	cancel()
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Collect results until channel closes
	var collectedResults []ParseJobResult
	for result := range results {
		collectedResults = append(collectedResults, result)
	}

	// Should have fewer results than jobs due to cancellation
	if len(collectedResults) >= 20 {
		t.Errorf("Expected fewer than 20 results due to cancellation, got %d", len(collectedResults))
	}

	t.Logf("Processed %d out of 20 jobs before cancellation", len(collectedResults))
}

// TestConcurrentParsing_WorkerScaling tests worker count scaling functionality.
func TestConcurrentParsing_WorkerScaling(t *testing.T) {
	parser, err := NewConcurrentPHPParser(2, nil)
	if err != nil {
		t.Fatalf("Failed to create concurrent parser: %v", err)
	}
	defer parser.Shutdown(context.Background())

	// Initially should have max 2 workers
	if parser.GetActiveWorkers() > 2 {
		t.Errorf("Expected at most 2 active workers initially")
	}

	// Scale up to 8 workers
	err = parser.SetMaxWorkers(8)
	if err != nil {
		t.Fatalf("Failed to scale up workers: %v", err)
	}

	// Verify scaling
	if parser.maxWorkers != 8 {
		t.Errorf("Expected max workers to be 8, got %d", parser.maxWorkers)
	}

	// Scale down to 4 workers
	err = parser.SetMaxWorkers(4)
	if err != nil {
		t.Fatalf("Failed to scale down workers: %v", err)
	}

	if parser.maxWorkers != 4 {
		t.Errorf("Expected max workers to be 4, got %d", parser.maxWorkers)
	}

	// Test invalid scaling
	err = parser.SetMaxWorkers(0)
	if err == nil {
		t.Error("Expected error when setting max workers to 0")
	}

	err = parser.SetMaxWorkers(-1)
	if err == nil {
		t.Error("Expected error when setting max workers to negative value")
	}
}

// TestConcurrentParsing_ResourceLeaks tests for resource leaks during concurrent operations.
func TestConcurrentParsing_ResourceLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resource leak test in short mode")
	}

	// Monitor initial memory usage
	runtime.GC()
	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)

	parser, err := NewConcurrentPHPParser(4, nil)
	if err != nil {
		t.Fatalf("Failed to create concurrent parser: %v", err)
	}

	// Process many jobs to stress test resource management
	for iteration := 0; iteration < 10; iteration++ {
		jobs := make([]ParseJob, 50)
		for i := 0; i < 50; i++ {
			jobs[i] = ParseJob{
				ID:      fmt.Sprintf("job%d-%d", iteration, i),
				Content: []byte(fmt.Sprintf("<?php class TestClass%d { public function test() {} }", i)),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		results, err := parser.ParseConcurrently(ctx, jobs)
		if err != nil {
			cancel()
			t.Fatalf("ParseConcurrently failed in iteration %d: %v", iteration, err)
		}

		// Consume all results
		for range results {
			// Just consume results to completion
		}
		cancel()

		// Force garbage collection
		runtime.GC()
		runtime.GC()
	}

	// Shutdown parser
	err = parser.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Failed to shutdown parser: %v", err)
	}

	// Check for major memory leaks
	runtime.GC()
	runtime.GC()
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	memIncrease := finalMem.HeapAlloc - initialMem.HeapAlloc
	t.Logf("Memory increase: %d bytes", memIncrease)

	// Allow for some memory increase but flag excessive increases
	if memIncrease > 10*1024*1024 { // 10MB threshold
		t.Errorf("Potential memory leak detected: %d bytes increase", memIncrease)
	}
}

// TestConcurrentParsing_HighConcurrency tests behavior under high concurrent load.
func TestConcurrentParsing_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high concurrency test in short mode")
	}

	parser, err := NewConcurrentPHPParser(8, nil)
	if err != nil {
		t.Fatalf("Failed to create concurrent parser: %v", err)
	}
	defer parser.Shutdown(context.Background())

	// Create large number of jobs
	numJobs := 1000
	jobs := make([]ParseJob, numJobs)
	for i := 0; i < numJobs; i++ {
		jobs[i] = ParseJob{
			ID:      fmt.Sprintf("job%d", i),
			Content: []byte(fmt.Sprintf("<?php class TestClass%d { public function method() { return %d; } }", i, i)),
		}
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results, err := parser.ParseConcurrently(ctx, jobs)
	if err != nil {
		t.Fatalf("ParseConcurrently failed: %v", err)
	}

	// Process all results
	var successCount, errorCount int
	for result := range results {
		if result.Error != nil {
			errorCount++
			t.Logf("Job %s failed: %v", result.JobID, result.Error)
		} else {
			successCount++
		}
	}

	elapsed := time.Since(start)
	t.Logf("Processed %d jobs in %v (%.2f jobs/sec)", numJobs, elapsed, float64(numJobs)/elapsed.Seconds())

	if successCount+errorCount != numJobs {
		t.Errorf("Expected %d total results, got %d", numJobs, successCount+errorCount)
	}

	// Most jobs should succeed
	if float64(successCount)/float64(numJobs) < 0.95 {
		t.Errorf("Success rate too low: %d/%d (%.2f%%)", successCount, numJobs, float64(successCount)/float64(numJobs)*100)
	}

	// Verify statistics
	stats := parser.GetStats()
	if stats.TotalJobsProcessed != int64(numJobs) {
		t.Errorf("Expected %d total jobs processed, got %d", numJobs, stats.TotalJobsProcessed)
	}
}

// TestConcurrentParsing_ManifestIntegration tests integration with manifest MaxWorkers configuration.
func TestConcurrentParsing_ManifestIntegration(t *testing.T) {
	tests := []struct {
		name       string
		maxWorkers *int
		expected   int
	}{
		{"nil MaxWorkers", nil, 4}, // Default
		{"MaxWorkers set to 8", &[]int{8}[0], 8},
		{"MaxWorkers set to 1", &[]int{1}[0], 1},
		{"MaxWorkers set to 16", &[]int{16}[0], 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewConcurrentPHPParserFromManifest(tt.maxWorkers, nil)
			if err != nil {
				t.Fatalf("Failed to create parser: %v", err)
			}
			defer parser.Shutdown(context.Background())

			if parser.maxWorkers != tt.expected {
				t.Errorf("Expected %d max workers, got %d", tt.expected, parser.maxWorkers)
			}
		})
	}
}

// TestConcurrentParsing_Integration_FileIndexer tests integration with file indexer.
func TestConcurrentParsing_Integration_FileIndexer(t *testing.T) {
	parser, err := NewConcurrentPHPParser(4, nil)
	if err != nil {
		t.Fatalf("Failed to create concurrent parser: %v", err)
	}
	defer parser.Shutdown(context.Background())

	// Simulate FileInfo from file indexer
	files := []indexer.FileInfo{
		{Path: "/test/Controller1.php", Size: 1024},
		{Path: "/test/Controller2.php", Size: 2048},
		{Path: "/test/Model1.php", Size: 512},
		{Path: "/test/Model2.php", Size: 1536},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := parser.ProcessFilesBatch(ctx, files)
	if err != nil {
		t.Fatalf("ProcessFilesBatch failed: %v", err)
	}

	var processedCount int
	for result := range results {
		processedCount++
		// Results may have errors due to missing files, but structure should be correct
		if result.JobID == "" {
			t.Errorf("Missing job ID in result")
		}
		if result.WorkerID == "" {
			t.Errorf("Missing worker ID in result")
		}
	}

	if processedCount != len(files) {
		t.Errorf("Expected %d processed files, got %d", len(files), processedCount)
	}
}

// TestParserPool_Basic tests basic parser pool functionality.
func TestParserPool_Basic(t *testing.T) {
	config := DefaultParserConfig()
	pool, err := NewParserPool(4, config)
	if err != nil {
		t.Fatalf("Failed to create parser pool: %v", err)
	}
	defer pool.Close()

	if pool.Size() != 4 {
		t.Errorf("Expected pool size 4, got %d", pool.Size())
	}

	if pool.ActiveCount() != 0 {
		t.Errorf("Expected 0 active parsers, got %d", pool.ActiveCount())
	}

	if pool.AvailableCount() != 4 {
		t.Errorf("Expected 4 available parsers, got %d", pool.AvailableCount())
	}

	// Test parser acquisition and release
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	parser, err := pool.AcquireParser(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire parser: %v", err)
	}

	if pool.ActiveCount() != 1 {
		t.Errorf("Expected 1 active parser after acquisition, got %d", pool.ActiveCount())
	}

	if pool.AvailableCount() != 3 {
		t.Errorf("Expected 3 available parsers after acquisition, got %d", pool.AvailableCount())
	}

	err = pool.ReleaseParser(parser)
	if err != nil {
		t.Errorf("Failed to release parser: %v", err)
	}

	if pool.ActiveCount() != 0 {
		t.Errorf("Expected 0 active parsers after release, got %d", pool.ActiveCount())
	}

	if pool.AvailableCount() != 4 {
		t.Errorf("Expected 4 available parsers after release, got %d", pool.AvailableCount())
	}
}

// TestParserPool_ConcurrentAccess tests thread-safe parser pool access.
func TestParserPool_ConcurrentAccess(t *testing.T) {
	config := DefaultParserConfig()
	pool, err := NewParserPool(4, config)
	if err != nil {
		t.Fatalf("Failed to create parser pool: %v", err)
	}
	defer pool.Close()

	const numGoroutines = 10
	const operationsPerGoroutine = 20

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*operationsPerGoroutine)

	// Start multiple goroutines that acquire and release parsers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				
				parser, err := pool.AcquireParser(ctx)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d operation %d: acquire failed: %w", goroutineID, j, err)
					cancel()
					continue
				}

				// Simulate some work
				time.Sleep(time.Millisecond)

				err = pool.ReleaseParser(parser)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d operation %d: release failed: %w", goroutineID, j, err)
				}
				
				cancel()
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}

	// Verify final state
	if pool.ActiveCount() != 0 {
		t.Errorf("Expected 0 active parsers at end, got %d", pool.ActiveCount())
	}

	// Verify pool health
	healthy, issues := pool.IsHealthy()
	if !healthy {
		t.Errorf("Pool is not healthy: %v", issues)
	}
}

// TestParserPool_Resize tests dynamic pool resizing functionality.
func TestParserPool_Resize(t *testing.T) {
	config := DefaultParserConfig()
	pool, err := NewParserPool(4, config)
	if err != nil {
		t.Fatalf("Failed to create parser pool: %v", err)
	}
	defer pool.Close()

	// Test increasing size
	err = pool.Resize(8)
	if err != nil {
		t.Fatalf("Failed to resize pool to 8: %v", err)
	}

	if pool.Size() != 8 {
		t.Errorf("Expected pool size 8 after resize, got %d", pool.Size())
	}

	if pool.AvailableCount() != 8 {
		t.Errorf("Expected 8 available parsers after resize, got %d", pool.AvailableCount())
	}

	// Test decreasing size
	err = pool.Resize(2)
	if err != nil {
		t.Fatalf("Failed to resize pool to 2: %v", err)
	}

	if pool.Size() != 2 {
		t.Errorf("Expected pool size 2 after resize, got %d", pool.Size())
	}

	if pool.AvailableCount() != 2 {
		t.Errorf("Expected 2 available parsers after resize, got %d", pool.AvailableCount())
	}

	// Test invalid resize
	err = pool.Resize(0)
	if err == nil {
		t.Error("Expected error when resizing to 0")
	}
}

// BenchmarkConcurrentParsing_Performance tests concurrent parsing performance.
func BenchmarkConcurrentParsing_Performance(b *testing.B) {
	parser, err := NewConcurrentPHPParser(4, nil)
	if err != nil {
		b.Fatalf("Failed to create concurrent parser: %v", err)
	}
	defer parser.Shutdown(context.Background())

	jobs := make([]ParseJob, 100)
	for i := 0; i < 100; i++ {
		jobs[i] = ParseJob{
			ID:      fmt.Sprintf("job%d", i),
			Content: []byte(fmt.Sprintf("<?php class TestClass%d { public function test() { return %d; } }", i, i)),
		}
	}

	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		results, err := parser.ParseConcurrently(ctx, jobs)
		if err != nil {
			b.Fatalf("ParseConcurrently failed: %v", err)
		}

		// Consume all results
		for range results {
		}
		
		cancel()
	}
}

// BenchmarkSequentialVsConcurrent compares sequential vs concurrent parsing performance.
func BenchmarkSequentialVsConcurrent(b *testing.B) {
	jobs := make([]ParseJob, 50)
	for i := 0; i < 50; i++ {
		jobs[i] = ParseJob{
			ID:      fmt.Sprintf("job%d", i),
			Content: []byte(fmt.Sprintf("<?php class TestClass%d { public function test() { return %d; } }", i, i)),
		}
	}

	b.Run("Sequential", func(b *testing.B) {
		parser, err := NewPHPParser(nil)
		if err != nil {
			b.Fatalf("Failed to create parser: %v", err)
		}
		defer parser.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, job := range jobs {
				_, err := parser.ParseContent(job.Content)
				if err != nil {
					b.Logf("Parse error for job %s: %v", job.ID, err)
				}
			}
		}
	})

	b.Run("Concurrent", func(b *testing.B) {
		parser, err := NewConcurrentPHPParser(4, nil)
		if err != nil {
			b.Fatalf("Failed to create concurrent parser: %v", err)
		}
		defer parser.Shutdown(context.Background())

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			results, err := parser.ParseConcurrently(ctx, jobs)
			if err != nil {
				b.Fatalf("ParseConcurrently failed: %v", err)
			}

			// Consume all results
			for range results {
			}
			
			cancel()
		}
	})
}