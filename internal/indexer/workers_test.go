package indexer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockFileProcessor implements FileProcessor for testing
type mockFileProcessor struct {
	processFunc      func(ctx context.Context, file FileInfo) (*ProcessResult, error)
	processDelay     time.Duration
	failureRate      float64
	processedCount   int64
	errorCount       int64
	shouldFail       bool
	mutex            sync.Mutex
}

func newMockFileProcessor() *mockFileProcessor {
	return &mockFileProcessor{
		processFunc: func(ctx context.Context, file FileInfo) (*ProcessResult, error) {
			return &ProcessResult{
				File:       file,
				Cached:     false,
				Error:      nil,
				DurationMs: 0,
			}, nil
		},
	}
}

func (m *mockFileProcessor) ProcessFile(ctx context.Context, file FileInfo) (*ProcessResult, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Simulate processing delay
	if m.processDelay > 0 {
		select {
		case <-time.After(m.processDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	
	// Simulate failures based on failure rate or shouldFail flag
	processCount := atomic.AddInt64(&m.processedCount, 1)
	if m.shouldFail || (m.failureRate > 0 && float64(processCount)/(m.failureRate*100) == 1) {
		atomic.AddInt64(&m.errorCount, 1)
		return &ProcessResult{
			File:  file,
			Error: errors.New("simulated processing error"),
		}, errors.New("simulated processing error")
	}
	
	if m.processFunc != nil {
		return m.processFunc(ctx, file)
	}
	
	return &ProcessResult{
		File:       file,
		Cached:     false,
		Error:      nil,
		DurationMs: 0,
	}, nil
}

func (m *mockFileProcessor) getProcessedCount() int64 {
	return atomic.LoadInt64(&m.processedCount)
}

func (m *mockFileProcessor) getErrorCount() int64 {
	return atomic.LoadInt64(&m.errorCount)
}

// Test helper functions
func createTestFiles(count int) []FileInfo {
	files := make([]FileInfo, count)
	for i := 0; i < count; i++ {
		files[i] = FileInfo{
			Path:        fmt.Sprintf("test/file_%d.php", i),
			AbsPath:     fmt.Sprintf("/tmp/test/file_%d.php", i),
			ModTime:     time.Now().Add(-time.Duration(i) * time.Minute),
			Size:        int64(1000 + i*100),
			IsDirectory: false,
		}
	}
	return files
}

func TestNewWorkerPoolManager(t *testing.T) {
	wpm := NewWorkerPoolManager()
	if wpm == nil {
		t.Fatal("NewWorkerPoolManager returned nil")
	}
	
	if wpm.GetActiveWorkers() != 0 {
		t.Errorf("Expected 0 active workers, got %d", wpm.GetActiveWorkers())
	}
}

func TestWorkerPoolManager_ProcessFiles_Basic(t *testing.T) {
	tests := []struct {
		name         string
		fileCount    int
		maxWorkers   int
		expectedErr  bool
	}{
		{
			name:       "Empty file list",
			fileCount:  0,
			maxWorkers: 2,
			expectedErr: false,
		},
		{
			name:       "Single file single worker",
			fileCount:  1,
			maxWorkers: 1,
			expectedErr: false,
		},
		{
			name:       "Multiple files single worker",
			fileCount:  5,
			maxWorkers: 1,
			expectedErr: false,
		},
		{
			name:       "Multiple files multiple workers",
			fileCount:  10,
			maxWorkers: 4,
			expectedErr: false,
		},
		{
			name:       "Zero workers (should default to CPU count)",
			fileCount:  3,
			maxWorkers: 0,
			expectedErr: false,
		},
		{
			name:       "Negative workers (should default to CPU count)",
			fileCount:  3,
			maxWorkers: -1,
			expectedErr: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wpm := NewWorkerPoolManager()
			processor := newMockFileProcessor()
			files := createTestFiles(tt.fileCount)
			
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			err := wpm.ProcessFiles(ctx, files, tt.maxWorkers, processor)
			
			if tt.expectedErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectedErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			
			if !tt.expectedErr && tt.fileCount > 0 {
				expectedCount := int64(tt.fileCount)
				if processor.getProcessedCount() != expectedCount {
					t.Errorf("Expected %d processed files, got %d", 
						expectedCount, processor.getProcessedCount())
				}
			}
			
			// Clean shutdown
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
			defer shutdownCancel()
			if err := wpm.Shutdown(shutdownCtx); err != nil {
				t.Errorf("Shutdown failed: %v", err)
			}
		})
	}
}

func TestWorkerPoolManager_ConcurrentProcessing(t *testing.T) {
	const fileCount = 50
	const maxWorkers = 8
	const processDelay = 10 * time.Millisecond
	
	wpm := NewWorkerPoolManager()
	processor := newMockFileProcessor()
	processor.processDelay = processDelay
	
	files := createTestFiles(fileCount)
	
	startTime := time.Now()
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
	if err != nil {
		t.Fatalf("ProcessFiles failed: %v", err)
	}
	
	duration := time.Since(startTime)
	
	// With concurrent processing, it should take significantly less time
	// than processing all files sequentially. Allow for some overhead.
	sequentialTime := time.Duration(fileCount) * processDelay
	maxAllowedTime := sequentialTime + (200 * time.Millisecond) // Add 200ms overhead
	if duration >= maxAllowedTime {
		t.Errorf("Expected concurrent processing to be faster. Sequential: %v, Max allowed: %v, Actual: %v", 
			sequentialTime, maxAllowedTime, duration)
	}
	
	if processor.getProcessedCount() != int64(fileCount) {
		t.Errorf("Expected %d processed files, got %d", 
			fileCount, processor.getProcessedCount())
	}
	
	// Clean shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := wpm.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestWorkerPoolManager_ErrorHandling(t *testing.T) {
	const fileCount = 10
	const maxWorkers = 3
	
	wpm := NewWorkerPoolManager()
	processor := newMockFileProcessor()
	processor.shouldFail = true
	
	var errorCount int32
	wpmInternal := wpm.(*workerPoolManager)
	wpmInternal.setErrorHandler(func(err error) {
		atomic.AddInt32(&errorCount, 1)
	})
	
	files := createTestFiles(fileCount)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Processing should continue even with errors
	err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
	if err != nil {
		t.Fatalf("ProcessFiles failed: %v", err)
	}
	
	// All files should have been attempted
	if processor.getProcessedCount() != int64(fileCount) {
		t.Errorf("Expected %d processed files, got %d", 
			fileCount, processor.getProcessedCount())
	}
	
	// All should have failed
	if processor.getErrorCount() != int64(fileCount) {
		t.Errorf("Expected %d errors, got %d", 
			fileCount, processor.getErrorCount())
	}
	
	// Error handler should have been called
	if atomic.LoadInt32(&errorCount) == 0 {
		t.Error("Expected error handler to be called")
	}
	
	// Clean shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := wpm.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestWorkerPoolManager_ContextCancellation(t *testing.T) {
	const fileCount = 100
	const maxWorkers = 4
	
	wpm := NewWorkerPoolManager()
	processor := newMockFileProcessor()
	processor.processDelay = 100 * time.Millisecond // Long delay
	
	files := createTestFiles(fileCount)
	
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	
	startTime := time.Now()
	err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
	duration := time.Since(startTime)
	
	// Should return error due to context timeout
	if err == nil {
		t.Error("Expected context cancellation error")
	}
	
	// Should not take too long to cancel
	if duration > 500*time.Millisecond {
		t.Errorf("Cancellation took too long: %v", duration)
	}
	
	// Some files should have been processed before cancellation
	processedCount := processor.getProcessedCount()
	if processedCount >= int64(fileCount) {
		t.Errorf("Expected fewer than %d files processed due to cancellation, got %d", 
			fileCount, processedCount)
	}
}

func TestWorkerPoolManager_GracefulShutdown(t *testing.T) {
	const fileCount = 20
	const maxWorkers = 4
	
	wpm := NewWorkerPoolManager()
	processor := newMockFileProcessor()
	processor.processDelay = 20 * time.Millisecond
	
	files := createTestFiles(fileCount)
	
	// Use a context that won't be cancelled during processing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Start processing in a goroutine
	var processErr error
	processDone := make(chan struct{})
	go func() {
		defer close(processDone)
		processErr = wpm.ProcessFiles(ctx, files, maxWorkers, processor)
	}()
	
	// Let some processing happen but not complete
	time.Sleep(50 * time.Millisecond)
	
	// Wait for processing to complete naturally
	<-processDone
	
	// Processing should complete successfully
	if processErr != nil {
		t.Errorf("Processing failed: %v", processErr)
	}
	
	// All files should have been processed
	if processor.getProcessedCount() != int64(fileCount) {
		t.Errorf("Expected %d processed files, got %d", 
			fileCount, processor.getProcessedCount())
	}
	
	// Now test shutdown after processing is complete
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	
	shutdownErr := wpm.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		t.Errorf("Graceful shutdown failed: %v", shutdownErr)
	}
}

func TestWorkerPoolManager_MultipleShutdowns(t *testing.T) {
	wpm := NewWorkerPoolManager()
	
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	
	// Multiple shutdown calls should not cause issues
	err1 := wpm.Shutdown(ctx)
	err2 := wpm.Shutdown(ctx)
	err3 := wpm.Shutdown(ctx)
	
	// All should succeed (or at least not panic)
	if err1 != nil {
		t.Errorf("First shutdown failed: %v", err1)
	}
	if err2 != nil {
		t.Errorf("Second shutdown failed: %v", err2)
	}
	if err3 != nil {
		t.Errorf("Third shutdown failed: %v", err3)
	}
}

func TestWorkerPoolManager_GetActiveWorkers(t *testing.T) {
	const fileCount = 10
	const maxWorkers = 3
	
	wpm := NewWorkerPoolManager()
	processor := newMockFileProcessor()
	processor.processDelay = 100 * time.Millisecond
	
	files := createTestFiles(fileCount)
	
	// Initially no active workers
	if activeWorkers := wpm.GetActiveWorkers(); activeWorkers != 0 {
		t.Errorf("Expected 0 active workers initially, got %d", activeWorkers)
	}
	
	ctx := context.Background()
	
	// Start processing in a goroutine
	processDone := make(chan struct{})
	go func() {
		defer close(processDone)
		wpm.ProcessFiles(ctx, files, maxWorkers, processor)
	}()
	
	// Check active workers during processing
	time.Sleep(50 * time.Millisecond) // Let processing start
	activeWorkers := wpm.GetActiveWorkers()
	
	if activeWorkers < 0 || activeWorkers > maxWorkers {
		t.Errorf("Expected active workers between 0 and %d, got %d", 
			maxWorkers, activeWorkers)
	}
	
	// Wait for completion
	<-processDone
	
	// Should have no active workers after completion
	if activeWorkers := wpm.GetActiveWorkers(); activeWorkers != 0 {
		t.Errorf("Expected 0 active workers after completion, got %d", activeWorkers)
	}
}

func TestWorkerPoolManager_GetPoolStats(t *testing.T) {
	const fileCount = 15
	const maxWorkers = 4
	
	wpm := NewWorkerPoolManager()
	processor := newMockFileProcessor()
	processor.processDelay = 10 * time.Millisecond
	
	files := createTestFiles(fileCount)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
	if err != nil {
		t.Fatalf("ProcessFiles failed: %v", err)
	}
	
	wpmInternal := wpm.(*workerPoolManager)
	stats := wpmInternal.getPoolStats()
	
	if stats.TotalProcessed != int64(fileCount) {
		t.Errorf("Expected %d total processed, got %d", 
			fileCount, stats.TotalProcessed)
	}
	
	if stats.TotalFailed != 0 {
		t.Errorf("Expected 0 total failed, got %d", stats.TotalFailed)
	}
	
	if stats.AverageTime <= 0 {
		t.Errorf("Expected positive average time, got %v", stats.AverageTime)
	}
	
	if stats.ThroughputPerSec <= 0 {
		t.Errorf("Expected positive throughput, got %f", stats.ThroughputPerSec)
	}
	
	// After completion, should have no active workers
	if stats.WorkersActive != 0 {
		t.Errorf("Expected 0 active workers after completion, got %d", stats.WorkersActive)
	}
	
	// Clean shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := wpm.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestWorkerPoolManager_ResourceManagement(t *testing.T) {
	const iterations = 10
	const filesPerIteration = 20
	const maxWorkers = 4
	
	initialGoroutines := runtime.NumGoroutine()
	
	for i := 0; i < iterations; i++ {
		wpm := NewWorkerPoolManager()
		processor := newMockFileProcessor()
		files := createTestFiles(filesPerIteration)
		
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		
		err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
		if err != nil {
			t.Fatalf("Iteration %d: ProcessFiles failed: %v", i, err)
		}
		
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		err = wpm.Shutdown(shutdownCtx)
		if err != nil {
			t.Fatalf("Iteration %d: Shutdown failed: %v", i, err)
		}
		
		cancel()
		shutdownCancel()
	}
	
	// Give goroutines time to clean up
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	runtime.GC() // Force garbage collection twice
	
	finalGoroutines := runtime.NumGoroutine()
	
	// Should not have significant goroutine leaks
	if finalGoroutines > initialGoroutines+2 {
		t.Errorf("Potential goroutine leak: initial=%d, final=%d", 
			initialGoroutines, finalGoroutines)
	}
}

func BenchmarkWorkerPoolManager_ProcessFiles(b *testing.B) {
	const fileCount = 100
	const maxWorkers = 8
	
	files := createTestFiles(fileCount)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		wpm := NewWorkerPoolManager()
		processor := newMockFileProcessor()
		
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		
		err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
		if err != nil {
			b.Fatalf("ProcessFiles failed: %v", err)
		}
		
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		err = wpm.Shutdown(shutdownCtx)
		if err != nil {
			b.Fatalf("Shutdown failed: %v", err)
		}
		
		cancel()
		shutdownCancel()
	}
}

func BenchmarkWorkerPoolManager_Throughput(b *testing.B) {
	filesCounts := []int{10, 50, 100, 500, 1000}
	
	for _, fileCount := range filesCounts {
		b.Run(fmt.Sprintf("files_%d", fileCount), func(b *testing.B) {
			const maxWorkers = 8
			files := createTestFiles(fileCount)
			
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				wpm := NewWorkerPoolManager()
				processor := newMockFileProcessor()
				
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				
				startTime := time.Now()
				err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
				duration := time.Since(startTime)
				
				if err != nil {
					b.Fatalf("ProcessFiles failed: %v", err)
				}
				
				throughput := float64(fileCount) / duration.Seconds()
				b.ReportMetric(throughput, "files/sec")
				
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
				wpm.Shutdown(shutdownCtx)
				
				cancel()
				shutdownCancel()
			}
		})
	}
}

func TestWorkerPoolManager_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}
	
	const fileCount = 1000
	const maxWorkers = 16
	
	wpm := NewWorkerPoolManager()
	processor := newMockFileProcessor()
	processor.processDelay = time.Millisecond // Small delay
	
	files := createTestFiles(fileCount)
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	startTime := time.Now()
	err := wpm.ProcessFiles(ctx, files, maxWorkers, processor)
	duration := time.Since(startTime)
	
	if err != nil {
		t.Fatalf("Stress test failed: %v", err)
	}
	
	if processor.getProcessedCount() != int64(fileCount) {
		t.Errorf("Expected %d processed files, got %d", 
			fileCount, processor.getProcessedCount())
	}
	
	throughput := float64(fileCount) / duration.Seconds()
	t.Logf("Processed %d files in %v (%.2f files/sec)", 
		fileCount, duration, throughput)
	
	// Should achieve reasonable throughput
	if throughput < 100 {
		t.Errorf("Low throughput: %.2f files/sec", throughput)
	}
	
	// Clean shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := wpm.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}