// Package indexer provides file discovery, enumeration, and caching for PHP project analysis.
// This package implements concurrent file indexing with configurable worker pools,
// intelligent caching strategies, and limit enforcement for large projects.
package indexer

import (
	"context"
	"time"
)

// FileDiscoverer finds and enumerates PHP files from target directories using glob patterns.
// It provides deterministic file ordering and efficient filtering for PHP files only.
type FileDiscoverer interface {
	// DiscoverFiles enumerates PHP files from target directories using glob patterns.
	// Returns files in deterministic order (sorted by path) for consistent results.
	DiscoverFiles(ctx context.Context, targets []string, globs []string, baseDir string) ([]FileInfo, error)

	// ValidateTargets checks that all target directories exist and are accessible.
	// Returns descriptive errors for missing or inaccessible directories.
	ValidateTargets(targets []string, baseDir string) error

	// FilterPHPFiles filters a list of files to include only PHP files.
	// Uses efficient extension checking and excludes non-PHP files.
	FilterPHPFiles(files []FileInfo) []FileInfo
}

// FileCacher handles per-file caching operations with configurable validation modes.
// Supports both mtime-only and sha256+mtime validation strategies.
type FileCacher interface {
	// GetCacheEntry retrieves a cache entry for the specified file path.
	// Returns nil if no valid cache entry exists.
	GetCacheEntry(path string) (*CacheEntry, error)

	// SetCacheEntry stores a cache entry for the specified file path.
	// Updates both in-memory and persistent cache storage.
	SetCacheEntry(path string, entry *CacheEntry) error

	// InvalidateCache removes the cache entry for the specified file path.
	// Used when files are deleted or modified beyond cache validation.
	InvalidateCache(path string) error

	// CleanupCache removes stale cache entries for files that no longer exist.
	// Should be called periodically to prevent cache bloat.
	CleanupCache() error

	// GetCacheStats returns statistics about cache performance and usage.
	GetCacheStats() CacheStats
}

// WorkerPoolManager manages concurrent file processing with configurable worker limits.
// Provides work distribution, result collection, and graceful shutdown capabilities.
type WorkerPoolManager interface {
	// ProcessFiles distributes file processing across a pool of workers.
	// Respects maxWorkers limit and provides context cancellation support.
	ProcessFiles(ctx context.Context, files []FileInfo, maxWorkers int, processor FileProcessor) error

	// GetActiveWorkers returns the current number of active workers.
	// Used for monitoring and debugging worker pool utilization.
	GetActiveWorkers() int

	// Shutdown gracefully terminates all workers and cleans up resources.
	// Waits for active work to complete within the provided context timeout.
	Shutdown(ctx context.Context) error
}

// FileProcessor processes individual files and returns processing results.
// This interface allows different processing strategies to be plugged into the worker pool.
type FileProcessor interface {
	// ProcessFile processes a single file and returns the result.
	// Implementation should handle all file-specific processing logic.
	ProcessFile(ctx context.Context, file FileInfo) (*ProcessResult, error)
}

// FileIndexer orchestrates the complete file indexing system.
// Combines file discovery, caching, and worker pool management for efficient indexing.
type FileIndexer interface {
	// IndexFiles performs complete file indexing with the provided configuration.
	// Returns comprehensive results including statistics and partial flags.
	IndexFiles(ctx context.Context, config IndexConfig) (*IndexResult, error)

	// RefreshIndex invalidates caches and re-indexes all files.
	// Used when cache corruption is suspected or forced refresh is needed.
	RefreshIndex(ctx context.Context) error

	// GetIndexStats returns current indexing statistics and performance metrics.
	GetIndexStats() IndexStats
}

// FileInfo represents metadata about a discovered file.
type FileInfo struct {
	// Path is the relative path from the base directory
	Path string

	// AbsPath is the absolute filesystem path
	AbsPath string

	// ModTime is the file's last modification time
	ModTime time.Time

	// Size is the file size in bytes
	Size int64

	// IsDirectory indicates if this is a directory (should be false for PHP files)
	IsDirectory bool
}

// CacheEntry represents a cached file entry with validation metadata.
type CacheEntry struct {
    // Path is the file path this cache entry represents
    Path string

    // Hash is the SHA256 hash when kind=sha256+mtime, empty when kind=mtime
    Hash string

    // Size is the file size in bytes when cached
    Size int64

    // ModTime is the file modification time when cached
    ModTime time.Time

    // ProcessedAt is when this entry was last processed
    ProcessedAt time.Time

	// Valid indicates if this cache entry is still valid
	Valid bool
}

// ProcessResult represents the result of processing a single file.
type ProcessResult struct {
	// File is the processed file information
	File FileInfo

	// Cached indicates if this file was served from cache
	Cached bool

	// Error contains any processing error that occurred
	Error error

	// DurationMs is the processing duration in milliseconds
	DurationMs int64
}

// IndexConfig contains configuration for the file indexing operation.
type IndexConfig struct {
	// Targets are the base directories to scan for PHP files
	Targets []string

	// Globs are the glob patterns to match files (e.g., "**/*.php")
	Globs []string

	// BaseDir is the base directory for relative path resolution
	BaseDir string

	// MaxWorkers is the maximum number of concurrent workers
	MaxWorkers int

	// MaxFiles is the maximum number of files to process
	MaxFiles int

	// CacheEnabled indicates if caching should be used
	CacheEnabled bool

    // CacheKind specifies the cache validation mode ("sha256+mtime" or "mtime")
    CacheKind string

    // VendorWhitelist lists allowed vendor subpaths (relative to project root)
    VendorWhitelist []string
}

// IndexResult contains the complete results of a file indexing operation.
type IndexResult struct {
	// Files are the successfully indexed files
	Files []FileInfo

	// Cached is the number of files served from cache
	Cached int

	// Fresh is the number of files processed fresh (not cached)
	Fresh int

	// Skipped is the number of files skipped due to errors
	Skipped int

	// DurationMs is the total indexing duration in milliseconds
	DurationMs int64

	// Partial indicates if indexing was incomplete due to limits
	Partial bool

	// TotalFiles is the total number of files discovered before limits
	TotalFiles int
}

// CacheStats provides statistics about cache performance and usage.
type CacheStats struct {
	// TotalEntries is the total number of cache entries
	TotalEntries int

	// ValidEntries is the number of valid cache entries
	ValidEntries int

	// HitRate is the cache hit rate as a percentage (0-100)
	HitRate float64

	// MemoryUsage is the estimated memory usage in bytes
	MemoryUsage int64

	// LastCleanup is the timestamp of the last cache cleanup
	LastCleanup time.Time
}

// IndexStats provides statistics about indexing performance and behavior.
type IndexStats struct {
	// TotalRuns is the total number of indexing runs
	TotalRuns int

	// AverageFiles is the average number of files processed per run
	AverageFiles int

	// AverageDuration is the average indexing duration in milliseconds
	AverageDuration int64

	// CacheStats provides cache-related statistics
	CacheStats CacheStats

	// LastRun is the timestamp of the last indexing run
	LastRun time.Time
}
