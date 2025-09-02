package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/garaekz/oxinfer/internal/manifest"
)

// Cache validation modes
const (
	CacheModeModTime      = "mtime"
	CacheModeSHA256MTime = "sha256+mtime"
	DefaultCacheSize     = 1000
)

var (
	ErrCacheNotFound       = errors.New("cache entry not found")
	ErrCacheInvalid        = errors.New("cache entry is invalid")
	ErrInvalidCacheKey     = errors.New("invalid cache key")
	ErrFileNotFound        = errors.New("file not found")
	ErrCacheFull           = errors.New("cache is full")
	ErrProjectKeyMismatch  = errors.New("project key mismatch")
	ErrCacheDirNotFound    = errors.New("cache directory not found")
	ErrPersistenceFailed   = errors.New("cache persistence failed")
)

// CacheError wraps cache-related errors with additional context
type CacheError struct {
	Op   string // Operation that failed
	Path string // File path involved
	Err  error  // Underlying error
}

func (e *CacheError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("cache %s for path %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("cache %s: %v", e.Op, e.Err)
}

func (e *CacheError) Unwrap() error {
	return e.Err
}

// NewCacheError creates a new CacheError
func NewCacheError(op, path string, err error) *CacheError {
	return &CacheError{Op: op, Path: path, Err: err}
}

// lruNode represents a node in the LRU linked list
type lruNode struct {
	key   string
	entry *CacheEntry
	prev  *lruNode
	next  *lruNode
}

// FileCacheImpl implements the FileCacher interface with configurable validation modes
type FileCacheImpl struct {
	cache       map[string]*lruNode // Cache storage mapped to LRU nodes
	config      *manifest.CacheConfig
	stats       CacheStats
	mutex       sync.RWMutex
	hasher      hash.Hash // Reusable SHA256 hasher
	maxSize     int
	head        *lruNode // LRU list head (most recently used)
	tail        *lruNode // LRU list tail (least recently used)
	hitCount    int64
	missCount   int64
	cacheDir    string   // Directory for on-disk cache persistence
	projectKey  string   // Project key for cache validation
	persistLoad bool     // Flag to track if persistence has been loaded
}

// NewFileCache creates a new FileCacheImpl with the provided configuration
func NewFileCache(config *manifest.CacheConfig) *FileCacheImpl {
	maxSize := DefaultCacheSize
	
	cache := &FileCacheImpl{
		cache:   make(map[string]*lruNode),
		config:  config,
		hasher:  sha256.New(),
		maxSize: maxSize,
		stats: CacheStats{
			LastCleanup: time.Now(),
		},
	}
	
	// Initialize LRU list with dummy head and tail nodes
	cache.head = &lruNode{}
	cache.tail = &lruNode{}
	cache.head.next = cache.tail
	cache.tail.prev = cache.head
	
	return cache
}

// NewFileCacheWithDir creates a new FileCacheImpl with cache directory support
// It automatically loads existing cache entries from disk if available
func NewFileCacheWithDir(config *manifest.CacheConfig, cacheDir, projectKey string) (*FileCacheImpl, error) {
	maxSize := DefaultCacheSize
	
	cache := &FileCacheImpl{
		cache:      make(map[string]*lruNode),
		config:     config,
		hasher:     sha256.New(),
		maxSize:    maxSize,
		cacheDir:   cacheDir,
		projectKey: projectKey,
		stats: CacheStats{
			LastCleanup: time.Now(),
		},
	}
	
	// Initialize LRU list with dummy head and tail nodes
	cache.head = &lruNode{}
	cache.tail = &lruNode{}
	cache.head.next = cache.tail
	cache.tail.prev = cache.head
	
	// Load from disk if cache directory exists
	if cacheDir != "" {
		if err := cache.loadFromDisk(); err != nil {
			// Log error but don't fail construction - cache can work without persistence
			// In production, you might want to log this error
		}
		cache.persistLoad = true // Mark as loaded to enable persistence
	}
	
	return cache, nil
}

// ComputeProjectKey computes a unique project key based on composer.json, composer.lock, and project root
func ComputeProjectKey(projectRoot, composerPath string) (string, error) {
	if projectRoot == "" {
		return "", fmt.Errorf("project root cannot be empty")
	}
	if composerPath == "" {
		return "", fmt.Errorf("composer path cannot be empty")
	}

	hasher := sha256.New()

	// Hash the resolved project root path
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	hasher.Write([]byte(absRoot))

	// Hash composer.json content
	absComposerPath := filepath.Join(absRoot, composerPath)
	composerData, err := os.ReadFile(absComposerPath)
	if err != nil {
		return "", fmt.Errorf("read composer.json: %w", err)
	}
	hasher.Write(composerData)

	// Hash composer.lock content if it exists
	composerLockPath := filepath.Join(filepath.Dir(absComposerPath), "composer.lock")
	if lockData, err := os.ReadFile(composerLockPath); err == nil {
		hasher.Write(lockData)
	}
	// Note: We don't fail if composer.lock doesn't exist

	// Return first 16 characters of hex-encoded hash
	hash := hex.EncodeToString(hasher.Sum(nil))
	if len(hash) < 16 {
		return "", fmt.Errorf("hash too short: %d", len(hash))
	}
	return hash[:16], nil
}

// CacheIndex represents the on-disk cache index metadata
type CacheIndex struct {
	ProjectKey   string            `json:"project_key"`
	CreatedAt    time.Time         `json:"created_at"`
	LastModified time.Time         `json:"last_modified"`
	Entries      map[string]string `json:"entries"` // path -> filename mapping
}

// DiskCacheEntry represents a single cache entry stored on disk
type DiskCacheEntry struct {
	Path        string    `json:"path"`
	Hash        string    `json:"hash"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	ProcessedAt time.Time `json:"processed_at"`
	Valid       bool      `json:"valid"`
}

// GetCacheEntry retrieves a cache entry for the specified file path
func (c *FileCacheImpl) GetCacheEntry(path string) (*CacheEntry, error) {
	if path == "" {
		return nil, NewCacheError("GetCacheEntry", path, ErrInvalidCacheKey)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	node, exists := c.cache[path]
	if !exists {
		c.missCount++
		return nil, NewCacheError("GetCacheEntry", path, ErrCacheNotFound)
	}

	// Move to front of LRU list (mark as recently used)
	c.moveToFront(node)

	// Validate cache entry
	valid, err := c.validateEntry(node.entry)
	if err != nil {
		return nil, NewCacheError("GetCacheEntry", path, fmt.Errorf("validation failed: %w", err))
	}

	if !valid {
		// Remove invalid entry
		c.removeNode(node)
		delete(c.cache, path)
		c.missCount++
		return nil, NewCacheError("GetCacheEntry", path, ErrCacheInvalid)
	}

	c.hitCount++
	node.entry.Valid = true
	return node.entry, nil
}

// SetCacheEntry stores a cache entry for the specified file path
func (c *FileCacheImpl) SetCacheEntry(path string, entry *CacheEntry) error {
	if path == "" {
		return NewCacheError("SetCacheEntry", path, ErrInvalidCacheKey)
	}
	if entry == nil {
		return NewCacheError("SetCacheEntry", path, errors.New("cache entry is nil"))
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if entry already exists
	if node, exists := c.cache[path]; exists {
		// Update existing entry
		node.entry = entry
		c.moveToFront(node)
		return nil
	}

	// Check cache size limit
	if len(c.cache) >= c.maxSize {
		// Evict least recently used entry
		if err := c.evictLRU(); err != nil {
			return NewCacheError("SetCacheEntry", path, fmt.Errorf("eviction failed: %w", err))
		}
	}

	// Create new node and add to cache
	node := &lruNode{
		key:   path,
		entry: entry,
	}
	c.cache[path] = node
	c.addToFront(node)

	// Trigger persistence if cache directory is configured
	if c.cacheDir != "" {
		// Note: In a production system, you might want to batch persistence operations
		// or use a background goroutine to avoid blocking the main thread
		go func() {
			if err := c.saveToDisk(); err != nil {
				// Log error but don't fail the cache operation
				// In production, you might want to log this error
			}
		}()
	}

	return nil
}

// InvalidateCache removes the cache entry for the specified file path
func (c *FileCacheImpl) InvalidateCache(path string) error {
	if path == "" {
		return NewCacheError("InvalidateCache", path, ErrInvalidCacheKey)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	node, exists := c.cache[path]
	if !exists {
		return NewCacheError("InvalidateCache", path, ErrCacheNotFound)
	}

	c.removeNode(node)
	delete(c.cache, path)
	return nil
}

// CleanupCache removes stale cache entries for files that no longer exist
func (c *FileCacheImpl) CleanupCache() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var keysToRemove []string

	// Check each cache entry to see if the file still exists
	for path, node := range c.cache {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			keysToRemove = append(keysToRemove, path)
			c.removeNode(node)
		}
	}

	// Remove non-existent files from cache
	for _, key := range keysToRemove {
		delete(c.cache, key)
	}

	c.stats.LastCleanup = time.Now()
	return nil
}

// GetCacheStats returns statistics about cache performance and usage
func (c *FileCacheImpl) GetCacheStats() CacheStats {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	totalRequests := c.hitCount + c.missCount
	hitRate := float64(0)
	if totalRequests > 0 {
		hitRate = float64(c.hitCount) / float64(totalRequests) * 100
	}

	validEntries := 0
	for _, node := range c.cache {
		if node.entry.Valid {
			validEntries++
		}
	}

	// Estimate memory usage (approximate)
	memoryUsage := int64(len(c.cache) * 200) // Rough estimate per entry

	return CacheStats{
		TotalEntries: len(c.cache),
		ValidEntries: validEntries,
		HitRate:      hitRate,
		MemoryUsage:  memoryUsage,
		LastCleanup:  c.stats.LastCleanup,
	}
}

// loadFromDisk loads cache entries from disk storage
func (c *FileCacheImpl) loadFromDisk() error {
	if c.cacheDir == "" {
		return nil // No cache directory configured
	}

	// Check if project key is valid
	if !c.validateProjectKey() {
		// Project key mismatch, clear cache and create new structure
		return c.initializeCacheDir()
	}

	// Load index file
	indexPath := filepath.Join(c.cacheDir, "index.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No index file, initialize cache directory
			return c.initializeCacheDir()
		}
		return NewCacheError("loadFromDisk", indexPath, fmt.Errorf("read index: %w", err))
	}

	var index CacheIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		return NewCacheError("loadFromDisk", indexPath, fmt.Errorf("unmarshal index: %w", err))
	}

	// Verify project key matches
	if index.ProjectKey != c.projectKey {
		// Project changed, reinitialize
		return c.initializeCacheDir()
	}

	// Load individual cache entries
	filesDir := filepath.Join(c.cacheDir, "files")
	for path, filename := range index.Entries {
		entryPath := filepath.Join(filesDir, filename)
		entryData, err := os.ReadFile(entryPath)
		if err != nil {
			continue // Skip corrupted entries
		}

		var diskEntry DiskCacheEntry
		if err := json.Unmarshal(entryData, &diskEntry); err != nil {
			continue // Skip corrupted entries
		}

		// Convert to in-memory cache entry
		cacheEntry := &CacheEntry{
			Path:        diskEntry.Path,
			Hash:        diskEntry.Hash,
			Size:        diskEntry.Size,
			ModTime:     diskEntry.ModTime,
			ProcessedAt: diskEntry.ProcessedAt,
			Valid:       diskEntry.Valid,
		}

		// Add to in-memory cache (without triggering disk save)
		node := &lruNode{
			key:   path,
			entry: cacheEntry,
		}
		c.cache[path] = node
		c.addToFront(node)
	}

	c.persistLoad = true
	return nil
}

// saveToDisk persists cache entries to disk storage
func (c *FileCacheImpl) saveToDisk() error {
	if c.cacheDir == "" {
		return nil // No cache directory configured
	}

	// Ensure cache directory structure exists
	if err := c.initializeCacheDir(); err != nil {
		return fmt.Errorf("initialize cache dir: %w", err)
	}

	// Create files directory
	filesDir := filepath.Join(c.cacheDir, "files")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return NewCacheError("saveToDisk", filesDir, fmt.Errorf("create files dir: %w", err))
	}

	// Build index and save individual entries
	index := CacheIndex{
		ProjectKey:   c.projectKey,
		CreatedAt:    time.Now(),
		LastModified: time.Now(),
		Entries:      make(map[string]string),
	}

	// Sort paths for deterministic output
	var paths []string
	for path := range c.cache {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		node := c.cache[path]
		if node == nil || node.entry == nil {
			continue
		}

		// Generate filename from hash of path
		hasher := sha256.New()
		hasher.Write([]byte(path))
		filename := hex.EncodeToString(hasher.Sum(nil))[:16] + ".json"

		// Convert to disk format
		diskEntry := DiskCacheEntry{
			Path:        node.entry.Path,
			Hash:        node.entry.Hash,
			Size:        node.entry.Size,
			ModTime:     node.entry.ModTime,
			ProcessedAt: node.entry.ProcessedAt,
			Valid:       node.entry.Valid,
		}

		// Serialize and write entry
		entryData, err := json.MarshalIndent(diskEntry, "", "  ")
		if err != nil {
			continue // Skip entries that can't be serialized
		}

		entryPath := filepath.Join(filesDir, filename)
		if err := os.WriteFile(entryPath, entryData, 0644); err != nil {
			continue // Skip entries that can't be written
		}

		index.Entries[path] = filename
	}

	// Write index file
	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return NewCacheError("saveToDisk", c.cacheDir, fmt.Errorf("marshal index: %w", err))
	}

	indexPath := filepath.Join(c.cacheDir, "index.json")
	if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
		return NewCacheError("saveToDisk", indexPath, fmt.Errorf("write index: %w", err))
	}

	return nil
}

// validateProjectKey checks if the cached project key matches the current project
func (c *FileCacheImpl) validateProjectKey() bool {
	if c.cacheDir == "" || c.projectKey == "" {
		return false
	}

	keyPath := filepath.Join(c.cacheDir, "project.key")
	cachedKey, err := os.ReadFile(keyPath)
	if err != nil {
		return false
	}

	return string(cachedKey) == c.projectKey
}

// initializeCacheDir creates the cache directory structure and writes the project key
func (c *FileCacheImpl) initializeCacheDir() error {
	if c.cacheDir == "" {
		return nil
	}

	// Create cache directory
	if err := os.MkdirAll(c.cacheDir, 0755); err != nil {
		return NewCacheError("initializeCacheDir", c.cacheDir, fmt.Errorf("create cache dir: %w", err))
	}

	// Write project key
	keyPath := filepath.Join(c.cacheDir, "project.key")
	if err := os.WriteFile(keyPath, []byte(c.projectKey), 0644); err != nil {
		return NewCacheError("initializeCacheDir", keyPath, fmt.Errorf("write project key: %w", err))
	}

	return nil
}

// SaveToDiskSync saves cache to disk synchronously (for testing)
func (c *FileCacheImpl) SaveToDiskSync() error {
	return c.saveToDisk()
}

// validateEntry checks if a cache entry is still valid based on the cache mode
func (c *FileCacheImpl) validateEntry(entry *CacheEntry) (bool, error) {
	if c.config == nil {
		return false, errors.New("cache config is nil")
	}

	// Get current file info
	stat, err := os.Stat(entry.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // File no longer exists, invalid
		}
		return false, fmt.Errorf("stat file: %w", err)
	}

	currentModTime := stat.ModTime()
	currentSize := stat.Size()

	// OPTIMIZATION: Check mtime + size first for quick validation
	// If both match, we can skip hash calculation in sha256+mtime mode
	if currentModTime.Equal(entry.ModTime) && currentSize == entry.Size {
		// For mtime-only mode, this is sufficient validation
		if c.getCacheKind() == CacheModeModTime {
			return true, nil
		}

		// For sha256+mtime mode, mtime+size match allows us to skip hash calculation
		// This is a safe optimization because if both mtime and size are unchanged,
		// the content is very likely unchanged as well
		if c.getCacheKind() == CacheModeSHA256MTime && entry.Hash != "" {
			return true, nil
		}
	}

	// If mtime or size changed, fall back to traditional validation
	if !currentModTime.Equal(entry.ModTime) {
		return false, nil
	}

	// For sha256+mtime mode, validate file hash when mtime matches but size differs
	// or when hash is missing (backward compatibility)
	if c.getCacheKind() == CacheModeSHA256MTime {
		if entry.Hash == "" {
			return false, nil // No hash stored, invalid
		}

		currentHash, err := c.calculateFileHash(entry.Path)
		if err != nil {
			return false, fmt.Errorf("calculate hash: %w", err)
		}

		if currentHash != entry.Hash {
			return false, nil
		}
	}

	return true, nil
}

// calculateFileHash computes the SHA256 hash of a file
func (c *FileCacheImpl) calculateFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// Reset hasher for reuse
	c.hasher.Reset()

	if _, err := io.Copy(c.hasher, file); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}

	return hex.EncodeToString(c.hasher.Sum(nil)), nil
}

// getCacheKind returns the cache validation mode with fallback to default
func (c *FileCacheImpl) getCacheKind() string {
	if c.config != nil && c.config.Kind != nil {
		return *c.config.Kind
	}
	return CacheModeModTime // Default to mtime mode
}

// LRU list manipulation methods

// addToFront adds a node right after the head (most recently used position)
func (c *FileCacheImpl) addToFront(node *lruNode) {
	node.prev = c.head
	node.next = c.head.next
	c.head.next.prev = node
	c.head.next = node
}

// removeNode removes a node from the LRU list
func (c *FileCacheImpl) removeNode(node *lruNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// moveToFront moves an existing node to the front (most recently used position)
func (c *FileCacheImpl) moveToFront(node *lruNode) {
	c.removeNode(node)
	c.addToFront(node)
}

// evictLRU removes the least recently used entry from the cache
func (c *FileCacheImpl) evictLRU() error {
	if c.tail.prev == c.head {
		return errors.New("cache is empty, cannot evict")
	}

	lru := c.tail.prev
	c.removeNode(lru)
	delete(c.cache, lru.key)
	return nil
}

// CreateCacheEntry creates a new cache entry for a file
func CreateCacheEntry(path string, fileInfo FileInfo, cacheKind string) (*CacheEntry, error) {
	entry := &CacheEntry{
		Path:        path,
		ModTime:     fileInfo.ModTime,
		Size:        fileInfo.Size, // Populate size field for optimization
		ProcessedAt: time.Now(),
		Valid:       true,
	}

	// Calculate hash for sha256+mtime mode
	if cacheKind == CacheModeSHA256MTime {
		hasher := sha256.New()
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open file for hash: %w", err)
		}
		defer file.Close()

		if _, err := io.Copy(hasher, file); err != nil {
			return nil, fmt.Errorf("calculate hash: %w", err)
		}

		entry.Hash = hex.EncodeToString(hasher.Sum(nil))
	}

	return entry, nil
}