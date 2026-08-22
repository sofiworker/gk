package gcache

import (
	"sync"
	"time"
)

type timedEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// TimedCache 是基于超时（TTL）淘汰的线程安全缓存。
// TimedCache is a thread-safe cache that evicts items by TTL.
type TimedCache[K comparable, V any] struct {
	lock     sync.RWMutex
	cache    map[K]*timedEntry[V]
	stop     chan struct{}
	stopOnce sync.Once
}

// NewTimedCache 创建 TimedCache 并启动后台清理；cleanupInterval 小于等于 0 时不启动。
// NewTimedCache starts background cleanup; interval <= 0 disables it.
func NewTimedCache[K comparable, V any](cleanupInterval time.Duration) *TimedCache[K, V] {
	c := &TimedCache[K, V]{
		cache: make(map[K]*timedEntry[V]),
		stop:  make(chan struct{}),
	}

	if cleanupInterval > 0 {
		go c.cleanupLoop(cleanupInterval)
	}

	return c
}

func (c *TimedCache[K, V]) Get(key K) (V, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	entry, ok := c.cache[key]
	if !ok {
		var zero V
		return zero, false
	}

	// 零值时间表示永不过期；a zero time means never expires.
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		var zero V
		return zero, false
	}

	return entry.value, true
}

func (c *TimedCache[K, V]) Set(key K, value V, ttl time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	c.cache[key] = &timedEntry[V]{
		value:     value,
		expiresAt: expiresAt,
	}
}

func (c *TimedCache[K, V]) Delete(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.cache, key)
}

// Len 返回缓存当前条目数（含已过期但未清理的条目）。
// Len returns the current item count, including expired-but-uncleaned items.
func (c *TimedCache[K, V]) Len() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return len(c.cache)
}

// Close 停止后台清理 goroutine，避免 goroutine 泄漏；多次调用是幂等的。
// Close stops the background cleanup goroutine to avoid leaks; it is idempotent.
func (c *TimedCache[K, V]) Close() {
	c.stopOnce.Do(func() {
		close(c.stop)
	})
}

func (c *TimedCache[K, V]) cleanupLoop(interval time.Duration) {
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

func (c *TimedCache[K, V]) evictExpired() {
	c.lock.Lock()
	defer c.lock.Unlock()

	now := time.Now()
	for key, entry := range c.cache {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(c.cache, key)
		}
	}
}
