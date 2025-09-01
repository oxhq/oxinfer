package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
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
	ErrCacheNotFound   = errors.New("cache entry not found")
	ErrCacheInvalid    = errors.New("cache entry is invalid")
	ErrInvalidCacheKey = errors.New("invalid cache key")
	ErrFileNotFound    = errors.New("file not found")
	ErrCacheFull       = errors.New("cache is full")
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
	cache     map[string]*lruNode // Cache storage mapped to LRU nodes
	config    *manifest.CacheConfig
	stats     CacheStats
	mutex     sync.RWMutex
	hasher    hash.Hash // Reusable SHA256 hasher
	maxSize   int
	head      *lruNode // LRU list head (most recently used)
	tail      *lruNode // LRU list tail (least recently used)
	hitCount  int64
	missCount int64
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

	// Check modification time first (both modes use this)
	if !currentModTime.Equal(entry.ModTime) {
		return false, nil
	}

	// For sha256+mtime mode, also validate file hash
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