package gcache

import (
	"context"
	"io"
	"strconv"
	"sync"
	"time"
)

type memoryItem struct {
	value     []byte
	expiresAt time.Time
}

// MemoryCache 是本包唯一自带的实现：一个带过期淘汰的进程内缓存。
// MemoryCache is the only implementation shipped here: an in-process cache with expiration.
type MemoryCache struct {
	mu              sync.RWMutex
	items           map[string]*memoryItem
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	once            sync.Once
}

func NewMemoryCache(opts ...Option) (*MemoryCache, error) {
	options := &Options{
		CleanupInterval: time.Minute,
	}
	for _, o := range opts {
		o(options)
	}

	cache := &MemoryCache{
		items:           make(map[string]*memoryItem),
		cleanupInterval: options.CleanupInterval,
		stopCleanup:     make(chan struct{}),
	}
	// 非正间隔下不启动 ticker：time.NewTicker 会 panic，且过期项仍会在读取时被剔除。
	// A non-positive interval starts no ticker: time.NewTicker would panic, and expired
	// items are still evicted on read.
	if cache.cleanupInterval > 0 {
		go cache.cleanupLoop()
	}
	return cache, nil
}

func (m *MemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()

	if !ok || m.isExpired(item) {
		_ = m.Delete(ctx, key)
		return nil, ErrCacheMiss
	}
	return cloneBytes(item.value), nil
}

func (m *MemoryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item := &memoryItem{
		value: cloneBytes(value),
	}
	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}
	m.items[key] = item
	return nil
}

func (m *MemoryCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

func (m *MemoryCache) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()
	return ok && !m.isExpired(item), nil
}

func (m *MemoryCache) Expire(_ context.Context, key string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[key]
	if !ok || m.isExpired(item) {
		delete(m.items, key)
		return ErrCacheMiss
	}
	if ttl <= 0 {
		item.expiresAt = time.Time{}
		return nil
	}
	item.expiresAt = time.Now().Add(ttl)
	return nil
}

func (m *MemoryCache) TTL(_ context.Context, key string) (time.Duration, error) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()

	if !ok || m.isExpired(item) {
		return 0, ErrCacheMiss
	}
	if item.expiresAt.IsZero() {
		return -1, nil
	}
	return time.Until(item.expiresAt), nil
}

func (m *MemoryCache) Add(_ context.Context, key string, delta int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[key]
	var (
		current int64
		expires time.Time
	)

	if ok && !m.isExpired(item) {
		var err error
		current, err = strconv.ParseInt(string(item.value), 10, 64)
		if err != nil {
			return 0, err
		}
		expires = item.expiresAt
	}

	current += delta
	m.items[key] = &memoryItem{
		value:     []byte(strconv.FormatInt(current, 10)),
		expiresAt: expires,
	}
	return current, nil
}

func (m *MemoryCache) Ping(context.Context) error {
	return nil
}

func (m *MemoryCache) Close() error {
	m.once.Do(func() {
		close(m.stopCleanup)
	})
	return nil
}

func (m *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanupExpired()
		case <-m.stopCleanup:
			return
		}
	}
}

func (m *MemoryCache) cleanupExpired() {
	now := time.Now()
	m.mu.Lock()
	for key, item := range m.items {
		if !item.expiresAt.IsZero() && now.After(item.expiresAt) {
			delete(m.items, key)
		}
	}
	m.mu.Unlock()
}

func (m *MemoryCache) isExpired(item *memoryItem) bool {
	if item == nil {
		return true
	}
	if item.expiresAt.IsZero() {
		return false
	}
	return time.Now().After(item.expiresAt)
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

// 能力以「是否实现接口」表达，因此这里逐个断言而不是断言一个大而全的接口。
// Capabilities are expressed by interface satisfaction, so each one is asserted
// separately instead of through a single all-encompassing interface.
var (
	_ KV        = (*MemoryCache)(nil)
	_ Exister   = (*MemoryCache)(nil)
	_ TTLReader = (*MemoryCache)(nil)
	_ Expirer   = (*MemoryCache)(nil)
	_ Counter   = (*MemoryCache)(nil)
	_ Pinger    = (*MemoryCache)(nil)
	_ io.Closer = (*MemoryCache)(nil)
)
