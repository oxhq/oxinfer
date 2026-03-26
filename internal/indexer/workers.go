// Package indexer provides concurrent file processing with configurable worker pools.
// This file implements the WorkerPoolManager for high-performance file processing
// with graceful shutdown, error resilience, and resource management.
package indexer

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// workerPoolManager implements WorkerPoolManager interface for concurrent file processing
type workerPoolManager struct {
	// Pool state management
	workers     []*worker
	workQueue   chan *workItem
	resultQueue chan *ProcessResult
	errorQueue  chan error

	// Synchronization
	mu           sync.RWMutex
	wg           sync.WaitGroup
	shutdownOnce sync.Once

	// State tracking
	activeWorkers int32
	totalWorkers  int32
	isShutdown    bool

	// Configuration
	maxWorkers      int
	processor       FileProcessor
	errorHandler    func(error)
	shutdownTimeout time.Duration

	// Statistics
	stats workerPoolStats
}

// worker represents a single worker goroutine in the pool
type worker struct {
	id             int
	manager        *workerPoolManager
	isActive       bool
	processedCount int64
	errorCount     int64
	startTime      time.Time
}

// workItem represents a unit of work to be processed by workers
type workItem struct {
	ctx         context.Context
	file        FileInfo
	submittedAt time.Time
	startedAt   *time.Time
	completedAt *time.Time
}

// workerPoolStats tracks performance metrics for the worker pool
type workerPoolStats struct {
	totalProcessed   int64
	totalFailed      int64
	totalSubmitted   int64
	averageTime      time.Duration
	throughputPerSec float64
	startTime        time.Time
}

// NewWorkerPoolManager creates a new worker pool manager instance
func NewWorkerPoolManager() WorkerPoolManager {
	return &workerPoolManager{
		shutdownTimeout: 10 * time.Second,
		stats: workerPoolStats{
			startTime: time.Now(),
		},
	}
}

// ProcessFiles implements WorkerPoolManager.ProcessFiles
func (wpm *workerPoolManager) ProcessFiles(ctx context.Context, files []FileInfo, maxWorkers int, processor FileProcessor) error {
	if len(files) == 0 {
		return nil
	}

	if maxWorkers <= 0 {
		maxWorkers = runtime.NumCPU()
	}

	// Initialize the pool
	if err := wpm.initializePool(maxWorkers, processor); err != nil {
		return fmt.Errorf("failed to initialize worker pool: %w", err)
	}
	defer wpm.cleanup()

	// Start workers
	if err := wpm.startWorkers(ctx); err != nil {
		return fmt.Errorf("failed to start workers: %w", err)
	}

	// Submit all files for processing
	go func() {
		defer close(wpm.workQueue) // Close queue after all work is submitted
		for _, file := range files {
			workItem := &workItem{
				ctx:         ctx,
				file:        file,
				submittedAt: time.Now(),
			}

			select {
			case wpm.workQueue <- workItem:
				atomic.AddInt64(&wpm.stats.totalSubmitted, 1)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for all workers to complete
	done := make(chan struct{})
	go func() {
		defer close(done)
		wpm.wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("processing timeout: %w", ctx.Err())
	}
}

// GetActiveWorkers implements WorkerPoolManager.GetActiveWorkers
func (wpm *workerPoolManager) GetActiveWorkers() int {
	return int(atomic.LoadInt32(&wpm.activeWorkers))
}

// Shutdown implements WorkerPoolManager.Shutdown
func (wpm *workerPoolManager) Shutdown(ctx context.Context) error {
	var shutdownErr error

	wpm.shutdownOnce.Do(func() {
		wpm.mu.Lock()
		wpm.isShutdown = true
		wpm.mu.Unlock()

		// Close work queue to signal no more work
		if wpm.workQueue != nil {
			close(wpm.workQueue)
		}

		// Wait for workers to finish with timeout
		done := make(chan struct{})
		go func() {
			defer close(done)
			wpm.wg.Wait()
		}()

		select {
		case <-done:
			// All workers finished gracefully
		case <-ctx.Done():
			shutdownErr = fmt.Errorf("worker pool shutdown timeout: %w", ctx.Err())
		}

		wpm.cleanup()
	})

	return shutdownErr
}

// initializePool sets up the worker pool with the specified configuration
func (wpm *workerPoolManager) initializePool(maxWorkers int, processor FileProcessor) error {
	wpm.mu.Lock()
	defer wpm.mu.Unlock()

	if wpm.isShutdown {
		return fmt.Errorf("cannot initialize shutdown pool")
	}

	wpm.maxWorkers = maxWorkers
	wpm.processor = processor
	wpm.totalWorkers = int32(maxWorkers)

	// Initialize channels with aggressive buffer sizes for maximum throughput
	// Work queue buffer size is 4x maxWorkers to prevent any blocking
	// This allows the submission goroutine to stay ahead of workers
	workQueueSize := maxWorkers * 4
	if workQueueSize < 32 {
		workQueueSize = 32 // Minimum aggressive buffering
	}
	
	// Result and error queues sized generously to prevent backpressure
	resultQueueSize := maxWorkers * 3
	errorQueueSize := maxWorkers * 2
	
	wpm.workQueue = make(chan *workItem, workQueueSize)
	wpm.resultQueue = make(chan *ProcessResult, resultQueueSize)
	wpm.errorQueue = make(chan error, errorQueueSize)

	// Create worker instances
	wpm.workers = make([]*worker, maxWorkers)
	for i := 0; i < maxWorkers; i++ {
		wpm.workers[i] = &worker{
			id:      i,
			manager: wpm,
		}
	}

	return nil
}

// startWorkers launches all worker goroutines
func (wpm *workerPoolManager) startWorkers(ctx context.Context) error {
	wpm.mu.Lock()
	defer wpm.mu.Unlock()

	if wpm.isShutdown {
		return fmt.Errorf("cannot start workers on shutdown pool")
	}

	// Start error handler goroutine (not tracked in WaitGroup to avoid deadlock)
	go wpm.handleErrors(ctx)

	// Start all workers
	for _, worker := range wpm.workers {
		wpm.wg.Add(1)
		go worker.run(ctx)
	}

	return nil
}

// cleanup releases all resources used by the worker pool
func (wpm *workerPoolManager) cleanup() {
	wpm.mu.Lock()
	defer wpm.mu.Unlock()

	// Note: workQueue is closed by the submission goroutine
	// Note: errorQueue is closed after workers complete
	// We don't close them here to avoid double-close panic
	wpm.workQueue = nil
	wpm.errorQueue = nil

	// Close result queue if it exists
	if wpm.resultQueue != nil {
		close(wpm.resultQueue)
		wpm.resultQueue = nil
	}

	// Reset worker references
	wpm.workers = nil
}

// handleErrors processes errors from workers
func (wpm *workerPoolManager) handleErrors(ctx context.Context) {

	for {
		select {
		case err, ok := <-wpm.errorQueue:
			if !ok {
				return // Error queue closed
			}

			if wpm.errorHandler != nil {
				wpm.errorHandler(err)
			}

		case <-ctx.Done():
			return
		}
	}
}

// run executes the worker's main processing loop
func (w *worker) run(ctx context.Context) {
	defer w.manager.wg.Done()
	w.startTime = time.Now()

	for {
		select {
		case workItem, ok := <-w.manager.workQueue:
			if !ok {
				// Work queue closed, worker should exit
				return
			}

			w.processWork(ctx, workItem)

		case <-ctx.Done():
			// Context cancelled, worker should exit
			return
		}
	}
}

// processWork processes a single work item
func (w *worker) processWork(ctx context.Context, item *workItem) {
	// Mark worker as active
	atomic.AddInt32(&w.manager.activeWorkers, 1)
	w.isActive = true

	defer func() {
		// Mark worker as inactive
		atomic.AddInt32(&w.manager.activeWorkers, -1)
		w.isActive = false
	}()

	startTime := time.Now()
	item.startedAt = &startTime

	// Process the file
	result, err := w.manager.processor.ProcessFile(item.ctx, item.file)

	completedTime := time.Now()
	item.completedAt = &completedTime

	// Update statistics
	if err != nil {
		atomic.AddInt64(&w.errorCount, 1)
		atomic.AddInt64(&w.manager.stats.totalFailed, 1)

		// Send error to error handler (non-blocking)
		select {
		case w.manager.errorQueue <- fmt.Errorf("worker %d failed to process file %s: %w", w.id, item.file.Path, err):
		default:
			// Error queue full, skip error reporting to avoid blocking
		}
	} else {
		atomic.AddInt64(&w.processedCount, 1)
		atomic.AddInt64(&w.manager.stats.totalProcessed, 1)

		// Update processing duration statistics
		duration := completedTime.Sub(startTime)
		w.updateAverageTime(duration)
	}

	// Note: Results are not collected in the current interface implementation
	// The ProcessFile method handles its own result processing
	if result != nil {
		result.DurationMs = completedTime.Sub(startTime).Milliseconds()
		if err != nil {
			result.Error = err
		}
	}
}

// updateAverageTime updates the rolling average processing time
func (w *worker) updateAverageTime(duration time.Duration) {
	// Simple exponential moving average for performance
	totalProcessed := atomic.LoadInt64(&w.manager.stats.totalProcessed)
	if totalProcessed == 1 {
		w.manager.stats.averageTime = duration
	} else {
		// Weighted average: 90% old value, 10% new value
		oldAvg := w.manager.stats.averageTime
		w.manager.stats.averageTime = time.Duration(float64(oldAvg)*0.9 + float64(duration)*0.1)
	}

	// Calculate throughput
	elapsed := time.Since(w.manager.stats.startTime).Seconds()
	if elapsed > 0 {
		w.manager.stats.throughputPerSec = float64(totalProcessed) / elapsed
	}
}

// getPoolStats returns current worker pool statistics (internal method)
func (wpm *workerPoolManager) getPoolStats() workerPoolStatsInternal {
	wpm.mu.RLock()
	defer wpm.mu.RUnlock()

	var queueDepth int
	if wpm.workQueue != nil {
		queueDepth = len(wpm.workQueue)
	}

	return workerPoolStatsInternal{
		WorkersActive:    int(atomic.LoadInt32(&wpm.activeWorkers)),
		WorkersIdle:      int(wpm.totalWorkers - atomic.LoadInt32(&wpm.activeWorkers)),
		QueueDepth:       queueDepth,
		TotalProcessed:   atomic.LoadInt64(&wpm.stats.totalProcessed),
		TotalFailed:      atomic.LoadInt64(&wpm.stats.totalFailed),
		AverageTime:      wpm.stats.averageTime,
		ThroughputPerSec: wpm.stats.throughputPerSec,
	}
}

// setErrorHandler configures the error handling function (internal method)
func (wpm *workerPoolManager) setErrorHandler(handler func(error)) {
	wpm.mu.Lock()
	defer wpm.mu.Unlock()
	wpm.errorHandler = handler
}

// workerPoolStatsInternal represents worker pool performance statistics (internal)
type workerPoolStatsInternal struct {
	WorkersActive    int           // Currently processing work
	WorkersIdle      int           // Waiting for work
	QueueDepth       int           // Pending work items
	TotalProcessed   int64         // Total work completed
	TotalFailed      int64         // Total work that failed
	AverageTime      time.Duration // Average processing time
	ThroughputPerSec float64       // Work items/second
}
