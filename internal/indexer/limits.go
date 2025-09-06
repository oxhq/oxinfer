package indexer

import (
	"fmt"
	"sort"
)

// LimitsEnforcer enforces manifest limits with graceful degradation
type LimitsEnforcer struct {
	maxFiles   int      // Maximum files to process
	maxWorkers int      // Maximum concurrent workers
	maxDepth   int      // Maximum directory traversal depth
	enforced   bool     // Whether any limits were enforced
	partial    bool     // Whether results are partial due to limits
	truncated  []string // Descriptions of what was truncated
}

// LimitStats contains statistics about limit enforcement
type LimitStats struct {
	MaxFiles   int      // Configured max files limit
	MaxWorkers int      // Configured max workers limit
	MaxDepth   int      // Configured max depth limit
	Enforced   bool     // Whether limits were hit
	Partial    bool     // Whether results are partial
	Truncated  []string // Descriptions of truncations
}

// NewLimitsEnforcer creates a new limits enforcer with the specified limits
func NewLimitsEnforcer(maxFiles, maxWorkers, maxDepth int) *LimitsEnforcer {
	return &LimitsEnforcer{
		maxFiles:   maxFiles,
		maxWorkers: maxWorkers,
		maxDepth:   maxDepth,
		truncated:  []string{},
	}
}

// EnforceFileLimit applies the MaxFiles limit to discovered files
// Returns the limited files and whether truncation occurred
func (e *LimitsEnforcer) EnforceFileLimit(files []FileInfo) ([]FileInfo, bool) {
	if e.maxFiles <= 0 || len(files) <= e.maxFiles {
		return files, false // No limit or limit not exceeded
	}

	// Sort files deterministically for consistent truncation
	sortedFiles := make([]FileInfo, len(files))
	copy(sortedFiles, files)
	sort.Slice(sortedFiles, func(i, j int) bool {
		return sortedFiles[i].Path < sortedFiles[j].Path
	})

	// Truncate to maxFiles
	truncated := sortedFiles[:e.maxFiles]

	// Record truncation details
	e.enforced = true
	e.partial = true
	e.truncated = append(e.truncated,
		fmt.Sprintf("maxFiles: %d files processed, %d files skipped",
			e.maxFiles, len(files)-e.maxFiles))

	return truncated, true
}

// EnforceWorkerLimit applies the MaxWorkers limit, ensuring at least 1 worker
// Returns the actual worker count to use
func (e *LimitsEnforcer) EnforceWorkerLimit(requestedWorkers int) int {
	if e.maxWorkers <= 0 {
		// No limit specified, use requested amount
		return requestedWorkers
	}

	if requestedWorkers <= e.maxWorkers {
		return requestedWorkers // Within limit
	}

	// Limit exceeded, enforce maximum
	e.enforced = true
	e.truncated = append(e.truncated,
		fmt.Sprintf("maxWorkers: limited to %d workers (requested %d)",
			e.maxWorkers, requestedWorkers))

	return e.maxWorkers
}

// ValidateDepthLimit checks if the configured max depth is reasonable
// This is used during configuration validation, not enforcement
func (e *LimitsEnforcer) ValidateDepthLimit() error {
	if e.maxDepth < 0 {
		return fmt.Errorf("maxDepth cannot be negative: %d", e.maxDepth)
	}

	if e.maxDepth > 100 {
		// Very deep traversals can cause performance issues
		e.truncated = append(e.truncated,
			fmt.Sprintf("maxDepth: %d is very deep, may impact performance", e.maxDepth))
	}

	return nil
}

// RecordDepthTruncation records when directories are skipped due to depth limits
// This is called by the FileDiscoverer when MaxDepth is exceeded
func (e *LimitsEnforcer) RecordDepthTruncation(truncatedPaths []string) {
	if len(truncatedPaths) > 0 {
		e.enforced = true
		e.partial = true
		e.truncated = append(e.truncated,
			fmt.Sprintf("maxDepth: %d directories skipped due to depth limit", len(truncatedPaths)))
	}
}

// GetStats returns current limit enforcement statistics
func (e *LimitsEnforcer) GetStats() LimitStats {
	return LimitStats{
		MaxFiles:   e.maxFiles,
		MaxWorkers: e.maxWorkers,
		MaxDepth:   e.maxDepth,
		Enforced:   e.enforced,
		Partial:    e.partial,
		Truncated:  append([]string{}, e.truncated...), // Return copy
	}
}

// Reset clears all enforcement state (but keeps limits configuration)
// Used when starting a new indexing operation
func (e *LimitsEnforcer) Reset() {
	e.enforced = false
	e.partial = false
	e.truncated = []string{}
}

// IsPartial returns true if any limits caused partial results
func (e *LimitsEnforcer) IsPartial() bool {
	return e.partial
}

// WasEnforced returns true if any limits were applied
func (e *LimitsEnforcer) WasEnforced() bool {
	return e.enforced
}

// GetTruncationReasons returns descriptions of what was truncated
func (e *LimitsEnforcer) GetTruncationReasons() []string {
	return append([]string{}, e.truncated...) // Return copy
}

// ApplyDefaultLimits applies sensible defaults for unspecified limits
func ApplyDefaultLimits(maxFiles, maxWorkers, maxDepth *int) (int, int, int) {
	// Default max files: 10,000 (reasonable for large projects)
	finalMaxFiles := 10000
	if maxFiles != nil && *maxFiles > 0 {
		finalMaxFiles = *maxFiles
	}

	// Default max workers: number of CPU cores
	finalMaxWorkers := 8 // Conservative default
	if maxWorkers != nil && *maxWorkers > 0 {
		finalMaxWorkers = *maxWorkers
	}

	// Default max depth: 20 levels (very deep but not infinite)
	finalMaxDepth := 20
	if maxDepth != nil && *maxDepth >= 0 {
		finalMaxDepth = *maxDepth
	}

	return finalMaxFiles, finalMaxWorkers, finalMaxDepth
}

// ValidateLimits checks that limit values are reasonable
func ValidateLimits(maxFiles, maxWorkers, maxDepth int) error {
	if maxFiles < 0 {
		return fmt.Errorf("maxFiles cannot be negative: %d", maxFiles)
	}

	if maxFiles > 100000 {
		return fmt.Errorf("maxFiles too large: %d (max: 100,000)", maxFiles)
	}

	if maxWorkers < 1 {
		return fmt.Errorf("maxWorkers must be at least 1: %d", maxWorkers)
	}

	if maxWorkers > 100 {
		return fmt.Errorf("maxWorkers too large: %d (max: 100)", maxWorkers)
	}

	if maxDepth < 0 {
		return fmt.Errorf("maxDepth cannot be negative: %d", maxDepth)
	}

	if maxDepth > 100 {
		return fmt.Errorf("maxDepth too large: %d (max: 100)", maxDepth)
	}

	return nil
}
