package gcache

import (
	"context"
	"strconv"
	"sync"
	"time"
)

type memoryItem struct {
	value     []byte
	expiresAt time.Time
}

// MemoryCache 为最小核心能力的内存实现
type MemoryCache struct {
	mu              sync.RWMutex
	items           map[string]*memoryItem
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	once            sync.Once
}

// NewMemoryCache 创建内存缓存，cleanupInterval<=0 时默认 1 分钟清理一次
func NewMemoryCache(cleanupInterval time.Duration) *MemoryCache {
	if cleanupInterval <= 0 {
		cleanupInterval = time.Minute
	}
	cache := &MemoryCache{
		items:           make(map[string]*memoryItem),
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
	}
	go cache.cleanupLoop()
	return cache
}

func (m *MemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()

	if !ok || m.isExpired(item) {
		m.Delete(ctx, key) // 清理过期数据
		return nil, ErrCacheMiss
	}
	return cloneBytes(item.value), nil
}

func (m *MemoryCache) Set(_ context.Context, key string, value []byte, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item := &memoryItem{
		value: cloneBytes(value),
	}
	if expiration > 0 {
		item.expiresAt = time.Now().Add(expiration)
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
	if !ok || m.isExpired(item) {
		return false, nil
	}
	return true, nil
}

func (m *MemoryCache) Expire(_ context.Context, key string, expiration time.Duration) error {
	if expiration <= 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[key]
	if !ok || m.isExpired(item) {
		delete(m.items, key)
		return ErrCacheMiss
	}
	item.expiresAt = time.Now().Add(expiration)
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

func (m *MemoryCache) Increment(ctx context.Context, key string, value int64) (int64, error) {
	return m.add(ctx, key, value)
}

func (m *MemoryCache) Decrement(ctx context.Context, key string, value int64) (int64, error) {
	return m.add(ctx, key, -value)
}

func (m *MemoryCache) add(_ context.Context, key string, delta int64) (int64, error) {
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

func (m *MemoryCache) Close() error {
	m.once.Do(func() {
		close(m.stopCleanup)
	})
	return nil
}

func (m *MemoryCache) Ping(context.Context) error {
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

var (
	_ BasicCache = (*MemoryCache)(nil)
)
