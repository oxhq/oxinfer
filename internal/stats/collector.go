// Package stats provides thread-safe statistics collection for the Oxinfer pipeline.
// It implements atomic operations for concurrent access and deterministic output for consistent results.
package stats

import (
	"sync/atomic"
	"time"
)

// StatsCollector provides thread-safe collection of processing metrics across all Oxinfer pipeline phases.
// All methods are safe for concurrent use by multiple goroutines.
type StatsCollector interface {
	// Core processing metrics
	RecordFilesProcessed(count int)
	RecordFilesSkipped(count int)
	RecordTotalFiles(count int)
	RecordProcessingTime(phase string, duration time.Duration)
	RecordError(phase string, err error)

	// Pattern matching statistics
	RecordMatch(matchType string, count int)

	// Shape inference statistics
	RecordInferenceOperation(operation string, count int)
	RecordPropertiesInferred(count int)

	// Cache statistics
	RecordCacheHit()
	RecordCacheMiss()

	// Processing state management
	SetPartialFlag(partial bool)
	MarkProcessingStart()
	MarkProcessingEnd()

	// Results access
	GetStats() *ProcessingStats
	Reset()
}

// DefaultStatsCollector implements the StatsCollector interface using atomic operations.
// It provides thread-safe statistics collection with deterministic output ordering.
type DefaultStatsCollector struct {
	stats *ProcessingStats
}

// NewStatsCollector creates a new DefaultStatsCollector instance.
func NewStatsCollector() StatsCollector {
	return &DefaultStatsCollector{
		stats: NewProcessingStats(),
	}
}

// RecordFilesProcessed atomically increments the count of files successfully processed.
func (c *DefaultStatsCollector) RecordFilesProcessed(count int) {
	if count > 0 {
		atomic.AddInt64(&c.stats.FilesParsed, int64(count))
	}
}

// RecordFilesSkipped atomically increments the count of files skipped during processing.
func (c *DefaultStatsCollector) RecordFilesSkipped(count int) {
	if count > 0 {
		atomic.AddInt64(&c.stats.FilesSkipped, int64(count))
	}
}

// RecordTotalFiles atomically sets the total number of files discovered before processing.
func (c *DefaultStatsCollector) RecordTotalFiles(count int) {
	if count > 0 {
		atomic.StoreInt64(&c.stats.TotalFiles, int64(count))
	}
}

// RecordProcessingTime atomically records the processing time for a specific phase.
// Multiple calls for the same phase will accumulate the duration.
func (c *DefaultStatsCollector) RecordProcessingTime(phase string, duration time.Duration) {
	if duration <= 0 {
		return
	}

	durationMs := duration.Nanoseconds() / int64(time.Millisecond)
	counter := c.stats.initializePhaseCounter(phase)
	atomic.AddInt64(counter, durationMs)

	// Also update total duration
	atomic.AddInt64(&c.stats.DurationMs, durationMs)
}

// RecordError atomically increments the error count and tracks the error by phase.
// The error parameter is used for categorization but not stored to avoid memory leaks.
func (c *DefaultStatsCollector) RecordError(phase string, err error) {
	if err == nil {
		return
	}

	atomic.AddInt64(&c.stats.ErrorCount, 1)

	// Track phase-specific error timing (increment by 1ms as a marker)
	if phase != "" {
		errorPhase := phase + "_errors"
		counter := c.stats.initializePhaseCounter(errorPhase)
		atomic.AddInt64(counter, 1)
	}
}

// RecordMatch atomically records pattern matching results by type.
func (c *DefaultStatsCollector) RecordMatch(matchType string, count int) {
	if count <= 0 || matchType == "" {
		return
	}

	counter := c.stats.initializeMatchCounter(matchType)
	atomic.AddInt64(counter, int64(count))
}

// RecordInferenceOperation atomically records shape inference operations by type.
func (c *DefaultStatsCollector) RecordInferenceOperation(operation string, count int) {
	if count <= 0 || operation == "" {
		return
	}

	atomic.AddInt64(&c.stats.InferenceOps, int64(count))

	// Track operation-specific metrics as phase stats
	if operation != "" {
		phaseKey := "inference_" + operation
		counter := c.stats.initializePhaseCounter(phaseKey)
		atomic.AddInt64(counter, int64(count))
	}
}

// RecordPropertiesInferred atomically records the number of properties inferred during shape analysis.
func (c *DefaultStatsCollector) RecordPropertiesInferred(count int) {
	if count > 0 {
		atomic.AddInt64(&c.stats.PropertiesInferred, int64(count))
	}
}

// RecordCacheHit atomically increments the cache hit counter.
func (c *DefaultStatsCollector) RecordCacheHit() {
	atomic.AddInt64(&c.stats.CacheHits, 1)
}

// RecordCacheMiss atomically increments the cache miss counter.
func (c *DefaultStatsCollector) RecordCacheMiss() {
	atomic.AddInt64(&c.stats.CacheMisses, 1)
}

// SetPartialFlag atomically sets whether the processing was partial (incomplete due to limits).
func (c *DefaultStatsCollector) SetPartialFlag(partial bool) {
	var value int32
	if partial {
		value = 1
	}
	atomic.StoreInt32(&c.stats.Partial, value)
}

// MarkProcessingStart atomically records the processing start time.
func (c *DefaultStatsCollector) MarkProcessingStart() {
	now := time.Now().UnixMilli()
	atomic.StoreInt64(&c.stats.StartTime, now)
}

// MarkProcessingEnd atomically records the processing end time.
func (c *DefaultStatsCollector) MarkProcessingEnd() {
	now := time.Now().UnixMilli()
	atomic.StoreInt64(&c.stats.EndTime, now)
}

// GetStats returns a copy of the current statistics.
// The returned ProcessingStats instance is safe for concurrent access.
func (c *DefaultStatsCollector) GetStats() *ProcessingStats {
	return c.stats
}

// Reset clears all statistics and reinitializes the collector.
// This method is NOT thread-safe and should only be called when no other operations are in progress.
func (c *DefaultStatsCollector) Reset() {
	c.stats = NewProcessingStats()
}

// RecordPhaseStart records the start of a processing phase.
// Returns a function that should be called when the phase completes to automatically record duration.
func (c *DefaultStatsCollector) RecordPhaseStart(phase string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		c.RecordProcessingTime(phase, duration)
	}
}

// BatchRecordMatches records multiple match results efficiently.
// The matches map contains match type -> count pairs.
func (c *DefaultStatsCollector) BatchRecordMatches(matches map[string]int) {
	// Sort match types for deterministic processing
	matchTypes := make([]string, 0, len(matches))
	for matchType := range matches {
		matchTypes = append(matchTypes, matchType)
	}

	// Simple sort implementation to avoid additional dependencies
	for i := 0; i < len(matchTypes); i++ {
		for j := i + 1; j < len(matchTypes); j++ {
			if matchTypes[i] > matchTypes[j] {
				matchTypes[i], matchTypes[j] = matchTypes[j], matchTypes[i]
			}
		}
	}

	// Process matches in deterministic order
	for _, matchType := range matchTypes {
		c.RecordMatch(matchType, matches[matchType])
	}
}

// RecordIndexingStats records comprehensive indexing statistics from an indexer result.
func (c *DefaultStatsCollector) RecordIndexingStats(filesProcessed, filesSkipped, totalFiles int, duration time.Duration, partial bool) {
	c.RecordFilesProcessed(filesProcessed)
	c.RecordFilesSkipped(filesSkipped)
	c.RecordTotalFiles(totalFiles)
	c.RecordProcessingTime(string(PhaseIndexing), duration)
	c.SetPartialFlag(partial)
}

// RecordParsingStats records parsing-specific statistics.
func (c *DefaultStatsCollector) RecordParsingStats(filesProcessed int, parseErrors int, duration time.Duration) {
	// Files processed is already recorded during indexing, so we track parsing success rate
	if parseErrors > 0 {
		c.RecordFilesSkipped(parseErrors)
		// Record errors under parsing phase
		for i := 0; i < parseErrors; i++ {
			c.RecordError(string(PhaseParsing), &ParseError{message: "parsing failed"})
		}
	}
	c.RecordProcessingTime(string(PhaseParsing), duration)
}

// RecordMatchingStats records pattern matching statistics from matcher results.
func (c *DefaultStatsCollector) RecordMatchingStats(matches map[string]int, duration time.Duration) {
	c.BatchRecordMatches(matches)
	c.RecordProcessingTime(string(PhaseMatching), duration)
}

// RecordInferenceStats records shape inference statistics.
func (c *DefaultStatsCollector) RecordInferenceStats(operations, properties int, duration time.Duration) {
	c.RecordInferenceOperation("total", operations)
	c.RecordPropertiesInferred(properties)
	c.RecordProcessingTime(string(PhaseInference), duration)
}

// ParseError represents a parsing error for statistics tracking.
type ParseError struct {
	message string
}

func (e *ParseError) Error() string {
	return e.message
}

// GetCacheStats returns cache hit and miss statistics.
func (c *DefaultStatsCollector) GetCacheStats() (hits, misses int64) {
	return atomic.LoadInt64(&c.stats.CacheHits), atomic.LoadInt64(&c.stats.CacheMisses)
}

// GetCacheHitRate returns the cache hit rate as a percentage (0-100).
// Returns 0 if no cache operations have been recorded.
func (c *DefaultStatsCollector) GetCacheHitRate() float64 {
	hits := atomic.LoadInt64(&c.stats.CacheHits)
	misses := atomic.LoadInt64(&c.stats.CacheMisses)
	total := hits + misses

	if total == 0 {
		return 0.0
	}

	return float64(hits) / float64(total) * 100.0
}

// GetTotalProcessingTime returns the total processing time including all phases.
func (c *DefaultStatsCollector) GetTotalProcessingTime() time.Duration {
	ms := atomic.LoadInt64(&c.stats.DurationMs)
	return time.Duration(ms) * time.Millisecond
}

// GetProcessingThroughput returns the number of files processed per second.
// Returns 0 if no processing time has been recorded.
func (c *DefaultStatsCollector) GetProcessingThroughput() float64 {
	files := atomic.LoadInt64(&c.stats.FilesParsed)
	durationMs := atomic.LoadInt64(&c.stats.DurationMs)

	if durationMs == 0 {
		return 0.0
	}

	durationSec := float64(durationMs) / 1000.0
	return float64(files) / durationSec
}

// GetErrorRate returns the error rate as a percentage of total files.
func (c *DefaultStatsCollector) GetErrorRate() float64 {
	errors := atomic.LoadInt64(&c.stats.ErrorCount)
	total := atomic.LoadInt64(&c.stats.TotalFiles)

	if total == 0 {
		return 0.0
	}

	return float64(errors) / float64(total) * 100.0
}
