package gcache

import (
	"sync"
	"time"
)

// timedEntry stores the value and the expiration time for a cache item.
type timedEntry struct {
	value     interface{}
	expiresAt time.Time
}

// TimedCache is a thread-safe cache that evicts items based on a timeout (TTL).
type TimedCache struct {
	lock  sync.RWMutex
	cache map[string]*timedEntry
	stop  chan struct{}
}

// NewTimedCache creates a new TimedCache and starts a background cleanup process
// that runs at the specified interval. If cleanupInterval is 0 or less, no
// background cleanup will occur.
func NewTimedCache(cleanupInterval time.Duration) *TimedCache {
	c := &TimedCache{
		cache: make(map[string]*timedEntry),
	}

	if cleanupInterval > 0 {
		c.stop = make(chan struct{})
		go c.cleanupLoop(cleanupInterval)
	}

	return c
}

// Get retrieves a value from the cache. It returns the value and true if the
// key exists and has not expired. Otherwise, it returns nil and false.
func (c *TimedCache) Get(key string) (interface{}, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	entry, ok := c.cache[key]
	if !ok {
		return nil, false
	}

	// Check if the item has expired. A zero time means it never expires.
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.value, true
}

// Set adds or updates a key-value pair in the cache with a specified TTL.
// If ttl is 0 or less, the item will never expire.
func (c *TimedCache) Set(key string, value interface{}, ttl time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	c.cache[key] = &timedEntry{
		value:     value,
		expiresAt: expiresAt,
	}
}

// Delete removes a key from the cache.
func (c *TimedCache) Delete(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.cache, key)
}

// Len returns the number of items currently in the cache.
// This includes items that may have expired but haven't been cleaned up yet.
func (c *TimedCache) Len() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return len(c.cache)
}

// Close stops the background cleanup goroutine. It should be called when the
// cache is no longer needed to prevent goroutine leaks.
func (c *TimedCache) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
	}
}

// cleanupLoop is the background process that periodically removes expired items.
func (c *TimedCache) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.evictExpired()
		case <-c.stop:
			return
		}
	}
}

// evictExpired iterates through the cache and removes expired items.
func (c *TimedCache) evictExpired() {
	c.lock.Lock()
	defer c.lock.Unlock()

	now := time.Now()
	for key, entry := range c.cache {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(c.cache, key)
		}
	}
}
