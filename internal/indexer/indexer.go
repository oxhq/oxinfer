package indexer

import (
    "context"
    "fmt"
    "sort"
    "sync"
    "time"

    "github.com/garaekz/oxinfer/internal/manifest"
    "path/filepath"
    "strings"
    "os"
    "runtime"
    "crypto/sha256"
    "encoding/hex"
)

// DefaultFileIndexer implements the FileIndexer interface
type DefaultFileIndexer struct {
	discoverer     FileDiscoverer    // T3.1 component
	cacher         FileCacher        // T3.2 component  
	workerPool     WorkerPoolManager // T3.3 component
	limitsEnforcer *LimitsEnforcer   // Limits logic
	config         IndexConfig       // Current configuration
	progress       IndexProgress     // Progress tracking
	callback       func(IndexProgress) // Progress callback
	stats          IndexStats        // Runtime statistics
	mu             sync.RWMutex      // Thread safety
}

// IndexProgress tracks the progress of file indexing operations
type IndexProgress struct {
	Phase          string    // Current operation phase
	FilesFound     int       // Files discovered
	FilesProcessed int       // Files processed so far
	FilesCached    int       // Files served from cache
	FilesSkipped   int       // Files skipped due to errors
	Elapsed        time.Duration // Time elapsed since start
	Estimated      time.Duration // Estimated total time
	CurrentFile    string    // Currently processing file
}

// SimpleFileProcessor implements FileProcessor for basic file indexing
type SimpleFileProcessor struct {
	cacher FileCacher
}

// ProcessFile implements FileProcessor.ProcessFile
func (p *SimpleFileProcessor) ProcessFile(ctx context.Context, file FileInfo) (*ProcessResult, error) {
	start := time.Now()
	
	// Check cache first if cacher is available
	if p.cacher != nil {
		if entry, err := p.cacher.GetCacheEntry(file.Path); err == nil && entry != nil {
			return &ProcessResult{
				File:       file,
				Cached:     true,
				Error:      nil,
				DurationMs: time.Since(start).Milliseconds(),
			}, nil
		}
	}
	
	// Simulate processing (this will be replaced with PHP parsing)
	// For now, just validate the file exists and is accessible
	result := &ProcessResult{
		File:       file,
		Cached:     false,
		Error:      nil,
		DurationMs: time.Since(start).Milliseconds(),
	}
	
	// Update cache if available
	if p.cacher != nil {
		entry := &CacheEntry{
			Path:        file.Path,
			ModTime:     file.ModTime,
			ProcessedAt: time.Now(),
			Valid:       true,
		}
		// Ignore cache errors during processing
		_ = p.cacher.SetCacheEntry(file.Path, entry)
	}
	
	return result, nil
}

// NewDefaultFileIndexer creates a new DefaultFileIndexer instance
func NewDefaultFileIndexer() *DefaultFileIndexer {
	// Initialize with sensible defaults to prevent nil pointer dereferences
	maxFiles, maxWorkers, maxDepth := ApplyDefaultLimits(nil, nil, nil)
	
	return &DefaultFileIndexer{
		discoverer:      NewFileDiscoverer(),
		limitsEnforcer:  NewLimitsEnforcer(maxFiles, maxWorkers, maxDepth),
		workerPool:      NewWorkerPoolManager(),
		// cacher will be set during LoadFromManifest if cache is enabled
		stats: IndexStats{
			LastRun: time.Now(),
		},
	}
}

// LoadFromManifest configures the indexer from a manifest
func (dfi *DefaultFileIndexer) LoadFromManifest(manifest *manifest.Manifest) error {
	dfi.mu.Lock()
	defer dfi.mu.Unlock()
	
	if manifest == nil {
		return fmt.Errorf("manifest cannot be nil")
	}
	
	// Extract limits with defaults (handle nil Limits)
	var maxFilesPtr, maxWorkersPtr, maxDepthPtr *int
	if manifest.Limits != nil {
		maxFilesPtr = manifest.Limits.MaxFiles
		maxWorkersPtr = manifest.Limits.MaxWorkers
		maxDepthPtr = manifest.Limits.MaxDepth
	}
	
	maxFiles, maxWorkers, maxDepth := ApplyDefaultLimits(
		maxFilesPtr,
		maxWorkersPtr,
		maxDepthPtr,
	)
	
	// Validate limits
	if err := ValidateLimits(maxFiles, maxWorkers, maxDepth); err != nil {
		return fmt.Errorf("invalid limits: %w", err)
	}
	
	// Validate base directory exists
	if err := dfi.discoverer.ValidateTargets([]string{}, manifest.Project.Root); err != nil {
		return fmt.Errorf("invalid project root: %w", err)
	}
	
	// Configure indexer
dfi.config = IndexConfig{
    Targets:      manifest.Scan.Targets,
    Globs:        getGlobsWithDefaults(manifest.Scan.Globs),
    BaseDir:      manifest.Project.Root,
    MaxWorkers:   maxWorkers,
    MaxFiles:     maxFiles,
    CacheEnabled: isCacheEnabled(manifest.Cache),
    CacheKind:    getCacheKind(manifest.Cache),
    VendorWhitelist: manifest.Scan.VendorWhitelist,
}
	
	// Initialize limits enforcer
	dfi.limitsEnforcer = NewLimitsEnforcer(maxFiles, maxWorkers, maxDepth)
	
    // Initialize cache if enabled (persisted on disk with project key)
    if dfi.config.CacheEnabled {
        // Resolve cache directory precedence: env OXINFER_CACHE_DIR then default <project.root>/.oxinfer/cache/v1
        cacheDir := os.Getenv("OXINFER_CACHE_DIR")
        if cacheDir == "" {
            cacheDir = filepath.Join(manifest.Project.Root, ".oxinfer", "cache", "v1")
        } else {
            if abs, err := filepath.Abs(cacheDir); err == nil {
                cacheDir = abs
            }
        }

        // Compute project key based on project root and composer path
        composerPath := manifest.Project.Composer
        if composerPath == "" {
            composerPath = "composer.json"
        }
        projectKey, err := ComputeProjectKey(manifest.Project.Root, composerPath)
        if err != nil {
            // Fallback to hashing project root only when composer.json is unavailable in tests/fixtures
            hasher := sha256.New()
            absRoot, _ := filepath.Abs(manifest.Project.Root)
            hasher.Write([]byte(absRoot))
            h := hex.EncodeToString(hasher.Sum(nil))
            if len(h) > 16 {
                projectKey = h[:16]
            } else {
                projectKey = h
            }
        }

        cacher, err := NewFileCacheWithDir(manifest.Cache, cacheDir, projectKey)
        if err != nil {
            return fmt.Errorf("init file cache: %w", err)
        }
        dfi.cacher = cacher
    }
	
	// Initialize worker pool
	dfi.workerPool = NewWorkerPoolManager()
	
	return nil
}

// IndexFiles performs complete file indexing with the provided configuration
func (dfi *DefaultFileIndexer) IndexFiles(ctx context.Context, config IndexConfig) (*IndexResult, error) {
	start := time.Now()
	dfi.stats.LastRun = start
	
	// Reset state for new indexing run and initialize progress
	dfi.updateProgress("initializing", "")
	dfi.mu.Lock()
	if dfi.limitsEnforcer != nil {
		dfi.limitsEnforcer.Reset()
	}
	dfi.mu.Unlock()
	
	// Use provided config or fall back to loaded config
	indexConfig := config
	if indexConfig.BaseDir == "" {
		dfi.mu.RLock()
		indexConfig = dfi.config
		dfi.mu.RUnlock()
	}
	
	// Validate configuration
	if err := dfi.validateConfig(indexConfig); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	// Update progress to discovering phase
	dfi.updateProgress("discovering", "")
	
    // Phase 1: Discover files
    files, err := dfi.discoverer.DiscoverFiles(ctx, indexConfig.Targets, indexConfig.Globs, indexConfig.BaseDir)
    if err != nil {
        return nil, fmt.Errorf("file discovery failed: %w", err)
    }

    // Apply vendor whitelist filtering: denylist vendor/** except whitelisted paths
    files = dfi.filterVendorFiles(files)
	
	// Update progress with discovered files
	dfi.updateProgress("enforcing-limits", "")
	dfi.mu.Lock()
	dfi.progress.FilesFound = len(files)
	dfi.mu.Unlock()
	
	// Phase 2: Enforce file limits
	var limitedFiles []FileInfo
	var truncated bool
	if dfi.limitsEnforcer != nil {
		limitedFiles, truncated = dfi.limitsEnforcer.EnforceFileLimit(files)
	} else {
		limitedFiles = files
		truncated = false
	}
	
	// Phase 3: Process files with worker pool
	dfi.updateProgress("processing", "")
	
	processor := &SimpleFileProcessor{cacher: dfi.cacher}
	var workerCount int
	if dfi.limitsEnforcer != nil {
		workerCount = dfi.limitsEnforcer.EnforceWorkerLimit(indexConfig.MaxWorkers)
	} else {
		// Fallback to config value if limitsEnforcer is somehow nil
		workerCount = indexConfig.MaxWorkers
	}
	
	// Process files concurrently
	err = dfi.workerPool.ProcessFiles(ctx, limitedFiles, workerCount, processor)
	if err != nil {
		return nil, fmt.Errorf("file processing failed: %w", err)
	}
	
	// Phase 4: Collect results and statistics
	dfi.updateProgress("finalizing", "")
	
	result := &IndexResult{
		Files:       limitedFiles,
		Fresh:       len(limitedFiles), // All processed as fresh for now
		Cached:      0,                 // Will be updated when cache integration is complete
		Skipped:     0,                 // Will be updated when error handling is complete
		DurationMs:  time.Since(start).Milliseconds(),
		Partial:     truncated,
		TotalFiles:  len(files),
	}
	
	// Update runtime statistics
	dfi.updateStats(result)
	
	return result, nil
}

// filterVendorFiles enforces vendor whitelist rules: all vendor paths are denied unless
// explicitly whitelisted via IndexConfig.VendorWhitelist. Whitelist entries are treated
// as relative to BaseDir; if they don't start with "vendor/", that prefix is added.
func (dfi *DefaultFileIndexer) filterVendorFiles(files []FileInfo) []FileInfo {
    dfi.mu.RLock()
    baseDir := dfi.config.BaseDir
    whitelist := dfi.config.VendorWhitelist
    dfi.mu.RUnlock()

    if len(whitelist) == 0 {
        // No whitelist: denylist vendor entirely
        out := make([]FileInfo, 0, len(files))
        for _, f := range files {
            // Use forward slashes check for vendor segment
            p := filepath.ToSlash(f.Path)
            if !strings.Contains(p, "/vendor/") && !strings.HasPrefix(p, "vendor/") {
                out = append(out, f)
            }
        }
        return out
    }

    // Build absolute, normalized whitelist prefixes
    var prefixes []string
    for _, w := range whitelist {
        entry := w
        if entry == "" {
            continue
        }
        entry = filepath.ToSlash(entry)
        if !strings.HasPrefix(entry, "vendor/") && !strings.HasPrefix(entry, "vendor\\") {
            entry = filepath.ToSlash(filepath.Join("vendor", entry))
        }
        abs := filepath.Clean(filepath.Join(baseDir, entry))
        if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != "" {
            abs = resolved
        }
        prefixes = append(prefixes, filepath.ToSlash(abs))
    }

    out := make([]FileInfo, 0, len(files))
    for _, f := range files {
        rel := filepath.ToSlash(f.Path)
        if strings.Contains(rel, "/vendor/") || strings.HasPrefix(rel, "vendor/") {
            // Allow only if AbsPath has whitelisted prefix
            abs := filepath.ToSlash(f.AbsPath)
            if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != "" {
                abs = filepath.ToSlash(resolved)
            }
            // Use case-insensitive check on Windows
            allowed := false
            for _, pref := range prefixes {
                // Normalize both sides
                a := abs
                p := pref
                if runtime.GOOS == "windows" {
                    if strings.HasPrefix(strings.ToLower(a), strings.ToLower(p)) {
                        allowed = true
                        break
                    }
                    continue
                }
                if strings.HasPrefix(a, p) {
                    allowed = true
                    break
                }
            }
            if allowed {
                out = append(out, f)
            }
            continue
        }
        out = append(out, f)
    }

    return out
}

// RefreshIndex invalidates caches and re-indexes all files
func (dfi *DefaultFileIndexer) RefreshIndex(ctx context.Context) error {
	dfi.mu.RLock()
	cacher := dfi.cacher
	config := dfi.config
	dfi.mu.RUnlock()
	
	// Clear cache if available
	if cacher != nil {
		if err := cacher.CleanupCache(); err != nil {
			return fmt.Errorf("cache cleanup failed: %w", err)
		}
	}
	
	// Re-index with current configuration
	_, err := dfi.IndexFiles(ctx, config)
	return err
}

// GetIndexStats returns current indexing statistics and performance metrics
func (dfi *DefaultFileIndexer) GetIndexStats() IndexStats {
	dfi.mu.RLock()
	defer dfi.mu.RUnlock()
	
	stats := dfi.stats
	if dfi.cacher != nil {
		stats.CacheStats = dfi.cacher.GetCacheStats()
	}
	
	return stats
}

// SetProgressCallback sets the progress monitoring callback
func (dfi *DefaultFileIndexer) SetProgressCallback(callback func(IndexProgress)) {
	dfi.mu.Lock()
	defer dfi.mu.Unlock()
	dfi.callback = callback
}

// GetProgress returns current progress information
func (dfi *DefaultFileIndexer) GetProgress() IndexProgress {
	dfi.mu.RLock()
	defer dfi.mu.RUnlock()
	return dfi.progress
}

// validateConfig ensures the configuration is valid and complete
func (dfi *DefaultFileIndexer) validateConfig(config IndexConfig) error {
	if config.BaseDir == "" {
		return fmt.Errorf("BaseDir cannot be empty")
	}
	
	if len(config.Targets) == 0 {
		return fmt.Errorf("Targets cannot be empty")
	}
	
	if len(config.Globs) == 0 {
		return fmt.Errorf("Globs cannot be empty")
	}
	
	if config.MaxWorkers < 1 {
		return fmt.Errorf("MaxWorkers must be at least 1")
	}
	
	if config.MaxFiles < 0 {
		return fmt.Errorf("MaxFiles cannot be negative")
	}
	
	return nil
}

// updateProgress updates the current progress and calls the callback if set
func (dfi *DefaultFileIndexer) updateProgress(phase, currentFile string) {
	dfi.mu.Lock()
	dfi.progress.Phase = phase
	dfi.progress.CurrentFile = currentFile
	dfi.progress.Elapsed = time.Since(dfi.stats.LastRun)
	callback := dfi.callback
	progress := dfi.progress
	dfi.mu.Unlock()
	
	// Call callback outside of lock to avoid deadlock
	if callback != nil {
		callback(progress)
	}
}

// updateStats updates runtime statistics after indexing
func (dfi *DefaultFileIndexer) updateStats(result *IndexResult) {
	dfi.mu.Lock()
	defer dfi.mu.Unlock()
	
	dfi.stats.TotalRuns++
	dfi.stats.LastRun = time.Now()
	
	// Update averages
	if dfi.stats.TotalRuns == 1 {
		dfi.stats.AverageFiles = len(result.Files)
		dfi.stats.AverageDuration = result.DurationMs
	} else {
		// Simple moving average
		totalFiles := float64(dfi.stats.AverageFiles)*float64(dfi.stats.TotalRuns-1) + float64(len(result.Files))
		dfi.stats.AverageFiles = int(totalFiles / float64(dfi.stats.TotalRuns))
		
		totalDuration := float64(dfi.stats.AverageDuration)*float64(dfi.stats.TotalRuns-1) + float64(result.DurationMs)
		dfi.stats.AverageDuration = int64(totalDuration / float64(dfi.stats.TotalRuns))
	}
}

// Helper functions for manifest processing

// getGlobsWithDefaults returns globs with sensible defaults if none specified
func getGlobsWithDefaults(globs []string) []string {
	if len(globs) > 0 {
		return globs
	}
	return []string{"**/*.php"} // Default PHP glob pattern
}

// isCacheEnabled determines if caching is enabled from manifest config
func isCacheEnabled(config *manifest.CacheConfig) bool {
	if config != nil && config.Enabled != nil {
		return *config.Enabled
	}
	return true // Default to enabled
}

// getCacheKind determines cache validation mode from manifest config  
func getCacheKind(config *manifest.CacheConfig) string {
	if config != nil && config.Kind != nil {
		return *config.Kind
	}
	return CacheModeModTime // Default to mtime mode
}

// SortFilesByPath sorts files deterministically by path for consistent results
func SortFilesByPath(files []FileInfo) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
}
