// Package parser provides safe concurrent PHP parsing using tree-sitter with resource pooling.
// Implements thread-safe concurrent parsing operations while managing parser instances
// and integrating with file indexer patterns and manifest configuration.
package parser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/garaekz/oxinfer/internal/indexer"
)

// ConcurrentPHPParser provides thread-safe concurrent PHP parsing capabilities.
// Uses parser pooling to safely handle multiple parsing operations simultaneously
// while respecting manifest configuration limits and resource constraints.
type DefaultConcurrentPHPParser struct {
	// pool manages thread-safe access to parser instances
	pool ParserPool
	
	// config contains parsing configuration and limits
	config *ConcurrentParsingConfig
	
	// maxWorkers limits the number of concurrent parsing operations
	maxWorkers int
	
	// stats tracks concurrent parsing performance and resource usage
	stats *ConcurrentStats
	
	// mu protects stats and configuration changes
	mu sync.RWMutex
	
	// closed indicates if the concurrent parser has been shut down
	closed bool
}

// ConcurrentParsingConfig contains configuration for concurrent parsing operations.
type ConcurrentParsingConfig struct {
	// MaxWorkers is the maximum number of concurrent parsing workers
	MaxWorkers int
	
	// WorkerTimeout is the maximum time a worker can spend on a single job
	WorkerTimeout time.Duration
	
	// QueueSize is the maximum number of queued parsing jobs
	QueueSize int
	
	// EnableProfiling enables detailed performance profiling
	EnableProfiling bool
	
	// ResultBufferSize is the size of the result channel buffer
	ResultBufferSize int
	
	// EnableErrorRecovery allows workers to continue after parse errors
	EnableErrorRecovery bool
}

// ConcurrentStats tracks concurrent parsing performance and resource usage.
type ConcurrentStats struct {
	// ParsedFiles is the total number of files parsed concurrently
	ParsedFiles int64
	
	// FailedFiles is the total number of files that failed to parse
	FailedFiles int64
	
	// ActiveWorkers is the current number of active parsing workers
	ActiveWorkers int32
	
	// TotalParseTime is the cumulative time spent parsing across all workers
	TotalParseTime time.Duration
	
	// PeakParallelism is the maximum number of workers active simultaneously
	PeakParallelism int32
	
	// QueuedJobs is the current number of jobs waiting for processing
	QueuedJobs int32
	
	// TotalJobsProcessed is the total number of parsing jobs completed
	TotalJobsProcessed int64
	
	// AverageParseTime is the average time per file across all workers
	AverageParseTime time.Duration
	
	// ErrorRate is the percentage of files that failed to parse
	ErrorRate float64
	
	// ThroughputPerSecond is the average files processed per second
	ThroughputPerSecond float64
	
	// MemoryUsage is the estimated memory usage in bytes
	MemoryUsage int64
	
	// LastUpdateTime is when stats were last updated
	LastUpdateTime time.Time
}

// ParseJob represents a single PHP parsing task for concurrent processing.
// Extended from types.go with additional concurrent parsing metadata.
type ConcurrentParseJob struct {
	ParseJob                    // Embed base ParseJob from types.go
	
	// WorkerID will be assigned when job is picked up by a worker
	WorkerID string
	
	// Retries tracks number of retry attempts for this job
	Retries int
	
	// MaxRetries is the maximum number of retry attempts allowed
	MaxRetries int
	
	// Context for cancellation and timeout handling
	Context context.Context
	
	// ResultChannel for sending results back to caller
	ResultChannel chan<- ParseJobResult
}

// ParseJobResult represents the result of a concurrent parsing operation.
// Extended from types.go with concurrent-specific metadata.
type ConcurrentParseJobResult struct {
	ParseJobResult              // Embed base ParseJobResult from types.go
	
	// Timestamp when result was generated
	Timestamp time.Time
	
	// Retries is the number of retry attempts made for this job
	Retries int
	
	// QueueTime is the time spent waiting in queue before processing
	QueueTime time.Duration
	
	// TotalTime includes queue time + processing time
	TotalTime time.Duration
}

// DefaultConcurrentParsingConfig returns sensible defaults for concurrent parsing.
func DefaultConcurrentParsingConfig() *ConcurrentParsingConfig {
	return &ConcurrentParsingConfig{
		MaxWorkers:          4,
		WorkerTimeout:       30 * time.Second,
		QueueSize:          1000,
		EnableProfiling:     false,
		ResultBufferSize:   100,
		EnableErrorRecovery: true,
	}
}

// NewConcurrentPHPParser creates a new concurrent PHP parser with the specified configuration.
// Integrates with manifest MaxWorkers limits and creates underlying parser pool.
func NewConcurrentPHPParser(maxWorkers int, parserConfig *ParserConfig) (*DefaultConcurrentPHPParser, error) {
	if maxWorkers <= 0 {
		return nil, errors.New("maxWorkers must be positive")
	}
	
	if parserConfig == nil {
		parserConfig = DefaultParserConfig()
	}
	
	// Create parser pool with same size as max workers for optimal resource usage
	pool, err := NewParserPool(maxWorkers, parserConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create parser pool: %w", err)
	}
	
	concurrent := &DefaultConcurrentPHPParser{
		pool:       pool,
		config:     DefaultConcurrentParsingConfig(),
		maxWorkers: maxWorkers,
		stats:      &ConcurrentStats{},
		closed:     false,
	}
	
	// Update config with actual max workers
	concurrent.config.MaxWorkers = maxWorkers
	
	return concurrent, nil
}

// NewConcurrentPHPParserFromManifest creates a concurrent parser using manifest configuration.
// Respects manifest MaxWorkers limits and integrates with configuration system.
func NewConcurrentPHPParserFromManifest(maxWorkers *int, parserConfig *ParserConfig) (*DefaultConcurrentPHPParser, error) {
	// Use manifest MaxWorkers if provided, otherwise default to 4
	workers := 4
	if maxWorkers != nil && *maxWorkers > 0 {
		workers = *maxWorkers
	}
	
	return NewConcurrentPHPParser(workers, parserConfig)
}

// ParseConcurrently processes multiple PHP files concurrently and returns results via channel.
// Implements the ConcurrentPHPParser interface with full error handling and resource management.
func (p *DefaultConcurrentPHPParser) ParseConcurrently(ctx context.Context, files []ParseJob) (<-chan ParseJobResult, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, errors.New("concurrent parser is closed")
	}
	p.mu.RUnlock()
	
	if len(files) == 0 {
		// Return closed channel for empty input
		resultChan := make(chan ParseJobResult)
		close(resultChan)
		return resultChan, nil
	}
	
	// Create result channel with buffer to prevent blocking
	resultChan := make(chan ParseJobResult, p.config.ResultBufferSize)
	
	// Create job queue channel
	jobChan := make(chan ConcurrentParseJob, len(files))
	
	// Convert ParseJob to ConcurrentParseJob and enqueue
	for i, job := range files {
		if job.ID == "" {
			job.ID = fmt.Sprintf("job-%d", i)
		}
		
		concurrentJob := ConcurrentParseJob{
			ParseJob:      job,
			WorkerID:      "",
			Retries:       0,
			MaxRetries:    3,
			Context:       ctx,
			ResultChannel: resultChan,
		}
		
		select {
		case jobChan <- concurrentJob:
			atomic.AddInt32(&p.stats.QueuedJobs, 1)
		case <-ctx.Done():
			close(resultChan)
			return resultChan, ctx.Err()
		}
	}
	close(jobChan)
	
	// Start worker goroutines
	var wg sync.WaitGroup
	
	for i := 0; i < p.maxWorkers; i++ {
		wg.Add(1)
		workerID := fmt.Sprintf("worker-%d", i)
		
		go func(workerID string) {
			defer wg.Done()
			p.parseWorker(ctx, workerID, jobChan, resultChan)
		}(workerID)
	}
	
	// Close result channel when all workers complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()
	
	return resultChan, nil
}

// parseWorker implements the worker goroutine that processes parsing jobs.
// Each worker gets its own parser instance from the pool for thread safety.
func (p *DefaultConcurrentPHPParser) parseWorker(ctx context.Context, workerID string, jobs <-chan ConcurrentParseJob, results chan<- ParseJobResult) {
	// Track worker lifecycle
	activeWorkers := atomic.AddInt32(&p.stats.ActiveWorkers, 1)
	defer atomic.AddInt32(&p.stats.ActiveWorkers, -1)
	
	// Update peak parallelism
	for {
		peak := atomic.LoadInt32(&p.stats.PeakParallelism)
		if activeWorkers <= peak || atomic.CompareAndSwapInt32(&p.stats.PeakParallelism, peak, activeWorkers) {
			break
		}
	}
	
	for {
		select {
		case job, ok := <-jobs:
			if !ok {
				return // No more jobs
			}
			
			// Process job with timeout and error recovery
			result := p.processJob(ctx, workerID, job)
			
			// Send result back
			select {
			case results <- result:
				// Successfully sent result
			case <-ctx.Done():
				return // Context cancelled
			}
			
			// Update job queue counter
			atomic.AddInt32(&p.stats.QueuedJobs, -1)
			
		case <-ctx.Done():
			return // Context cancelled
		}
	}
}

// processJob handles the actual parsing of a single PHP file.
// Manages parser acquisition, error handling, retries, and resource cleanup.
func (p *DefaultConcurrentPHPParser) processJob(ctx context.Context, workerID string, job ConcurrentParseJob) ParseJobResult {
	startTime := time.Now()
	jobID := job.ID
	
	// Create job-specific context with timeout
	jobCtx, cancel := context.WithTimeout(ctx, p.config.WorkerTimeout)
	defer cancel()
	
	// Acquire parser from pool
	parser, err := p.pool.AcquireParser(jobCtx)
	if err != nil {
		return p.createErrorResult(job, fmt.Errorf("failed to acquire parser: %w", err), workerID, time.Since(startTime))
	}
	
	// Ensure parser is released back to pool
	defer func() {
		if releaseErr := p.pool.ReleaseParser(parser); releaseErr != nil {
			// Log error but don't fail the job
			fmt.Printf("Warning: failed to release parser for job %s: %v\n", jobID, releaseErr)
		}
	}()
	
	// Parse the PHP content
	var parseResult *SyntaxTree
	var parseErr error
	
	if len(job.Content) > 0 {
		// Parse from provided content
		parseResult, parseErr = parser.ParseContent(job.Content)
	} else if job.FilePath != "" {
		// Parse from file path
		parseResult, parseErr = parser.ParseFile(jobCtx, job.FilePath)
	} else {
		parseErr = errors.New("no content or file path provided")
	}
	
	duration := time.Since(startTime)
	
	// Create result
	if parseErr != nil {
		// Update statistics for parsing failure
		p.updateConcurrentStats(false, duration)
		return p.createErrorResult(job, parseErr, workerID, duration)
	}
	
	// Calculate tree statistics
	treeSize := p.calculateTreeSize(parseResult.Root)
	maxDepth := p.calculateMaxDepth(parseResult.Root)
	nodeTypes := p.calculateNodeTypes(parseResult.Root)
	
	// Check for syntax errors in the parsed tree
	hasErrors, syntaxErrors := p.extractSyntaxErrorsFromTree(parseResult.Root, job.Content)
	
	// If there are syntax errors, treat as parse failure
	if hasErrors {
		errorMsg := fmt.Sprintf("syntax errors detected in parsed content: %d errors", len(syntaxErrors))
		// Update statistics for syntax error failure
		p.updateConcurrentStats(false, duration)
		return p.createErrorResult(job, errors.New(errorMsg), workerID, duration)
	}
	
	// Update statistics for successful parse
	p.updateConcurrentStats(true, duration)
	
	// Wrap SyntaxTree result in our ParseResult format
	result := &ParseResult{
		Tree:      nil, // Tree-sitter tree is handled internally by SyntaxTree
		RootNode:  nil, // Raw tree-sitter node is handled internally
		FilePath:  job.FilePath,
		Content:   job.Content,
		HasErrors: hasErrors,
		Errors:    syntaxErrors,
		ParsedAt:  time.Now(),
		Stats: ParseStats{
			ParseTime:   duration,
			TreeSize:    treeSize,
			ContentSize: int64(len(job.Content)),
			ErrorCount:  len(syntaxErrors),
			MaxDepth:    maxDepth,
			NodeTypes:   nodeTypes,
		},
	}
	
	return ParseJobResult{
		JobID:     jobID,
		Result:    result,
		Error:     nil,
		Duration:  duration,
		WorkerID:  workerID,
		CacheHit:  false,
	}
}

// createErrorResult creates a standardized error result for failed parsing jobs.
func (p *DefaultConcurrentPHPParser) createErrorResult(job ConcurrentParseJob, err error, workerID string, duration time.Duration) ParseJobResult {
	return ParseJobResult{
		JobID:     job.ID,
		Result:    nil,
		Error:     err,
		Duration:  duration,
		WorkerID:  workerID,
		CacheHit:  false,
	}
}

// updateConcurrentStats updates concurrent parsing statistics atomically.
func (p *DefaultConcurrentPHPParser) updateConcurrentStats(success bool, duration time.Duration) {
	atomic.AddInt64(&p.stats.TotalJobsProcessed, 1)
	
	if success {
		atomic.AddInt64(&p.stats.ParsedFiles, 1)
	} else {
		atomic.AddInt64(&p.stats.FailedFiles, 1)
	}
	
	// Update timing statistics (requires mutex for duration arithmetic)
	p.mu.Lock()
	p.stats.TotalParseTime += duration
	p.stats.LastUpdateTime = time.Now()
	
	// Calculate derived statistics
	totalJobs := atomic.LoadInt64(&p.stats.TotalJobsProcessed)
	if totalJobs > 0 {
		p.stats.AverageParseTime = p.stats.TotalParseTime / time.Duration(totalJobs)
		
		failedFiles := atomic.LoadInt64(&p.stats.FailedFiles)
		p.stats.ErrorRate = float64(failedFiles) / float64(totalJobs) * 100.0
		
		// Calculate throughput (files per second)
		elapsedSeconds := p.stats.TotalParseTime.Seconds()
		if elapsedSeconds > 0 {
			p.stats.ThroughputPerSecond = float64(totalJobs) / elapsedSeconds
		}
	}
	p.mu.Unlock()
}

// SetMaxWorkers updates the maximum number of concurrent workers.
// Implements the ConcurrentPHPParser interface requirement.
func (p *DefaultConcurrentPHPParser) SetMaxWorkers(maxWorkers int) error {
	if maxWorkers <= 0 {
		return errors.New("maxWorkers must be positive")
	}
	
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return errors.New("cannot set max workers on closed parser")
	}
	
	// Update configuration
	p.maxWorkers = maxWorkers
	p.config.MaxWorkers = maxWorkers
	
	// Resize parser pool to match new worker count
	if poolWithResize, ok := p.pool.(*DefaultParserPool); ok {
		if err := poolWithResize.Resize(maxWorkers); err != nil {
			return fmt.Errorf("failed to resize parser pool: %w", err)
		}
	}
	
	return nil
}

// GetActiveWorkers returns the current number of active parsing workers.
// Implements the ConcurrentPHPParser interface requirement.
func (p *DefaultConcurrentPHPParser) GetActiveWorkers() int {
	return int(atomic.LoadInt32(&p.stats.ActiveWorkers))
}

// GetStats returns a copy of current concurrent parsing statistics.
// Provides insights into parsing performance and resource utilization.
func (p *DefaultConcurrentPHPParser) GetStats() ConcurrentStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	// Create copy with atomic values
	statsCopy := *p.stats
	statsCopy.ParsedFiles = atomic.LoadInt64(&p.stats.ParsedFiles)
	statsCopy.FailedFiles = atomic.LoadInt64(&p.stats.FailedFiles)
	statsCopy.ActiveWorkers = atomic.LoadInt32(&p.stats.ActiveWorkers)
	statsCopy.PeakParallelism = atomic.LoadInt32(&p.stats.PeakParallelism)
	statsCopy.QueuedJobs = atomic.LoadInt32(&p.stats.QueuedJobs)
	statsCopy.TotalJobsProcessed = atomic.LoadInt64(&p.stats.TotalJobsProcessed)
	
	return statsCopy
}

// Shutdown gracefully shuts down the concurrent parser and releases all resources.
// Implements the ConcurrentPHPParser interface requirement.
func (p *DefaultConcurrentPHPParser) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if p.closed {
		return nil // Already closed
	}
	
	p.closed = true
	
	// Wait for active workers to finish or timeout
	timeout := 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	// Poll until all workers are idle or timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	shutdownLoop:
	for {
		select {
		case <-shutdownCtx.Done():
			// Timeout reached, force shutdown
			break shutdownLoop
		case <-ticker.C:
			if atomic.LoadInt32(&p.stats.ActiveWorkers) == 0 {
				// All workers are idle
				break shutdownLoop
			}
		}
	}
	
	// Close parser pool
	if err := p.pool.Close(); err != nil {
		return fmt.Errorf("failed to close parser pool: %w", err)
	}
	
	return nil
}

// IsHealthy returns whether the concurrent parser is functioning properly.
// Checks pool health and worker status for potential issues.
func (p *DefaultConcurrentPHPParser) IsHealthy() (bool, []string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if p.closed {
		return false, []string{"concurrent parser is closed"}
	}
	
	var issues []string
	
	// Check pool health
	if poolWithHealth, ok := p.pool.(*DefaultParserPool); ok {
		healthy, poolIssues := poolWithHealth.IsHealthy()
		if !healthy {
			issues = append(issues, poolIssues...)
		}
	}
	
	// Check for stuck workers
	activeWorkers := atomic.LoadInt32(&p.stats.ActiveWorkers)
	if activeWorkers > int32(p.maxWorkers) {
		issues = append(issues, fmt.Sprintf("more active workers (%d) than max workers (%d)", activeWorkers, p.maxWorkers))
	}
	
	// Check error rate
	stats := p.GetStats()
	if stats.TotalJobsProcessed > 0 && stats.ErrorRate > 50.0 {
		issues = append(issues, fmt.Sprintf("high error rate: %.1f%%", stats.ErrorRate))
	}
	
	return len(issues) == 0, issues
}

// ProcessFilesBatch provides a higher-level interface for processing batches of files.
// Integrates with file indexer for seamless concurrent processing.
func (p *DefaultConcurrentPHPParser) ProcessFilesBatch(ctx context.Context, files []indexer.FileInfo) (<-chan ParseJobResult, error) {
	// Convert FileInfo to ParseJob
	jobs := make([]ParseJob, len(files))
	for i, file := range files {
		jobs[i] = ParseJob{
			ID:          fmt.Sprintf("file-%d", i),
			FilePath:    file.Path,
			Content:     nil, // Will be loaded by parser
			Priority:    1,
			Config:      nil, // Use default config
			SubmittedAt: time.Now(),
			Deadline:    time.Now().Add(p.config.WorkerTimeout),
		}
	}
	
	return p.ParseConcurrently(ctx, jobs)
}

// calculateTreeSize recursively counts the total number of nodes in a syntax tree.
func (p *DefaultConcurrentPHPParser) calculateTreeSize(node *SyntaxNode) int {
	if node == nil {
		return 0
	}
	
	size := 1 // Count this node
	for _, child := range node.Children {
		size += p.calculateTreeSize(child)
	}
	
	return size
}

// calculateMaxDepth recursively calculates the maximum depth of a syntax tree.
func (p *DefaultConcurrentPHPParser) calculateMaxDepth(node *SyntaxNode) int {
	if node == nil {
		return 0
	}
	
	maxChildDepth := 0
	for _, child := range node.Children {
		childDepth := p.calculateMaxDepth(child)
		if childDepth > maxChildDepth {
			maxChildDepth = childDepth
		}
	}
	
	return maxChildDepth + 1
}

// calculateNodeTypes recursively counts occurrences of each node type in a syntax tree.
func (p *DefaultConcurrentPHPParser) calculateNodeTypes(node *SyntaxNode) map[string]int {
	nodeTypes := make(map[string]int)
	p.collectNodeTypes(node, nodeTypes)
	return nodeTypes
}

// collectNodeTypes is a helper function for calculateNodeTypes that performs the actual counting.
func (p *DefaultConcurrentPHPParser) collectNodeTypes(node *SyntaxNode, nodeTypes map[string]int) {
	if node == nil {
		return
	}
	
	nodeTypes[node.Type]++
	
	for _, child := range node.Children {
		p.collectNodeTypes(child, nodeTypes)
	}
}

// extractSyntaxErrorsFromTree checks for error nodes in the parsed tree.
// Returns true and error details if syntax errors are found.
func (p *DefaultConcurrentPHPParser) extractSyntaxErrorsFromTree(node *SyntaxNode, content []byte) (bool, []ParseError) {
	var errors []ParseError
	hasErrors := false
	
	p.walkSyntaxTree(node, func(n *SyntaxNode) {
		if n.Type == "ERROR" {
			hasErrors = true
			
			// Create parse error for this error node
			parseError := ParseError{
				Type:         "syntax_error",
				Message:      "Syntax error in parsed content",
				Line:         n.StartPoint.Row + 1, // Convert to 1-indexed
				Column:       n.StartPoint.Column + 1,
				StartByte:    uint32(n.StartByte),
				EndByte:      uint32(n.EndByte),
				NodeType:     n.Type,
				ActualText:   extractTextFromContent(content, int(n.StartByte), int(n.EndByte)),
			}
			
			errors = append(errors, parseError)
		}
	})
	
	return hasErrors, errors
}

// walkSyntaxTree performs a depth-first traversal of the syntax tree.
func (p *DefaultConcurrentPHPParser) walkSyntaxTree(node *SyntaxNode, visitor func(*SyntaxNode)) {
	if node == nil {
		return
	}
	
	visitor(node)
	
	for _, child := range node.Children {
		p.walkSyntaxTree(child, visitor)
	}
}

// extractTextFromContent safely extracts text from content between byte positions.
func extractTextFromContent(content []byte, start, end int) string {
	if start < 0 || end > len(content) || start >= end {
		return ""
	}
	return string(content[start:end])
}