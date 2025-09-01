package psr4

import (
	"sync"
	"time"
)

// PSR4Cache provides thread-safe caching for PSR-4 resolution operations.
// It implements LRU-style eviction and supports TTL-based expiration.
type PSR4Cache struct {
	mu    sync.RWMutex
	items map[string]*cacheItem
	
	// Configuration
	maxSize int
	defaultTTL time.Duration
	
	// LRU tracking
	accessOrder []string
}

// cacheItem represents a cached resolution result.
type cacheItem struct {
	value     string
	expiresAt time.Time
	lastAccess time.Time
}

// NewPSR4Cache creates a new cache with the specified maximum size.
// Items are cached with a default TTL of 5 minutes.
func NewPSR4Cache(maxSize int) *PSR4Cache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	
	return &PSR4Cache{
		items:       make(map[string]*cacheItem),
		maxSize:     maxSize,
		defaultTTL:  5 * time.Minute,
		accessOrder: make([]string, 0, maxSize),
	}
}

// NewPSR4CacheWithTTL creates a new cache with custom TTL settings.
func NewPSR4CacheWithTTL(maxSize int, ttl time.Duration) *PSR4Cache {
	cache := NewPSR4Cache(maxSize)
	cache.defaultTTL = ttl
	return cache
}

// GetClass retrieves a cached class resolution result.
// Returns the cached path and true if found and not expired, empty string and false otherwise.
func (c *PSR4Cache) GetClass(fqcn string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	item, exists := c.items[fqcn]
	if !exists {
		return "", false
	}
	
	// Check if expired
	if time.Now().After(item.expiresAt) {
		delete(c.items, fqcn)
		c.removeFromAccessOrder(fqcn)
		return "", false
	}
	
	// Update access time and order
	item.lastAccess = time.Now()
	c.updateAccessOrder(fqcn)
	
	return item.value, true
}

// SetClass caches a class resolution result.
// The item will expire according to the cache's default TTL.
func (c *PSR4Cache) SetClass(fqcn, filePath string) {
	c.SetClassWithTTL(fqcn, filePath, c.defaultTTL)
}

// SetClassWithTTL caches a class resolution result with custom TTL.
func (c *PSR4Cache) SetClassWithTTL(fqcn, filePath string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	
	// Create or update cache item
	item := &cacheItem{
		value:      filePath,
		expiresAt:  now.Add(ttl),
		lastAccess: now,
	}
	
	// Check if we need to evict items to make room
	if _, exists := c.items[fqcn]; !exists {
		// New item - check if we need to evict
		if len(c.items) >= c.maxSize {
			c.evictLRU()
		}
	}
	
	c.items[fqcn] = item
	c.updateAccessOrder(fqcn)
}

// InvalidateClass removes a specific class from the cache.
func (c *PSR4Cache) InvalidateClass(fqcn string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	delete(c.items, fqcn)
	c.removeFromAccessOrder(fqcn)
}

// Clear removes all cached items.
func (c *PSR4Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.items = make(map[string]*cacheItem)
	c.accessOrder = c.accessOrder[:0]
}

// Size returns the current number of cached items.
func (c *PSR4Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Stats returns cache statistics.
type CacheStats struct {
	Size         int
	MaxSize      int
	HitRate      float64
	TotalHits    int64
	TotalMisses  int64
	ExpiredItems int
}

// GetStats returns current cache statistics.
func (c *PSR4Cache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	// Count expired items
	now := time.Now()
	expiredCount := 0
	for _, item := range c.items {
		if now.After(item.expiresAt) {
			expiredCount++
		}
	}
	
	return CacheStats{
		Size:         len(c.items),
		MaxSize:      c.maxSize,
		ExpiredItems: expiredCount,
	}
}

// Cleanup removes expired items from the cache.
// This method should be called periodically to maintain cache efficiency.
func (c *PSR4Cache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	var expiredKeys []string
	
	// Find expired items
	for key, item := range c.items {
		if now.After(item.expiresAt) {
			expiredKeys = append(expiredKeys, key)
		}
	}
	
	// Remove expired items
	for _, key := range expiredKeys {
		delete(c.items, key)
		c.removeFromAccessOrder(key)
	}
	
	return len(expiredKeys)
}

// evictLRU removes the least recently used item from the cache.
// Must be called with write lock held.
func (c *PSR4Cache) evictLRU() {
	if len(c.accessOrder) == 0 {
		return
	}
	
	// Find the least recently used item
	lruKey := c.accessOrder[0]
	
	// Remove from cache and access order
	delete(c.items, lruKey)
	c.accessOrder = c.accessOrder[1:]
}

// updateAccessOrder updates the access order for LRU tracking.
// Must be called with write lock held.
func (c *PSR4Cache) updateAccessOrder(key string) {
	// Remove key from current position if it exists
	c.removeFromAccessOrder(key)
	
	// Add to end (most recently used)
	c.accessOrder = append(c.accessOrder, key)
}

// removeFromAccessOrder removes a key from the access order tracking.
// Must be called with write lock held.
func (c *PSR4Cache) removeFromAccessOrder(key string) {
	for i, k := range c.accessOrder {
		if k == key {
			// Remove by swapping with last element and truncating
			c.accessOrder[i] = c.accessOrder[len(c.accessOrder)-1]
			c.accessOrder = c.accessOrder[:len(c.accessOrder)-1]
			break
		}
	}
}

// CacheConfig holds configuration for cache behavior.
type CacheConfig struct {
	// Enabled controls whether caching is active
	Enabled bool
	// MaxSize is the maximum number of items to cache
	MaxSize int
	// DefaultTTL is the default time-to-live for cache entries
	DefaultTTL time.Duration
	// CleanupInterval is how often to run cleanup of expired items
	CleanupInterval time.Duration
}

// DefaultCacheConfig returns a sensible default cache configuration.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:         true,
		MaxSize:         1000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

// NewPSR4CacheWithConfig creates a cache using the provided configuration.
func NewPSR4CacheWithConfig(config CacheConfig) *PSR4Cache {
	if !config.Enabled {
		return nil
	}
	
	return NewPSR4CacheWithTTL(config.MaxSize, config.DefaultTTL)
}

// IsEmpty returns true if the cache contains no items.
func (c *PSR4Cache) IsEmpty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items) == 0
}

// ContainsClass returns true if the cache contains an entry for the given FQCN.
// This does not update access order or check expiration.
func (c *PSR4Cache) ContainsClass(fqcn string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	item, exists := c.items[fqcn]
	if !exists {
		return false
	}
	
	// Check if expired
	return !time.Now().After(item.expiresAt)
}

// GetMaxSize returns the maximum number of items the cache can hold.
func (c *PSR4Cache) GetMaxSize() int {
	return c.maxSize
}

// GetDefaultTTL returns the default TTL for cache entries.
func (c *PSR4Cache) GetDefaultTTL() time.Duration {
	return c.defaultTTL
}

// SetDefaultTTL updates the default TTL for new cache entries.
// Existing entries are not affected.
func (c *PSR4Cache) SetDefaultTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultTTL = ttl
}