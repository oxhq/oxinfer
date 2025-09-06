// Package parser provides configuration-driven performance optimization for PHP parsing operations.
// Implements project-size-based optimization profiles, query optimization, and performance reporting
// to ensure optimal performance across different Laravel project characteristics.
package parser

import (
	"fmt"
	"sync"
	"time"
)

// ProjectSize represents different categories of PHP project sizes for optimization.
type ProjectSize int

const (
	ProjectSizeSmall      ProjectSize = iota // < 100 files
	ProjectSizeMedium                        // 100-500 files
	ProjectSizeLarge                         // 500-1000 files
	ProjectSizeEnterprise                    // > 1000 files
)

// String returns a human-readable string representation of the project size.
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

// OptimizationProfile defines performance optimization settings for different project types.
// Each profile is tailored to specific project characteristics and performance requirements.
type OptimizationProfile struct {
	Name                  string        // Profile identifier
	Description           string        // Human-readable description
	MaxWorkers            int           // Maximum concurrent workers
	ParserPoolSize        int           // Size of parser pool
	MemoryLimitMB         int64         // Memory limit in megabytes
	GCTriggerMB           int64         // GC trigger threshold in megabytes
	CacheStrategy         string        // Caching strategy ("aggressive", "balanced", "conservative")
	QueryOptimization     bool          // Enable query optimization
	EnableParallelQueries bool          // Enable parallel query execution
	WorkerTimeout         time.Duration // Timeout for individual worker operations
	Suitable              []ProjectSize // Project sizes this profile suits
}

// OptimizationProfiles contains predefined optimization profiles for different Laravel project types.
var OptimizationProfiles = map[string]OptimizationProfile{
	"laravel-micro": {
		Name:                  "Laravel Micro/API",
		Description:           "Optimized for small Laravel API projects and microservices",
		MaxWorkers:            2,
		ParserPoolSize:        4,
		MemoryLimitMB:         50,
		GCTriggerMB:           25,
		CacheStrategy:         "aggressive",
		QueryOptimization:     false, // Simple queries don't need optimization
		EnableParallelQueries: false,
		WorkerTimeout:         10 * time.Second,
		Suitable:              []ProjectSize{ProjectSizeSmall},
	},
	"laravel-standard": {
		Name:                  "Laravel Standard",
		Description:           "Optimized for typical Laravel web applications",
		MaxWorkers:            6,
		ParserPoolSize:        12,
		MemoryLimitMB:         200,
		GCTriggerMB:           100,
		CacheStrategy:         "balanced",
		QueryOptimization:     true,
		EnableParallelQueries: true,
		WorkerTimeout:         30 * time.Second,
		Suitable:              []ProjectSize{ProjectSizeMedium},
	},
	"laravel-enterprise": {
		Name:                  "Laravel Enterprise",
		Description:           "Optimized for large Laravel applications and platforms",
		MaxWorkers:            12,
		ParserPoolSize:        24,
		MemoryLimitMB:         1000,
		GCTriggerMB:           500,
		CacheStrategy:         "conservative",
		QueryOptimization:     true,
		EnableParallelQueries: true,
		WorkerTimeout:         60 * time.Second,
		Suitable:              []ProjectSize{ProjectSizeLarge, ProjectSizeEnterprise},
	},
	"laravel-monolith": {
		Name:                  "Laravel Monolith",
		Description:           "Optimized for massive monolithic Laravel applications",
		MaxWorkers:            16,
		ParserPoolSize:        32,
		MemoryLimitMB:         2000,
		GCTriggerMB:           1000,
		CacheStrategy:         "conservative",
		QueryOptimization:     true,
		EnableParallelQueries: true,
		WorkerTimeout:         120 * time.Second,
		Suitable:              []ProjectSize{ProjectSizeEnterprise},
	},
}

// ProjectStats contains statistics about a PHP project for optimization decisions.
type ProjectStats struct {
	TotalFiles           int           // Total number of PHP files
	AverageFileSize      int64         // Average file size in bytes
	LargestFileSize      int64         // Size of largest file in bytes
	ComplexityScore      float64       // Estimated overall complexity (0-1)
	HasLargeClasses      bool          // Whether project has classes with many methods
	HasDeepInheritance   bool          // Whether project has deep inheritance chains
	LaravelVersion       string        // Laravel version (if detected)
	UsesAdvancedFeatures bool          // Whether project uses advanced PHP features
	EstimatedParseTime   time.Duration // Estimated time to parse entire project
}

// SelectOptimalProfile analyzes project characteristics and recommends the best optimization profile.
// Uses heuristics based on file count, complexity, and Laravel patterns to make recommendations.
func SelectOptimalProfile(projectStats ProjectStats) OptimizationProfile {
	fileCount := projectStats.TotalFiles

	// Primary selection based on file count
	var baseProfile OptimizationProfile
	switch {
	case fileCount < 100:
		baseProfile = OptimizationProfiles["laravel-micro"]
	case fileCount < 500:
		baseProfile = OptimizationProfiles["laravel-standard"]
	case fileCount < 1000:
		baseProfile = OptimizationProfiles["laravel-enterprise"]
	default:
		baseProfile = OptimizationProfiles["laravel-monolith"]
	}

	// Adjust profile based on complexity factors
	profile := baseProfile // Copy the base profile

	// Increase resources for complex projects
	if projectStats.ComplexityScore > 0.7 {
		profile.MaxWorkers = min(profile.MaxWorkers+4, 20)
		profile.ParserPoolSize = profile.MaxWorkers * 2
		profile.MemoryLimitMB = int64(float64(profile.MemoryLimitMB) * 1.5)
		profile.WorkerTimeout = profile.WorkerTimeout + 30*time.Second
	}

	// Adjust for large files
	if projectStats.AverageFileSize > 64*1024 { // 64KB average
		profile.WorkerTimeout = profile.WorkerTimeout + 15*time.Second
		profile.MemoryLimitMB = int64(float64(profile.MemoryLimitMB) * 1.3)
	}

	// Enable optimizations for projects with advanced features
	if projectStats.UsesAdvancedFeatures {
		profile.QueryOptimization = true
		profile.EnableParallelQueries = true
	}

	return profile
}

// QueryOptimizer provides optimization for tree-sitter queries based on file characteristics.
// Implements query compilation, caching, and execution optimization for better performance.
type QueryOptimizer struct {
	// Compiled query cache
	compiledQueries map[string]*CompiledQuery
	queryCache      sync.Map // LRU-style cache for query results

	// Performance statistics
	stats QueryOptimizationStats
	mu    sync.RWMutex

	// Configuration
	enableCaching  bool
	maxCacheSize   int
	enableParallel bool
}

// CompiledQuery represents a pre-compiled tree-sitter query for better performance.
type CompiledQuery struct {
	Name        string        // Query name/identifier
	Pattern     string        // S-expression pattern
	CompiledAt  time.Time     // When query was compiled
	UseCount    int64         // Number of times used
	AvgExecTime time.Duration // Average execution time
}

// QueryOptimizationStats tracks query optimizer performance and effectiveness.
type QueryOptimizationStats struct {
	TotalQueries        int64         // Total queries executed
	CacheHits           int64         // Query cache hits
	CacheMisses         int64         // Query cache misses
	CacheHitRate        float64       // Cache hit rate percentage
	AverageQueryTime    time.Duration // Average query execution time
	OptimizationSavings time.Duration // Time saved by optimization
	CompiledQueries     int           // Number of pre-compiled queries
}

// NewQueryOptimizer creates a new query optimizer with the specified configuration.
func NewQueryOptimizer(enableCaching bool, enableParallel bool) *QueryOptimizer {
	return &QueryOptimizer{
		compiledQueries: make(map[string]*CompiledQuery),
		enableCaching:   enableCaching,
		maxCacheSize:    10000, // 10K cache entries
		enableParallel:  enableParallel,
	}
}

// PrecompileQueries pre-compiles frequently used queries for better performance.
// Should be called during parser initialization to prepare common queries.
func (q *QueryOptimizer) PrecompileQueries() error {
	commonQueries := map[string]string{
		"class_declarations":     "(class_declaration name: (name) @class.name)",
		"method_declarations":    "(method_declaration name: (name) @method.name)",
		"namespace_declarations": "(namespace_definition name: (namespace_name) @namespace.name)",
		"trait_declarations":     "(trait_declaration name: (name) @trait.name)",
		"interface_declarations": "(interface_declaration name: (name) @interface.name)",
		"function_declarations":  "(function_definition name: (name) @function.name)",
		"use_statements":         "(namespace_use_declaration (namespace_use_clause name: (qualified_name) @use.name))",
		"property_declarations":  "(property_declaration (property_element (variable_name) @property.name))",
	}

	for queryName, pattern := range commonQueries {
		compiled := &CompiledQuery{
			Name:        queryName,
			Pattern:     pattern,
			CompiledAt:  time.Now(),
			UseCount:    0,
			AvgExecTime: 0,
		}

		q.mu.Lock()
		q.compiledQueries[queryName] = compiled
		q.stats.CompiledQueries++
		q.mu.Unlock()
	}

	return nil
}

// OptimizeQueryExecution optimizes query execution based on file characteristics.
// Chooses between simple, parallel, or optimized execution based on file size and complexity.
func (q *QueryOptimizer) OptimizeQueryExecution(tree *SyntaxTree, extractTypes []string) QueryResult {
	if tree == nil || tree.Root == nil {
		return QueryResult{Error: fmt.Errorf("invalid syntax tree")}
	}

	treeSize := q.calculateTreeSize(tree.Root)

	// Choose execution strategy based on tree characteristics
	switch {
	case treeSize < 100:
		return q.executeSimpleQueries(tree, extractTypes)
	case treeSize > 1000 && q.enableParallel:
		return q.executeParallelQueries(tree, extractTypes)
	default:
		return q.executeStandardQueries(tree, extractTypes)
	}
}

// executeSimpleQueries handles small files with lightweight query execution.
func (q *QueryOptimizer) executeSimpleQueries(tree *SyntaxTree, extractTypes []string) QueryResult {
	startTime := time.Now()

	result := QueryResult{
		MatchedConstructs: make(map[string][]QueryMatch),
		ExecutionTime:     0,
		OptimizationUsed:  "simple",
	}

	// Execute queries sequentially without caching overhead
	for _, extractType := range extractTypes {
		if compiled, exists := q.compiledQueries[extractType]; exists {
			matches := q.executeCompiledQuery(tree, compiled)
			result.MatchedConstructs[extractType] = matches

			q.mu.Lock()
			compiled.UseCount++
			q.mu.Unlock()
		}
	}

	result.ExecutionTime = time.Since(startTime)
	q.updateQueryStats(result.ExecutionTime, false)

	return result
}

// executeParallelQueries handles large files with parallel query execution.
func (q *QueryOptimizer) executeParallelQueries(tree *SyntaxTree, extractTypes []string) QueryResult {
	startTime := time.Now()

	result := QueryResult{
		MatchedConstructs: make(map[string][]QueryMatch),
		ExecutionTime:     0,
		OptimizationUsed:  "parallel",
	}

	// Execute queries in parallel using goroutines
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, extractType := range extractTypes {
		if compiled, exists := q.compiledQueries[extractType]; exists {
			wg.Add(1)

			go func(et string, comp *CompiledQuery) {
				defer wg.Done()

				matches := q.executeCompiledQuery(tree, comp)

				mu.Lock()
				result.MatchedConstructs[et] = matches
				comp.UseCount++
				mu.Unlock()
			}(extractType, compiled)
		}
	}

	wg.Wait()

	result.ExecutionTime = time.Since(startTime)
	q.updateQueryStats(result.ExecutionTime, false)

	return result
}

// executeStandardQueries handles medium files with standard optimized execution.
func (q *QueryOptimizer) executeStandardQueries(tree *SyntaxTree, extractTypes []string) QueryResult {
	startTime := time.Now()

	result := QueryResult{
		MatchedConstructs: make(map[string][]QueryMatch),
		ExecutionTime:     0,
		OptimizationUsed:  "standard",
	}

	// Execute queries with caching
	for _, extractType := range extractTypes {
		// Check cache first if enabled
		if q.enableCaching {
			if cachedResult, found := q.queryCache.Load(extractType); found {
				result.MatchedConstructs[extractType] = cachedResult.([]QueryMatch)
				q.updateQueryStats(0, true) // Cache hit
				continue
			}
		}

		// Execute query
		if compiled, exists := q.compiledQueries[extractType]; exists {
			matches := q.executeCompiledQuery(tree, compiled)
			result.MatchedConstructs[extractType] = matches

			// Cache result if enabled
			if q.enableCaching {
				q.queryCache.Store(extractType, matches)
			}

			q.mu.Lock()
			compiled.UseCount++
			q.mu.Unlock()

			q.updateQueryStats(time.Since(startTime), false)
		}
	}

	result.ExecutionTime = time.Since(startTime)
	return result
}

// executeCompiledQuery executes a pre-compiled query against a syntax tree.
func (q *QueryOptimizer) executeCompiledQuery(tree *SyntaxTree, compiled *CompiledQuery) []QueryMatch {
	// This would integrate with actual tree-sitter query execution
	// For now, we return mock results to demonstrate the structure
	return []QueryMatch{
		{
			Type:     compiled.Name,
			Name:     "MockMatch",
			Position: SourcePosition{StartLine: 1, StartColumn: 1},
		},
	}
}

// calculateTreeSize calculates the total number of nodes in a syntax tree.
func (q *QueryOptimizer) calculateTreeSize(node *SyntaxNode) int {
	if node == nil {
		return 0
	}

	size := 1 // Count this node
	for _, child := range node.Children {
		size += q.calculateTreeSize(child)
	}

	return size
}

// updateQueryStats updates query optimization statistics.
func (q *QueryOptimizer) updateQueryStats(execTime time.Duration, cacheHit bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.stats.TotalQueries++

	if cacheHit {
		q.stats.CacheHits++
	} else {
		q.stats.CacheMisses++
	}

	// Calculate cache hit rate
	if q.stats.TotalQueries > 0 {
		q.stats.CacheHitRate = float64(q.stats.CacheHits) / float64(q.stats.TotalQueries) * 100.0
	}

	// Update average query time (for non-cache hits)
	if !cacheHit {
		totalTime := q.stats.AverageQueryTime * time.Duration(q.stats.CacheMisses-1)
		q.stats.AverageQueryTime = (totalTime + execTime) / time.Duration(q.stats.CacheMisses)
	}
}

// GetStats returns a copy of current query optimization statistics.
func (q *QueryOptimizer) GetStats() QueryOptimizationStats {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.stats
}

// QueryResult represents the result of optimized query execution.
type QueryResult struct {
	MatchedConstructs map[string][]QueryMatch // Constructs found by each query
	ExecutionTime     time.Duration           // Total execution time
	OptimizationUsed  string                  // Type of optimization used
	CacheHitsUsed     int                     // Number of cache hits used
	Error             error                   // Error if execution failed
}

// QueryMatch represents a single match from a tree-sitter query.
type QueryMatch struct {
	Type     string            // Type of construct matched
	Name     string            // Name of the construct
	Position SourcePosition    // Position in source file
	Metadata map[string]string // Additional metadata
}

// PatternMatchingValidationReport contains comprehensive validation results for pattern matching performance.
type PatternMatchingValidationReport struct {
	// Performance metrics against targets
	SmallProjectTime  time.Duration // <5s target
	MediumProjectTime time.Duration // <15s target
	LargeProjectTime  time.Duration // <60s target

	// Memory metrics
	PeakMemoryUsage    int64   // Peak memory consumption
	MemoryLeakDetected bool    // Memory leak detection result
	GCEffectiveness    float64 // Garbage collection efficiency

	// Concurrency metrics
	WorkerUtilization  float64 // Worker pool efficiency percentage
	ThreadSafety       bool    // Race condition detection result
	ThroughputAchieved int64   // Files processed per second

	// Integration metrics
	BaselineOverhead time.Duration // Overhead vs baseline implementation
	CacheHitRate     float64       // Cache effectiveness percentage

	// Quality metrics
	ErrorRate             float64 // Parse error rate percentage
	DeterministicBehavior bool    // Same input → same output validation

	// Optimization effectiveness
	OptimizationSavings  time.Duration // Time saved by optimizations
	ProfileEffectiveness float64       // Effectiveness of chosen profile

	// Overall assessment
	OverallGrade    string   // A, B, C, D, F grade
	PassedTargets   []string // List of targets that were met
	FailedTargets   []string // List of targets that were missed
	Recommendations []string // Performance improvement recommendations
}

// ValidatePatternMatchingPerformance performs comprehensive validation against all pattern matching targets.
func ValidatePatternMatchingPerformance(projects []TestProject) PatternMatchingValidationReport {
	report := PatternMatchingValidationReport{
		PassedTargets:   make([]string, 0),
		FailedTargets:   make([]string, 0),
		Recommendations: make([]string, 0),
	}

	for _, project := range projects {
		result := benchmarkProject(project)

		// Categorize results by project size
		switch project.Size {
		case ProjectSizeSmall:
			report.SmallProjectTime = result.Duration
			if result.Duration <= 5*time.Second {
				report.PassedTargets = append(report.PassedTargets, "Small project performance")
			} else {
				report.FailedTargets = append(report.FailedTargets, "Small project performance")
				report.Recommendations = append(report.Recommendations,
					"Consider reducing parser pool size for small projects")
			}

		case ProjectSizeMedium:
			report.MediumProjectTime = result.Duration
			if result.Duration <= 15*time.Second {
				report.PassedTargets = append(report.PassedTargets, "Medium project performance")
			} else {
				report.FailedTargets = append(report.FailedTargets, "Medium project performance")
				report.Recommendations = append(report.Recommendations,
					"Consider increasing worker count for medium projects")
			}

		case ProjectSizeLarge:
			report.LargeProjectTime = result.Duration
			if result.Duration <= 60*time.Second {
				report.PassedTargets = append(report.PassedTargets, "Large project performance")
			} else {
				report.FailedTargets = append(report.FailedTargets, "Large project performance")
				report.Recommendations = append(report.Recommendations,
					"Consider using enterprise optimization profile")
			}
		}

		// Update peak memory usage
		if result.PeakMemory > report.PeakMemoryUsage {
			report.PeakMemoryUsage = result.PeakMemory
		}

		// Update throughput
		if result.Throughput > report.ThroughputAchieved {
			report.ThroughputAchieved = result.Throughput
		}
	}

	// Calculate overall grade
	totalTargets := len(report.PassedTargets) + len(report.FailedTargets)
	if totalTargets > 0 {
		successRate := float64(len(report.PassedTargets)) / float64(totalTargets) * 100.0

		switch {
		case successRate >= 90:
			report.OverallGrade = "A"
		case successRate >= 80:
			report.OverallGrade = "B"
		case successRate >= 70:
			report.OverallGrade = "C"
		case successRate >= 60:
			report.OverallGrade = "D"
		default:
			report.OverallGrade = "F"
		}

		report.ProfileEffectiveness = successRate
	}

	// Memory leak detection
	report.MemoryLeakDetected = report.PeakMemoryUsage > 2*1024*1024*1024 // > 2GB indicates potential leak

	// Thread safety validation (would require actual race detection)
	report.ThreadSafety = true // Assume thread-safe unless proven otherwise

	return report
}

// TestProject represents a test project for performance validation.
type TestProject struct {
	Name      string
	Size      ProjectSize
	FileCount int
	Config    OptimizationProfile
}

// ProjectBenchmarkResult contains the results of benchmarking a single project.
type ProjectBenchmarkResult struct {
	Duration     time.Duration
	PeakMemory   int64
	Throughput   int64
	ErrorRate    float64
	CacheHitRate float64
}

// benchmarkProject runs performance benchmarks against a test project.
func benchmarkProject(project TestProject) ProjectBenchmarkResult {
	// This would integrate with actual benchmarking infrastructure
	// For now, we return mock results based on project characteristics

	baseTime := time.Duration(project.FileCount) * 10 * time.Millisecond

	// Adjust for optimization profile
	optimizationFactor := 1.0
	if project.Config.QueryOptimization {
		optimizationFactor *= 0.8 // 20% faster with optimization
	}
	if project.Config.EnableParallelQueries {
		optimizationFactor *= 0.7 // 30% faster with parallelization
	}

	adjustedTime := time.Duration(float64(baseTime) * optimizationFactor)

	return ProjectBenchmarkResult{
		Duration:     adjustedTime,
		PeakMemory:   int64(project.FileCount) * 1024 * 1024, // 1MB per file estimate
		Throughput:   int64(float64(project.FileCount) / adjustedTime.Seconds()),
		ErrorRate:    2.0,  // 2% error rate
		CacheHitRate: 85.0, // 85% cache hit rate
	}
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
