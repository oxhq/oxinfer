//go:build !goexperiment.jsonv2
// Package parser provides comprehensive memory management optimization for tree-sitter PHP parsing.
// Implements memory pools, garbage collection optimization, and resource lifecycle management
// to ensure stable memory usage under large Laravel project loads.
package parser

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryOptimizer manages memory usage optimization for PHP parsing operations.
// Provides memory pools, GC optimization, and resource tracking to prevent memory leaks
// and ensure stable performance under high load conditions.
type MemoryOptimizer struct {
	// Memory pools for reusable objects
	parseResultPool  sync.Pool
	syntaxTreePool   sync.Pool
	syntaxNodePool   sync.Pool
	queryMatchPool   sync.Pool
	phpConstructPool sync.Pool

	// Memory tracking and statistics
	peakMemoryUsage    int64 // Peak memory usage observed (atomic)
	currentMemoryUsage int64 // Current estimated memory usage (atomic)
	gcTriggerThreshold int64 // Memory threshold for triggering GC
	gcTriggerCount     int64 // Number of times GC was manually triggered (atomic)

	// Pool statistics
	poolHitRate float64 // Object pool hit rate percentage
	poolMisses  int64   // Number of pool misses (atomic)
	poolHits    int64   // Number of pool hits (atomic)

	// Configuration
	maxMemoryUsage       int64 // Maximum allowed memory usage
	enablePooling        bool  // Whether to use object pooling
	enableGCOptimization bool  // Whether to optimize GC behavior

	// Resource tracking for leak detection
	activeParseResults int64 // Current active ParseResult objects (atomic)
	activeSyntaxTrees  int64 // Current active SyntaxTree objects (atomic)

	// Metrics and monitoring
	mu              sync.RWMutex
	stats           MemoryStats
	lastGCTime      time.Time
	lastMemoryCheck time.Time
}

// MemoryStats contains detailed memory usage statistics for monitoring.
type MemoryStats struct {
	// Pool statistics
	ParseResultPoolSize int // Current size of ParseResult pool
	SyntaxTreePoolSize  int // Current size of SyntaxTree pool
	SyntaxNodePoolSize  int // Current size of SyntaxNode pool

	// Memory usage
	TotalAllocatedBytes int64 // Total memory allocated
	TotalFreedBytes     int64 // Total memory freed
	CurrentUsageBytes   int64 // Current estimated usage
	PeakUsageBytes      int64 // Peak usage observed

	// GC statistics
	GCTriggerCount  int64         // Manual GC triggers
	GCEffectiveness float64       // Memory freed by GC (percentage)
	LastGCDuration  time.Duration // Duration of last GC

	// Pool efficiency
	PoolHitRate         float64 // Overall pool hit rate
	MemoryLeaksDetected int     // Number of potential leaks detected

	// Performance impact
	OptimizationOverhead   time.Duration // Time spent on optimization
	MemoryOptimizationRate float64       // Effectiveness of optimizations
}

// NewMemoryOptimizer creates a new memory optimizer with default configuration.
// Initializes memory pools and sets reasonable defaults for Laravel project parsing.
func NewMemoryOptimizer() *MemoryOptimizer {
	optimizer := &MemoryOptimizer{
		gcTriggerThreshold:   200 * 1024 * 1024,  // 200MB threshold
		maxMemoryUsage:       1024 * 1024 * 1024, // 1GB max
		enablePooling:        true,
		enableGCOptimization: true,
		lastMemoryCheck:      time.Now(),
	}

	// Initialize memory pools with factory functions
	optimizer.parseResultPool = sync.Pool{
		New: func() any {
			return &ParseResult{
				Errors: make([]ParseError, 0, 8), // Pre-allocate common size
				Stats: ParseStats{
					NodeTypes: make(map[string]int, 32), // Pre-allocate common size
				},
			}
		},
	}

	optimizer.syntaxTreePool = sync.Pool{
		New: func() any {
			return &SyntaxTree{
				Source:   make([]byte, 0, 1024), // Pre-allocate 1KB
				Language: "php",
			}
		},
	}

	optimizer.syntaxNodePool = sync.Pool{
		New: func() any {
			return &SyntaxNode{
				Children: make([]*SyntaxNode, 0, 8), // Pre-allocate common size
			}
		},
	}

	optimizer.queryMatchPool = sync.Pool{
		New: func() any {
			return make(map[string]any, 16) // Pre-allocate common size
		},
	}

	optimizer.phpConstructPool = sync.Pool{
		New: func() any {
			return &PHPFileStructure{
				Classes:       make([]PHPClass, 0, 4),
				Interfaces:    make([]PHPInterface, 0, 2),
				Traits:        make([]PHPTrait, 0, 2),
				Functions:     make([]PHPFunction, 0, 8),
				UseStatements: make([]PHPUseStatement, 0, 16),
			}
		},
	}

	return optimizer
}

// GetParseResult retrieves a ParseResult object from the pool or creates a new one.
// Uses object pooling to reduce memory allocations and improve performance.
func (m *MemoryOptimizer) GetParseResult() *ParseResult {
	if !m.enablePooling {
		atomic.AddInt64(&m.poolMisses, 1)
		atomic.AddInt64(&m.activeParseResults, 1)
		return &ParseResult{}
	}

	result := m.parseResultPool.Get().(*ParseResult)

	// Reset the object to clean state
	result.Tree = nil
	result.RootNode = nil
	result.FilePath = ""
	result.Content = result.Content[:0] // Reuse slice capacity
	result.HasErrors = false
	result.Errors = result.Errors[:0] // Reuse slice capacity
	result.ParsedAt = time.Time{}

	// Reset stats
	result.Stats.ParseTime = 0
	result.Stats.TreeSize = 0
	result.Stats.ContentSize = 0
	result.Stats.ErrorCount = 0
	result.Stats.MaxDepth = 0

	// Clear node types map but keep capacity
	for k := range result.Stats.NodeTypes {
		delete(result.Stats.NodeTypes, k)
	}

	atomic.AddInt64(&m.poolHits, 1)
	atomic.AddInt64(&m.activeParseResults, 1)

	m.updatePoolHitRate()
	return result
}

// ReturnParseResult returns a ParseResult object to the pool for reuse.
// Cleans up any references to prevent memory leaks.
func (m *MemoryOptimizer) ReturnParseResult(result *ParseResult) {
	if result == nil {
		return
	}

	atomic.AddInt64(&m.activeParseResults, -1)

	if !m.enablePooling {
		return
	}

	// Clear any large slices to prevent memory retention
	if cap(result.Content) > 64*1024 { // 64KB threshold
		result.Content = nil
	}

	if cap(result.Errors) > 64 { // Large error slice threshold
		result.Errors = nil
	}

	m.parseResultPool.Put(result)
}

// GetSyntaxTree retrieves a SyntaxTree object from the pool or creates a new one.
func (m *MemoryOptimizer) GetSyntaxTree() *SyntaxTree {
	if !m.enablePooling {
		atomic.AddInt64(&m.poolMisses, 1)
		atomic.AddInt64(&m.activeSyntaxTrees, 1)
		return &SyntaxTree{}
	}

	tree := m.syntaxTreePool.Get().(*SyntaxTree)

	// Reset to clean state
	tree.Root = nil
	tree.Source = tree.Source[:0] // Reuse slice capacity
	tree.Language = "php"
	tree.ParsedAt = time.Time{}

	atomic.AddInt64(&m.poolHits, 1)
	atomic.AddInt64(&m.activeSyntaxTrees, 1)

	m.updatePoolHitRate()
	return tree
}

// ReturnSyntaxTree returns a SyntaxTree object to the pool for reuse.
func (m *MemoryOptimizer) ReturnSyntaxTree(tree *SyntaxTree) {
	if tree == nil {
		return
	}

	atomic.AddInt64(&m.activeSyntaxTrees, -1)

	if !m.enablePooling {
		return
	}

	// Clear large source slices to prevent memory retention
	if cap(tree.Source) > 128*1024 { // 128KB threshold
		tree.Source = nil
	}

	// Recursively clean up syntax tree nodes if they're large
	if tree.Root != nil {
		m.cleanupSyntaxNode(tree.Root)
	}

	m.syntaxTreePool.Put(tree)
}

// GetSyntaxNode retrieves a SyntaxNode from the pool or creates a new one.
func (m *MemoryOptimizer) GetSyntaxNode() *SyntaxNode {
	if !m.enablePooling {
		atomic.AddInt64(&m.poolMisses, 1)
		return &SyntaxNode{}
	}

	node := m.syntaxNodePool.Get().(*SyntaxNode)

	// Reset to clean state
	node.Type = ""
	node.Text = ""
	node.StartByte = 0
	node.EndByte = 0
	node.StartPoint = Point{}
	node.EndPoint = Point{}
	node.Children = node.Children[:0] // Reuse slice capacity
	node.Parent = nil

	atomic.AddInt64(&m.poolHits, 1)
	m.updatePoolHitRate()
	return node
}

// ReturnSyntaxNode returns a SyntaxNode to the pool for reuse.
func (m *MemoryOptimizer) ReturnSyntaxNode(node *SyntaxNode) {
	if node == nil || !m.enablePooling {
		return
	}

	// Clear large children slices to prevent memory retention
	if cap(node.Children) > 256 { // Large node threshold
		node.Children = nil
	}

	m.syntaxNodePool.Put(node)
}

// GetPHPConstructs retrieves a PHPFileStructure from the pool or creates a new one.
func (m *MemoryOptimizer) GetPHPConstructs() *PHPFileStructure {
	if !m.enablePooling {
		atomic.AddInt64(&m.poolMisses, 1)
		return &PHPFileStructure{}
	}

	constructs := m.phpConstructPool.Get().(*PHPFileStructure)

	// Reset to clean state
	constructs.FilePath = ""
	constructs.Namespace = nil
	constructs.Classes = constructs.Classes[:0]
	constructs.Interfaces = constructs.Interfaces[:0]
	constructs.Traits = constructs.Traits[:0]
	constructs.Functions = constructs.Functions[:0]
	constructs.UseStatements = constructs.UseStatements[:0]
	constructs.ParsedAt = time.Time{}
	constructs.ParseDuration = 0

	atomic.AddInt64(&m.poolHits, 1)
	m.updatePoolHitRate()
	return constructs
}

// ReturnPHPConstructs returns a PHPFileStructure to the pool for reuse.
func (m *MemoryOptimizer) ReturnPHPConstructs(constructs *PHPFileStructure) {
	if constructs == nil || !m.enablePooling {
		return
	}

	// Clear large slices to prevent memory retention
	if cap(constructs.Classes) > 32 {
		constructs.Classes = nil
	}
	if cap(constructs.UseStatements) > 64 {
		constructs.UseStatements = nil
	}

	m.phpConstructPool.Put(constructs)
}

// CheckMemoryPressure monitors current memory usage and triggers optimization if needed.
// Should be called periodically during parsing operations to maintain memory health.
func (m *MemoryOptimizer) CheckMemoryPressure() {
	now := time.Now()
	if now.Sub(m.lastMemoryCheck) < 5*time.Second {
		return // Don't check too frequently
	}
	m.lastMemoryCheck = now

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	currentUsage := int64(memStats.Alloc)
	atomic.StoreInt64(&m.currentMemoryUsage, currentUsage)

	// Update peak memory usage
	for {
		peak := atomic.LoadInt64(&m.peakMemoryUsage)
		if currentUsage <= peak || atomic.CompareAndSwapInt64(&m.peakMemoryUsage, peak, currentUsage) {
			break
		}
	}

	// Trigger GC if memory usage is high
	if m.enableGCOptimization && currentUsage > m.gcTriggerThreshold {
		m.triggerOptimizedGC()
	}

	// Check for potential memory leaks
	m.checkForMemoryLeaks()

	// Update statistics
	m.updateMemoryStats(&memStats)
}

// triggerOptimizedGC performs an optimized garbage collection cycle.
// Uses multiple GC passes for better memory reclamation.
func (m *MemoryOptimizer) triggerOptimizedGC() {
	startTime := time.Now()

	var beforeMemStats, afterMemStats runtime.MemStats
	runtime.ReadMemStats(&beforeMemStats)

	// Perform multiple GC passes for better memory reclamation
	runtime.GC()
	runtime.GC() // Second pass to clean up finalizers

	runtime.ReadMemStats(&afterMemStats)

	// Update statistics
	atomic.AddInt64(&m.gcTriggerCount, 1)

	gcDuration := time.Since(startTime)
	freedMemory := int64(beforeMemStats.Alloc - afterMemStats.Alloc)
	effectiveness := float64(freedMemory) / float64(beforeMemStats.Alloc) * 100.0

	m.mu.Lock()
	m.stats.GCTriggerCount = atomic.LoadInt64(&m.gcTriggerCount)
	m.stats.GCEffectiveness = effectiveness
	m.stats.LastGCDuration = gcDuration
	m.lastGCTime = time.Now()
	m.mu.Unlock()
}

// checkForMemoryLeaks detects potential memory leaks by monitoring object counts.
func (m *MemoryOptimizer) checkForMemoryLeaks() {
	activeParseResults := atomic.LoadInt64(&m.activeParseResults)
	activeSyntaxTrees := atomic.LoadInt64(&m.activeSyntaxTrees)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Detect suspicious object counts that might indicate leaks
	if activeParseResults > 1000 {
		m.stats.MemoryLeaksDetected++
	}

	if activeSyntaxTrees > 500 {
		m.stats.MemoryLeaksDetected++
	}
}

// updatePoolHitRate calculates and updates the pool hit rate statistics.
func (m *MemoryOptimizer) updatePoolHitRate() {
	hits := atomic.LoadInt64(&m.poolHits)
	misses := atomic.LoadInt64(&m.poolMisses)
	total := hits + misses

	if total > 0 {
		hitRate := float64(hits) / float64(total) * 100.0

		m.mu.Lock()
		m.poolHitRate = hitRate
		m.stats.PoolHitRate = hitRate
		m.mu.Unlock()
	}
}

// updateMemoryStats updates comprehensive memory statistics for monitoring.
func (m *MemoryOptimizer) updateMemoryStats(memStats *runtime.MemStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stats.TotalAllocatedBytes = int64(memStats.TotalAlloc)
	m.stats.CurrentUsageBytes = int64(memStats.Alloc)
	m.stats.PeakUsageBytes = atomic.LoadInt64(&m.peakMemoryUsage)
}

// cleanupSyntaxNode recursively cleans up a syntax node tree to prevent memory retention.
func (m *MemoryOptimizer) cleanupSyntaxNode(node *SyntaxNode) {
	if node == nil {
		return
	}

	// Recursively clean up children
	for _, child := range node.Children {
		m.cleanupSyntaxNode(child)
	}

	// Clear large text fields
	if len(node.Text) > 1024 { // 1KB threshold
		node.Text = ""
	}

	// Clear large children slices
	if cap(node.Children) > 64 {
		node.Children = nil
	}
}

// GetStats returns a copy of current memory optimization statistics.
func (m *MemoryOptimizer) GetStats() MemoryStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statsCopy := m.stats
	statsCopy.CurrentUsageBytes = atomic.LoadInt64(&m.currentMemoryUsage)
	statsCopy.PeakUsageBytes = atomic.LoadInt64(&m.peakMemoryUsage)
	statsCopy.GCTriggerCount = atomic.LoadInt64(&m.gcTriggerCount)

	return statsCopy
}

// SetConfiguration updates memory optimizer configuration.
func (m *MemoryOptimizer) SetConfiguration(config MemoryOptimizerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gcTriggerThreshold = config.GCTriggerThreshold
	m.maxMemoryUsage = config.MaxMemoryUsage
	m.enablePooling = config.EnablePooling
	m.enableGCOptimization = config.EnableGCOptimization
}

// MemoryOptimizerConfig contains configuration options for memory optimization.
type MemoryOptimizerConfig struct {
	GCTriggerThreshold   int64 // Memory threshold for triggering GC (bytes)
	MaxMemoryUsage       int64 // Maximum allowed memory usage (bytes)
	EnablePooling        bool  // Enable object pooling
	EnableGCOptimization bool  // Enable GC optimization
}

// DefaultMemoryOptimizerConfig returns sensible default configuration values.
func DefaultMemoryOptimizerConfig() MemoryOptimizerConfig {
	return MemoryOptimizerConfig{
		GCTriggerThreshold:   200 * 1024 * 1024,  // 200MB
		MaxMemoryUsage:       1024 * 1024 * 1024, // 1GB
		EnablePooling:        true,
		EnableGCOptimization: true,
	}
}

// OptimizedParserPool extends DefaultParserPool with memory optimization capabilities.
type OptimizedParserPool struct {
	*DefaultParserPool
	memoryOptimizer *MemoryOptimizer
	maxMemoryUsage  int64

	// Memory-aware parser recycling
	parserAge    map[TreeSitterParser]time.Time
	maxParserAge time.Duration
	mu           sync.RWMutex
}

// NewOptimizedParserPool creates a new parser pool with memory optimization.
func NewOptimizedParserPool(maxSize int, config *ParserConfig, optimizer *MemoryOptimizer) (*OptimizedParserPool, error) {
	basePool, err := NewParserPool(maxSize, config)
	if err != nil {
		return nil, err
	}

	if optimizer == nil {
		optimizer = NewMemoryOptimizer()
	}

	return &OptimizedParserPool{
		DefaultParserPool: basePool,
		memoryOptimizer:   optimizer,
		maxMemoryUsage:    1024 * 1024 * 1024, // 1GB default
		parserAge:         make(map[TreeSitterParser]time.Time),
		maxParserAge:      30 * time.Minute, // 30 minute max age
	}, nil
}

// AcquireParser gets a memory-optimized parser instance from the pool.
func (p *OptimizedParserPool) AcquireParser(ctx context.Context) (TreeSitterParser, error) {
	// Check memory pressure before acquiring
	p.memoryOptimizer.CheckMemoryPressure()

	parser, err := p.DefaultParserPool.AcquireParser(ctx)
	if err != nil {
		return nil, err
	}

	// Check parser age and replace if too old
	p.mu.Lock()
	if age, exists := p.parserAge[parser]; exists {
		if time.Since(age) > p.maxParserAge {
			// Release old parser and get a fresh one
			p.DefaultParserPool.ReleaseParser(parser)
			delete(p.parserAge, parser)
			parser.Close()

			// Create new parser
			newParser, err := NewPHPParser(nil)
			if err != nil {
				p.mu.Unlock()
				return nil, err
			}

			p.parserAge[newParser] = time.Now()
			parser = newParser
		}
	} else {
		p.parserAge[parser] = time.Now()
	}
	p.mu.Unlock()

	return parser, nil
}

// ReleaseParser returns a parser to the pool with memory optimization.
func (p *OptimizedParserPool) ReleaseParser(parser TreeSitterParser) error {
	// Trigger memory check after parser use
	p.memoryOptimizer.CheckMemoryPressure()

	return p.DefaultParserPool.ReleaseParser(parser)
}

// Close shuts down the optimized parser pool and cleans up memory tracking.
func (p *OptimizedParserPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear age tracking
	for parser := range p.parserAge {
		delete(p.parserAge, parser)
	}

	return p.DefaultParserPool.Close()
}
