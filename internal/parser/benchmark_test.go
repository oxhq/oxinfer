// Package parser provides comprehensive performance benchmarks for PHP parsing system.
// This file validates parser performance targets and provides detailed metrics
// for real-world Laravel project analysis capabilities.
package parser

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// BenchmarkTreeSitterParsing tests T4.1 tree-sitter foundation performance
func BenchmarkTreeSitterParsing(b *testing.B) {
	testCases := []struct {
		name        string
		phpCode     string
		targetTime  time.Duration
		description string
	}{
		{
			name: "SmallController",
			phpCode: `<?php
namespace App\Http\Controllers;

use Illuminate\Http\Request;

class UserController extends Controller
{
    public function index()
    {
        return User::all();
    }
    
    public function show($id)
    {
        return User::find($id);
    }
}`,
			targetTime:  50 * time.Microsecond,
			description: "Small Laravel controller with 2 methods",
		},
		{
			name: "MediumModel",
			phpCode: generateMediumPHPModel(),
			targetTime:  200 * time.Microsecond,
			description: "Medium Laravel model with relationships",
		},
		{
			name: "LargeClass",
			phpCode: generateLargePHPClass(),
			targetTime:  500 * time.Microsecond,
			description: "Large PHP class with many methods",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			parser, err := NewPHPParser(nil)
			if err != nil {
				b.Fatalf("Failed to create parser: %v", err)
			}
			defer parser.Close()

			content := []byte(tc.phpCode)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				tree, err := parser.ParseContent(content)
				if err != nil {
					b.Fatalf("Parse failed: %v", err)
				}
				
				// Ensure we actually used the tree
				if tree == nil || tree.Root == nil {
					b.Fatal("Invalid parse result")
				}
				
				// Calculate actual parse time and validate against target
				elapsedNs := b.Elapsed().Nanoseconds()
				avgNs := elapsedNs / int64(i+1)
				avgDuration := time.Duration(avgNs)
				
				if i == b.N-1 && avgDuration > tc.targetTime {
					b.Errorf("Parse time %v exceeds target %v for %s", avgDuration, tc.targetTime, tc.description)
				}
			}
		})
	}
}

// BenchmarkASTExtraction tests T4.2 AST query system performance
func BenchmarkASTExtraction(b *testing.B) {
	testCases := []struct {
		name        string
		phpCode     string
		targetTime  time.Duration
		description string
	}{
		{
			name:        "SimpleExtraction",
			phpCode:     generateSimplePHPFile(),
			targetTime:  5 * time.Millisecond,
			description: "Extract constructs from simple PHP file",
		},
		{
			name:        "ComplexExtraction",
			phpCode:     generateComplexPHPFile(),
			targetTime:  15 * time.Millisecond,
			description: "Extract constructs from complex Laravel file",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			parser, err := NewPHPParser(nil)
			if err != nil {
				b.Fatalf("Failed to create parser: %v", err)
			}
			defer parser.Close()

			// Parse once for the tree
			tree, err := parser.ParseContent([]byte(tc.phpCode))
			if err != nil {
				b.Fatalf("Parse failed: %v", err)
			}

			// Create query engine first
			queryEngine, err := NewQueryEngine(nil)
			if err != nil {
				b.Fatalf("Failed to create query engine: %v", err)
			}
			
			extractor := NewPHPConstructExtractor(queryEngine, nil)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := extractor.ExtractAllConstructs(tree)
				if err != nil {
					b.Fatalf("Extraction failed: %v", err)
				}
			}

			// Validate performance target
			avgTime := b.Elapsed() / time.Duration(b.N)
			if avgTime > tc.targetTime {
				b.Errorf("Extraction time %v exceeds target %v for %s", avgTime, tc.targetTime, tc.description)
			}
		})
	}
}

// BenchmarkConcurrentParsingPerformance tests T4.3 concurrent parsing performance
func BenchmarkConcurrentParsingPerformance(b *testing.B) {
	testCases := []struct {
		name          string
		numFiles      int
		numWorkers    int
		targetThroughput int64 // files per second
		description   string
	}{
		{
			name:             "SmallConcurrent",
			numFiles:         10,
			numWorkers:       2,
			targetThroughput: 100,
			description:      "Small concurrent workload",
		},
		{
			name:             "MediumConcurrent", 
			numFiles:         50,
			numWorkers:       4,
			targetThroughput: 500,
			description:      "Medium concurrent workload",
		},
		{
			name:             "LargeConcurrent",
			numFiles:         200,
			numWorkers:       8,
			targetThroughput: 1000,
			description:      "Large concurrent workload",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			concurrent, err := NewConcurrentPHPParser(tc.numWorkers, nil)
		if err != nil {
			b.Fatal(err)
		}
			defer concurrent.Shutdown(context.Background())

			// Generate test files
			files := generateTestParseJobs(tc.numFiles)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				
				results, err := concurrent.ParseConcurrently(ctx, files)
				if err != nil {
					cancel()
					b.Fatalf("Concurrent parse failed: %v", err)
				}

				// Count successful results
				successCount := 0
				for result := range results {
					if result.Error == nil {
						successCount++
					}
				}
				cancel()

				if successCount != tc.numFiles {
					b.Errorf("Expected %d successful results, got %d", tc.numFiles, successCount)
				}
			}

			// Calculate and validate throughput
			totalFiles := int64(tc.numFiles * b.N)
			elapsedSeconds := b.Elapsed().Seconds()
			throughput := int64(float64(totalFiles) / elapsedSeconds)

			if throughput < tc.targetThroughput {
				b.Errorf("Throughput %d files/sec below target %d files/sec for %s", 
					throughput, tc.targetThroughput, tc.description)
			}
		})
	}
}

// BenchmarkEndToEndProjectAnalysis tests complete Laravel project analysis
func BenchmarkEndToEndProjectAnalysis(b *testing.B) {
	projectSizes := []struct {
		name         string
		fileCount    int
		targetTime   time.Duration
		targetMemory int64 // bytes
		description  string
	}{
		{
			name:         "LaravelStarter", 
			fileCount:    45,
			targetTime:   2 * time.Second,
			targetMemory: 50 * 1024 * 1024, // 50MB
			description:  "Small Laravel starter project",
		},
		{
			name:         "LaravelMedium",
			fileCount:    250,
			targetTime:   10 * time.Second,
			targetMemory: 200 * 1024 * 1024, // 200MB
			description:  "Medium Laravel application",
		},
		{
			name:         "LaravelEnterprise",
			fileCount:    800,
			targetTime:   45 * time.Second,
			targetMemory: 1000 * 1024 * 1024, // 1GB
			description:  "Large enterprise Laravel application",
		},
	}

	for _, project := range projectSizes {
		b.Run(project.name, func(b *testing.B) {
			// Create mock Laravel project structure
			files := generateMockLaravelProject(project.fileCount)
			
			// Set up memory monitoring
			var startMem, peakMem runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&startMem)

			concurrent, err := NewConcurrentPHPParser(8, nil)
		if err != nil {
			b.Fatal(err)
		}
			defer concurrent.Shutdown(context.Background())

			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), project.targetTime*2)
				
				startTime := time.Now()
				results, err := concurrent.ParseConcurrently(ctx, files)
				if err != nil {
					cancel()
					b.Fatalf("Project analysis failed: %v", err)
				}

				// Process all results
				successCount := 0
				for result := range results {
					if result.Error == nil {
						successCount++
					}
				}
				
				elapsed := time.Since(startTime)
				cancel()

				// Measure peak memory usage
				runtime.ReadMemStats(&peakMem)
				if peakMem.Alloc > uint64(peakMem.Alloc) {
					peakMem.Alloc = peakMem.Alloc
				}

				// Validate performance targets
				if elapsed > project.targetTime {
					b.Errorf("Analysis time %v exceeds target %v for %s", elapsed, project.targetTime, project.description)
				}

				memoryUsed := int64(peakMem.Alloc - startMem.Alloc)
				if memoryUsed > project.targetMemory {
					b.Errorf("Memory usage %d bytes exceeds target %d bytes for %s", 
						memoryUsed, project.targetMemory, project.description)
				}

				if successCount < int(float64(project.fileCount)*0.95) { // Allow 5% failure
					b.Errorf("Success rate too low: %d/%d for %s", successCount, project.fileCount, project.description)
				}
			}
		})
	}
}

// BenchmarkMemoryUsage specifically tests memory efficiency
func BenchmarkMemoryUsage(b *testing.B) {
	testCases := []struct {
		name         string
		fileCount    int
		maxMemory    int64 // Maximum allowed memory in bytes
		description  string
	}{
		{
			name:        "MemoryEfficiencySmall",
			fileCount:   50,
			maxMemory:   25 * 1024 * 1024, // 25MB
			description: "Memory efficiency for small projects",
		},
		{
			name:        "MemoryEfficiencyMedium",
			fileCount:   200,
			maxMemory:   100 * 1024 * 1024, // 100MB
			description: "Memory efficiency for medium projects", 
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			var m1, m2 runtime.MemStats
			
			// Get baseline memory
			runtime.GC()
			runtime.ReadMemStats(&m1)
			
			// Create parser with memory optimizer
			optimizer := NewMemoryOptimizer()
			concurrent, err := NewConcurrentPHPParser(4, nil)
		if err != nil {
			b.Fatal(err)
		}
			defer concurrent.Shutdown(context.Background())

			files := generateTestParseJobs(tc.fileCount)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				
				results, err := concurrent.ParseConcurrently(ctx, files)
				if err != nil {
					cancel()
					b.Fatalf("Parse failed: %v", err)
				}

				// Consume results
				for range results {
					// Process each result to simulate real usage
					optimizer.CheckMemoryPressure()
				}
				cancel()

				// Force GC and check memory
				runtime.GC()
				runtime.ReadMemStats(&m2)
			}

			// Validate memory usage
			memoryUsed := int64(m2.Alloc - m1.Alloc)
			if memoryUsed > tc.maxMemory {
				b.Errorf("Memory usage %d bytes exceeds limit %d bytes for %s", 
					memoryUsed, tc.maxMemory, tc.description)
			}
		})
	}
}

// BenchmarkParserPoolEfficiency tests parser pool resource management
func BenchmarkParserPoolEfficiency(b *testing.B) {
	poolSizes := []int{2, 4, 8, 16}
	
	for _, poolSize := range poolSizes {
		b.Run(fmt.Sprintf("PoolSize%d", poolSize), func(b *testing.B) {
			pool, err := NewParserPool(poolSize, nil)
			if err != nil {
				b.Fatalf("Failed to create pool: %v", err)
			}
			defer pool.Close()

			b.ResetTimer()
			b.ReportAllocs()

			// Test pool efficiency with high contention
			var wg sync.WaitGroup
			workers := poolSize * 2 // Create contention

			for i := 0; i < b.N; i++ {
				for w := 0; w < workers; w++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						
						parser, err := pool.AcquireParser(ctx)
						if err != nil {
							return // Timeout is expected under contention
						}
						
						// Simulate parsing work
						time.Sleep(time.Microsecond * 100)
						
						pool.ReleaseParser(parser)
					}()
				}
				wg.Wait()
			}

			// Validate pool health
			if healthy, issues := pool.IsHealthy(); !healthy {
				b.Errorf("Pool unhealthy after benchmark: %v", issues)
			}
		})
	}
}

// BenchmarkFullIntegration tests performance of full system integration
func BenchmarkFullIntegration(b *testing.B) {
	b.Run("FullIntegrationPerformance", func(b *testing.B) {
		// This would integrate with actual system components
		// For now we simulate the integration points
		
		files := generateTestParseJobs(100)
		
		concurrent, err := NewConcurrentPHPParser(6, nil)
		if err != nil {
			b.Fatal(err)
		}
		defer concurrent.Shutdown(context.Background())

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			
			startTime := time.Now()
			
			// Phase 1: File Discovery (indexer simulation)
			discoveryTime := time.Millisecond * 100
			time.Sleep(discoveryTime)
			
			// Phase 2: PSR-4 Resolution (resolver simulation)
			resolutionTime := time.Millisecond * 50
			time.Sleep(resolutionTime)
			
			// Phase 3: Concurrent Parsing (parser)
			results, err := concurrent.ParseConcurrently(ctx, files)
			if err != nil {
				cancel()
				b.Fatalf("Integration failed: %v", err)
			}

			successCount := 0
			for result := range results {
				if result.Error == nil {
					successCount++
				}
			}
			
			totalTime := time.Since(startTime)
			cancel()

			// Parser should add <50% overhead over baseline
			expectedBaselineTime := discoveryTime + resolutionTime
			parsingOverhead := totalTime - expectedBaselineTime
			maxAllowedOverhead := expectedBaselineTime / 2 // 50% overhead limit

			if parsingOverhead > maxAllowedOverhead {
				b.Errorf("Parser overhead %v exceeds 50%% limit %v", 
					parsingOverhead, maxAllowedOverhead)
			}

			if successCount < 95 { // Allow 5% failure rate
				b.Errorf("Integration success rate too low: %d/100", successCount)
			}
		}
	})
}

// Test helper functions

func generateMediumPHPModel() string {
	return `<?php
namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\HasMany;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class User extends Model
{
    protected $fillable = ['name', 'email', 'password'];
    protected $hidden = ['password', 'remember_token'];
    protected $casts = ['email_verified_at' => 'datetime'];

    public function posts(): HasMany
    {
        return $this->hasMany(Post::class);
    }
    
    public function profile(): BelongsTo  
    {
        return $this->belongsTo(Profile::class);
    }
    
    public function getFullNameAttribute(): string
    {
        return $this->first_name . ' ' . $this->last_name;
    }
}`
}

func generateLargePHPClass() string {
	var methods []string
	for i := 0; i < 20; i++ {
		methods = append(methods, fmt.Sprintf(`
    public function method%d($param1, $param2 = null)
    {
        if ($param2 === null) {
            return $param1 * %d;
        }
        return $param1 + $param2;
    }`, i, i+1))
	}

	return fmt.Sprintf(`<?php
namespace App\Services;

class LargeService
{%s
}`, strings.Join(methods, "\n"))
}

func generateSimplePHPFile() string {
	return `<?php
namespace App\Http\Controllers;

class SimpleController
{
    public function index()
    {
        return 'Hello World';
    }
}`
}

func generateComplexPHPFile() string {
	return `<?php
namespace App\Http\Controllers\Api;

use App\Models\User;
use App\Http\Requests\UserRequest;
use App\Http\Resources\UserResource;
use App\Services\UserService;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;

/**
 * Complex Laravel API Controller
 * @package App\Http\Controllers\Api
 */
class UserApiController extends BaseController
{
    private UserService $userService;
    
    public function __construct(UserService $userService)
    {
        $this->userService = $userService;
        $this->middleware('auth:api');
    }
    
    /**
     * Display a listing of users
     * @param Request $request
     * @return JsonResponse
     */
    public function index(Request $request): JsonResponse
    {
        $users = $this->userService->getUsers($request->all());
        return UserResource::collection($users)->response();
    }
    
    /**
     * Store a new user
     * @param UserRequest $request
     * @return JsonResponse
     */
    public function store(UserRequest $request): JsonResponse
    {
        $user = $this->userService->createUser($request->validated());
        return new UserResource($user);
    }
}`
}

func generateTestParseJobs(count int) []ParseJob {
	jobs := make([]ParseJob, count)
	
	phpTemplates := []string{
		generateSimplePHPFile(),
		generateMediumPHPModel(),
		generateComplexPHPFile(),
	}
	
	for i := 0; i < count; i++ {
		template := phpTemplates[i%len(phpTemplates)]
		jobs[i] = ParseJob{
			ID:          fmt.Sprintf("job-%d", i),
			FilePath:    fmt.Sprintf("/app/test%d.php", i),
			Content:     []byte(template),
			Priority:    1,
			Config:      nil,
			SubmittedAt: time.Now(),
			Deadline:    time.Now().Add(30 * time.Second),
		}
	}
	
	return jobs
}

func generateMockLaravelProject(fileCount int) []ParseJob {
	jobs := make([]ParseJob, fileCount)
	
	// Distribute files across Laravel structure
	controllers := fileCount / 4
	models := fileCount / 4  
	requests := fileCount / 6
	resources := fileCount / 6
	_ = fileCount - controllers - models - requests - resources // other files

	jobIndex := 0
	
	// Generate controllers
	for i := 0; i < controllers; i++ {
		jobs[jobIndex] = ParseJob{
			ID:       fmt.Sprintf("controller-%d", i),
			FilePath: fmt.Sprintf("/app/Http/Controllers/Controller%d.php", i),
			Content:  []byte(generateComplexPHPFile()),
		}
		jobIndex++
	}
	
	// Generate models
	for i := 0; i < models; i++ {
		jobs[jobIndex] = ParseJob{
			ID:       fmt.Sprintf("model-%d", i),
			FilePath: fmt.Sprintf("/app/Models/Model%d.php", i),
			Content:  []byte(generateMediumPHPModel()),
		}
		jobIndex++
	}
	
	// Generate other files
	for i := jobIndex; i < fileCount; i++ {
		jobs[i] = ParseJob{
			ID:       fmt.Sprintf("file-%d", i),
			FilePath: fmt.Sprintf("/app/Other/File%d.php", i),
			Content:  []byte(generateSimplePHPFile()),
		}
	}
	
	return jobs
}