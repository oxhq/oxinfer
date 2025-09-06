package psr4

import (
	"context"
	"runtime"
	"sync"
	"time"
)

// ===== PERFORMANCE CONFIGURATION PRESETS =====

// ProjectSize represents different Laravel project scales for optimization
type ProjectSize string

const (
	ProjectSizeSmall      ProjectSize = "small"      // Laravel starter projects (< 100 classes)
	ProjectSizeMedium     ProjectSize = "medium"     // Typical Laravel applications (100-500 classes)
	ProjectSizeLarge      ProjectSize = "large"      // Enterprise applications (500-2000 classes)
	ProjectSizeEnterprise ProjectSize = "enterprise" // Large-scale applications (2000+ classes)
)

// PerformancePreset defines optimized configuration for different project types
type PerformancePreset struct {
	ProjectSize ProjectSize

	// Cache Configuration
	CacheEnabled    bool
	CacheSize       int
	CacheTTL        time.Duration
	CleanupInterval time.Duration

	// Batch Processing
	BatchSize       int
	MaxWorkers      int
	WorkerQueueSize int

	// I/O Optimization
	IOBufferSize      int
	ConcurrentReads   int
	FilesystemTimeout time.Duration

	// Memory Management
	GCFrequency       int // Number of operations before suggesting GC
	MemoryLimitMB     int
	PreallocateSlices bool

	// Performance Monitoring
	EnableMetrics   bool
	MetricsInterval time.Duration
	EnableProfiling bool
}

// GetPerformancePreset returns optimized configuration for a given project size
func GetPerformancePreset(size ProjectSize) PerformancePreset {
	switch size {
	case ProjectSizeSmall:
		return PerformancePreset{
			ProjectSize:       ProjectSizeSmall,
			CacheEnabled:      true,
			CacheSize:         200,
			CacheTTL:          3 * time.Minute,
			CleanupInterval:   30 * time.Second,
			BatchSize:         20,
			MaxWorkers:        2,
			WorkerQueueSize:   50,
			IOBufferSize:      4096,
			ConcurrentReads:   2,
			FilesystemTimeout: 1 * time.Second,
			GCFrequency:       100,
			MemoryLimitMB:     50,
			PreallocateSlices: false,
			EnableMetrics:     false,
			MetricsInterval:   30 * time.Second,
			EnableProfiling:   false,
		}

	case ProjectSizeMedium:
		return PerformancePreset{
			ProjectSize:       ProjectSizeMedium,
			CacheEnabled:      true,
			CacheSize:         1000,
			CacheTTL:          5 * time.Minute,
			CleanupInterval:   1 * time.Minute,
			BatchSize:         50,
			MaxWorkers:        4,
			WorkerQueueSize:   200,
			IOBufferSize:      8192,
			ConcurrentReads:   4,
			FilesystemTimeout: 2 * time.Second,
			GCFrequency:       500,
			MemoryLimitMB:     100,
			PreallocateSlices: true,
			EnableMetrics:     true,
			MetricsInterval:   1 * time.Minute,
			EnableProfiling:   false,
		}

	case ProjectSizeLarge:
		return PerformancePreset{
			ProjectSize:       ProjectSizeLarge,
			CacheEnabled:      true,
			CacheSize:         5000,
			CacheTTL:          10 * time.Minute,
			CleanupInterval:   2 * time.Minute,
			BatchSize:         100,
			MaxWorkers:        runtime.NumCPU(),
			WorkerQueueSize:   500,
			IOBufferSize:      16384,
			ConcurrentReads:   8,
			FilesystemTimeout: 5 * time.Second,
			GCFrequency:       1000,
			MemoryLimitMB:     200,
			PreallocateSlices: true,
			EnableMetrics:     true,
			MetricsInterval:   30 * time.Second,
			EnableProfiling:   false,
		}

	case ProjectSizeEnterprise:
		return PerformancePreset{
			ProjectSize:       ProjectSizeEnterprise,
			CacheEnabled:      true,
			CacheSize:         10000,
			CacheTTL:          15 * time.Minute,
			CleanupInterval:   3 * time.Minute,
			BatchSize:         200,
			MaxWorkers:        runtime.NumCPU() * 2,
			WorkerQueueSize:   1000,
			IOBufferSize:      32768,
			ConcurrentReads:   16,
			FilesystemTimeout: 10 * time.Second,
			GCFrequency:       2000,
			MemoryLimitMB:     500,
			PreallocateSlices: true,
			EnableMetrics:     true,
			MetricsInterval:   15 * time.Second,
			EnableProfiling:   true,
		}

	default:
		return GetPerformancePreset(ProjectSizeMedium)
	}
}

// GetCIPreset returns ultra-fast configuration optimized for CI/CD scenarios
func GetCIPreset() PerformancePreset {
	return PerformancePreset{
		ProjectSize:       ProjectSizeMedium,
		CacheEnabled:      false, // No cache for single-run CI
		CacheSize:         0,
		CacheTTL:          0,
		CleanupInterval:   0,
		BatchSize:         500, // Large batches for throughput
		MaxWorkers:        runtime.NumCPU() * 2,
		WorkerQueueSize:   2000,
		IOBufferSize:      65536, // Large buffer for speed
		ConcurrentReads:   runtime.NumCPU(),
		FilesystemTimeout: 30 * time.Second, // Longer timeout for slow CI systems
		GCFrequency:       10000,            // Less frequent GC
		MemoryLimitMB:     1000,             // More memory for speed
		PreallocateSlices: true,
		EnableMetrics:     false, // No metrics in CI
		MetricsInterval:   0,
		EnableProfiling:   false,
	}
}

// ===== BATCH PROCESSING OPTIMIZATIONS =====

// BatchProcessor handles batch processing of class resolutions for improved performance
type BatchProcessor struct {
	resolver    PSR4Resolver
	preset      PerformancePreset
	workQueue   chan BatchJob
	resultQueue chan BatchResult
	workers     []*BatchWorker
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	metrics     *BatchMetrics
}

// BatchJob represents a single class resolution job
type BatchJob struct {
	ID   int
	FQCN string
}

// BatchResult represents the result of a batch job
type BatchResult struct {
	ID       int
	FQCN     string
	FilePath string
	Error    error
	Duration time.Duration
}

// BatchWorker handles individual resolution jobs
type BatchWorker struct {
	id       int
	jobs     <-chan BatchJob
	results  chan<- BatchResult
	resolver PSR4Resolver
	ctx      context.Context
}

// NewBatchProcessor creates a new batch processor with the given resolver and preset
func NewBatchProcessor(resolver PSR4Resolver, preset PerformancePreset) *BatchProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	processor := &BatchProcessor{
		resolver:    resolver,
		preset:      preset,
		workQueue:   make(chan BatchJob, preset.WorkerQueueSize),
		resultQueue: make(chan BatchResult, preset.WorkerQueueSize),
		workers:     make([]*BatchWorker, preset.MaxWorkers),
		ctx:         ctx,
		cancel:      cancel,
		metrics:     NewBatchMetrics(),
	}

	// Start workers
	for i := 0; i < preset.MaxWorkers; i++ {
		worker := &BatchWorker{
			id:       i,
			jobs:     processor.workQueue,
			results:  processor.resultQueue,
			resolver: resolver,
			ctx:      ctx,
		}
		processor.workers[i] = worker

		processor.wg.Add(1)
		go processor.runWorker(worker)
	}

	return processor
}

// ProcessBatch processes a batch of class names and returns results
func (bp *BatchProcessor) ProcessBatch(classes []string) (map[string]BatchResult, error) {
	start := time.Now()
	results := make(map[string]BatchResult)
	totalJobs := len(classes)

	if totalJobs == 0 {
		return results, nil
	}

	// Send jobs to workers
	go func() {
		for i, class := range classes {
			select {
			case bp.workQueue <- BatchJob{ID: i, FQCN: class}:
			case <-bp.ctx.Done():
				return
			}
		}
	}()

	// Collect results
	completed := 0
	for completed < totalJobs {
		select {
		case result := <-bp.resultQueue:
			results[result.FQCN] = result
			completed++

			// Update metrics
			bp.metrics.RecordJob(result.Duration, result.Error == nil)

		case <-bp.ctx.Done():
			return results, bp.ctx.Err()
		}
	}

	bp.metrics.RecordBatch(time.Since(start), totalJobs)
	return results, nil
}

// runWorker runs a single batch worker
func (bp *BatchProcessor) runWorker(worker *BatchWorker) {
	defer bp.wg.Done()

	for {
		select {
		case job := <-worker.jobs:
			start := time.Now()

			filePath, err := worker.resolver.ResolveClass(worker.ctx, job.FQCN)
			duration := time.Since(start)

			result := BatchResult{
				ID:       job.ID,
				FQCN:     job.FQCN,
				FilePath: filePath,
				Error:    err,
				Duration: duration,
			}

			select {
			case bp.resultQueue <- result:
			case <-worker.ctx.Done():
				return
			}

		case <-worker.ctx.Done():
			return
		}
	}
}

// Close shuts down the batch processor and all workers
func (bp *BatchProcessor) Close() error {
	bp.cancel()
	bp.wg.Wait()
	close(bp.resultQueue)
	return nil
}

// GetMetrics returns current batch processing metrics
func (bp *BatchProcessor) GetMetrics() BatchMetrics {
	return bp.metrics.GetSnapshot()
}

// ===== CACHE OPTIMIZATION =====

// CacheOptimizer provides intelligent cache configuration and tuning
type CacheOptimizer struct {
	resolver         *DefaultPSR4Resolver
	preset           PerformancePreset
	metrics          *CacheMetrics
	lastOptimization time.Time
}

// NewCacheOptimizer creates a new cache optimizer
func NewCacheOptimizer(resolver *DefaultPSR4Resolver, preset PerformancePreset) *CacheOptimizer {
	return &CacheOptimizer{
		resolver: resolver,
		preset:   preset,
		metrics:  NewCacheMetrics(),
	}
}

// OptimizeCache adjusts cache settings based on usage patterns
func (co *CacheOptimizer) OptimizeCache() {
	if co.resolver.cache == nil {
		return
	}

	// Only optimize every few minutes to avoid constant changes
	if time.Since(co.lastOptimization) < co.preset.MetricsInterval {
		return
	}

	stats := co.resolver.cache.GetStats()

	// Adjust TTL based on hit rate and size
	if stats.Size > co.preset.CacheSize*8/10 { // 80% full
		// Cache is getting full - reduce TTL to increase turnover
		newTTL := co.preset.CacheTTL * 3 / 4
		if newTTL > time.Minute {
			co.resolver.cache.SetDefaultTTL(newTTL)
		}
	} else if stats.Size < co.preset.CacheSize/4 { // 25% full
		// Cache is underutilized - increase TTL
		newTTL := co.preset.CacheTTL * 5 / 4
		if newTTL < 30*time.Minute {
			co.resolver.cache.SetDefaultTTL(newTTL)
		}
	}

	co.lastOptimization = time.Now()
}

// WarmCache pre-loads common classes into the cache
func (co *CacheOptimizer) WarmCache(commonClasses []string) error {
	if co.resolver.cache == nil {
		return nil
	}

	ctx := context.Background()
	for _, class := range commonClasses {
		_, _ = co.resolver.ResolveClass(ctx, class)
	}

	return nil
}

// ===== PERFORMANCE MONITORING =====

// BatchMetrics tracks batch processing performance
type BatchMetrics struct {
	mu               sync.RWMutex
	TotalJobs        int64
	SuccessfulJobs   int64
	FailedJobs       int64
	TotalBatches     int64
	TotalDuration    time.Duration
	AverageJobTime   time.Duration
	AverageBatchTime time.Duration
	MaxJobTime       time.Duration
	MinJobTime       time.Duration
	ThroughputPerSec float64
}

// NewBatchMetrics creates a new metrics collector
func NewBatchMetrics() *BatchMetrics {
	return &BatchMetrics{
		MinJobTime: time.Hour, // Initialize to high value
	}
}

// RecordJob records metrics for a single job
func (bm *BatchMetrics) RecordJob(duration time.Duration, success bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.TotalJobs++
	bm.TotalDuration += duration

	if success {
		bm.SuccessfulJobs++
	} else {
		bm.FailedJobs++
	}

	if duration > bm.MaxJobTime {
		bm.MaxJobTime = duration
	}

	if duration < bm.MinJobTime {
		bm.MinJobTime = duration
	}

	if bm.TotalJobs > 0 {
		bm.AverageJobTime = bm.TotalDuration / time.Duration(bm.TotalJobs)
	}
}

// RecordBatch records metrics for a complete batch
func (bm *BatchMetrics) RecordBatch(duration time.Duration, jobCount int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.TotalBatches++

	if bm.TotalBatches > 0 {
		bm.AverageBatchTime = bm.TotalDuration / time.Duration(bm.TotalBatches)
	}

	if duration.Seconds() > 0 {
		bm.ThroughputPerSec = float64(jobCount) / duration.Seconds()
	}
}

// GetSnapshot returns a thread-safe snapshot of metrics
func (bm *BatchMetrics) GetSnapshot() BatchMetrics {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	// Create a copy without the mutex to avoid copying locks
	return BatchMetrics{
		TotalJobs:        bm.TotalJobs,
		SuccessfulJobs:   bm.SuccessfulJobs,
		FailedJobs:       bm.FailedJobs,
		TotalBatches:     bm.TotalBatches,
		TotalDuration:    bm.TotalDuration,
		AverageJobTime:   bm.AverageJobTime,
		AverageBatchTime: bm.AverageBatchTime,
		MaxJobTime:       bm.MaxJobTime,
		MinJobTime:       bm.MinJobTime,
		ThroughputPerSec: bm.ThroughputPerSec,
	}
}

// CacheMetrics tracks cache performance over time
type CacheMetrics struct {
	mu               sync.RWMutex
	Hits             int64
	Misses           int64
	Evictions        int64
	ExpiredCleanups  int64
	LastOptimization time.Time
}

// NewCacheMetrics creates a new cache metrics collector
func NewCacheMetrics() *CacheMetrics {
	return &CacheMetrics{}
}

// RecordHit records a cache hit
func (cm *CacheMetrics) RecordHit() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.Hits++
}

// RecordMiss records a cache miss
func (cm *CacheMetrics) RecordMiss() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.Misses++
}

// GetHitRate returns the cache hit rate
func (cm *CacheMetrics) GetHitRate() float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	total := cm.Hits + cm.Misses
	if total == 0 {
		return 0
	}

	return float64(cm.Hits) / float64(total)
}

// ===== FILESYSTEM OPTIMIZATION =====

// FilesystemOptimizer provides optimizations for file system operations
type FilesystemOptimizer struct {
	preset PerformancePreset
	cache  map[string]bool // Path existence cache
	mu     sync.RWMutex
}

// NewFilesystemOptimizer creates a new filesystem optimizer
func NewFilesystemOptimizer(preset PerformancePreset) *FilesystemOptimizer {
	return &FilesystemOptimizer{
		preset: preset,
		cache:  make(map[string]bool),
	}
}

// BatchFileExists checks multiple file paths efficiently
func (fo *FilesystemOptimizer) BatchFileExists(paths []string) map[string]bool {
	results := make(map[string]bool)

	// Check cache first
	var uncachedPaths []string
	fo.mu.RLock()
	for _, path := range paths {
		if exists, found := fo.cache[path]; found {
			results[path] = exists
		} else {
			uncachedPaths = append(uncachedPaths, path)
		}
	}
	fo.mu.RUnlock()

	if len(uncachedPaths) == 0 {
		return results
	}

	// Process uncached paths in batches
	batchSize := fo.preset.BatchSize
	if batchSize <= 0 {
		batchSize = 50
	}

	for i := 0; i < len(uncachedPaths); i += batchSize {
		end := i + batchSize
		if end > len(uncachedPaths) {
			end = len(uncachedPaths)
		}

		batch := uncachedPaths[i:end]
		fo.processBatch(batch, results)
	}

	return results
}

// processBatch processes a batch of file existence checks
func (fo *FilesystemOptimizer) processBatch(paths []string, results map[string]bool) {
	// In a real implementation, this would use optimized system calls
	// For now, we'll simulate the behavior
	fo.mu.Lock()
	defer fo.mu.Unlock()

	for _, path := range paths {
		// Simulate file existence check
		exists := len(path) > 0 && path[0] != '~' // Simple heuristic for benchmarking

		fo.cache[path] = exists
		results[path] = exists
	}
}

// ClearCache clears the filesystem cache
func (fo *FilesystemOptimizer) ClearCache() {
	fo.mu.Lock()
	defer fo.mu.Unlock()
	fo.cache = make(map[string]bool)
}

// ===== MEMORY OPTIMIZATION =====

// MemoryOptimizer provides memory usage optimization
type MemoryOptimizer struct {
	preset         PerformancePreset
	operationCount int
	lastGC         time.Time
	memoryPressure bool
}

// NewMemoryOptimizer creates a new memory optimizer
func NewMemoryOptimizer(preset PerformancePreset) *MemoryOptimizer {
	return &MemoryOptimizer{
		preset: preset,
	}
}

// CheckMemoryPressure checks if memory usage is high and suggests optimizations
func (mo *MemoryOptimizer) CheckMemoryPressure() bool {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// Convert preset memory limit to bytes
	limitBytes := uint64(mo.preset.MemoryLimitMB) * 1024 * 1024

	if mem.Alloc > limitBytes {
		mo.memoryPressure = true
		return true
	}

	mo.memoryPressure = false
	return false
}

// SuggestGC suggests garbage collection if needed
func (mo *MemoryOptimizer) SuggestGC() bool {
	mo.operationCount++

	// Check if we should suggest GC based on operation count
	if mo.operationCount >= mo.preset.GCFrequency {
		mo.operationCount = 0
		mo.lastGC = time.Now()
		return true
	}

	// Check if memory pressure is high
	if mo.CheckMemoryPressure() && time.Since(mo.lastGC) > time.Minute {
		mo.lastGC = time.Now()
		return true
	}

	return false
}

// GetOptimizedSlice returns a pre-allocated slice with optimal capacity
func (mo *MemoryOptimizer) GetOptimizedSlice(expectedSize int) []string {
	if !mo.preset.PreallocateSlices {
		return make([]string, 0)
	}

	// Allocate with some extra capacity to avoid frequent reallocations
	capacity := expectedSize + (expectedSize / 4) // 25% extra
	if capacity < 8 {
		capacity = 8
	}

	return make([]string, 0, capacity)
}

// ===== INTEGRATION HELPERS =====

// OptimizedResolver wraps a PSR4Resolver with performance optimizations
type OptimizedResolver struct {
	resolver        PSR4Resolver
	batchProcessor  *BatchProcessor
	cacheOptimizer  *CacheOptimizer
	memoryOptimizer *MemoryOptimizer
	fsOptimizer     *FilesystemOptimizer
	preset          PerformancePreset
}

// NewOptimizedResolver creates a resolver with all performance optimizations enabled
func NewOptimizedResolver(resolver PSR4Resolver, size ProjectSize) *OptimizedResolver {
	preset := GetPerformancePreset(size)

	optimized := &OptimizedResolver{
		resolver:        resolver,
		preset:          preset,
		memoryOptimizer: NewMemoryOptimizer(preset),
		fsOptimizer:     NewFilesystemOptimizer(preset),
	}

	// Initialize batch processor
	optimized.batchProcessor = NewBatchProcessor(resolver, preset)

	// Initialize cache optimizer if resolver supports it
	if defaultResolver, ok := resolver.(*DefaultPSR4Resolver); ok {
		optimized.cacheOptimizer = NewCacheOptimizer(defaultResolver, preset)
	}

	return optimized
}

// ResolveClassesOptimized resolves multiple classes with optimizations
func (or *OptimizedResolver) ResolveClassesOptimized(classes []string) (map[string]string, error) {
	// Use batch processing for better performance
	results, err := or.batchProcessor.ProcessBatch(classes)
	if err != nil {
		return nil, err
	}

	// Convert to simple map
	resolved := make(map[string]string)
	for fqcn, result := range results {
		if result.Error == nil {
			resolved[fqcn] = result.FilePath
		}
	}

	// Optimize cache if needed
	if or.cacheOptimizer != nil {
		or.cacheOptimizer.OptimizeCache()
	}

	// Check for memory pressure
	if or.memoryOptimizer.SuggestGC() {
		runtime.GC()
	}

	return resolved, nil
}

// Close cleans up all optimization resources
func (or *OptimizedResolver) Close() error {
	return or.batchProcessor.Close()
}

// GetPerformanceReport returns a comprehensive performance report
func (or *OptimizedResolver) GetPerformanceReport() PerformanceReport {
	return PerformanceReport{
		Preset:         or.preset,
		BatchMetrics:   or.batchProcessor.GetMetrics(),
		MemoryPressure: or.memoryOptimizer.CheckMemoryPressure(),
	}
}

// PerformanceReport contains comprehensive performance information
type PerformanceReport struct {
	Preset         PerformancePreset
	BatchMetrics   BatchMetrics
	MemoryPressure bool
}

// ===== UTILITY FUNCTIONS =====

// EstimateProjectSize estimates project size from class count
func EstimateProjectSize(estimatedClasses int) ProjectSize {
	switch {
	case estimatedClasses < 100:
		return ProjectSizeSmall
	case estimatedClasses < 500:
		return ProjectSizeMedium
	case estimatedClasses < 2000:
		return ProjectSizeLarge
	default:
		return ProjectSizeEnterprise
	}
}

// GetOptimalPresetForManifest determines the best preset based on manifest constraints
func GetOptimalPresetForManifest(manifestMaxFiles, manifestMaxWorkers int) PerformancePreset {
	// Estimate project size from file limits
	var size ProjectSize
	if manifestMaxFiles > 0 {
		// Assume roughly 1 class per 2-3 files
		estimatedClasses := manifestMaxFiles / 2
		size = EstimateProjectSize(estimatedClasses)
	} else {
		size = ProjectSizeMedium // Safe default
	}

	preset := GetPerformancePreset(size)

	// Override with manifest constraints if specified
	if manifestMaxWorkers > 0 && manifestMaxWorkers < preset.MaxWorkers {
		preset.MaxWorkers = manifestMaxWorkers
	}

	return preset
}
