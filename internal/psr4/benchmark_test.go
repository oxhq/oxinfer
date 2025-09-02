package psr4

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// ===== END-TO-END RESOLUTION BENCHMARKS =====

// BenchmarkEndToEnd_SingleResolution benchmarks single class resolution performance
func BenchmarkEndToEnd_SingleResolution(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	fqcn := "App\\Http\\Controllers\\UserController"
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := resolver.ResolveClass(context.Background(), fqcn)
		if err != nil {
			b.Fatalf("Resolution failed: %v", err)
		}
	}
}

// BenchmarkEndToEnd_BatchResolution benchmarks batch resolution with various sizes
func BenchmarkEndToEnd_BatchResolution(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	
	// Test different batch sizes representing Laravel project scenarios
	batchSizes := []struct {
		name    string
		size    int
		classes []string
	}{
		{"Small_10", 10, generateLaravelClassNames(10)},
		{"Medium_100", 100, generateLaravelClassNames(100)},
		{"Large_500", 500, generateLaravelClassNames(500)},
		{"Enterprise_1000", 1000, generateLaravelClassNames(1000)},
	}
	
	for _, batch := range batchSizes {
		b.Run(batch.name, func(b *testing.B) {
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				for _, class := range batch.classes {
					_, _ = resolver.ResolveClass(context.Background(), class)
				}
			}
			
			// Report throughput
			classesPerSecond := float64(len(batch.classes)*b.N) / b.Elapsed().Seconds()
			b.ReportMetric(classesPerSecond, "classes/sec")
		})
	}
}

// BenchmarkEndToEnd_CacheEffectiveness compares cache hit vs miss performance
func BenchmarkEndToEnd_CacheEffectiveness(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	classes := generateLaravelClassNames(100)
	
	// Warm up cache
	ctx := context.Background()
	for _, class := range classes {
		resolver.ResolveClass(ctx, class)
	}
	
	b.Run("CacheHits", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			class := classes[i%len(classes)]
			_, _ = resolver.ResolveClass(ctx, class)
		}
	})
	
	// Create resolver without cache for miss comparison
	config := &ResolverConfig{
		ProjectRoot:  setupTestProject(b),
		ComposerPath: "composer.json",
		IncludeDev:   true,
		CacheEnabled: false,
		CacheSize:    0,
	}
	resolverNoCache, _ := NewPSR4Resolver(config)
	
	b.Run("CacheMisses", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			class := classes[i%len(classes)]
			_, _ = resolverNoCache.ResolveClass(ctx, class)
		}
	})
}

// ===== REAL-WORLD LARAVEL PROJECT BENCHMARKS =====

// BenchmarkLaravel_ProjectSimulation simulates various Laravel project sizes
func BenchmarkLaravel_ProjectSimulation(b *testing.B) {
	scenarios := []struct {
		name     string
		classes  int
		ns       int // number of namespaces
		expected time.Duration
	}{
		{"Starter_50_Classes", 50, 5, 50 * time.Millisecond},
		{"Typical_200_Classes", 200, 8, 200 * time.Millisecond},
		{"Medium_500_Classes", 500, 12, 500 * time.Millisecond},
		{"Large_1000_Classes", 1000, 15, 1 * time.Second},
		{"Enterprise_2000_Classes", 2000, 20, 2 * time.Second},
	}
	
	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			resolver := setupComplexLaravelProject(b, scenario.classes, scenario.ns)
			classes := generateComplexLaravelClasses(scenario.classes, scenario.ns)
			
			start := time.Now()
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				for _, class := range classes {
					_, _ = resolver.ResolveClass(context.Background(), class)
				}
			}
			
			elapsed := time.Since(start)
			if elapsed > scenario.expected*time.Duration(b.N) {
				b.Logf("WARNING: Performance target missed. Expected <%v per iteration, got %v",
					scenario.expected, elapsed/time.Duration(b.N))
			}
			
			// Report key metrics
			classesPerSecond := float64(len(classes)*b.N) / b.Elapsed().Seconds()
			b.ReportMetric(classesPerSecond, "classes/sec")
		})
	}
}

// BenchmarkLaravel_ComplexComposer benchmarks complex composer.json scenarios
func BenchmarkLaravel_ComplexComposer(b *testing.B) {
	// Create complex composer.json with multiple namespaces, dev dependencies
	complexComposer := `{
  "name": "laravel/complex-app",
  "autoload": {
    "psr-4": {
      "App\\": "app/",
      "Database\\Factories\\": "database/factories/",
      "Database\\Seeders\\": "database/seeders/",
      "Modules\\User\\": "modules/user/src/",
      "Modules\\Order\\": "modules/order/src/",
      "Modules\\Product\\": "modules/product/src/",
      "Services\\Payment\\": "services/payment/src/",
      "Services\\Notification\\": "services/notification/src/"
    }
  },
  "autoload-dev": {
    "psr-4": {
      "Tests\\": "tests/",
      "Modules\\User\\Tests\\": "modules/user/tests/",
      "Modules\\Order\\Tests\\": "modules/order/tests/",
      "Modules\\Product\\Tests\\": "modules/product/tests/",
      "Services\\Payment\\Tests\\": "services/payment/tests/",
      "Services\\Notification\\Tests\\": "services/notification/tests/"
    }
  }
}`
	
	projectDir := setupCustomComposerProject(b, complexComposer)
	config := &ResolverConfig{
		ProjectRoot:  projectDir,
		ComposerPath: "composer.json",
		IncludeDev:   true,
		CacheEnabled: true,
		CacheSize:    2000,
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}
	
	// Generate classes across all namespaces
	classes := []string{
		"App\\Http\\Controllers\\UserController",
		"App\\Models\\User",
		"Database\\Factories\\UserFactory",
		"Database\\Seeders\\UserSeeder",
		"Modules\\User\\Controllers\\ProfileController",
		"Modules\\Order\\Models\\Order",
		"Modules\\Product\\Services\\ProductService",
		"Services\\Payment\\Providers\\StripeProvider",
		"Services\\Notification\\Channels\\SlackChannel",
		"Tests\\Feature\\UserTest",
		"Modules\\User\\Tests\\Unit\\UserServiceTest",
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		for _, class := range classes {
			_, _ = resolver.ResolveClass(context.Background(), class)
		}
	}
}

// ===== SCALABILITY AND CONCURRENCY BENCHMARKS =====

// BenchmarkConcurrency_ParallelResolution tests concurrent resolution performance
func BenchmarkConcurrency_ParallelResolution(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	classes := generateLaravelClassNames(100)
	
	workerCounts := []int{1, 5, 10, 25, 50, 100}
	
	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workers), func(b *testing.B) {
			b.ReportAllocs()
			
			b.RunParallel(func(pb *testing.PB) {
				ctx := context.Background()
				classIndex := 0
				
				for pb.Next() {
					class := classes[classIndex%len(classes)]
					classIndex++
					_, _ = resolver.ResolveClass(ctx, class)
				}
			})
		})
	}
}

// BenchmarkConcurrency_CacheContention tests cache performance under load
func BenchmarkConcurrency_CacheContention(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	classes := generateLaravelClassNames(50) // Smaller set for more cache hits
	
	// Warm cache
	ctx := context.Background()
	for _, class := range classes {
		resolver.ResolveClass(ctx, class)
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			class := classes[rand.Intn(len(classes))]
			_, _ = resolver.ResolveClass(ctx, class)
		}
	})
}

// BenchmarkScalability_MemoryUsage tests memory usage patterns with large datasets
func BenchmarkScalability_MemoryUsage(b *testing.B) {
	var memBefore, memAfter runtime.MemStats
	
	dataSizes := []struct {
		name    string
		classes int
		cache   int
	}{
		{"Small_100_Classes", 100, 200},
		{"Medium_500_Classes", 500, 1000},
		{"Large_2000_Classes", 2000, 4000},
		{"XLarge_5000_Classes", 5000, 10000},
	}
	
	for _, dataSize := range dataSizes {
		b.Run(dataSize.name, func(b *testing.B) {
			runtime.GC()
			runtime.ReadMemStats(&memBefore)
			
			resolver := setupLargeBenchmarkResolver(b, dataSize.cache)
			classes := generateLaravelClassNames(dataSize.classes)
			
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				for _, class := range classes {
					_, _ = resolver.ResolveClass(context.Background(), class)
				}
			}
			
			runtime.ReadMemStats(&memAfter)
			b.ReportMetric(float64(memAfter.Alloc-memBefore.Alloc)/1024/1024, "MB")
			b.ReportMetric(float64(memAfter.TotalAlloc-memBefore.TotalAlloc)/1024/1024, "total_MB")
		})
	}
}

// BenchmarkContext_Cancellation tests context cancellation overhead
func BenchmarkContext_Cancellation(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	classes := generateLaravelClassNames(10)
	
	b.Run("WithTimeout", func(b *testing.B) {
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			class := classes[i%len(classes)]
			_, _ = resolver.ResolveClass(ctx, class)
			cancel()
		}
	})
	
	b.Run("WithoutTimeout", func(b *testing.B) {
		b.ReportAllocs()
		ctx := context.Background()
		
		for i := 0; i < b.N; i++ {
			class := classes[i%len(classes)]
			_, _ = resolver.ResolveClass(ctx, class)
		}
	})
}

// ===== CACHE-SPECIFIC BENCHMARKS =====

// BenchmarkCache_Operations benchmarks cache operation performance
func BenchmarkCache_Operations(b *testing.B) {
	cache := NewPSR4Cache(1000)
	classes := generateLaravelClassNames(100)
	
	b.Run("Set", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			class := classes[i%len(classes)]
			path := "/path/to/" + class + ".php"
			cache.SetClass(class, path)
		}
	})
	
	// Populate cache for Get benchmark
	for i, class := range classes {
		path := fmt.Sprintf("/path/to/%s_%d.php", class, i)
		cache.SetClass(class, path)
	}
	
	b.Run("Get_Hit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			class := classes[i%len(classes)]
			_, _ = cache.GetClass(class)
		}
	})
	
	b.Run("Get_Miss", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			class := fmt.Sprintf("NonExistent\\Class\\%d", i)
			_, _ = cache.GetClass(class)
		}
	})
}

// BenchmarkCache_SizeEfficiency tests cache performance with various sizes
func BenchmarkCache_SizeEfficiency(b *testing.B) {
	cacheSizes := []int{100, 500, 1000, 2000, 5000}
	classes := generateLaravelClassNames(1000)
	
	for _, size := range cacheSizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			cache := NewPSR4Cache(size)
			
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				class := classes[i%len(classes)]
				path := "/path/to/" + class + ".php"
				
				// Set and then get to test both operations
				cache.SetClass(class, path)
				_, _ = cache.GetClass(class)
			}
			
			stats := cache.GetStats()
			b.ReportMetric(float64(stats.Size), "cached_items")
		})
	}
}

// ===== HELPER FUNCTIONS =====

// setupBenchmarkResolver creates a resolver optimized for benchmarking
func setupBenchmarkResolver(b *testing.B) *DefaultPSR4Resolver {
	b.Helper()
	
	config := &ResolverConfig{
		ProjectRoot:  setupTestProject(b),
		ComposerPath: "composer.json",
		IncludeDev:   true,
		CacheEnabled: true,
		CacheSize:    1000,
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}
	
	return resolver
}

// setupLargeBenchmarkResolver creates a resolver with larger cache for scalability tests
func setupLargeBenchmarkResolver(b *testing.B, cacheSize int) *DefaultPSR4Resolver {
	b.Helper()
	
	config := &ResolverConfig{
		ProjectRoot:  setupTestProject(b),
		ComposerPath: "composer.json",
		IncludeDev:   true,
		CacheEnabled: true,
		CacheSize:    cacheSize,
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		b.Fatalf("Failed to create resolver: %v", err)
	}
	
	return resolver
}

// setupComplexLaravelProject creates a project structure simulating a complex Laravel app
func setupComplexLaravelProject(b *testing.B, numClasses, numNamespaces int) *DefaultPSR4Resolver {
	b.Helper()
	
	// Create a more complex composer.json with multiple namespaces
	namespaces := make(map[string]string)
	namespaces["App\\"] = "app/"
	
	for i := 0; i < numNamespaces; i++ {
		ns := fmt.Sprintf("Module%d\\", i)
		path := fmt.Sprintf("modules/module%d/", i)
		namespaces[ns] = path
	}
	
	// For benchmarking, we'll use a simplified setup
	projectDir := setupTestProject(b)
	config := &ResolverConfig{
		ProjectRoot:  projectDir,
		ComposerPath: "composer.json",
		IncludeDev:   true,
		CacheEnabled: true,
		CacheSize:    numClasses * 2, // Cache sized for the expected load
	}
	
	resolver, err := NewPSR4Resolver(config)
	if err != nil {
		b.Fatalf("Failed to create complex resolver: %v", err)
	}
	
	return resolver
}

// setupTestProject creates a temporary project for benchmarking
func setupTestProject(b *testing.B) string {
	b.Helper()
	
	tmpDir := b.TempDir()
	
	// Create a basic Laravel-style composer.json
	composerJSON := `{
  "name": "test/laravel-app",
  "autoload": {
    "psr-4": {
      "App\\": "app/",
      "Database\\Factories\\": "database/factories/",
      "Database\\Seeders\\": "database/seeders/"
    }
  },
  "autoload-dev": {
    "psr-4": {
      "Tests\\": "tests/"
    }
  }
}`
	
	composerPath := filepath.Join(tmpDir, "composer.json")
	if err := os.WriteFile(composerPath, []byte(composerJSON), 0644); err != nil {
		b.Fatalf("Failed to create composer.json: %v", err)
	}
	
	// Create directory structure
	dirs := []string{"app", "database/factories", "database/seeders", "tests"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			b.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
	
	return tmpDir
}

// setupCustomComposerProject creates a project with custom composer.json
func setupCustomComposerProject(b *testing.B, composerContent string) string {
	b.Helper()
	
	tmpDir := b.TempDir()
	composerPath := filepath.Join(tmpDir, "composer.json")
	
	if err := os.WriteFile(composerPath, []byte(composerContent), 0644); err != nil {
		b.Fatalf("Failed to create custom composer.json: %v", err)
	}
	
	return tmpDir
}

// generateLaravelClassNames generates realistic Laravel class names for testing
func generateLaravelClassNames(count int) []string {
	patterns := []string{
		"App\\Http\\Controllers\\%sController",
		"App\\Models\\%s",
		"App\\Services\\%sService",
		"App\\Jobs\\Process%sJob",
		"App\\Events\\%sCreated",
		"App\\Listeners\\Send%sNotification",
		"App\\Http\\Requests\\%sRequest",
		"Database\\Factories\\%sFactory",
		"Database\\Seeders\\%sSeeder",
		"Tests\\Feature\\%sTest",
		"Tests\\Unit\\%sTest",
	}
	
	entities := []string{
		"User", "Order", "Product", "Customer", "Invoice", "Payment", "Article", "Category",
		"Comment", "Tag", "Role", "Permission", "Team", "Project", "Task", "File", "Report",
		"Notification", "Message", "Address", "Company", "Contact", "Event", "Booking",
	}
	
	var classes []string
	for i := 0; i < count; i++ {
		pattern := patterns[i%len(patterns)]
		entity := entities[i%len(entities)]
		if i >= len(entities) {
			entity = fmt.Sprintf("%s%d", entity, i/len(entities))
		}
		classes = append(classes, fmt.Sprintf(pattern, entity))
	}
	
	return classes
}

// generateComplexLaravelClasses generates classes across multiple namespaces
func generateComplexLaravelClasses(count, numNamespaces int) []string {
	var classes []string
	
	// Base App namespace classes
	baseClasses := generateLaravelClassNames(count / 2)
	classes = append(classes, baseClasses...)
	
	// Module-specific classes
	for i := 0; i < numNamespaces && len(classes) < count; i++ {
		moduleClasses := []string{
			fmt.Sprintf("Module%d\\Controllers\\Controller", i),
			fmt.Sprintf("Module%d\\Models\\Model", i),
			fmt.Sprintf("Module%d\\Services\\Service", i),
		}
		classes = append(classes, moduleClasses...)
	}
	
	// Pad with additional classes if needed
	for len(classes) < count {
		classes = append(classes, fmt.Sprintf("Generated\\Class%d", len(classes)))
	}
	
	return classes[:count]
}

// ===== VALIDATION BENCHMARKS =====

// BenchmarkValidation_PerformanceTargets validates PSR-4 resolver performance targets
func BenchmarkValidation_PerformanceTargets(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	
	// PSR-4 resolver performance targets:
	// - Medium project (200-600 PHP files): <10s cold/full, <2s incremental with cache
	// - Stable memory usage; no catastrophic spikes
	
	b.Run("MediumProject_ColdStart", func(b *testing.B) {
		classes := generateLaravelClassNames(400) // Medium project size
		target := 10 * time.Second
		
		start := time.Now()
		for i := 0; i < b.N; i++ {
			// Clear cache to simulate cold start
			resolver.cache.Clear()
			
			for _, class := range classes {
				_, _ = resolver.ResolveClass(context.Background(), class)
			}
		}
		elapsed := time.Since(start) / time.Duration(b.N)
		
		if elapsed > target {
			b.Errorf("Cold start performance target missed: %v > %v", elapsed, target)
		}
		
		b.ReportMetric(elapsed.Seconds(), "seconds")
	})
	
	b.Run("MediumProject_IncrementalWithCache", func(b *testing.B) {
		classes := generateLaravelClassNames(400)
		target := 2 * time.Second
		
		// Warm cache
		ctx := context.Background()
		for _, class := range classes {
			resolver.ResolveClass(ctx, class)
		}
		
		start := time.Now()
		for i := 0; i < b.N; i++ {
			for _, class := range classes {
				_, _ = resolver.ResolveClass(ctx, class)
			}
		}
		elapsed := time.Since(start) / time.Duration(b.N)
		
		if elapsed > target {
			b.Errorf("Incremental performance target missed: %v > %v", elapsed, target)
		}
		
		b.ReportMetric(elapsed.Seconds(), "seconds")
	})
}

// BenchmarkValidation_CrossPlatform tests cross-platform performance consistency
func BenchmarkValidation_CrossPlatform(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	classes := generateLaravelClassNames(100)
	
	var times []time.Duration
	var mu sync.Mutex
	
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		
		for pb.Next() {
			start := time.Now()
			for _, class := range classes {
				_, _ = resolver.ResolveClass(ctx, class)
			}
			elapsed := time.Since(start)
			
			mu.Lock()
			times = append(times, elapsed)
			mu.Unlock()
		}
	})
	
	// Calculate variance to check consistency
	if len(times) > 1 {
		var sum, variance time.Duration
		for _, t := range times {
			sum += t
		}
		mean := sum / time.Duration(len(times))
		
		for _, t := range times {
			diff := t - mean
			if diff < 0 {
				diff = -diff
			}
			variance += diff * diff
		}
		variance /= time.Duration(len(times))
		
		b.ReportMetric(mean.Seconds(), "mean_seconds")
		b.ReportMetric(variance.Seconds(), "variance_seconds")
	}
}

// BenchmarkValidation_ThreadSafety tests thread safety under high concurrency
func BenchmarkValidation_ThreadSafety(b *testing.B) {
	resolver := setupBenchmarkResolver(b)
	classes := generateLaravelClassNames(50)
	
	// High concurrency test
	var wg sync.WaitGroup
	numGoroutines := runtime.NumCPU() * 10
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		wg.Add(numGoroutines)
		
		for j := 0; j < numGoroutines; j++ {
			go func(goroutineID int) {
				defer wg.Done()
				ctx := context.Background()
				
				for k := 0; k < len(classes); k++ {
					class := classes[k%len(classes)]
					_, _ = resolver.ResolveClass(ctx, class)
				}
			}(j)
		}
		
		wg.Wait()
	}
}