// Package indexer provides comprehensive performance benchmarks for indexer validation.
// This file contains end-to-end benchmarks, component performance tests, and real-world
// Laravel project simulation to validate against performance targets.
package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// BenchmarkResult captures comprehensive performance metrics for analysis
type BenchmarkResult struct {
	Name              string        `json:"name"`
	FilesProcessed    int           `json:"files_processed"`
	Duration          time.Duration `json:"duration_ns"`
	FilesPerSecond    float64       `json:"files_per_second"`
	MemoryAllocated   int64         `json:"memory_allocated_bytes"`
	MemoryAllocations int64         `json:"memory_allocations"`
	CacheHitRate      float64       `json:"cache_hit_rate_percent"`
	WorkerUtilization float64       `json:"worker_utilization_percent"`
	Deterministic     bool          `json:"deterministic"`
}

// BenchmarkSuite manages benchmark execution and result collection
type BenchmarkSuite struct {
	tempDir string
}

// NewBenchmarkSuite creates a new benchmark suite with temporary test data
func NewBenchmarkSuite(t testing.TB) *BenchmarkSuite {
	tempDir := t.TempDir()
	suite := &BenchmarkSuite{
		tempDir: tempDir,
	}
	return suite
}

// createTestProject creates a test Laravel project structure with specified file count
func (bs *BenchmarkSuite) createTestProject(name string, fileCount int, complexity ProjectComplexity) string {
	projectDir := filepath.Join(bs.tempDir, name)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		panic(fmt.Sprintf("failed to create project directory: %v", err))
	}

	// Create Laravel-style directory structure
	dirs := []string{"app", "app/Http", "app/Http/Controllers", "app/Models", "routes", "database/migrations"}
	if complexity >= ComplexityMedium {
		dirs = append(dirs, "app/Services", "app/Repositories", "app/Events", "app/Listeners")
	}
	if complexity >= ComplexityLarge {
		dirs = append(dirs, "app/Jobs", "app/Mail", "app/Notifications", "app/Policies", "packages/custom/src")
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(projectDir, dir), 0755); err != nil {
			panic(fmt.Sprintf("failed to create directory %s: %v", dir, err))
		}
	}

	// Distribute files across directories
	filesPerDir := fileCount / len(dirs)
	remainder := fileCount % len(dirs)

	fileIndex := 0
	for i, dir := range dirs {
		count := filesPerDir
		if i < remainder {
			count++
		}

		for j := 0; j < count; j++ {
			filename := fmt.Sprintf("File%d.php", fileIndex)
			content := bs.generatePHPContent(filename, complexity)
			filePath := filepath.Join(projectDir, dir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				panic(fmt.Sprintf("failed to create file %s: %v", filePath, err))
			}
			fileIndex++
		}
	}

	return projectDir
}

// ProjectComplexity defines the complexity level of generated test projects
type ProjectComplexity int

const (
	ComplexitySmall ProjectComplexity = iota
	ComplexityMedium
	ComplexityLarge
)

// generatePHPContent creates PHP content of varying complexity based on the project type
func (bs *BenchmarkSuite) generatePHPContent(filename string, complexity ProjectComplexity) string {
	base := fmt.Sprintf(`<?php

namespace App\\Generated;

/**
 * Generated test file: %s
 * Complexity: %v
 * Generated at: %s
 */
class %s
{
`, filename, complexity, time.Now().Format(time.RFC3339), strings.TrimSuffix(filename, ".php"))

	switch complexity {
	case ComplexitySmall:
		base += `    public function simpleMethod()
    {
        return 'Hello World';
    }
`
	case ComplexityMedium:
		base += `    private $property;
    
    public function __construct($value)
    {
        $this->property = $value;
    }
    
    public function complexMethod($param1, $param2 = null)
    {
        if ($param2 === null) {
            return $this->property . $param1;
        }
        
        return array_merge($this->property, [$param1, $param2]);
    }
    
    protected function helperMethod()
    {
        return array_map('strtoupper', $this->property);
    }
`
	case ComplexityLarge:
		base += `    private $dependencies = [];
    private $cache = [];
    
    public function __construct(array $dependencies)
    {
        $this->dependencies = $dependencies;
    }
    
    public function process(array $data)
    {
        $hash = md5(serialize($data));
        if (isset($this->cache[$hash])) {
            return $this->cache[$hash];
        }
        
        $result = [];
        foreach ($data as $key => $value) {
            if (is_array($value)) {
                $result[$key] = $this->processNestedArray($value);
            } else {
                $result[$key] = $this->processValue($value);
            }
        }
        
        $this->cache[$hash] = $result;
        return $result;
    }
    
    private function processNestedArray(array $array)
    {
        return array_filter(array_map([$this, 'processValue'], $array));
    }
    
    private function processValue($value)
    {
        if (in_array('validator', $this->dependencies)) {
            return $this->validateValue($value);
        }
        return $value;
    }
    
    private function validateValue($value)
    {
        return is_string($value) ? trim($value) : $value;
    }
`
	}

	base += "}\n"
	return base
}

// ==============================================================================
// END-TO-END INDEXING BENCHMARKS
// ==============================================================================

// BenchmarkIndexing_SingleFile measures minimal overhead for single file processing
func BenchmarkIndexing_SingleFile(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("single", 1, ComplexitySmall)

	indexer := NewDefaultFileIndexer()
	manifest := createTestManifest(projectDir, 1, 4, true)

	if err := indexer.LoadFromManifest(manifest); err != nil {
		b.Fatalf("Failed to load manifest: %v", err)
	}

	ctx := context.Background()
	config := IndexConfig{
		Targets:      []string{"app"},
		Globs:        []string{"**/*.php"},
		BaseDir:      projectDir,
		MaxWorkers:   1,
		MaxFiles:     1,
		CacheEnabled: false,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := indexer.IndexFiles(ctx, config)
		if err != nil {
			b.Fatalf("Indexing failed: %v", err)
		}
		if len(result.Files) != 1 {
			b.Fatalf("Expected 1 file, got %d", len(result.Files))
		}
	}
}

// BenchmarkIndexing_SmallLaravel measures small Laravel project performance (50 files)
func BenchmarkIndexing_SmallLaravel(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("small-laravel", 50, ComplexitySmall)

	indexer := NewDefaultFileIndexer()
	manifest := createTestManifest(projectDir, 100, 4, true)

	if err := indexer.LoadFromManifest(manifest); err != nil {
		b.Fatalf("Failed to load manifest: %v", err)
	}

	ctx := context.Background()
	config := IndexConfig{
		Targets:      []string{"app", "routes"},
		Globs:        []string{"**/*.php"},
		BaseDir:      projectDir,
		MaxWorkers:   4,
		MaxFiles:     100,
		CacheEnabled: false, // Cold start measurement
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := indexer.IndexFiles(ctx, config)
		if err != nil {
			b.Fatalf("Indexing failed: %v", err)
		}
		if len(result.Files) == 0 {
			b.Fatalf("No files processed")
		}
		// Target: <100ms for 50 files
		if result.DurationMs > 100 && b.N == 1 {
			b.Logf("WARNING: Small Laravel took %dms (target: <100ms)", result.DurationMs)
		}
	}
}

// BenchmarkIndexing_MediumLaravel measures medium Laravel project performance (250 files)
func BenchmarkIndexing_MediumLaravel(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("medium-laravel", 250, ComplexityMedium)

	indexer := NewDefaultFileIndexer()
	manifest := createTestManifest(projectDir, 500, 8, true)

	if err := indexer.LoadFromManifest(manifest); err != nil {
		b.Fatalf("Failed to load manifest: %v", err)
	}

	ctx := context.Background()
	config := IndexConfig{
		Targets:      []string{"app", "routes", "database"},
		Globs:        []string{"**/*.php"},
		BaseDir:      projectDir,
		MaxWorkers:   8,
		MaxFiles:     500,
		CacheEnabled: false, // Cold start measurement
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := indexer.IndexFiles(ctx, config)
		if err != nil {
			b.Fatalf("Indexing failed: %v", err)
		}
		if len(result.Files) == 0 {
			b.Fatalf("No files processed")
		}
		// Target: <1s for 250 files
		if result.DurationMs > 1000 && b.N == 1 {
			b.Logf("WARNING: Medium Laravel took %dms (target: <1000ms)", result.DurationMs)
		}
	}
}

// BenchmarkIndexing_LargeLaravel measures large Laravel project performance (800 files)
func BenchmarkIndexing_LargeLaravel(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("large-laravel", 800, ComplexityLarge)

	indexer := NewDefaultFileIndexer()
	manifest := createTestManifest(projectDir, 1000, 12, true)

	if err := indexer.LoadFromManifest(manifest); err != nil {
		b.Fatalf("Failed to load manifest: %v", err)
	}

	ctx := context.Background()
	config := IndexConfig{
		Targets:      []string{"app", "routes", "database", "packages"},
		Globs:        []string{"**/*.php"},
		BaseDir:      projectDir,
		MaxWorkers:   12,
		MaxFiles:     1000,
		CacheEnabled: false, // Cold start measurement
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := indexer.IndexFiles(ctx, config)
		if err != nil {
			b.Fatalf("Indexing failed: %v", err)
		}
		if len(result.Files) == 0 {
			b.Fatalf("No files processed")
		}
		// Target: <5s for 800 files
		if result.DurationMs > 5000 && b.N == 1 {
			b.Logf("WARNING: Large Laravel took %dms (target: <5000ms)", result.DurationMs)
		}
	}
}

// ==============================================================================
// CACHE PERFORMANCE BENCHMARKS
// ==============================================================================

// BenchmarkIndexing_CacheHitVsMiss compares cache hit vs miss performance
func BenchmarkIndexing_CacheHitVsMiss(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("cache-test", 100, ComplexityMedium)

	// Subtest: Cold start (cache miss)
	b.Run("CacheMiss", func(b *testing.B) {
		indexer := NewDefaultFileIndexer()
		manifest := createTestManifest(projectDir, 200, 4, true)

		if err := indexer.LoadFromManifest(manifest); err != nil {
			b.Fatalf("Failed to load manifest: %v", err)
		}

		ctx := context.Background()
		config := IndexConfig{
			Targets:      []string{"app"},
			Globs:        []string{"**/*.php"},
			BaseDir:      projectDir,
			MaxWorkers:   4,
			MaxFiles:     200,
			CacheEnabled: true,
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// Clear cache for each iteration to simulate cold start
			if err := indexer.RefreshIndex(ctx); err != nil {
				b.Fatalf("Failed to refresh index: %v", err)
			}

			result, err := indexer.IndexFiles(ctx, config)
			if err != nil {
				b.Fatalf("Indexing failed: %v", err)
			}
			if result.Cached > 0 {
				b.Fatalf("Expected cache miss, got %d cached files", result.Cached)
			}
		}
	})

	// Subtest: Warm start (cache hit)
	b.Run("CacheHit", func(b *testing.B) {
		indexer := NewDefaultFileIndexer()
		manifest := createTestManifest(projectDir, 200, 4, true)

		if err := indexer.LoadFromManifest(manifest); err != nil {
			b.Fatalf("Failed to load manifest: %v", err)
		}

		ctx := context.Background()
		config := IndexConfig{
			Targets:      []string{"app"},
			Globs:        []string{"**/*.php"},
			BaseDir:      projectDir,
			MaxWorkers:   4,
			MaxFiles:     200,
			CacheEnabled: true,
		}

		// Prime the cache
		_, err := indexer.IndexFiles(ctx, config)
		if err != nil {
			b.Fatalf("Failed to prime cache: %v", err)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			result, err := indexer.IndexFiles(ctx, config)
			if err != nil {
				b.Fatalf("Indexing failed: %v", err)
			}
			// Should be significantly faster with cache hits
			if result.DurationMs > 200 && b.N == 1 {
				b.Logf("WARNING: Cache hit took %dms (expected <200ms)", result.DurationMs)
			}
		}
	})
}

// BenchmarkIndexing_IncrementalWithCache measures incremental performance (90%+ cache hits)
func BenchmarkIndexing_IncrementalWithCache(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("incremental", 500, ComplexityMedium)

	indexer := NewDefaultFileIndexer()
	manifest := createTestManifest(projectDir, 600, 8, true)

	if err := indexer.LoadFromManifest(manifest); err != nil {
		b.Fatalf("Failed to load manifest: %v", err)
	}

	ctx := context.Background()
	config := IndexConfig{
		Targets:      []string{"app", "routes"},
		Globs:        []string{"**/*.php"},
		BaseDir:      projectDir,
		MaxWorkers:   8,
		MaxFiles:     600,
		CacheEnabled: true,
	}

	// Prime the cache
	_, err := indexer.IndexFiles(ctx, config)
	if err != nil {
		b.Fatalf("Failed to prime cache: %v", err)
	}

	// Modify 10% of files to simulate incremental changes
	files, err := filepath.Glob(filepath.Join(projectDir, "app", "**", "*.php"))
	if err != nil {
		b.Fatalf("Failed to glob files: %v", err)
	}

	modifyCount := len(files) / 10 // 10% of files
	for i := 0; i < modifyCount; i++ {
		content := fmt.Sprintf("<?php\n// Modified at %s\nclass Modified%d {}\n", time.Now().Format(time.RFC3339), i)
		if err := os.WriteFile(files[i], []byte(content), 0644); err != nil {
			b.Fatalf("Failed to modify file: %v", err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := indexer.IndexFiles(ctx, config)
		if err != nil {
			b.Fatalf("Indexing failed: %v", err)
		}
		// Target: <2s incremental with 90%+ cache hits
		if result.DurationMs > 2000 && b.N == 1 {
			b.Logf("WARNING: Incremental indexing took %dms (target: <2000ms)", result.DurationMs)
		}
	}
}

// ==============================================================================
// CONCURRENCY & SCALABILITY BENCHMARKS
// ==============================================================================

// BenchmarkIndexing_WorkerScaling measures performance across different worker counts
func BenchmarkIndexing_WorkerScaling(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("scaling", 200, ComplexityMedium)

	workerCounts := []int{1, 2, 4, 8, 16}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers%d", workers), func(b *testing.B) {
			indexer := NewDefaultFileIndexer()
			manifest := createTestManifest(projectDir, 300, workers, false) // No cache for pure worker measurement

			if err := indexer.LoadFromManifest(manifest); err != nil {
				b.Fatalf("Failed to load manifest: %v", err)
			}

			ctx := context.Background()
			config := IndexConfig{
				Targets:      []string{"app", "routes"},
				Globs:        []string{"**/*.php"},
				BaseDir:      projectDir,
				MaxWorkers:   workers,
				MaxFiles:     300,
				CacheEnabled: false,
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				result, err := indexer.IndexFiles(ctx, config)
				if err != nil {
					b.Fatalf("Indexing failed: %v", err)
				}
				if len(result.Files) == 0 {
					b.Fatalf("No files processed")
				}
			}
		})
	}
}

// BenchmarkIndexing_MemoryUsage measures memory usage under different project sizes
func BenchmarkIndexing_MemoryUsage(b *testing.B) {
	fileCounts := []int{50, 250, 800}

	for _, count := range fileCounts {
		b.Run(fmt.Sprintf("Files%d", count), func(b *testing.B) {
			suite := NewBenchmarkSuite(b)
			projectDir := suite.createTestProject(fmt.Sprintf("memory-%d", count), count, ComplexityMedium)

			indexer := NewDefaultFileIndexer()
			manifest := createTestManifest(projectDir, count+100, 8, true)

			if err := indexer.LoadFromManifest(manifest); err != nil {
				b.Fatalf("Failed to load manifest: %v", err)
			}

			ctx := context.Background()
			config := IndexConfig{
				Targets:      []string{"app", "routes", "database"},
				Globs:        []string{"**/*.php"},
				BaseDir:      projectDir,
				MaxWorkers:   8,
				MaxFiles:     count + 100,
				CacheEnabled: true,
			}

			// Force garbage collection before measurement
			runtime.GC()
			var memBefore runtime.MemStats
			runtime.ReadMemStats(&memBefore)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				result, err := indexer.IndexFiles(ctx, config)
				if err != nil {
					b.Fatalf("Indexing failed: %v", err)
				}
				if len(result.Files) == 0 {
					b.Fatalf("No files processed")
				}
			}

			// Check memory usage didn't spike beyond reasonable limits
			var memAfter runtime.MemStats
			runtime.ReadMemStats(&memAfter)
			memUsed := memAfter.Alloc - memBefore.Alloc

			// Target: <100MB memory usage for largest projects
			if memUsed > 100*1024*1024 && b.N == 1 {
				b.Logf("WARNING: Memory usage %d bytes (target: <100MB)", memUsed)
			}
		})
	}
}

// ==============================================================================
// DETERMINISM VALIDATION BENCHMARKS
// ==============================================================================

// BenchmarkIndexing_Determinism validates output consistency across multiple runs
func BenchmarkIndexing_Determinism(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("determinism", 100, ComplexityMedium)

	indexer := NewDefaultFileIndexer()
	manifest := createTestManifest(projectDir, 150, 4, true)

	if err := indexer.LoadFromManifest(manifest); err != nil {
		b.Fatalf("Failed to load manifest: %v", err)
	}

	ctx := context.Background()
	config := IndexConfig{
		Targets:      []string{"app", "routes"},
		Globs:        []string{"**/*.php"},
		BaseDir:      projectDir,
		MaxWorkers:   4,
		MaxFiles:     150,
		CacheEnabled: false, // Ensure fresh runs
	}

	// First run to establish baseline
	result1, err := indexer.IndexFiles(ctx, config)
	if err != nil {
		b.Fatalf("First indexing failed: %v", err)
	}

	hash1 := hashResult(result1)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result, err := indexer.IndexFiles(ctx, config)
		if err != nil {
			b.Fatalf("Indexing failed: %v", err)
		}

		hash := hashResult(result)
		if hash != hash1 {
			b.Fatalf("Non-deterministic output detected! Hash mismatch")
		}

		// Verify file order is consistent
		if len(result.Files) != len(result1.Files) {
			b.Fatalf("File count mismatch: got %d, expected %d", len(result.Files), len(result1.Files))
		}

		for j, file := range result.Files {
			if file.Path != result1.Files[j].Path {
				b.Fatalf("File order mismatch at index %d: got %s, expected %s", j, file.Path, result1.Files[j].Path)
			}
		}
	}
}

// ==============================================================================
// COMPONENT PERFORMANCE BENCHMARKS
// ==============================================================================

// BenchmarkComponent_FileDiscoverer measures FileDiscoverer performance
func BenchmarkComponent_FileDiscoverer(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("discoverer", 500, ComplexityMedium)

	discoverer := NewFileDiscoverer()
	targets := []string{"app", "routes", "database"}
	globs := []string{"**/*.php"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		files, err := discoverer.DiscoverFiles(context.Background(), targets, globs, projectDir)
		if err != nil {
			b.Fatalf("Discovery failed: %v", err)
		}
		if len(files) == 0 {
			b.Fatalf("No files discovered")
		}
	}
}

// BenchmarkComponent_FileCacher measures FileCacher performance under concurrent load
func BenchmarkComponent_FileCacher(b *testing.B) {
	cacheConfig := &manifest.CacheConfig{
		Enabled: &[]bool{true}[0],
		Kind:    &[]string{CacheModeModTime}[0],
	}

	cacher := NewFileCache(cacheConfig)

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		path := fmt.Sprintf("app/Models/Model%d.php", i)
		entry := &CacheEntry{
			Path:        path,
			ModTime:     time.Now(),
			ProcessedAt: time.Now(),
			Valid:       true,
		}
		_ = cacher.SetCacheEntry(path, entry)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := fmt.Sprintf("app/Models/Model%d.php", i%1000)
			_, _ = cacher.GetCacheEntry(path)
			i++
		}
	})
}

// BenchmarkComponent_WorkerPool measures WorkerPool throughput
func BenchmarkComponent_WorkerPool(b *testing.B) {
	suite := NewBenchmarkSuite(b)
	projectDir := suite.createTestProject("workers", 1000, ComplexitySmall)

	// Discover files first
	discoverer := NewFileDiscoverer()
	files, err := discoverer.DiscoverFiles(context.Background(), []string{"app"}, []string{"**/*.php"}, projectDir)
	if err != nil {
		b.Fatalf("Discovery failed: %v", err)
	}

	workerPool := NewWorkerPoolManager()
	processor := &SimpleFileProcessor{}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := workerPool.ProcessFiles(context.Background(), files, 8, processor)
		if err != nil {
			b.Fatalf("Processing failed: %v", err)
		}
	}
}

// ==============================================================================
// HELPER FUNCTIONS
// ==============================================================================

// createTestManifest creates a manifest for benchmark testing
func createTestManifest(baseDir string, maxFiles, maxWorkers int, cacheEnabled bool) *manifest.Manifest {
	return &manifest.Manifest{
		Project: manifest.ProjectConfig{
			Root: baseDir,
		},
		Scan: manifest.ScanConfig{
			Targets: []string{"app", "routes"},
			Globs:   []string{"**/*.php"},
		},
		Limits: &manifest.LimitsConfig{
			MaxFiles:   &maxFiles,
			MaxWorkers: &maxWorkers,
			MaxDepth:   &[]int{20}[0],
		},
		Cache: &manifest.CacheConfig{
			Enabled: &cacheEnabled,
			Kind:    &[]string{CacheModeModTime}[0],
		},
	}
}

// hashResult creates a deterministic hash of IndexResult for consistency validation
func hashResult(result *IndexResult) string {
	hasher := sha256.New()

	// Hash file paths in order (should be deterministically sorted)
	for _, file := range result.Files {
		hasher.Write([]byte(file.Path))
		hasher.Write([]byte{0}) // separator
	}

	// Hash counts and flags
	hasher.Write([]byte(fmt.Sprintf("%d:%d:%d:%t:%d",
		result.Cached, result.Fresh, result.Skipped, result.Partial, result.TotalFiles)))

	return fmt.Sprintf("%x", hasher.Sum(nil))
}

// reportBenchmarkResults outputs detailed performance analysis
