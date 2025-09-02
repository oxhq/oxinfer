package perf

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// OptimizedWorkerPool provides enhanced worker pool management with performance optimizations.
type OptimizedWorkerPool struct {
	// Core configuration
	maxWorkers    int
	minWorkers    int
	processor     WorkerProcessor
	
	// Dynamic scaling
	scaler        *WorkerScaler
	loadBalancer  *WorkLoadBalancer
	
	// Work distribution
	workQueue     chan WorkItem
	resultQueue   chan WorkResult
	priorityQueue *PriorityWorkQueue
	
	// State management
	workers       []*PerformantWorker
	activeWorkers int32
	totalWorkers  int32
	shutdown      int32
	
	// Performance tracking
	metrics       *PoolMetrics
	mu            sync.RWMutex
	
	// Resource management
	memoryLimit   int64
	memoryUsed    int64
	lastGC        time.Time
}

// WorkerProcessor defines the interface for processing work items.
type WorkerProcessor interface {
	ProcessWork(ctx context.Context, item WorkItem) (WorkResult, error)
	EstimateWorkload(item WorkItem) int // Returns estimated processing time in milliseconds
}

// WorkItem represents a unit of work to be processed.
type WorkItem interface {
	ID() string
	Priority() int
	EstimatedDuration() time.Duration
	Context() context.Context
}

// WorkResult represents the result of processing a work item.
type WorkResult interface {
	ItemID() string
	Success() bool
	Error() error
	ProcessingTime() time.Duration
	MemoryUsed() int64
}

// WorkerScaler manages dynamic worker pool scaling based on load.
type WorkerScaler struct {
	pool           *OptimizedWorkerPool
	scaleUpThreshold   float64 // Queue utilization to trigger scale up
	scaleDownThreshold float64 // Queue utilization to trigger scale down
	scaleUpDelay       time.Duration
	scaleDownDelay     time.Duration
	lastScaleEvent     time.Time
	
	mu sync.RWMutex
}

// WorkLoadBalancer distributes work items efficiently across workers.
type WorkLoadBalancer struct {
	workQueues     []chan WorkItem // Per-worker queues for work stealing
	globalQueue    chan WorkItem   // Global overflow queue
	workerLoads    []int64        // Current load per worker
	stealThreshold int            // Threshold for work stealing
	
	mu sync.RWMutex
}

// PriorityWorkQueue manages work items with priority-based ordering.
type PriorityWorkQueue struct {
	high   chan WorkItem
	medium chan WorkItem
	low    chan WorkItem
	
	// Statistics
	enqueuedHigh   int64
	enqueuedMedium int64
	enqueuedLow    int64
}

// PerformantWorker represents an optimized worker with performance tracking.
type PerformantWorker struct {
	id              int
	pool            *OptimizedWorkerPool
	workQueue       chan WorkItem
	localQueue      []WorkItem // Local work stealing queue
	
	// Performance tracking
	processedCount  int64
	errorCount      int64
	totalTime       time.Duration
	avgProcessTime  time.Duration
	lastActivity    time.Time
	
	// State management
	active          bool
	ctx             context.Context
	cancel          context.CancelFunc
	
	mu sync.RWMutex
}

// PoolMetrics tracks comprehensive worker pool performance metrics.
type PoolMetrics struct {
	// Throughput metrics
	TotalProcessed    int64         `json:"totalProcessed"`
	TotalFailed       int64         `json:"totalFailed"`
	ProcessingRate    float64       `json:"processingRate"`    // Items per second
	AverageLatency    time.Duration `json:"averageLatencyMs"`
	
	// Worker metrics
	WorkerUtilization float64       `json:"workerUtilization"` // Percentage of workers actively processing
	ScaleEvents       int64         `json:"scaleEvents"`       // Number of scaling events
	WorkStealEvents   int64         `json:"workStealEvents"`   // Number of work stealing events
	
	// Queue metrics
	QueueDepthHigh    int64         `json:"queueDepthHigh"`    // High priority queue depth
	QueueDepthMedium  int64         `json:"queueDepthMedium"`  // Medium priority queue depth
	QueueDepthLow     int64         `json:"queueDepthLow"`     // Low priority queue depth
	MaxQueueDepth     int64         `json:"maxQueueDepth"`     // Peak queue depth
	
	// Memory metrics
	MemoryUsage       int64         `json:"memoryUsageMB"`
	MemoryEfficiency  float64       `json:"memoryEfficiency"`  // Useful memory / total memory
	GCPressure        float64       `json:"gcPressure"`        // GC events per second
	
	// Timing metrics
	StartTime         time.Time     `json:"startTime"`
	LastUpdateTime    time.Time     `json:"lastUpdateTime"`
	UptimeDuration    time.Duration `json:"uptimeDurationMs"`
}

// NewOptimizedWorkerPool creates a new optimized worker pool.
func NewOptimizedWorkerPool(config *WorkerPoolConfig) (*OptimizedWorkerPool, error) {
	if config == nil {
		config = DefaultWorkerPoolConfig()
	}
	
	if err := validateWorkerPoolConfig(config); err != nil {
		return nil, fmt.Errorf("invalid worker pool config: %w", err)
	}
	
	pool := &OptimizedWorkerPool{
		maxWorkers:    config.MaxWorkers,
		minWorkers:    config.MinWorkers,
		workQueue:     make(chan WorkItem, config.QueueCapacity),
		resultQueue:   make(chan WorkResult, config.ResultCapacity),
		memoryLimit:   config.MemoryLimitMB * 1024 * 1024,
		metrics:       &PoolMetrics{StartTime: time.Now()},
	}
	
	// Initialize priority queue
	pool.priorityQueue = &PriorityWorkQueue{
		high:   make(chan WorkItem, config.QueueCapacity/3),
		medium: make(chan WorkItem, config.QueueCapacity/3),
		low:    make(chan WorkItem, config.QueueCapacity/3),
	}
	
	// Initialize scaler
	pool.scaler = &WorkerScaler{
		pool:               pool,
		scaleUpThreshold:   config.ScaleUpThreshold,
		scaleDownThreshold: config.ScaleDownThreshold,
		scaleUpDelay:       config.ScaleUpDelay,
		scaleDownDelay:     config.ScaleDownDelay,
	}
	
	// Initialize load balancer
	pool.loadBalancer = &WorkLoadBalancer{
		workQueues:     make([]chan WorkItem, config.MaxWorkers),
		globalQueue:    pool.workQueue,
		workerLoads:    make([]int64, config.MaxWorkers),
		stealThreshold: config.StealThreshold,
	}
	
	return pool, nil
}

// WorkerPoolConfig contains configuration for the optimized worker pool.
type WorkerPoolConfig struct {
	// Worker scaling
	MinWorkers         int           `json:"minWorkers"`
	MaxWorkers         int           `json:"maxWorkers"`
	ScaleUpThreshold   float64       `json:"scaleUpThreshold"`   // Queue utilization to scale up
	ScaleDownThreshold float64       `json:"scaleDownThreshold"` // Queue utilization to scale down
	ScaleUpDelay       time.Duration `json:"scaleUpDelayMs"`
	ScaleDownDelay     time.Duration `json:"scaleDownDelayMs"`
	
	// Queue configuration
	QueueCapacity      int           `json:"queueCapacity"`
	ResultCapacity     int           `json:"resultCapacity"`
	StealThreshold     int           `json:"stealThreshold"`     // Items in queue to trigger stealing
	
	// Resource limits
	MemoryLimitMB      int64         `json:"memoryLimitMB"`
	CPUThrottle        float64       `json:"cpuThrottle"`        // CPU usage limit (0-1)
	
	// Performance tuning
	WorkerIdleTimeout  time.Duration `json:"workerIdleTimeoutMs"`
	ShutdownTimeout    time.Duration `json:"shutdownTimeoutMs"`
	MetricsInterval    time.Duration `json:"metricsIntervalMs"`
}

// DefaultWorkerPoolConfig returns optimized default configuration for the worker pool.
func DefaultWorkerPoolConfig() *WorkerPoolConfig {
	return &WorkerPoolConfig{
		MinWorkers:         1,
		MaxWorkers:         runtime.NumCPU() * 2, // Start with 2x CPU cores
		ScaleUpThreshold:   0.8,  // Scale up when 80% queue utilization
		ScaleDownThreshold: 0.2,  // Scale down when 20% queue utilization
		ScaleUpDelay:       100 * time.Millisecond,
		ScaleDownDelay:     2 * time.Second,
		QueueCapacity:      1000,
		ResultCapacity:     1000,
		StealThreshold:     10,   // Steal work when >10 items in queue
		MemoryLimitMB:      400,  // 400MB limit per pool
		CPUThrottle:        0.9,  // Use up to 90% CPU
		WorkerIdleTimeout:  5 * time.Minute,
		ShutdownTimeout:    10 * time.Second,
		MetricsInterval:    1 * time.Second,
	}
}

// Start initializes and starts the optimized worker pool.
func (pool *OptimizedWorkerPool) Start(ctx context.Context, processor WorkerProcessor) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	
	if atomic.LoadInt32(&pool.shutdown) == 1 {
		return fmt.Errorf("worker pool is shut down")
	}
	
	pool.processor = processor
	
	// Initialize load balancer queues
	for i := 0; i < pool.maxWorkers; i++ {
		pool.loadBalancer.workQueues[i] = make(chan WorkItem, 100)
	}
	
	// Start with minimum workers
	for i := 0; i < pool.minWorkers; i++ {
		if err := pool.addWorker(ctx, i); err != nil {
			return fmt.Errorf("failed to start worker %d: %w", i, err)
		}
	}
	
	// Start background scaler
	go pool.scaler.run(ctx)
	
	// Start metrics collection
	go pool.collectMetrics(ctx)
	
	return nil
}

// addWorker creates and starts a new worker.
func (pool *OptimizedWorkerPool) addWorker(ctx context.Context, id int) error {
	workerCtx, cancel := context.WithCancel(ctx)
	
	worker := &PerformantWorker{
		id:           id,
		pool:         pool,
		workQueue:    pool.loadBalancer.workQueues[id],
		localQueue:   make([]WorkItem, 0, 50),
		lastActivity: time.Now(),
		active:       true,
		ctx:          workerCtx,
		cancel:       cancel,
	}
	
	pool.workers = append(pool.workers, worker)
	atomic.AddInt32(&pool.totalWorkers, 1)
	atomic.AddInt32(&pool.activeWorkers, 1)
	
	// Start worker goroutine
	go worker.run()
	
	return nil
}

// SubmitWork submits a work item to the pool with priority handling.
func (pool *OptimizedWorkerPool) SubmitWork(item WorkItem) error {
	if atomic.LoadInt32(&pool.shutdown) == 1 {
		return fmt.Errorf("worker pool is shut down")
	}
	
	// Route to appropriate priority queue
	priority := item.Priority()
	switch {
	case priority >= 80:
		return pool.submitToPriorityQueue(pool.priorityQueue.high, item)
	case priority >= 40:
		return pool.submitToPriorityQueue(pool.priorityQueue.medium, item)
	default:
		return pool.submitToPriorityQueue(pool.priorityQueue.low, item)
	}
}

// submitToPriorityQueue submits work to a specific priority queue with load balancing.
func (pool *OptimizedWorkerPool) submitToPriorityQueue(queue chan WorkItem, item WorkItem) error {
	// Find least loaded worker
	pool.loadBalancer.mu.RLock()
	leastLoadedWorker := 0
	minLoad := pool.loadBalancer.workerLoads[0]
	
	for i := 1; i < len(pool.loadBalancer.workerLoads); i++ {
		if pool.loadBalancer.workerLoads[i] < minLoad {
			minLoad = pool.loadBalancer.workerLoads[i]
			leastLoadedWorker = i
		}
	}
	pool.loadBalancer.mu.RUnlock()
	
	// Try to submit to least loaded worker
	workerQueue := pool.loadBalancer.workQueues[leastLoadedWorker]
	select {
	case workerQueue <- item:
		atomic.AddInt64(&pool.loadBalancer.workerLoads[leastLoadedWorker], 1)
		atomic.AddInt64(&pool.metrics.TotalProcessed, 1)
		return nil
	case <-time.After(10 * time.Millisecond):
		// Worker queue full, try global queue
		select {
		case pool.workQueue <- item:
			return nil
		default:
			return fmt.Errorf("all queues full, cannot submit work")
		}
	}
}

// run executes the worker's main processing loop with work stealing.
func (worker *PerformantWorker) run() {
	defer func() {
		atomic.AddInt32(&worker.pool.activeWorkers, -1)
		worker.active = false
	}()
	
	for {
		select {
		case <-worker.ctx.Done():
			return
		case item := <-worker.workQueue:
			worker.processWorkItem(item)
		case item := <-worker.pool.workQueue:
			worker.processWorkItem(item)
		default:
			// No work available, try work stealing
			if worker.tryWorkStealing() {
				continue
			}
			
			// Check for idle timeout
			if time.Since(worker.lastActivity) > worker.pool.scaler.pool.getWorkerIdleTimeout() {
				if worker.pool.canScaleDown() {
					return // Worker exits due to idle timeout
				}
			}
			
			// Brief sleep to prevent busy waiting
			time.Sleep(time.Millisecond)
		}
	}
}

// processWorkItem processes a single work item with performance tracking.
func (worker *PerformantWorker) processWorkItem(item WorkItem) {
	start := time.Now()
	worker.lastActivity = start
	
	// Process the work item
	_, err := worker.pool.processor.ProcessWork(item.Context(), item)
	
	processingTime := time.Since(start)
	
	// Update metrics
	atomic.AddInt64(&worker.processedCount, 1)
	if err != nil {
		atomic.AddInt64(&worker.errorCount, 1)
		atomic.AddInt64(&worker.pool.metrics.TotalFailed, 1)
	}
	
	// Update average processing time
	worker.mu.Lock()
	worker.totalTime += processingTime
	count := atomic.LoadInt64(&worker.processedCount)
	if count > 0 {
		worker.avgProcessTime = time.Duration(int64(worker.totalTime) / count)
	}
	worker.mu.Unlock()
	
	// Update worker load
	workerIndex := worker.id
	if workerIndex < len(worker.pool.loadBalancer.workerLoads) {
		atomic.AddInt64(&worker.pool.loadBalancer.workerLoads[workerIndex], -1)
	}
	
	// Submit result
	select {
	case worker.pool.resultQueue <- &optimizedWorkResult{
		itemID:         item.ID(),
		success:        err == nil,
		err:            err,
		processingTime: processingTime,
		memoryUsed:     worker.estimateMemoryUsage(),
	}:
	case <-worker.ctx.Done():
		return
	}
}

// tryWorkStealing attempts to steal work from other workers.
func (worker *PerformantWorker) tryWorkStealing() bool {
	worker.pool.loadBalancer.mu.RLock()
	defer worker.pool.loadBalancer.mu.RUnlock()
	
	// Find workers with work to steal
	for i, load := range worker.pool.loadBalancer.workerLoads {
		if i == worker.id || load <= int64(worker.pool.loadBalancer.stealThreshold) {
			continue
		}
		
		// Try to steal work from this worker
		workerQueue := worker.pool.loadBalancer.workQueues[i]
		select {
		case item := <-workerQueue:
			atomic.AddInt64(&worker.pool.loadBalancer.workerLoads[i], -1)
			atomic.AddInt64(&worker.pool.metrics.WorkStealEvents, 1)
			worker.processWorkItem(item)
			return true
		default:
			continue
		}
	}
	
	return false
}

// run executes the worker scaler's main loop.
func (scaler *WorkerScaler) run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond) // Check every 100ms
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scaler.evaluateScaling()
		}
	}
}

// evaluateScaling determines if the pool should scale up or down.
func (scaler *WorkerScaler) evaluateScaling() {
	scaler.mu.Lock()
	defer scaler.mu.Unlock()
	
	now := time.Now()
	if now.Sub(scaler.lastScaleEvent) < scaler.scaleUpDelay {
		return // Too soon since last scale event
	}
	
	// Calculate queue utilization
	totalQueueDepth := int64(len(scaler.pool.workQueue))
	totalQueueDepth += atomic.LoadInt64(&scaler.pool.priorityQueue.enqueuedHigh)
	totalQueueDepth += atomic.LoadInt64(&scaler.pool.priorityQueue.enqueuedMedium)
	totalQueueDepth += atomic.LoadInt64(&scaler.pool.priorityQueue.enqueuedLow)
	
	totalCapacity := int64(cap(scaler.pool.workQueue))
	utilization := float64(totalQueueDepth) / float64(totalCapacity)
	
	activeWorkers := atomic.LoadInt32(&scaler.pool.activeWorkers)
	
	// Scale up decision
	if utilization > scaler.scaleUpThreshold && int(activeWorkers) < scaler.pool.maxWorkers {
		if scaler.pool.canScaleUp() {
			scaler.scaleUp()
			scaler.lastScaleEvent = now
		}
	}
	
	// Scale down decision
	if utilization < scaler.scaleDownThreshold && int(activeWorkers) > scaler.pool.minWorkers {
		if now.Sub(scaler.lastScaleEvent) >= scaler.scaleDownDelay {
			scaler.scaleDown()
			scaler.lastScaleEvent = now
		}
	}
}

// scaleUp adds a new worker to the pool.
func (scaler *WorkerScaler) scaleUp() {
	activeWorkers := int(atomic.LoadInt32(&scaler.pool.activeWorkers))
	if activeWorkers >= scaler.pool.maxWorkers {
		return
	}
	
	workerID := len(scaler.pool.workers)
	ctx := context.Background() // TODO: Use proper context
	
	if err := scaler.pool.addWorker(ctx, workerID); err == nil {
		atomic.AddInt64(&scaler.pool.metrics.ScaleEvents, 1)
	}
}

// scaleDown removes an idle worker from the pool.
func (scaler *WorkerScaler) scaleDown() {
	activeWorkers := int(atomic.LoadInt32(&scaler.pool.activeWorkers))
	if activeWorkers <= scaler.pool.minWorkers {
		return
	}
	
	// Find least active worker
	scaler.pool.mu.RLock()
	var leastActiveWorker *PerformantWorker
	oldestIdle := time.Now()
	
	for _, worker := range scaler.pool.workers {
		if worker.active {
			worker.mu.RLock()
			lastActivity := worker.lastActivity
			worker.mu.RUnlock()
			
			if lastActivity.Before(oldestIdle) {
				oldestIdle = lastActivity
				leastActiveWorker = worker
			}
		}
	}
	scaler.pool.mu.RUnlock()
	
	// Terminate the least active worker
	if leastActiveWorker != nil {
		leastActiveWorker.cancel()
		atomic.AddInt64(&scaler.pool.metrics.ScaleEvents, 1)
	}
}

// canScaleUp checks if the pool can add more workers.
func (pool *OptimizedWorkerPool) canScaleUp() bool {
	// Check memory limits
	if atomic.LoadInt64(&pool.memoryUsed) > pool.memoryLimit {
		return false
	}
	
	// Check if we have available worker slots
	return int(atomic.LoadInt32(&pool.activeWorkers)) < pool.maxWorkers
}

// canScaleDown checks if the pool can remove workers.
func (pool *OptimizedWorkerPool) canScaleDown() bool {
	return int(atomic.LoadInt32(&pool.activeWorkers)) > pool.minWorkers
}

// getWorkerIdleTimeout returns the worker idle timeout configuration.
func (pool *OptimizedWorkerPool) getWorkerIdleTimeout() time.Duration {
	return 5 * time.Minute // Default idle timeout
}

// estimateMemoryUsage estimates the current memory usage of a worker.
func (worker *PerformantWorker) estimateMemoryUsage() int64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	// Rough estimate: total heap size / number of active workers
	activeWorkers := atomic.LoadInt32(&worker.pool.activeWorkers)
	if activeWorkers == 0 {
		return 0
	}
	
	return int64(memStats.HeapInuse) / int64(activeWorkers)
}

// collectMetrics periodically updates pool performance metrics.
func (pool *OptimizedWorkerPool) collectMetrics(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pool.updateMetrics()
		}
	}
}

// updateMetrics updates the current performance metrics.
func (pool *OptimizedWorkerPool) updateMetrics() {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	
	now := time.Now()
	pool.metrics.LastUpdateTime = now
	pool.metrics.UptimeDuration = now.Sub(pool.metrics.StartTime)
	
	// Calculate worker utilization
	activeWorkers := atomic.LoadInt32(&pool.activeWorkers)
	totalWorkers := atomic.LoadInt32(&pool.totalWorkers)
	if totalWorkers > 0 {
		pool.metrics.WorkerUtilization = float64(activeWorkers) / float64(totalWorkers)
	}
	
	// Update queue depths
	pool.metrics.QueueDepthHigh = int64(len(pool.priorityQueue.high))
	pool.metrics.QueueDepthMedium = int64(len(pool.priorityQueue.medium))
	pool.metrics.QueueDepthLow = int64(len(pool.priorityQueue.low))
	
	totalQueueDepth := pool.metrics.QueueDepthHigh + pool.metrics.QueueDepthMedium + pool.metrics.QueueDepthLow
	if totalQueueDepth > pool.metrics.MaxQueueDepth {
		pool.metrics.MaxQueueDepth = totalQueueDepth
	}
	
	// Update memory metrics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	pool.metrics.MemoryUsage = int64(memStats.HeapInuse / 1024 / 1024)
	
	// Calculate processing rate
	if pool.metrics.UptimeDuration > 0 {
		pool.metrics.ProcessingRate = float64(pool.metrics.TotalProcessed) / pool.metrics.UptimeDuration.Seconds()
	}
}

// GetMetrics returns the current pool performance metrics.
func (pool *OptimizedWorkerPool) GetMetrics() *PoolMetrics {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	
	// Return a copy to prevent external modification
	metrics := *pool.metrics
	return &metrics
}

// Shutdown gracefully shuts down the worker pool.
func (pool *OptimizedWorkerPool) Shutdown(timeout time.Duration) error {
	if !atomic.CompareAndSwapInt32(&pool.shutdown, 0, 1) {
		return nil // Already shut down
	}
	
	pool.mu.Lock()
	defer pool.mu.Unlock()
	
	// Cancel all workers
	for _, worker := range pool.workers {
		if worker.active {
			worker.cancel()
		}
	}
	
	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		for _, worker := range pool.workers {
			// Wait for worker to become inactive
			for worker.active {
				time.Sleep(10 * time.Millisecond)
			}
		}
		close(done)
	}()
	
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("worker pool shutdown timed out after %v", timeout)
	}
}

// optimizedWorkResult implements WorkResult interface.
type optimizedWorkResult struct {
	itemID         string
	success        bool
	err            error
	processingTime time.Duration
	memoryUsed     int64
}

func (r *optimizedWorkResult) ItemID() string { return r.itemID }
func (r *optimizedWorkResult) Success() bool { return r.success }
func (r *optimizedWorkResult) Error() error { return r.err }
func (r *optimizedWorkResult) ProcessingTime() time.Duration { return r.processingTime }
func (r *optimizedWorkResult) MemoryUsed() int64 { return r.memoryUsed }

// validateWorkerPoolConfig validates the worker pool configuration.
func validateWorkerPoolConfig(config *WorkerPoolConfig) error {
	if config.MinWorkers <= 0 {
		return fmt.Errorf("minimum workers must be positive")
	}
	
	if config.MaxWorkers <= config.MinWorkers {
		return fmt.Errorf("maximum workers must be greater than minimum workers")
	}
	
	if config.ScaleUpThreshold <= 0 || config.ScaleUpThreshold > 1 {
		return fmt.Errorf("scale up threshold must be between 0 and 1")
	}
	
	if config.ScaleDownThreshold <= 0 || config.ScaleDownThreshold >= config.ScaleUpThreshold {
		return fmt.Errorf("scale down threshold must be positive and less than scale up threshold")
	}
	
	if config.QueueCapacity <= 0 {
		return fmt.Errorf("queue capacity must be positive")
	}
	
	if config.MemoryLimitMB <= 0 {
		return fmt.Errorf("memory limit must be positive")
	}
	
	return nil
}