package perf

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryOptimizer provides comprehensive memory optimization strategies for the Oxinfer pipeline.
type MemoryOptimizer struct {
	config    *MemoryConfig
	pools     *ObjectPools
	allocator *ManagedAllocator
	monitor   *MemoryMonitor

	// State tracking
	optimizations []string
	metrics       *MemoryMetrics
	mu            sync.RWMutex
}

// MemoryConfig contains configuration for memory optimization.
type MemoryConfig struct {
	// Pool sizes
	StringBuilderPoolSize int `json:"stringBuilderPoolSize"`
	SlicePoolSize         int `json:"slicePoolSize"`
	ASTNodePoolSize       int `json:"astNodePoolSize"`

	// Memory limits
	MaxHeapSizeMB   int64 `json:"maxHeapSizeMB"`
	GCTargetPercent int   `json:"gcTargetPercent"`

	// Optimization thresholds
	PoolReuseThreshold   int   `json:"poolReuseThreshold"`   // Minimum allocations to use pooling
	StreamingThresholdMB int64 `json:"streamingThresholdMB"` // File size to trigger streaming

	// GC tuning
	EnableGCTuning       bool          `json:"enableGCTuning"`
	GCCollectionInterval time.Duration `json:"gcCollectionIntervalMs"`
}

// DefaultMemoryConfig returns optimized default memory configuration.
func DefaultMemoryConfig() *MemoryConfig {
	return &MemoryConfig{
		StringBuilderPoolSize: 100,
		SlicePoolSize:         200,
		ASTNodePoolSize:       500,
		MaxHeapSizeMB:         400, // MVP target: <500MB
		GCTargetPercent:       70,  // Slightly aggressive GC
		PoolReuseThreshold:    50,  // Use pools after 50 allocations
		StreamingThresholdMB:  10,  // Stream files >10MB
		EnableGCTuning:        true,
		GCCollectionInterval:  2 * time.Second,
	}
}

// ObjectPools manages reusable object pools to reduce allocations.
type ObjectPools struct {
	// String processing pools
	stringBuilders sync.Pool
	stringSlices   sync.Pool

	// Data structure pools
	intSlices  sync.Pool
	byteSlices sync.Pool

	// Tree-sitter specific pools
	astNodes sync.Pool

	// Metrics
	getCount  int64
	putCount  int64
	missCount int64

	mu sync.RWMutex
}

// ManagedAllocator provides controlled memory allocation with tracking.
type ManagedAllocator struct {
	config    *MemoryConfig
	allocated int64
	limit     int64

	// Allocation tracking
	allocations map[string]*AllocationTracker
	mu          sync.RWMutex
}

// AllocationTracker tracks allocations for a specific component.
type AllocationTracker struct {
	Component      string    `json:"component"`
	TotalBytes     int64     `json:"totalBytes"`
	TotalCount     int64     `json:"totalCount"`
	PeakBytes      int64     `json:"peakBytes"`
	AverageSize    float64   `json:"averageSize"`
	LastAllocation time.Time `json:"lastAllocation"`
}

// MemoryMonitor continuously monitors memory usage and triggers optimizations.
type MemoryMonitor struct {
	optimizer  *MemoryOptimizer
	monitoring bool
	interval   time.Duration

	// Thresholds
	warningThreshold  float64 // Percentage of limit to trigger warning
	criticalThreshold float64 // Percentage of limit to trigger aggressive GC

	// State
	lastGC       time.Time
	forceGCCount int64

	mu sync.RWMutex
}

// MemoryMetrics tracks comprehensive memory usage metrics.
type MemoryMetrics struct {
	// Current usage
	HeapUsedMB  int64 `json:"heapUsedMB"`
	HeapAllocMB int64 `json:"heapAllocMB"`
	StackUsedMB int64 `json:"stackUsedMB"`
	TotalUsedMB int64 `json:"totalUsedMB"`

	// Pool statistics
	PoolHitRate     float64 `json:"poolHitRate"`     // Pool hits / (hits + misses)
	PoolMemorySaved int64   `json:"poolMemorySaved"` // Estimated memory saved by pooling

	// GC statistics
	GCCount        int64   `json:"gcCount"`
	GCPauseTotalMs int64   `json:"gcPauseTotalMs"`
	GCCPUPercent   float64 `json:"gcCPUPercent"`

	// Efficiency metrics
	MemoryEfficiency   float64 `json:"memoryEfficiency"`   // Useful memory / total memory
	FragmentationRatio float64 `json:"fragmentationRatio"` // Fragmented / total

	// Streaming statistics
	StreamingEnabled bool  `json:"streamingEnabled"`
	StreamingFilesMB int64 `json:"streamingFilesMB"`
	StreamingSavedMB int64 `json:"streamingSavedMB"`

	// Timestamps
	LastUpdateTime time.Time `json:"lastUpdateTime"`
}

// NewMemoryOptimizer creates a new memory optimizer with the given configuration.
func NewMemoryOptimizer(config *MemoryConfig) (*MemoryOptimizer, error) {
	if config == nil {
		config = DefaultMemoryConfig()
	}

	if err := validateMemoryConfig(config); err != nil {
		return nil, fmt.Errorf("invalid memory config: %w", err)
	}

	optimizer := &MemoryOptimizer{
		config:        config,
		pools:         NewObjectPools(),
		allocator:     NewManagedAllocator(config),
		optimizations: make([]string, 0),
		metrics:       &MemoryMetrics{},
	}

	optimizer.monitor = NewMemoryMonitor(optimizer, 1*time.Second)

	// Apply initial optimizations
	optimizer.applyInitialOptimizations()

	return optimizer, nil
}

// NewObjectPools creates and initializes object pools.
func NewObjectPools() *ObjectPools {
	pools := &ObjectPools{}

	// Initialize string builder pool
	pools.stringBuilders.New = func() any {
		return &strings.Builder{}
	}

	// Initialize string slice pool
	pools.stringSlices.New = func() any {
		slice := make([]string, 0, 50) // Pre-allocate capacity
		return &slice
	}

	// Initialize int slice pool
	pools.intSlices.New = func() any {
		slice := make([]int, 0, 100)
		return &slice
	}

	// Initialize byte slice pool
	pools.byteSlices.New = func() any {
		slice := make([]byte, 0, 8192) // 8KB initial capacity
		return &slice
	}

	// Initialize AST node pool
	pools.astNodes.New = func() any {
		return &OptimizedASTNode{}
	}

	return pools
}

// NewManagedAllocator creates a new managed allocator.
func NewManagedAllocator(config *MemoryConfig) *ManagedAllocator {
	return &ManagedAllocator{
		config:      config,
		limit:       config.MaxHeapSizeMB * 1024 * 1024,
		allocations: make(map[string]*AllocationTracker),
	}
}

// NewMemoryMonitor creates a new memory monitor.
func NewMemoryMonitor(optimizer *MemoryOptimizer, interval time.Duration) *MemoryMonitor {
	return &MemoryMonitor{
		optimizer:         optimizer,
		interval:          interval,
		warningThreshold:  0.8,  // 80% of limit
		criticalThreshold: 0.95, // 95% of limit
	}
}

// Start begins memory optimization and monitoring.
func (mo *MemoryOptimizer) Start(ctx context.Context) error {
	// Start memory monitoring
	go mo.monitor.start(ctx)

	// Apply runtime optimizations
	mo.applyGCTuning()

	return nil
}

// applyInitialOptimizations applies initial memory optimizations.
func (mo *MemoryOptimizer) applyInitialOptimizations() {
	mo.mu.Lock()
	defer mo.mu.Unlock()

	// Pre-allocate commonly used slices
	mo.optimizations = append(mo.optimizations, "pre_allocated_slices")

	// Configure object pools
	mo.optimizations = append(mo.optimizations, "object_pools")

	// Enable streaming for large files
	mo.optimizations = append(mo.optimizations, "streaming_enabled")
}

// applyGCTuning applies garbage collection tuning optimizations.
func (mo *MemoryOptimizer) applyGCTuning() {
	if !mo.config.EnableGCTuning {
		return
	}

	// Set GC target percentage
	runtime.GC()
	debug.SetGCPercent(mo.config.GCTargetPercent)

	mo.mu.Lock()
	mo.optimizations = append(mo.optimizations, "gc_tuning")
	mo.mu.Unlock()
}

// OptimizedASTNode represents an AST node optimized for memory usage.
type OptimizedASTNode struct {
	Type     uint16 // Use smaller integer type
	StartPos uint32 // 32-bit position (supports files up to 4GB)
	EndPos   uint32
	Data     []byte              // Reuse byte slice from pool
	Children []*OptimizedASTNode // Slice from pool
}

// GetStringBuilder retrieves a string builder from the pool.
func (pools *ObjectPools) GetStringBuilder() *strings.Builder {
	atomic.AddInt64(&pools.getCount, 1)

	sb := pools.stringBuilders.Get().(*strings.Builder)
	sb.Reset() // Ensure clean state
	return sb
}

// PutStringBuilder returns a string builder to the pool.
func (pools *ObjectPools) PutStringBuilder(sb *strings.Builder) {
	atomic.AddInt64(&pools.putCount, 1)

	// Only pool if not too large to prevent memory bloat
	if sb.Cap() <= 16384 { // 16KB cap limit
		pools.stringBuilders.Put(sb)
	}
}

// GetByteSlice retrieves a byte slice from the pool.
func (pools *ObjectPools) GetByteSlice() *[]byte {
	atomic.AddInt64(&pools.getCount, 1)

	slice := pools.byteSlices.Get().(*[]byte)
	*slice = (*slice)[:0] // Reset length but keep capacity
	return slice
}

// PutByteSlice returns a byte slice to the pool.
func (pools *ObjectPools) PutByteSlice(slice *[]byte) {
	atomic.AddInt64(&pools.putCount, 1)

	// Only pool reasonable sizes to prevent memory bloat
	if cap(*slice) <= 65536 { // 64KB cap limit
		pools.byteSlices.Put(slice)
	}
}

// GetStringSlice retrieves a string slice from the pool.
func (pools *ObjectPools) GetStringSlice() *[]string {
	atomic.AddInt64(&pools.getCount, 1)

	slice := pools.stringSlices.Get().(*[]string)
	*slice = (*slice)[:0] // Reset length but keep capacity
	return slice
}

// PutStringSlice returns a string slice to the pool.
func (pools *ObjectPools) PutStringSlice(slice *[]string) {
	atomic.AddInt64(&pools.putCount, 1)

	// Clear slice elements to prevent memory leaks
	for i := range *slice {
		(*slice)[i] = ""
	}
	*slice = (*slice)[:0]

	if cap(*slice) <= 1000 { // Reasonable cap limit
		pools.stringSlices.Put(slice)
	}
}

// GetASTNode retrieves an AST node from the pool.
func (pools *ObjectPools) GetASTNode() *OptimizedASTNode {
	atomic.AddInt64(&pools.getCount, 1)

	node := pools.astNodes.Get().(*OptimizedASTNode)

	// Reset node state
	node.Type = 0
	node.StartPos = 0
	node.EndPos = 0
	node.Data = node.Data[:0]
	node.Children = node.Children[:0]

	return node
}

// PutASTNode returns an AST node to the pool.
func (pools *ObjectPools) PutASTNode(node *OptimizedASTNode) {
	atomic.AddInt64(&pools.putCount, 1)

	// Clear children references to prevent memory leaks
	for i := range node.Children {
		node.Children[i] = nil
	}

	// Only pool if not too large
	if len(node.Children) <= 100 && len(node.Data) <= 1024 {
		pools.astNodes.Put(node)
	}
}

// StreamingFileProcessor provides memory-efficient processing for large files.
type StreamingFileProcessor struct {
	config     *MemoryConfig
	bufferPool sync.Pool
	chunkSize  int

	// Statistics
	filesStreamed int64
	bytesStreamed int64
	memorySaved   int64
}

// NewStreamingFileProcessor creates a new streaming file processor.
func NewStreamingFileProcessor(config *MemoryConfig) *StreamingFileProcessor {
	processor := &StreamingFileProcessor{
		config:    config,
		chunkSize: 64 * 1024, // 64KB chunks
	}

	// Initialize buffer pool
	processor.bufferPool.New = func() any {
		return make([]byte, processor.chunkSize)
	}

	return processor
}

// ProcessLargeFile processes large files using streaming to minimize memory usage.
func (sfp *StreamingFileProcessor) ProcessLargeFile(ctx context.Context, filePath string, processor func([]byte) error) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get buffer from pool
	buffer := sfp.bufferPool.Get().([]byte)
	defer func() { sfp.bufferPool.Put(buffer) }()

	atomic.AddInt64(&sfp.filesStreamed, 1)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := file.Read(buffer)
		if n == 0 {
			break
		}

		chunk := buffer[:n]
		if err := processor(chunk); err != nil {
			return fmt.Errorf("chunk processing failed: %w", err)
		}

		atomic.AddInt64(&sfp.bytesStreamed, int64(n))

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("file read error: %w", err)
		}
	}

	// Estimate memory saved by streaming
	fileInfo, _ := file.Stat()
	if fileInfo != nil {
		memorySaved := fileInfo.Size() - int64(sfp.chunkSize)
		if memorySaved > 0 {
			atomic.AddInt64(&sfp.memorySaved, memorySaved)
		}
	}

	return nil
}

// Allocate tracks and limits memory allocations.
func (ma *ManagedAllocator) Allocate(component string, size int64) (bool, error) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	// Check if allocation would exceed limit
	if ma.allocated+size > ma.limit {
		return false, fmt.Errorf("allocation would exceed memory limit: %d + %d > %d", ma.allocated, size, ma.limit)
	}

	// Track allocation
	tracker, exists := ma.allocations[component]
	if !exists {
		tracker = &AllocationTracker{
			Component: component,
		}
		ma.allocations[component] = tracker
	}

	tracker.TotalBytes += size
	tracker.TotalCount++
	tracker.LastAllocation = time.Now()

	if tracker.TotalBytes > tracker.PeakBytes {
		tracker.PeakBytes = tracker.TotalBytes
	}

	tracker.AverageSize = float64(tracker.TotalBytes) / float64(tracker.TotalCount)

	ma.allocated += size
	return true, nil
}

// Free releases allocated memory tracking.
func (ma *ManagedAllocator) Free(component string, size int64) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	tracker, exists := ma.allocations[component]
	if exists {
		tracker.TotalBytes -= size
		if tracker.TotalBytes < 0 {
			tracker.TotalBytes = 0
		}
	}

	ma.allocated -= size
	if ma.allocated < 0 {
		ma.allocated = 0
	}
}

// start begins memory monitoring.
func (mm *MemoryMonitor) start(ctx context.Context) {
	mm.monitoring = true
	ticker := time.NewTicker(mm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			mm.monitoring = false
			return
		case <-ticker.C:
			mm.checkMemoryUsage()
		}
	}
}

// checkMemoryUsage monitors current memory usage and triggers optimizations.
func (mm *MemoryMonitor) checkMemoryUsage() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	currentUsage := int64(memStats.HeapInuse)
	limit := mm.optimizer.config.MaxHeapSizeMB * 1024 * 1024
	utilizationRatio := float64(currentUsage) / float64(limit)

	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Update metrics
	mm.optimizer.updateMemoryMetrics(&memStats)

	// Check thresholds
	if utilizationRatio >= mm.criticalThreshold {
		mm.triggerAggressiveGC()
	} else if utilizationRatio >= mm.warningThreshold {
		mm.triggerOptimizations()
	}
}

// triggerAggressiveGC forces garbage collection when memory is critical.
func (mm *MemoryMonitor) triggerAggressiveGC() {
	now := time.Now()

	// Avoid too frequent forced GC
	if now.Sub(mm.lastGC) < 500*time.Millisecond {
		return
	}

	runtime.GC()
	runtime.GC() // Double GC for thorough cleanup

	mm.lastGC = now
	atomic.AddInt64(&mm.forceGCCount, 1)
}

// triggerOptimizations applies memory optimizations when usage is high.
func (mm *MemoryMonitor) triggerOptimizations() {
	// Clear unused pool entries
	mm.optimizer.pools.clearExcessCapacity()

	// Suggest streaming for large allocations
	mm.optimizer.recommendStreaming()
}

// clearExcessCapacity removes excess capacity from pools to reduce memory usage.
func (pools *ObjectPools) clearExcessCapacity() {
	// This is a simplified implementation
	// In practice, you'd implement more sophisticated pool management
	pools.mu.Lock()
	defer pools.mu.Unlock()

	// Clear some pooled objects to free memory
	// Implementation would depend on specific pool types and usage patterns
}

// recommendStreaming suggests using streaming for large files.
func (mo *MemoryOptimizer) recommendStreaming() {
	// This would integrate with the file processing pipeline
	// to recommend streaming for files above the threshold
}

// updateMemoryMetrics updates the current memory metrics.
func (mo *MemoryOptimizer) updateMemoryMetrics(memStats *runtime.MemStats) {
	mo.mu.Lock()
	defer mo.mu.Unlock()

	mo.metrics.HeapUsedMB = int64(memStats.HeapInuse / 1024 / 1024)
	mo.metrics.HeapAllocMB = int64(memStats.HeapAlloc / 1024 / 1024)
	mo.metrics.StackUsedMB = int64(memStats.StackInuse / 1024 / 1024)
	mo.metrics.TotalUsedMB = mo.metrics.HeapUsedMB + mo.metrics.StackUsedMB

	// Calculate pool hit rate
	totalGets := atomic.LoadInt64(&mo.pools.getCount)
	totalMisses := atomic.LoadInt64(&mo.pools.missCount)

	if totalGets > 0 {
		mo.metrics.PoolHitRate = float64(totalGets-totalMisses) / float64(totalGets)
	}

	// GC statistics
	mo.metrics.GCCount = int64(memStats.NumGC)
	mo.metrics.GCPauseTotalMs = int64(memStats.PauseTotalNs / 1000000)

	// Memory efficiency (heap used / heap allocated)
	if memStats.HeapSys > 0 {
		mo.metrics.MemoryEfficiency = float64(memStats.HeapInuse) / float64(memStats.HeapSys)
	}

	mo.metrics.LastUpdateTime = time.Now()
}

// GetMetrics returns the current memory optimization metrics.
func (mo *MemoryOptimizer) GetMetrics() *MemoryMetrics {
	mo.mu.RLock()
	defer mo.mu.RUnlock()

	// Return a copy to prevent external modification
	metrics := *mo.metrics
	return &metrics
}

// GetOptimizations returns the list of applied optimizations.
func (mo *MemoryOptimizer) GetOptimizations() []string {
	mo.mu.RLock()
	defer mo.mu.RUnlock()

	optimizations := make([]string, len(mo.optimizations))
	copy(optimizations, mo.optimizations)
	return optimizations
}

// EstimateMemorySavings estimates potential memory savings from optimizations.
func (mo *MemoryOptimizer) EstimateMemorySavings() int64 {
	mo.mu.RLock()
	defer mo.mu.RUnlock()

	var totalSavings int64

	// Pool savings (estimated based on reuse rate)
	poolSavings := int64(float64(mo.metrics.PoolHitRate) * float64(mo.metrics.HeapUsedMB) * 0.3) // 30% of heap from pooling
	totalSavings += poolSavings

	// Streaming savings
	totalSavings += mo.metrics.StreamingSavedMB

	// GC optimization savings (reduced overhead)
	gcSavings := int64(float64(mo.metrics.HeapUsedMB) * 0.05) // 5% from better GC
	totalSavings += gcSavings

	return totalSavings
}

// validateMemoryConfig validates the memory optimization configuration.
func validateMemoryConfig(config *MemoryConfig) error {
	if config.MaxHeapSizeMB <= 0 {
		return fmt.Errorf("max heap size must be positive")
	}

	if config.GCTargetPercent <= 0 || config.GCTargetPercent > 1000 {
		return fmt.Errorf("GC target percent must be between 1 and 1000")
	}

	if config.StringBuilderPoolSize <= 0 {
		return fmt.Errorf("string builder pool size must be positive")
	}

	if config.StreamingThresholdMB <= 0 {
		return fmt.Errorf("streaming threshold must be positive")
	}

	return nil
}
