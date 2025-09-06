//go:build goexperiment.jsonv2

// Package stats provides thread-safe statistics collection for the Oxinfer pipeline.
// It tracks processing metrics across all phases including indexing, parsing, matching, and inference.
package stats

import (
	"encoding/json/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ProcessingStats contains comprehensive metrics about the entire processing pipeline.
// All fields are designed for atomic operations and deterministic JSON output.
type ProcessingStats struct {
	// Core processing metrics
	FilesParsed  int64 `json:"filesParsed"`
	FilesSkipped int64 `json:"skipped"`
	DurationMs   int64 `json:"durationMs"`
	Partial      int32 `json:"partial"` // 0 or 1, treated as bool in JSON
	TotalFiles   int64 `json:"totalFiles,omitempty"`

	// Phase-specific timing metrics (stored as atomic int64, marshaled as map[string]int64)
	phaseStatsMu sync.RWMutex
	phaseStats   map[string]*int64 `json:"-"`

	// Pattern matching statistics (stored as atomic int64, marshaled as map[string]int)
	matchStatsMu sync.RWMutex
	matchStats   map[string]*int64 `json:"-"`

	// Error tracking
	ErrorCount int64 `json:"errorCount,omitempty"`

	// Processing timestamps
	StartTime int64 `json:"startTime,omitempty"` // Unix timestamp in milliseconds
	EndTime   int64 `json:"endTime,omitempty"`   // Unix timestamp in milliseconds

	// Inference-specific metrics
	InferenceOps       int64 `json:"inferenceOps,omitempty"` // Number of shape inference operations
	PropertiesInferred int64 `json:"propertiesInferred,omitempty"`

	// Cache statistics
	CacheHits   int64 `json:"cacheHits,omitempty"`
	CacheMisses int64 `json:"cacheMisses,omitempty"`
}

// PhaseStats represents processing time for different pipeline phases.
// Keys are phase names, values are duration in milliseconds.
type PhaseStats map[string]int64

// MatchStats represents pattern matching results by pattern type.
// Keys are pattern type names, values are match counts.
type MatchStats map[string]int

// MarshalJSON provides deterministic JSON encoding with sorted keys.
func (p *ProcessingStats) MarshalJSON() ([]byte, error) {
	// Create a struct for JSON marshaling with deterministic phase and match stats
	type jsonStats struct {
		FilesParsed        int64      `json:"filesParsed"`
		FilesSkipped       int64      `json:"skipped"`
		DurationMs         int64      `json:"durationMs"`
		Partial            bool       `json:"partial"`
		TotalFiles         int64      `json:"totalFiles,omitempty"`
		PhaseStats         PhaseStats `json:"phaseStats,omitempty"`
		MatchStats         MatchStats `json:"matchStats,omitempty"`
		ErrorCount         int64      `json:"errorCount,omitempty"`
		StartTime          int64      `json:"startTime,omitempty"`
		EndTime            int64      `json:"endTime,omitempty"`
		InferenceOps       int64      `json:"inferenceOps,omitempty"`
		PropertiesInferred int64      `json:"propertiesInferred,omitempty"`
		CacheHits          int64      `json:"cacheHits,omitempty"`
		CacheMisses        int64      `json:"cacheMisses,omitempty"`
	}

	js := jsonStats{
		FilesParsed:        atomic.LoadInt64(&p.FilesParsed),
		FilesSkipped:       atomic.LoadInt64(&p.FilesSkipped),
		DurationMs:         atomic.LoadInt64(&p.DurationMs),
		Partial:            atomic.LoadInt32(&p.Partial) == 1,
		TotalFiles:         atomic.LoadInt64(&p.TotalFiles),
		ErrorCount:         atomic.LoadInt64(&p.ErrorCount),
		StartTime:          atomic.LoadInt64(&p.StartTime),
		EndTime:            atomic.LoadInt64(&p.EndTime),
		InferenceOps:       atomic.LoadInt64(&p.InferenceOps),
		PropertiesInferred: atomic.LoadInt64(&p.PropertiesInferred),
		CacheHits:          atomic.LoadInt64(&p.CacheHits),
		CacheMisses:        atomic.LoadInt64(&p.CacheMisses),
	}

	// Convert phase stats with deterministic ordering
	if phaseStats := p.GetPhaseStatsTyped(); len(phaseStats) > 0 {
		js.PhaseStats = make(PhaseStats, len(phaseStats))
		keys := make([]string, 0, len(phaseStats))
		for phase := range phaseStats {
			keys = append(keys, phase)
		}
		sort.Strings(keys)

		for _, phase := range keys {
			if value := phaseStats[phase]; value > 0 {
				js.PhaseStats[phase] = value
			}
		}

		// Don't include empty phase stats
		if len(js.PhaseStats) == 0 {
			js.PhaseStats = nil
		}
	}

	// Convert match stats with deterministic ordering
	if matchStats := p.GetMatchStatsTyped(); len(matchStats) > 0 {
		js.MatchStats = make(MatchStats, len(matchStats))
		keys := make([]string, 0, len(matchStats))
		for matchType := range matchStats {
			keys = append(keys, matchType)
		}
		sort.Strings(keys)

		for _, matchType := range keys {
			if value := matchStats[matchType]; value > 0 {
				js.MatchStats[matchType] = value
			}
		}

		// Don't include empty match stats
		if len(js.MatchStats) == 0 {
			js.MatchStats = nil
		}
	}

	return json.Marshal(js, json.Deterministic(true))
}

// PhaseType represents the different phases of the processing pipeline.
type PhaseType string

const (
	PhaseIndexing  PhaseType = "indexing"
	PhaseParsing   PhaseType = "parsing"
	PhaseMatching  PhaseType = "matching"
	PhaseInference PhaseType = "inference"
	PhaseEmission  PhaseType = "emission"
	PhaseTotal     PhaseType = "total"
)

// MatchType represents the different types of patterns that can be matched.
type MatchType string

const (
	MatchTypeHTTPStatus   MatchType = "http_status"
	MatchTypeRequestUsage MatchType = "request_usage"
	MatchTypeResource     MatchType = "resource"
	MatchTypePivot        MatchType = "pivot"
	MatchTypeAttribute    MatchType = "attribute"
	MatchTypeScope        MatchType = "scope"
	MatchTypePolymorphic  MatchType = "polymorphic"
	MatchTypeBroadcast    MatchType = "broadcast"
)

// InferenceOpType represents the different types of shape inference operations.
type InferenceOpType string

const (
	InferenceOpConsolidation InferenceOpType = "consolidation"
	InferenceOpMerging       InferenceOpType = "merging"
	InferenceOpValidation    InferenceOpType = "validation"
	InferenceOpTransform     InferenceOpType = "transform"
)

// ErrorType represents the different categories of errors that can occur.
type ErrorType string

const (
	ErrorTypeParsing   ErrorType = "parsing"
	ErrorTypeMatching  ErrorType = "matching"
	ErrorTypeInference ErrorType = "inference"
	ErrorTypeEmission  ErrorType = "emission"
	ErrorTypeIO        ErrorType = "io"
	ErrorTypeLimit     ErrorType = "limit"
)

// NewProcessingStats creates a new ProcessingStats instance with initialized maps.
func NewProcessingStats() *ProcessingStats {
	return &ProcessingStats{
		phaseStats: make(map[string]*int64),
		matchStats: make(map[string]*int64),
		StartTime:  time.Now().UnixMilli(),
	}
}

// GetPhaseStats returns a snapshot of phase statistics.
// The returned map is safe for concurrent reading.
func (p *ProcessingStats) GetPhaseStats() any {
	p.phaseStatsMu.RLock()
	defer p.phaseStatsMu.RUnlock()

	result := make(map[string]int64, len(p.phaseStats))

	// Create sorted list of phase names for deterministic output
	phases := make([]string, 0, len(p.phaseStats))
	for phase := range p.phaseStats {
		phases = append(phases, phase)
	}
	sort.Strings(phases)

	for _, phase := range phases {
		if counter := p.phaseStats[phase]; counter != nil {
			result[phase] = atomic.LoadInt64(counter)
		}
	}

	return result
}

// GetPhaseStatsTyped returns a snapshot of phase statistics as PhaseStats type.
// This method is for internal use where type safety is needed.
func (p *ProcessingStats) GetPhaseStatsTyped() PhaseStats {
	if statsInterface := p.GetPhaseStats(); statsInterface != nil {
		if stats, ok := statsInterface.(map[string]int64); ok {
			// Convert to PhaseStats type alias
			result := make(PhaseStats, len(stats))
			for k, v := range stats {
				result[k] = v
			}
			return result
		}
	}
	return make(PhaseStats)
}

// GetMatchStats returns a snapshot of match statistics.
// The returned map is safe for concurrent reading.
func (p *ProcessingStats) GetMatchStats() any {
	p.matchStatsMu.RLock()
	defer p.matchStatsMu.RUnlock()

	result := make(map[string]int, len(p.matchStats))

	// Create sorted list of match types for deterministic output
	matchTypes := make([]string, 0, len(p.matchStats))
	for matchType := range p.matchStats {
		matchTypes = append(matchTypes, matchType)
	}
	sort.Strings(matchTypes)

	for _, matchType := range matchTypes {
		if counter := p.matchStats[matchType]; counter != nil {
			result[matchType] = int(atomic.LoadInt64(counter))
		}
	}

	return result
}

// GetMatchStatsTyped returns a snapshot of match statistics as MatchStats type.
// This method is for internal use where type safety is needed.
func (p *ProcessingStats) GetMatchStatsTyped() MatchStats {
	if statsInterface := p.GetMatchStats(); statsInterface != nil {
		if stats, ok := statsInterface.(map[string]int); ok {
			// Convert to MatchStats type alias
			result := make(MatchStats, len(stats))
			for k, v := range stats {
				result[k] = v
			}
			return result
		}
	}
	return make(MatchStats)
}

// IsPartial returns whether the processing was partial (incomplete due to limits).
func (p *ProcessingStats) IsPartial() bool {
	return atomic.LoadInt32(&p.Partial) == 1
}

// GetProcessingTime returns the total processing duration in milliseconds.
func (p *ProcessingStats) GetProcessingTime() int64 {
	return atomic.LoadInt64(&p.DurationMs)
}

// GetFilesProcessed returns the total number of files that were successfully processed.
func (p *ProcessingStats) GetFilesProcessed() int64 {
	return atomic.LoadInt64(&p.FilesParsed)
}

// GetFilesSkipped returns the number of files that were skipped during processing.
func (p *ProcessingStats) GetFilesSkipped() int64 {
	return atomic.LoadInt64(&p.FilesSkipped)
}

// GetErrorCount returns the total number of errors encountered during processing.
func (p *ProcessingStats) GetErrorCount() int64 {
	return atomic.LoadInt64(&p.ErrorCount)
}

// GetTotalFiles returns the total number of files discovered before processing.
func (p *ProcessingStats) GetTotalFiles() int64 {
	return atomic.LoadInt64(&p.TotalFiles)
}

// GetStartTime returns the processing start timestamp.
func (p *ProcessingStats) GetStartTime() int64 {
	return atomic.LoadInt64(&p.StartTime)
}

// GetEndTime returns the processing end timestamp.
func (p *ProcessingStats) GetEndTime() int64 {
	return atomic.LoadInt64(&p.EndTime)
}

// GetInferenceOps returns the total number of inference operations performed.
func (p *ProcessingStats) GetInferenceOps() int64 {
	return atomic.LoadInt64(&p.InferenceOps)
}

// GetPropertiesInferred returns the total number of properties inferred.
func (p *ProcessingStats) GetPropertiesInferred() int64 {
	return atomic.LoadInt64(&p.PropertiesInferred)
}

// GetCacheHits returns the number of cache hits.
func (p *ProcessingStats) GetCacheHits() int64 {
	return atomic.LoadInt64(&p.CacheHits)
}

// GetCacheMisses returns the number of cache misses.
func (p *ProcessingStats) GetCacheMisses() int64 {
	return atomic.LoadInt64(&p.CacheMisses)
}

// initializePhaseCounter ensures a phase counter exists and is initialized to zero.
func (p *ProcessingStats) initializePhaseCounter(phase string) *int64 {
	p.phaseStatsMu.RLock()
	if counter, exists := p.phaseStats[phase]; exists {
		p.phaseStatsMu.RUnlock()
		return counter
	}
	p.phaseStatsMu.RUnlock()

	// Upgrade to write lock to create new counter
	p.phaseStatsMu.Lock()
	defer p.phaseStatsMu.Unlock()

	// Check again in case another goroutine created it
	if counter, exists := p.phaseStats[phase]; exists {
		return counter
	}

	// Create new counter initialized to 0
	counter := new(int64)
	p.phaseStats[phase] = counter
	return counter
}

// initializeMatchCounter ensures a match counter exists and is initialized to zero.
func (p *ProcessingStats) initializeMatchCounter(matchType string) *int64 {
	p.matchStatsMu.RLock()
	if counter, exists := p.matchStats[matchType]; exists {
		p.matchStatsMu.RUnlock()
		return counter
	}
	p.matchStatsMu.RUnlock()

	// Upgrade to write lock to create new counter
	p.matchStatsMu.Lock()
	defer p.matchStatsMu.Unlock()

	// Check again in case another goroutine created it
	if counter, exists := p.matchStats[matchType]; exists {
		return counter
	}

	// Create new counter initialized to 0
	counter := new(int64)
	p.matchStats[matchType] = counter
	return counter
}
