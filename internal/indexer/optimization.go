// Package indexer provides performance optimization strategies and configuration profiles
// for different Laravel project types and sizes. This file implements intelligent
// configuration selection and performance monitoring to maximize indexing throughput.
package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// ProjectSize categorizes Laravel projects by their scale and complexity
type ProjectSize int

const (
	ProjectSizeSmall      ProjectSize = iota // < 100 PHP files
	ProjectSizeMedium                        // 100-500 PHP files
	ProjectSizeLarge                         // 500-1000 PHP files
	ProjectSizeEnterprise                    // > 1000 PHP files
)

// String returns human-readable project size description
func (ps ProjectSize) String() string {
	switch ps {
	case ProjectSizeSmall:
		return "Small"
	case ProjectSizeMedium:
		return "Medium"
	case ProjectSizeLarge:
		return "Large"
	case ProjectSizeEnterprise:
		return "Enterprise"
	default:
		return "Unknown"
	}
}

// OptimizationProfile defines performance-tuned configuration for specific project types
type OptimizationProfile struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	CacheConfig  CacheConfig       `json:"cache_config"`
	WorkerConfig WorkerPoolConfig  `json:"worker_config"`
	LimitsConfig LimitsConfig      `json:"limits_config"`
	Suitable     []ProjectSize     `json:"suitable_for"`
	Performance  PerformanceTarget `json:"performance_target"`
}

// CacheConfig defines optimized caching parameters for different scenarios
type CacheConfig struct {
	Enabled         bool          `json:"enabled"`
	Kind            string        `json:"kind"`             // "mtime" or "sha256+mtime"
	MaxSize         int           `json:"max_size"`         // Maximum cache entries
	CleanupInterval time.Duration `json:"cleanup_interval"` // Cache cleanup frequency
}

// WorkerPoolConfig defines optimized worker pool parameters
type WorkerPoolConfig struct {
	MaxWorkers    int           `json:"max_workers"`    // Maximum concurrent workers
	QueueSize     int           `json:"queue_size"`     // Work queue buffer size
	WorkerTimeout time.Duration `json:"worker_timeout"` // Individual worker timeout
}

// LimitsConfig defines resource limits for different project scales
type LimitsConfig struct {
	MaxFiles  int   `json:"max_files"`  // Maximum files to process
	MaxDepth  int   `json:"max_depth"`  // Maximum directory depth
	MaxMemory int64 `json:"max_memory"` // Memory usage limit in bytes
}

// PerformanceTarget defines expected performance characteristics
type PerformanceTarget struct {
	ColdStartMs      int64   `json:"cold_start_ms"`     // Cold start target (ms)
	IncrementalMs    int64   `json:"incremental_ms"`    // Incremental target (ms)
	ThroughputFPS    float64 `json:"throughput_fps"`    // Files per second
	CacheHitRate     float64 `json:"cache_hit_rate"`    // Expected cache hit rate %
	MemoryUsageMB    int64   `json:"memory_usage_mb"`   // Expected memory usage MB
	WorkerEfficiency float64 `json:"worker_efficiency"` // Worker utilization %
}

// OptimizationProfiles contains pre-defined performance configurations for common Laravel project types
var OptimizationProfiles = map[string]OptimizationProfile{
	"laravel-starter": {
		Name:        "Laravel Starter Project",
		Description: "Optimized for small Laravel projects with basic structure (< 100 files)",
		CacheConfig: CacheConfig{
			Enabled:         true,
			Kind:            CacheModeModTime, // Fast validation for small projects
			MaxSize:         500,
			CleanupInterval: 5 * time.Minute,
		},
		WorkerConfig: WorkerPoolConfig{
			MaxWorkers:    2,  // Conservative for small projects
			QueueSize:     50, // Small queue
			WorkerTimeout: 30 * time.Second,
		},
		LimitsConfig: LimitsConfig{
			MaxFiles:  200,
			MaxDepth:  10,
			MaxMemory: 50 * 1024 * 1024, // 50MB
		},
		Suitable: []ProjectSize{ProjectSizeSmall},
		Performance: PerformanceTarget{
			ColdStartMs:      100,  // < 100ms
			IncrementalMs:    50,   // < 50ms
			ThroughputFPS:    1000, // 1000 files/sec
			CacheHitRate:     95,   // 95% cache hits
			MemoryUsageMB:    25,   // 25MB typical
			WorkerEfficiency: 85,   // 85% utilization
		},
	},

	"laravel-standard": {
		Name:        "Standard Laravel Application",
		Description: "Optimized for typical Laravel applications with moderate complexity (100-500 files)",
		CacheConfig: CacheConfig{
			Enabled:         true,
			Kind:            CacheModeModTime,
			MaxSize:         2000,
			CleanupInterval: 10 * time.Minute,
		},
		WorkerConfig: WorkerPoolConfig{
			MaxWorkers:    4,   // Balanced concurrency
			QueueSize:     200, // Moderate queue
			WorkerTimeout: 60 * time.Second,
		},
		LimitsConfig: LimitsConfig{
			MaxFiles:  1000,
			MaxDepth:  15,
			MaxMemory: 100 * 1024 * 1024, // 100MB
		},
		Suitable: []ProjectSize{ProjectSizeMedium},
		Performance: PerformanceTarget{
			ColdStartMs:      1000, // < 1s
			IncrementalMs:    200,  // < 200ms
			ThroughputFPS:    500,  // 500 files/sec
			CacheHitRate:     90,   // 90% cache hits
			MemoryUsageMB:    50,   // 50MB typical
			WorkerEfficiency: 80,   // 80% utilization
		},
	},

	"laravel-enterprise": {
		Name:        "Enterprise Laravel Project",
		Description: "Optimized for large Laravel projects with complex structure (500+ files)",
		CacheConfig: CacheConfig{
			Enabled:         true,
			Kind:            CacheModeSHA256MTime, // Precise validation for large projects
			MaxSize:         10000,
			CleanupInterval: 15 * time.Minute,
		},
		WorkerConfig: WorkerPoolConfig{
			MaxWorkers:    8,   // High concurrency
			QueueSize:     500, // Large queue
			WorkerTimeout: 120 * time.Second,
		},
		LimitsConfig: LimitsConfig{
			MaxFiles:  5000,
			MaxDepth:  25,
			MaxMemory: 200 * 1024 * 1024, // 200MB
		},
		Suitable: []ProjectSize{ProjectSizeLarge, ProjectSizeEnterprise},
		Performance: PerformanceTarget{
			ColdStartMs:      5000, // < 5s
			IncrementalMs:    1000, // < 1s
			ThroughputFPS:    200,  // 200 files/sec
			CacheHitRate:     85,   // 85% cache hits
			MemoryUsageMB:    75,   // 75MB typical
			WorkerEfficiency: 75,   // 75% utilization
		},
	},

	"laravel-monolith": {
		Name:        "Laravel Monolith/Multi-Package",
		Description: "Optimized for very large monolithic Laravel applications (1000+ files)",
		CacheConfig: CacheConfig{
			Enabled:         true,
			Kind:            CacheModeSHA256MTime,
			MaxSize:         20000,
			CleanupInterval: 30 * time.Minute,
		},
		WorkerConfig: WorkerPoolConfig{
			MaxWorkers:    12,   // Maximum concurrency
			QueueSize:     1000, // Large queue
			WorkerTimeout: 300 * time.Second,
		},
		LimitsConfig: LimitsConfig{
			MaxFiles:  15000,
			MaxDepth:  30,
			MaxMemory: 500 * 1024 * 1024, // 500MB
		},
		Suitable: []ProjectSize{ProjectSizeEnterprise},
		Performance: PerformanceTarget{
			ColdStartMs:      10000, // < 10s
			IncrementalMs:    2000,  // < 2s
			ThroughputFPS:    100,   // 100 files/sec
			CacheHitRate:     80,    // 80% cache hits
			MemoryUsageMB:    150,   // 150MB typical
			WorkerEfficiency: 70,    // 70% utilization
		},
	},

	"performance-focused": {
		Name:        "Maximum Performance Profile",
		Description: "Optimized for maximum throughput, suitable for CI/CD or batch processing",
		CacheConfig: CacheConfig{
			Enabled:         true,
			Kind:            CacheModeModTime, // Fastest validation
			MaxSize:         50000,
			CleanupInterval: 60 * time.Minute,
		},
		WorkerConfig: WorkerPoolConfig{
			MaxWorkers:    runtime.NumCPU(), // Use all CPU cores
			QueueSize:     2000,             // Very large queue
			WorkerTimeout: 600 * time.Second,
		},
		LimitsConfig: LimitsConfig{
			MaxFiles:  50000,
			MaxDepth:  50,
			MaxMemory: 1024 * 1024 * 1024, // 1GB
		},
		Suitable: []ProjectSize{ProjectSizeSmall, ProjectSizeMedium, ProjectSizeLarge, ProjectSizeEnterprise},
		Performance: PerformanceTarget{
			ColdStartMs:      5000, // Varies by project size
			IncrementalMs:    500,  // < 500ms
			ThroughputFPS:    2000, // 2000 files/sec
			CacheHitRate:     95,   // 95% cache hits
			MemoryUsageMB:    500,  // 500MB typical
			WorkerEfficiency: 95,   // 95% utilization
		},
	},

	"memory-constrained": {
		Name:        "Memory-Constrained Profile",
		Description: "Optimized for environments with limited memory availability",
		CacheConfig: CacheConfig{
			Enabled:         true,
			Kind:            CacheModeModTime,
			MaxSize:         1000,            // Smaller cache
			CleanupInterval: 2 * time.Minute, // Frequent cleanup
		},
		WorkerConfig: WorkerPoolConfig{
			MaxWorkers:    2,  // Fewer workers
			QueueSize:     25, // Small queue
			WorkerTimeout: 30 * time.Second,
		},
		LimitsConfig: LimitsConfig{
			MaxFiles:  2000,
			MaxDepth:  15,
			MaxMemory: 25 * 1024 * 1024, // 25MB
		},
		Suitable: []ProjectSize{ProjectSizeSmall, ProjectSizeMedium},
		Performance: PerformanceTarget{
			ColdStartMs:      2000, // Slower but memory-efficient
			IncrementalMs:    500,  // < 500ms
			ThroughputFPS:    100,  // 100 files/sec
			CacheHitRate:     85,   // 85% cache hits
			MemoryUsageMB:    15,   // 15MB typical
			WorkerEfficiency: 60,   // 60% utilization
		},
	},
}

// ProjectStats contains analyzed characteristics of a Laravel project
type ProjectStats struct {
	FileCount       int         `json:"file_count"`
	DirectoryDepth  int         `json:"directory_depth"`
	AverageFileSize int64       `json:"average_file_size"`
	Complexity      ProjectSize `json:"complexity"`
	HasVendor       bool        `json:"has_vendor"`
	HasPackages     bool        `json:"has_packages"`
	Structure       []string    `json:"structure"` // Detected Laravel directories
	EstimatedMemory int64       `json:"estimated_memory_bytes"`
}

// PerformanceReport contains comprehensive performance analysis and metrics
type PerformanceReport struct {
	ProjectStats      ProjectStats        `json:"project_stats"`
	SelectedProfile   OptimizationProfile `json:"selected_profile"`
	ActualPerformance PerformanceMetrics  `json:"actual_performance"`
	Bottlenecks       []string            `json:"bottlenecks"`
	Recommendations   []string            `json:"recommendations"`
	Efficiency        EfficiencyMetrics   `json:"efficiency"`
	Timestamp         time.Time           `json:"timestamp"`
}

// PerformanceMetrics captures actual measured performance
type PerformanceMetrics struct {
	ColdStartMs       int64   `json:"cold_start_ms"`
	IncrementalMs     int64   `json:"incremental_ms"`
	ActualThroughput  float64 `json:"actual_throughput_fps"`
	CacheHitRate      float64 `json:"cache_hit_rate_percent"`
	MemoryUsageMB     int64   `json:"memory_usage_mb"`
	WorkerUtilization float64 `json:"worker_utilization_percent"`
	FilesProcessed    int     `json:"files_processed"`
	ErrorCount        int     `json:"error_count"`
}

// EfficiencyMetrics measures how well the system performed against targets
type EfficiencyMetrics struct {
	SpeedEfficiency  float64 `json:"speed_efficiency_percent"`  // Actual vs target speed
	MemoryEfficiency float64 `json:"memory_efficiency_percent"` // Memory usage efficiency
	CacheEfficiency  float64 `json:"cache_efficiency_percent"`  // Cache hit rate efficiency
	WorkerEfficiency float64 `json:"worker_efficiency_percent"` // Worker utilization efficiency
	OverallScore     float64 `json:"overall_score_percent"`     // Weighted overall score
}

// PerformanceOptimizer provides intelligent performance optimization and monitoring
type PerformanceOptimizer struct {
	profiles map[string]OptimizationProfile
	stats    ProjectStats
	history  []PerformanceMetrics
}

// NewPerformanceOptimizer creates a new performance optimizer instance
func NewPerformanceOptimizer() *PerformanceOptimizer {
	return &PerformanceOptimizer{
		profiles: OptimizationProfiles,
		history:  make([]PerformanceMetrics, 0),
	}
}

// AnalyzeProject analyzes a Laravel project and determines its characteristics
func (po *PerformanceOptimizer) AnalyzeProject(ctx context.Context, manifest *manifest.Manifest) (ProjectStats, error) {
	if manifest == nil {
		return ProjectStats{}, fmt.Errorf("manifest cannot be nil")
	}

	discoverer := NewFileDiscoverer()

	// Discover all PHP files in the project
	files, err := discoverer.DiscoverFiles(ctx, manifest.Scan.Targets, manifest.Scan.Globs, manifest.Project.Root)
	if err != nil {
		return ProjectStats{}, fmt.Errorf("failed to discover files: %w", err)
	}

	stats := ProjectStats{
		FileCount:       len(files),
		DirectoryDepth:  calculateMaxDepth(files),
		AverageFileSize: calculateAverageFileSize(files),
		Structure:       analyzeDirectoryStructure(files),
		EstimatedMemory: estimateMemoryUsage(len(files)),
	}

	// Determine project complexity
	stats.Complexity = categorizeProjectSize(stats.FileCount)

	// Detect special Laravel features
	stats.HasVendor = containsPath(files, "vendor")
	stats.HasPackages = containsPath(files, "packages")

	po.stats = stats
	return stats, nil
}

// SelectOptimalConfiguration chooses the best optimization profile for a project
func (po *PerformanceOptimizer) SelectOptimalConfiguration(stats ProjectStats) OptimizationProfile {
	// Score each profile for suitability
	bestProfile := po.profiles["laravel-standard"] // Default fallback
	bestScore := 0.0

	for _, profile := range po.profiles {
		score := po.scoreProfile(profile, stats)
		if score > bestScore {
			bestScore = score
			bestProfile = profile
		}
	}

	// Apply dynamic adjustments based on system capabilities
	adjustedProfile := po.adjustProfileForSystem(bestProfile, stats)

	return adjustedProfile
}

// scoreProfile calculates how well a profile matches project characteristics
func (po *PerformanceOptimizer) scoreProfile(profile OptimizationProfile, stats ProjectStats) float64 {
	score := 0.0

	// Check if project size is suitable
	sizeMatch := false
	for _, size := range profile.Suitable {
		if size == stats.Complexity {
			sizeMatch = true
			break
		}
	}
	if sizeMatch {
		score += 40.0 // 40 points for size match
	}

	// Evaluate memory constraints
	if profile.LimitsConfig.MaxMemory >= stats.EstimatedMemory {
		score += 20.0 // 20 points for adequate memory
	}

	// Evaluate file capacity
	if profile.LimitsConfig.MaxFiles >= stats.FileCount {
		score += 20.0 // 20 points for adequate file limit
	}

	// Evaluate depth capacity
	if profile.LimitsConfig.MaxDepth >= stats.DirectoryDepth {
		score += 10.0 // 10 points for adequate depth
	}

	// Bonus for special features
	if stats.HasVendor && strings.Contains(profile.Name, "Enterprise") {
		score += 5.0 // 5 points for vendor directory handling
	}
	if stats.HasPackages && strings.Contains(profile.Name, "Monolith") {
		score += 5.0 // 5 points for packages handling
	}

	return score
}

// adjustProfileForSystem makes dynamic adjustments based on system capabilities
func (po *PerformanceOptimizer) adjustProfileForSystem(profile OptimizationProfile, stats ProjectStats) OptimizationProfile {
	adjusted := profile

	// Adjust worker count based on CPU cores
	maxWorkers := runtime.NumCPU()
	if adjusted.WorkerConfig.MaxWorkers > maxWorkers {
		adjusted.WorkerConfig.MaxWorkers = maxWorkers
	}

	// Ensure minimum viable workers
	if adjusted.WorkerConfig.MaxWorkers < 1 {
		adjusted.WorkerConfig.MaxWorkers = 1
	}

	// Adjust cache size based on available memory
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	availableMemory := int64(memStats.Sys)
	maxCacheMemory := availableMemory / 4 // Use at most 25% of available memory for cache

	estimatedCacheMemory := int64(adjusted.CacheConfig.MaxSize * 1024) // Rough estimate
	if estimatedCacheMemory > maxCacheMemory {
		adjusted.CacheConfig.MaxSize = int(maxCacheMemory / 1024)
		if adjusted.CacheConfig.MaxSize < 100 {
			adjusted.CacheConfig.MaxSize = 100 // Minimum viable cache
		}
	}

	return adjusted
}

// ApplyOptimizationProfile applies an optimization profile to a manifest
func (po *PerformanceOptimizer) ApplyOptimizationProfile(inputManifest *manifest.Manifest, profile OptimizationProfile) *manifest.Manifest {
	// Create a copy to avoid modifying the original
	result := *inputManifest

	// Create limits if not present
	if result.Limits == nil {
		result.Limits = &manifest.LimitsConfig{}
	}
	if result.Cache == nil {
		result.Cache = &manifest.CacheConfig{}
	}

	// Apply limits configuration
	result.Limits.MaxFiles = &profile.LimitsConfig.MaxFiles
	result.Limits.MaxWorkers = &profile.WorkerConfig.MaxWorkers
	result.Limits.MaxDepth = &profile.LimitsConfig.MaxDepth

	// Apply cache configuration
	result.Cache.Enabled = &profile.CacheConfig.Enabled
	result.Cache.Kind = &profile.CacheConfig.Kind

	return &result
}

// MeasurePerformance captures performance metrics from an IndexResult
func (po *PerformanceOptimizer) MeasurePerformance(result *IndexResult, memStats runtime.MemStats) PerformanceMetrics {
	metrics := PerformanceMetrics{
		ColdStartMs:    result.DurationMs,
		FilesProcessed: len(result.Files),
		ErrorCount:     result.Skipped,
		MemoryUsageMB:  int64(memStats.Alloc / 1024 / 1024),
	}

	// Calculate throughput
	if result.DurationMs > 0 {
		metrics.ActualThroughput = float64(len(result.Files)) * 1000.0 / float64(result.DurationMs)
	}

	// Calculate cache hit rate
	if result.Cached+result.Fresh > 0 {
		metrics.CacheHitRate = float64(result.Cached) / float64(result.Cached+result.Fresh) * 100.0
	}

	po.history = append(po.history, metrics)
	return metrics
}

// GeneratePerformanceReport creates a comprehensive performance analysis report
func (po *PerformanceOptimizer) GeneratePerformanceReport(profile OptimizationProfile, metrics PerformanceMetrics) PerformanceReport {
	efficiency := po.calculateEfficiency(profile, metrics)
	bottlenecks := po.identifyBottlenecks(profile, metrics)
	recommendations := po.generateRecommendations(profile, metrics, efficiency)

	return PerformanceReport{
		ProjectStats:      po.stats,
		SelectedProfile:   profile,
		ActualPerformance: metrics,
		Efficiency:        efficiency,
		Bottlenecks:       bottlenecks,
		Recommendations:   recommendations,
		Timestamp:         time.Now(),
	}
}

// calculateEfficiency measures efficiency against performance targets
func (po *PerformanceOptimizer) calculateEfficiency(profile OptimizationProfile, metrics PerformanceMetrics) EfficiencyMetrics {
	target := profile.Performance

	// Speed efficiency (lower is better for time metrics)
	speedEff := 100.0
	if metrics.ColdStartMs > 0 && target.ColdStartMs > 0 {
		speedEff = float64(target.ColdStartMs) / float64(metrics.ColdStartMs) * 100.0
		if speedEff > 100.0 {
			speedEff = 100.0 // Cap at 100%
		}
	}

	// Memory efficiency (lower usage is better)
	memoryEff := 100.0
	if metrics.MemoryUsageMB > 0 && target.MemoryUsageMB > 0 {
		memoryEff = float64(target.MemoryUsageMB) / float64(metrics.MemoryUsageMB) * 100.0
		if memoryEff > 100.0 {
			memoryEff = 100.0
		}
	}

	// Cache efficiency
	cacheEff := 100.0
	if target.CacheHitRate > 0 {
		cacheEff = metrics.CacheHitRate / target.CacheHitRate * 100.0
		if cacheEff > 100.0 {
			cacheEff = 100.0
		}
	}

	// Worker efficiency
	workerEff := metrics.WorkerUtilization

	// Overall weighted score
	overall := (speedEff*0.3 + memoryEff*0.2 + cacheEff*0.3 + workerEff*0.2)

	return EfficiencyMetrics{
		SpeedEfficiency:  speedEff,
		MemoryEfficiency: memoryEff,
		CacheEfficiency:  cacheEff,
		WorkerEfficiency: workerEff,
		OverallScore:     overall,
	}
}

// identifyBottlenecks analyzes performance metrics to identify bottlenecks
func (po *PerformanceOptimizer) identifyBottlenecks(profile OptimizationProfile, metrics PerformanceMetrics) []string {
	var bottlenecks []string

	// Performance vs targets
	target := profile.Performance

	if metrics.ColdStartMs > target.ColdStartMs*2 {
		bottlenecks = append(bottlenecks, fmt.Sprintf("Cold start significantly slower than target (%dms vs %dms)",
			metrics.ColdStartMs, target.ColdStartMs))
	}

	if metrics.ActualThroughput < target.ThroughputFPS*0.5 {
		bottlenecks = append(bottlenecks, fmt.Sprintf("Low throughput (%0.1f fps vs %0.1f fps target)",
			metrics.ActualThroughput, target.ThroughputFPS))
	}

	if metrics.CacheHitRate < target.CacheHitRate*0.8 {
		bottlenecks = append(bottlenecks, fmt.Sprintf("Poor cache hit rate (%0.1f%% vs %0.1f%% target)",
			metrics.CacheHitRate, target.CacheHitRate))
	}

	if metrics.WorkerUtilization < target.WorkerEfficiency*0.7 {
		bottlenecks = append(bottlenecks, "Low worker utilization suggests I/O or coordination bottlenecks")
	}

	if metrics.MemoryUsageMB > target.MemoryUsageMB*2 {
		bottlenecks = append(bottlenecks, "Excessive memory usage may indicate memory leaks or inefficient algorithms")
	}

	if metrics.ErrorCount > 0 {
		bottlenecks = append(bottlenecks, fmt.Sprintf("Processing errors occurred (%d files failed)", metrics.ErrorCount))
	}

	return bottlenecks
}

// generateRecommendations provides optimization suggestions based on performance analysis
func (po *PerformanceOptimizer) generateRecommendations(profile OptimizationProfile, metrics PerformanceMetrics, efficiency EfficiencyMetrics) []string {
	var recommendations []string

	// Performance-based recommendations
	if efficiency.SpeedEfficiency < 70 {
		recommendations = append(recommendations, "Consider increasing worker count or switching to a performance-focused profile")
	}

	if efficiency.CacheEfficiency < 70 {
		recommendations = append(recommendations, "Cache hit rate is low - verify file modification patterns or consider sha256+mtime validation")
	}

	if efficiency.MemoryEfficiency < 70 {
		recommendations = append(recommendations, "Memory usage is high - consider reducing cache size or using memory-constrained profile")
	}

	if efficiency.WorkerEfficiency < 60 {
		recommendations = append(recommendations, "Low worker utilization - consider reducing worker count or investigating I/O bottlenecks")
	}

	// Historical trend recommendations
	if len(po.history) >= 2 {
		recent := po.history[len(po.history)-1]
		previous := po.history[len(po.history)-2]

		if float64(recent.ColdStartMs) > float64(previous.ColdStartMs)*1.5 {
			recommendations = append(recommendations, "Performance degradation detected - consider cache cleanup or system resource check")
		}
	}

	// Profile-specific recommendations
	if strings.Contains(profile.Name, "Starter") && metrics.FilesProcessed > 100 {
		recommendations = append(recommendations, "Project has grown - consider upgrading to Standard Laravel profile")
	}

	if strings.Contains(profile.Name, "Standard") && metrics.FilesProcessed > 500 {
		recommendations = append(recommendations, "Project has grown significantly - consider upgrading to Enterprise profile")
	}

	if efficiency.OverallScore > 95 {
		recommendations = append(recommendations, "Excellent performance! Current configuration is optimal for this project.")
	}

	return recommendations
}

// Helper functions

func calculateMaxDepth(files []FileInfo) int {
	maxDepth := 0
	for _, file := range files {
		depth := strings.Count(file.Path, string(filepath.Separator))
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth
}

func calculateAverageFileSize(files []FileInfo) int64 {
	if len(files) == 0 {
		return 0
	}

	total := int64(0)
	for _, file := range files {
		total += file.Size
	}
	return total / int64(len(files))
}

func analyzeDirectoryStructure(files []FileInfo) []string {
	directories := make(map[string]bool)

	for _, file := range files {
		parts := strings.Split(file.Path, string(filepath.Separator))
		if len(parts) > 0 {
			directories[parts[0]] = true
		}
	}

	var structure []string
	for dir := range directories {
		structure = append(structure, dir)
	}

	return structure
}

func containsPath(files []FileInfo, pathSegment string) bool {
	for _, file := range files {
		if strings.Contains(file.Path, pathSegment) {
			return true
		}
	}
	return false
}

func categorizeProjectSize(fileCount int) ProjectSize {
	switch {
	case fileCount < 100:
		return ProjectSizeSmall
	case fileCount < 500:
		return ProjectSizeMedium
	case fileCount < 1000:
		return ProjectSizeLarge
	default:
		return ProjectSizeEnterprise
	}
}

func estimateMemoryUsage(fileCount int) int64 {
	// Rough estimation: 10KB per file for metadata + processing overhead
	basePerFile := int64(10 * 1024)
	overhead := int64(20 * 1024 * 1024) // 20MB base overhead

	return int64(fileCount)*basePerFile + overhead
}
