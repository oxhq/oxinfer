package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
)

func TestNewFileCache(t *testing.T) {
	config := &manifest.CacheConfig{
		Enabled: &[]bool{true}[0],
		Kind:    &[]string{CacheModeModTime}[0],
	}

	cache := NewFileCache(config)

	if cache == nil {
		t.Fatal("NewFileCache returned nil")
	}

	if cache.config != config {
		t.Error("Config not set correctly")
	}

	if cache.maxSize != DefaultCacheSize {
		t.Errorf("Expected maxSize %d, got %d", DefaultCacheSize, cache.maxSize)
	}

	if cache.cache == nil {
		t.Error("Cache map not initialized")
	}

	if cache.head == nil || cache.tail == nil {
		t.Error("LRU list not initialized correctly")
	}

	if cache.head.next != cache.tail || cache.tail.prev != cache.head {
		t.Error("LRU list not linked correctly")
	}
}

func TestGetCacheEntry_NotFound(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	entry, err := cache.GetCacheEntry("nonexistent.php")

	if err == nil {
		t.Error("Expected error for non-existent cache entry")
	}

	if entry != nil {
		t.Error("Expected nil entry for non-existent cache")
	}

	if !strings.Contains(err.Error(), "cache entry not found") {
		t.Errorf("Expected 'cache entry not found' error, got: %v", err)
	}
}

func TestGetCacheEntry_InvalidKey(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	entry, err := cache.GetCacheEntry("")

	if err == nil {
		t.Error("Expected error for empty cache key")
	}

	if entry != nil {
		t.Error("Expected nil entry for invalid key")
	}

	if !strings.Contains(err.Error(), "invalid cache key") {
		t.Errorf("Expected 'invalid cache key' error, got: %v", err)
	}
}

func TestSetCacheEntry_Success(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	entry := &CacheEntry{
		Path:        "test.php",
		ModTime:     time.Now(),
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	err := cache.SetCacheEntry("test.php", entry)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify entry was stored
	if len(cache.cache) != 1 {
		t.Errorf("Expected cache size 1, got %d", len(cache.cache))
	}

	stored, exists := cache.cache["test.php"]
	if !exists {
		t.Error("Entry not found in cache")
	}

	if stored.entry != entry {
		t.Error("Stored entry doesn't match original")
	}
}

func TestSetCacheEntry_InvalidInputs(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	// Test empty key
	err := cache.SetCacheEntry("", &CacheEntry{})
	if err == nil || !strings.Contains(err.Error(), "invalid cache key") {
		t.Errorf("Expected invalid cache key error, got: %v", err)
	}

	// Test nil entry
	err = cache.SetCacheEntry("test.php", nil)
	if err == nil || !strings.Contains(err.Error(), "cache entry is nil") {
		t.Errorf("Expected nil entry error, got: %v", err)
	}
}

func TestSetCacheEntry_UpdateExisting(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	entry1 := &CacheEntry{
		Path:        "test.php",
		ModTime:     time.Now().Add(-time.Hour),
		ProcessedAt: time.Now().Add(-time.Hour),
		Valid:       true,
	}

	entry2 := &CacheEntry{
		Path:        "test.php",
		ModTime:     time.Now(),
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	// Set first entry
	err := cache.SetCacheEntry("test.php", entry1)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Update with second entry
	err = cache.SetCacheEntry("test.php", entry2)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify only one entry exists and it's the updated one
	if len(cache.cache) != 1 {
		t.Errorf("Expected cache size 1, got %d", len(cache.cache))
	}

	stored, exists := cache.cache["test.php"]
	if !exists {
		t.Error("Entry not found in cache")
	}

	if stored.entry != entry2 {
		t.Error("Entry was not updated correctly")
	}
}

func TestInvalidateCache_Success(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	entry := &CacheEntry{
		Path:        "test.php",
		ModTime:     time.Now(),
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	// Set entry
	err := cache.SetCacheEntry("test.php", entry)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Invalidate entry
	err = cache.InvalidateCache("test.php")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify entry was removed
	if len(cache.cache) != 0 {
		t.Errorf("Expected cache size 0, got %d", len(cache.cache))
	}
}

func TestInvalidateCache_NotFound(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	err := cache.InvalidateCache("nonexistent.php")
	if err == nil {
		t.Error("Expected error for non-existent cache entry")
	}

	if !strings.Contains(err.Error(), "cache entry not found") {
		t.Errorf("Expected 'cache entry not found' error, got: %v", err)
	}
}

func TestLRUEviction(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})
	cache.maxSize = 3 // Set small cache size for testing

	entries := []*CacheEntry{
		{Path: "file1.php", ModTime: time.Now(), ProcessedAt: time.Now(), Valid: true},
		{Path: "file2.php", ModTime: time.Now(), ProcessedAt: time.Now(), Valid: true},
		{Path: "file3.php", ModTime: time.Now(), ProcessedAt: time.Now(), Valid: true},
		{Path: "file4.php", ModTime: time.Now(), ProcessedAt: time.Now(), Valid: true},
	}

	// Fill cache to capacity
	for i := 0; i < 3; i++ {
		err := cache.SetCacheEntry(entries[i].Path, entries[i])
		if err != nil {
			t.Errorf("Unexpected error setting entry %d: %v", i, err)
		}
	}

	if len(cache.cache) != 3 {
		t.Errorf("Expected cache size 3, got %d", len(cache.cache))
	}

	// Add fourth entry, should evict LRU (file1.php)
	err := cache.SetCacheEntry(entries[3].Path, entries[3])
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if len(cache.cache) != 3 {
		t.Errorf("Expected cache size 3 after eviction, got %d", len(cache.cache))
	}

	// file1.php should be evicted
	_, exists := cache.cache["file1.php"]
	if exists {
		t.Error("Expected file1.php to be evicted")
	}

	// Other files should still exist
	for _, path := range []string{"file2.php", "file3.php", "file4.php"} {
		_, exists := cache.cache[path]
		if !exists {
			t.Errorf("Expected %s to still exist in cache", path)
		}
	}
}

func TestLRUOrdering(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})
	cache.maxSize = 2

	// Create temporary files for proper validation
	tempFile1 := createTempFile(t, "file1.php", "content1")
	tempFile2 := createTempFile(t, "file2.php", "content2")
	tempFile3 := createTempFile(t, "file3.php", "content3")
	defer os.Remove(tempFile1)
	defer os.Remove(tempFile2)
	defer os.Remove(tempFile3)

	stat1, _ := os.Stat(tempFile1)
	stat2, _ := os.Stat(tempFile2)
	stat3, _ := os.Stat(tempFile3)

	entry1 := &CacheEntry{Path: tempFile1, ModTime: stat1.ModTime(), ProcessedAt: time.Now(), Valid: true}
	entry2 := &CacheEntry{Path: tempFile2, ModTime: stat2.ModTime(), ProcessedAt: time.Now(), Valid: true}
	entry3 := &CacheEntry{Path: tempFile3, ModTime: stat3.ModTime(), ProcessedAt: time.Now(), Valid: true}

	// Add two entries
	cache.SetCacheEntry("file1.php", entry1)
	cache.SetCacheEntry("file2.php", entry2)

	// Access file1.php to make it most recently used
	_, err := cache.GetCacheEntry("file1.php")
	if err != nil {
		t.Errorf("Unexpected error accessing file1.php: %v", err)
	}

	// Add third entry, should evict file2.php (LRU)
	cache.SetCacheEntry("file3.php", entry3)

	// file2.php should be evicted
	_, exists := cache.cache["file2.php"]
	if exists {
		t.Error("Expected file2.php to be evicted (was LRU)")
	}

	// file1.php and file3.php should still exist
	_, exists = cache.cache["file1.php"]
	if !exists {
		t.Error("Expected file1.php to still exist (was recently accessed)")
	}

	_, exists = cache.cache["file3.php"]
	if !exists {
		t.Error("Expected file3.php to still exist (newly added)")
	}
}

func TestGetCacheStats(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	// Test empty cache stats
	stats := cache.GetCacheStats()
	if stats.TotalEntries != 0 {
		t.Errorf("Expected TotalEntries 0, got %d", stats.TotalEntries)
	}
	if stats.ValidEntries != 0 {
		t.Errorf("Expected ValidEntries 0, got %d", stats.ValidEntries)
	}
	if stats.HitRate != 0 {
		t.Errorf("Expected HitRate 0, got %f", stats.HitRate)
	}

	// Add some entries
	entry1 := &CacheEntry{Path: "file1.php", ModTime: time.Now(), ProcessedAt: time.Now(), Valid: true}
	entry2 := &CacheEntry{Path: "file2.php", ModTime: time.Now(), ProcessedAt: time.Now(), Valid: false}

	cache.SetCacheEntry("file1.php", entry1)
	cache.SetCacheEntry("file2.php", entry2)

	// Simulate some hits and misses
	cache.hitCount = 7
	cache.missCount = 3

	stats = cache.GetCacheStats()
	if stats.TotalEntries != 2 {
		t.Errorf("Expected TotalEntries 2, got %d", stats.TotalEntries)
	}
	if stats.ValidEntries != 1 {
		t.Errorf("Expected ValidEntries 1, got %d", stats.ValidEntries)
	}
	if stats.HitRate != 70.0 {
		t.Errorf("Expected HitRate 70.0, got %f", stats.HitRate)
	}
	if stats.MemoryUsage <= 0 {
		t.Errorf("Expected positive MemoryUsage, got %d", stats.MemoryUsage)
	}
}

func TestConcurrency(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})
	cache.maxSize = 1000

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	// Test concurrent reads and writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("file_%d_%d.php", goroutineID, j)
				entry := &CacheEntry{
					Path:        key,
					ModTime:     time.Now(),
					ProcessedAt: time.Now(),
					Valid:       true,
				}

				// Write operation
				err := cache.SetCacheEntry(key, entry)
				if err != nil {
					t.Errorf("Goroutine %d: Set error: %v", goroutineID, err)
					return
				}

				// Read operation
				_, err = cache.GetCacheEntry(key)
				if err != nil && !strings.Contains(err.Error(), "validation failed") && 
				   !strings.Contains(err.Error(), "cache entry is invalid") && 
				   !strings.Contains(err.Error(), "cache entry not found") {
					// Validation failure and invalid cache are expected since files don't actually exist
					t.Errorf("Goroutine %d: Get error: %v", goroutineID, err)
					return
				}

				// Invalidate operation (some of the time)
				if j%10 == 0 {
					err = cache.InvalidateCache(key)
					if err != nil && !strings.Contains(err.Error(), "cache entry not found") {
						t.Errorf("Goroutine %d: Invalidate error: %v", goroutineID, err)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify cache is still in a consistent state
	stats := cache.GetCacheStats()
	if stats.TotalEntries < 0 {
		t.Error("Cache in inconsistent state after concurrent operations")
	}
}

func TestCacheValidation_ModTime(t *testing.T) {
	config := &manifest.CacheConfig{
		Kind: &[]string{CacheModeModTime}[0],
	}
	cache := NewFileCache(config)

	// Create a temporary file
	content := "test content"
	tempFile := createTempFile(t, "test.php", content)
	defer os.Remove(tempFile)

	// Get file info
	stat, err := os.Stat(tempFile)
	if err != nil {
		t.Fatalf("Failed to stat temp file: %v", err)
	}

	// Create cache entry
	entry := &CacheEntry{
		Path:        tempFile,
		ModTime:     stat.ModTime(),
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	cache.SetCacheEntry(tempFile, entry)

	// Should find valid entry
	retrieved, err := cache.GetCacheEntry(tempFile)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if retrieved == nil {
		t.Error("Expected valid cache entry")
	}

	// Modify file (change mtime)
	time.Sleep(10 * time.Millisecond) // Ensure different mtime
	err = os.WriteFile(tempFile, []byte("modified content"), 0644)
	if err != nil {
		t.Fatalf("Failed to modify temp file: %v", err)
	}

	// Cache should now be invalid
	_, err = cache.GetCacheEntry(tempFile)
	if err == nil {
		t.Error("Expected cache entry to be invalid after file modification")
	}
	if !strings.Contains(err.Error(), "cache entry is invalid") {
		t.Errorf("Expected 'cache entry is invalid' error, got: %v", err)
	}
}

func TestCacheValidation_SHA256ModTime(t *testing.T) {
	config := &manifest.CacheConfig{
		Kind: &[]string{CacheModeSHA256MTime}[0],
	}
	cache := NewFileCache(config)

	// Create a temporary file
	content := "test content"
	tempFile := createTempFile(t, "test.php", content)
	defer os.Remove(tempFile)

	// Get file info and calculate hash
	stat, err := os.Stat(tempFile)
	if err != nil {
		t.Fatalf("Failed to stat temp file: %v", err)
	}

	hash := calculateSHA256(t, tempFile)

	// Create cache entry
	entry := &CacheEntry{
		Path:        tempFile,
		ModTime:     stat.ModTime(),
		Hash:        hash,
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	cache.SetCacheEntry(tempFile, entry)

	// Should find valid entry
	retrieved, err := cache.GetCacheEntry(tempFile)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if retrieved == nil {
		t.Error("Expected valid cache entry")
	}

	// Modify file content but preserve mtime (simulate edge case)
	modTime := stat.ModTime()
	err = os.WriteFile(tempFile, []byte("different content"), 0644)
	if err != nil {
		t.Fatalf("Failed to modify temp file: %v", err)
	}
	err = os.Chtimes(tempFile, modTime, modTime)
	if err != nil {
		t.Fatalf("Failed to restore mtime: %v", err)
	}

	// Cache should detect hash mismatch
	_, err = cache.GetCacheEntry(tempFile)
	if err == nil {
		t.Error("Expected cache entry to be invalid after content change")
	}
	if !strings.Contains(err.Error(), "cache entry is invalid") {
		t.Errorf("Expected 'cache entry is invalid' error, got: %v", err)
	}
}

func TestCleanupCache(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})

	// Create temporary files
	tempFile1 := createTempFile(t, "test1.php", "content1")
	tempFile2 := createTempFile(t, "test2.php", "content2")

	// Add cache entries
	entry1 := &CacheEntry{Path: tempFile1, ModTime: time.Now(), Valid: true}
	entry2 := &CacheEntry{Path: tempFile2, ModTime: time.Now(), Valid: true}
	entry3 := &CacheEntry{Path: "nonexistent.php", ModTime: time.Now(), Valid: true}

	cache.SetCacheEntry(tempFile1, entry1)
	cache.SetCacheEntry(tempFile2, entry2)
	cache.SetCacheEntry("nonexistent.php", entry3)

	// Verify all entries exist
	if len(cache.cache) != 3 {
		t.Errorf("Expected 3 cache entries, got %d", len(cache.cache))
	}

	// Remove one file
	os.Remove(tempFile1)

	// Cleanup cache
	err := cache.CleanupCache()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should have removed entries for non-existent files
	if len(cache.cache) != 1 {
		t.Errorf("Expected 1 cache entry after cleanup, got %d", len(cache.cache))
	}

	// Only tempFile2 should remain
	_, exists := cache.cache[tempFile2]
	if !exists {
		t.Error("Expected tempFile2 entry to remain")
	}

	// Clean up remaining temp file
	os.Remove(tempFile2)
}

func TestCreateCacheEntry(t *testing.T) {
	// Create temporary file
	content := "test content"
	tempFile := createTempFile(t, "test.php", content)
	defer os.Remove(tempFile)

	stat, err := os.Stat(tempFile)
	if err != nil {
		t.Fatalf("Failed to stat temp file: %v", err)
	}

	fileInfo := FileInfo{
		Path:        tempFile,
		AbsPath:     tempFile,
		ModTime:     stat.ModTime(),
		Size:        stat.Size(),
		IsDirectory: false,
	}

	// Test mtime mode
	entry, err := CreateCacheEntry(tempFile, fileInfo, CacheModeModTime)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if entry.Hash != "" {
		t.Error("Expected empty hash for mtime mode")
	}
	if !entry.Valid {
		t.Error("Expected entry to be valid")
	}

	// Test sha256+mtime mode
	entry, err = CreateCacheEntry(tempFile, fileInfo, CacheModeSHA256MTime)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if entry.Hash == "" {
		t.Error("Expected hash for sha256+mtime mode")
	}

	expectedHash := calculateSHA256(t, tempFile)
	if entry.Hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, entry.Hash)
	}
}

func TestMemoryBehavior(t *testing.T) {
	cache := NewFileCache(&manifest.CacheConfig{})
	cache.maxSize = 1000

	// Add many entries to test memory usage
	numEntries := 500
	for i := 0; i < numEntries; i++ {
		entry := &CacheEntry{
			Path:        fmt.Sprintf("file_%d.php", i),
			ModTime:     time.Now(),
			ProcessedAt: time.Now(),
			Valid:       true,
		}
		cache.SetCacheEntry(entry.Path, entry)
	}

	stats := cache.GetCacheStats()
	if stats.TotalEntries != numEntries {
		t.Errorf("Expected %d entries, got %d", numEntries, stats.TotalEntries)
	}

	// Memory usage should be reasonable
	expectedMinMemory := int64(numEntries * 100) // At least 100 bytes per entry
	expectedMaxMemory := int64(numEntries * 1000) // At most 1KB per entry
	
	if stats.MemoryUsage < expectedMinMemory {
		t.Errorf("Memory usage seems too low: %d bytes", stats.MemoryUsage)
	}
	if stats.MemoryUsage > expectedMaxMemory {
		t.Errorf("Memory usage seems too high: %d bytes", stats.MemoryUsage)
	}
}

// Helper functions

func createTempFile(t *testing.T, name, content string) string {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, name)
	
	err := os.WriteFile(tempFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	
	return tempFile
}

func calculateSHA256(t *testing.T, filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open file for hash: %v", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		t.Fatalf("Failed to calculate hash: %v", err)
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// Benchmark tests

func BenchmarkCacheSet(b *testing.B) {
	cache := NewFileCache(&manifest.CacheConfig{})
	entry := &CacheEntry{
		Path:        "test.php",
		ModTime:     time.Now(),
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("file_%d.php", i%1000)
		cache.SetCacheEntry(key, entry)
	}
}

func BenchmarkCacheGet(b *testing.B) {
	cache := NewFileCache(&manifest.CacheConfig{})
	
	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		entry := &CacheEntry{
			Path:        fmt.Sprintf("file_%d.php", i),
			ModTime:     time.Now(),
			ProcessedAt: time.Now(),
			Valid:       true,
		}
		cache.SetCacheEntry(entry.Path, entry)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("file_%d.php", i%1000)
		cache.GetCacheEntry(key) // Ignore validation errors for benchmark
	}
}

func BenchmarkCacheConcurrent(b *testing.B) {
	cache := NewFileCache(&manifest.CacheConfig{})
	entry := &CacheEntry{
		Path:        "test.php",
		ModTime:     time.Now(),
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("file_%d.php", i%1000)
			if i%2 == 0 {
				cache.SetCacheEntry(key, entry)
			} else {
				cache.GetCacheEntry(key) // Ignore errors for benchmark
			}
			i++
		}
	})
}