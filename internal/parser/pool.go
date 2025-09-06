// Package parser provides thread-safe parser pool management for concurrent PHP parsing.
// Implements resource pooling pattern to efficiently reuse tree-sitter parser instances
// with proper lifecycle management and resource cleanup.
package parser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ParserPool manages thread-safe access to tree-sitter parser instances.
// Implements resource pooling to efficiently handle concurrent parsing operations
// while ensuring each goroutine gets its own parser instance for thread safety.
type DefaultParserPool struct {
	// parsers is a buffered channel holding available parser instances
	parsers chan *DefaultPHPParser

	// config is the configuration used for all parsers in the pool
	config *ParserConfig

	// maxSize is the maximum number of parser instances in the pool
	maxSize int

	// activeCount tracks the number of parsers currently in use (atomic)
	activeCount int64

	// totalCount tracks the total number of created parsers (atomic)
	totalCount int64

	// mu protects pool metadata and shutdown state
	mu sync.RWMutex

	// closed indicates if the pool has been shut down
	closed bool

	// metrics tracks pool performance and usage statistics
	metrics *PoolMetrics
}

// PoolMetrics tracks parser pool performance and resource usage.
type PoolMetrics struct {
	// TotalAcquisitions is the total number of parser acquisitions
	TotalAcquisitions int64

	// TotalReleases is the total number of parser releases
	TotalReleases int64

	// WaitTime tracks cumulative time spent waiting for parsers
	TotalWaitTime time.Duration

	// FailedAcquisitions tracks acquisition failures
	FailedAcquisitions int64

	// CreatedParsers is the total number of parsers created
	CreatedParsers int64

	// DestroyedParsers is the total number of parsers destroyed
	DestroyedParsers int64

	// LastAcquisitionTime tracks when last parser was acquired
	LastAcquisitionTime time.Time

	// PeakActiveCount is the maximum number of active parsers observed
	PeakActiveCount int64
}

// NewParserPool creates a new parser pool with the specified maximum size.
// All parsers in the pool will use the provided configuration.
// Returns error if pool cannot be initialized or parsers cannot be created.
func NewParserPool(maxSize int, config *ParserConfig) (*DefaultParserPool, error) {
	if maxSize <= 0 {
		return nil, errors.New("pool size must be positive")
	}

	if config == nil {
		config = DefaultParserConfig()
	}

	// Validate configuration
	if err := ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid parser config: %w", err)
	}

	pool := &DefaultParserPool{
		parsers: make(chan *DefaultPHPParser, maxSize),
		config:  config,
		maxSize: maxSize,
		closed:  false,
		metrics: &PoolMetrics{},
	}

	// Pre-populate pool with parser instances for immediate availability
	for i := 0; i < maxSize; i++ {
		parser, err := NewPHPParser(config)
		if err != nil {
			// Clean up any parsers we've already created
			pool.Close()
			return nil, fmt.Errorf("failed to create parser %d: %w", i, err)
		}

		pool.parsers <- parser
		atomic.AddInt64(&pool.totalCount, 1)
		atomic.AddInt64(&pool.metrics.CreatedParsers, 1)
	}

	return pool, nil
}

// AcquireParser gets a parser instance from the pool.
// Blocks if no parsers are available until timeout or context cancellation.
// The caller is responsible for calling ReleaseParser when done.
func (p *DefaultParserPool) AcquireParser(ctx context.Context) (TreeSitterParser, error) {
	startTime := time.Now()

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, errors.New("parser pool is closed")
	}
	p.mu.RUnlock()

	// Try to acquire parser with context cancellation support
	select {
	case parser := <-p.parsers:
		// Successfully acquired parser
		activeCount := atomic.AddInt64(&p.activeCount, 1)
		atomic.AddInt64(&p.metrics.TotalAcquisitions, 1)

		// Update peak active count
		for {
			peak := atomic.LoadInt64(&p.metrics.PeakActiveCount)
			if activeCount <= peak || atomic.CompareAndSwapInt64(&p.metrics.PeakActiveCount, peak, activeCount) {
				break
			}
		}

		// Update metrics
		waitTime := time.Since(startTime)
		p.mu.Lock()
		p.metrics.TotalWaitTime += waitTime
		p.metrics.LastAcquisitionTime = time.Now()
		p.mu.Unlock()

		return parser, nil

	case <-ctx.Done():
		// Context was cancelled or timed out
		atomic.AddInt64(&p.metrics.FailedAcquisitions, 1)
		return nil, fmt.Errorf("failed to acquire parser: %w", ctx.Err())
	}
}

// ReleaseParser returns a parser instance to the pool.
// The parser should not be used after being released.
// Returns error if parser cannot be returned or pool is closed.
func (p *DefaultParserPool) ReleaseParser(parser TreeSitterParser) error {
	if parser == nil {
		return errors.New("cannot release nil parser")
	}

	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()

	if closed {
		// Pool is closed, clean up the parser
		parser.Close()
		atomic.AddInt64(&p.metrics.DestroyedParsers, 1)
		return errors.New("parser pool is closed")
	}

	// Verify parser is still functional before returning to pool
	if !parser.IsInitialized() {
		// Parser is not usable, create a replacement
		newParser, err := NewPHPParser(p.config)
		if err != nil {
			// Can't create replacement, reduce pool size
			atomic.AddInt64(&p.activeCount, -1)
			atomic.AddInt64(&p.metrics.DestroyedParsers, 1)
			return fmt.Errorf("parser invalid and replacement failed: %w", err)
		}

		// Replace broken parser
		parser.Close()
		parser = newParser
		atomic.AddInt64(&p.metrics.CreatedParsers, 1)
		atomic.AddInt64(&p.metrics.DestroyedParsers, 1)
	}

	// Try to return parser to pool (non-blocking)
	select {
	case p.parsers <- parser.(*DefaultPHPParser):
		// Successfully returned to pool
		atomic.AddInt64(&p.activeCount, -1)
		atomic.AddInt64(&p.metrics.TotalReleases, 1)
		return nil

	default:
		// Pool is full (should not happen with proper usage)
		parser.Close()
		atomic.AddInt64(&p.activeCount, -1)
		atomic.AddInt64(&p.metrics.DestroyedParsers, 1)
		return errors.New("parser pool is full, parser destroyed")
	}
}

// Size returns the current pool size (total parser instances).
func (p *DefaultParserPool) Size() int {
	return p.maxSize
}

// ActiveCount returns the number of parsers currently in use.
func (p *DefaultParserPool) ActiveCount() int {
	return int(atomic.LoadInt64(&p.activeCount))
}

// AvailableCount returns the number of parsers currently available in the pool.
func (p *DefaultParserPool) AvailableCount() int {
	return len(p.parsers)
}

// GetMetrics returns a copy of the current pool metrics.
// The returned metrics are a snapshot and will not be updated.
func (p *DefaultParserPool) GetMetrics() PoolMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Return copy to prevent concurrent access
	metricsCopy := *p.metrics
	metricsCopy.CreatedParsers = atomic.LoadInt64(&p.metrics.CreatedParsers)
	metricsCopy.DestroyedParsers = atomic.LoadInt64(&p.metrics.DestroyedParsers)
	metricsCopy.TotalAcquisitions = atomic.LoadInt64(&p.metrics.TotalAcquisitions)
	metricsCopy.TotalReleases = atomic.LoadInt64(&p.metrics.TotalReleases)
	metricsCopy.FailedAcquisitions = atomic.LoadInt64(&p.metrics.FailedAcquisitions)
	metricsCopy.PeakActiveCount = atomic.LoadInt64(&p.metrics.PeakActiveCount)

	return metricsCopy
}

// Resize dynamically adjusts the pool size.
// If increasing size, new parsers are created immediately.
// If decreasing size, excess parsers are removed when they become available.
func (p *DefaultParserPool) Resize(newSize int) error {
	if newSize <= 0 {
		return errors.New("pool size must be positive")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return errors.New("cannot resize closed pool")
	}

	currentSize := p.maxSize
	if newSize == currentSize {
		return nil // No change needed
	}

	if newSize > currentSize {
		// Increasing pool size - create new channel first, then add parsers

		// Create new channel with larger capacity
		newChannel := make(chan *DefaultPHPParser, newSize)

		// Transfer existing parsers to new channel
		oldChannel := p.parsers
		existingParsers := make([]*DefaultPHPParser, 0, currentSize)

		// Drain existing channel
		close(oldChannel)
		for parser := range oldChannel {
			existingParsers = append(existingParsers, parser)
		}

		// Add existing parsers to new channel
		for _, parser := range existingParsers {
			newChannel <- parser
		}

		// Create additional parser instances
		additionalParsers := newSize - currentSize
		for i := 0; i < additionalParsers; i++ {
			parser, err := NewPHPParser(p.config)
			if err != nil {
				// Clean up new channel
				close(newChannel)
				for parser := range newChannel {
					parser.Close()
				}
				return fmt.Errorf("failed to create additional parser %d: %w", i, err)
			}

			newChannel <- parser
			atomic.AddInt64(&p.totalCount, 1)
			atomic.AddInt64(&p.metrics.CreatedParsers, 1)
		}

		p.parsers = newChannel

	} else {
		// Decreasing pool size - remove excess parsers
		excessParsers := currentSize - newSize

		// Create new channel with smaller capacity
		newChannel := make(chan *DefaultPHPParser, newSize)

		// Transfer limited number of parsers to new channel
		close(p.parsers)
		transferred := 0
		for parser := range p.parsers {
			if transferred < newSize {
				newChannel <- parser
				transferred++
			} else {
				// Destroy excess parser
				parser.Close()
				atomic.AddInt64(&p.totalCount, -1)
				atomic.AddInt64(&p.metrics.DestroyedParsers, 1)
				excessParsers--
				if excessParsers <= 0 {
					break
				}
			}
		}

		p.parsers = newChannel
	}

	p.maxSize = newSize
	return nil
}

// Close shuts down the pool and releases all parser resources.
// All active parsers should be returned before calling Close.
// After Close is called, no new parsers can be acquired.
func (p *DefaultParserPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil // Already closed
	}

	p.closed = true

	// Close channel and clean up all remaining parsers
	if p.parsers != nil {
		close(p.parsers)
		for parser := range p.parsers {
			parser.Close()
			atomic.AddInt64(&p.metrics.DestroyedParsers, 1)
		}
	}

	return nil
}

// IsHealthy returns true if the pool is functioning properly.
// Checks for common pool health indicators like resource leaks.
func (p *DefaultParserPool) IsHealthy() (bool, []string) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return false, []string{"pool is closed"}
	}

	var issues []string

	// Check for resource leaks
	activeCount := atomic.LoadInt64(&p.activeCount)
	totalAcquisitions := atomic.LoadInt64(&p.metrics.TotalAcquisitions)
	totalReleases := atomic.LoadInt64(&p.metrics.TotalReleases)

	if totalAcquisitions-totalReleases != activeCount {
		issues = append(issues, fmt.Sprintf("resource leak detected: acquisitions=%d, releases=%d, active=%d",
			totalAcquisitions, totalReleases, activeCount))
	}

	// Check pool utilization
	if activeCount > int64(p.maxSize) {
		issues = append(issues, fmt.Sprintf("active count %d exceeds pool size %d", activeCount, p.maxSize))
	}

	// Check for excessive failed acquisitions
	failedAcquisitions := atomic.LoadInt64(&p.metrics.FailedAcquisitions)
	if totalAcquisitions > 0 {
		failureRate := float64(failedAcquisitions) / float64(totalAcquisitions)
		if failureRate > 0.1 { // More than 10% failures
			issues = append(issues, fmt.Sprintf("high failure rate: %.2f%% acquisitions failed", failureRate*100))
		}
	}

	return len(issues) == 0, issues
}

// GetPoolStatus returns detailed status information about the pool.
// Useful for monitoring and debugging pool behavior.
func (p *DefaultParserPool) GetPoolStatus() PoolStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	metrics := p.GetMetrics()

	return PoolStatus{
		MaxSize:         p.maxSize,
		ActiveCount:     int(atomic.LoadInt64(&p.activeCount)),
		AvailableCount:  len(p.parsers),
		TotalCount:      int(atomic.LoadInt64(&p.totalCount)),
		Closed:          p.closed,
		Metrics:         metrics,
		LastAcquisition: metrics.LastAcquisitionTime,
	}
}

// PoolStatus contains comprehensive pool status information.
type PoolStatus struct {
	MaxSize         int         // Maximum pool size
	ActiveCount     int         // Currently active parsers
	AvailableCount  int         // Currently available parsers
	TotalCount      int         // Total parsers ever created
	Closed          bool        // Whether pool is closed
	Metrics         PoolMetrics // Detailed metrics
	LastAcquisition time.Time   // Last parser acquisition time
}
