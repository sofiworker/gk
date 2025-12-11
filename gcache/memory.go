package gcache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

var (
	ErrNotSupported = errors.New("gcache: operation not supported by MemoryCache")
)

type memoryItem struct {
	value     []byte
	expiresAt time.Time
}

type MemoryCache struct {
	mu              sync.RWMutex
	items           map[string]*memoryItem
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	once            sync.Once
}

func NewMemoryCache(opts ...Option) (*MemoryCache, error) {
	options := &Options{
		CleanupInterval: time.Minute, // Default cleanup interval
	}
	for _, o := range opts {
		o(options)
	}

	cache := &MemoryCache{
		items:           make(map[string]*memoryItem),
		cleanupInterval: options.CleanupInterval,
		stopCleanup:     make(chan struct{}),
	}
	go cache.cleanupLoop()
	return cache, nil
}

func (m *MemoryCache) GetWithContext(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()

	if !ok || m.isExpired(item) {
		m.DeleteWithContext(ctx, key)
		return nil, ErrCacheMiss
	}
	return cloneBytes(item.value), nil
}

func (m *MemoryCache) Get(key string) ([]byte, error) {
	return m.GetWithContext(context.Background(), key)
}

func (m *MemoryCache) SetWithContext(_ context.Context, key string, value []byte, expiration time.Duration) error {
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

func (m *MemoryCache) Set(key string, value []byte, expiration time.Duration) error {
	return m.SetWithContext(context.Background(), key, value, expiration)
}

func (m *MemoryCache) DeleteWithContext(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

func (m *MemoryCache) Delete(key string) error {
	return m.DeleteWithContext(context.Background(), key)
}

func (m *MemoryCache) ExistsWithContext(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()
	if !ok || m.isExpired(item) {
		return false, nil
	}
	return true, nil
}

func (m *MemoryCache) Exists(key string) (bool, error) {
	return m.ExistsWithContext(context.Background(), key)
}

func (m *MemoryCache) ExpireWithContext(_ context.Context, key string, expiration time.Duration) error {
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

func (m *MemoryCache) Expire(key string, expiration time.Duration) error {
	return m.ExpireWithContext(context.Background(), key, expiration)
}

func (m *MemoryCache) TTLWithContext(_ context.Context, key string) (time.Duration, error) {
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

func (m *MemoryCache) TTL(key string) (time.Duration, error) {
	return m.TTLWithContext(context.Background(), key)
}

func (m *MemoryCache) IncrementWithContext(ctx context.Context, key string, value int64) (int64, error) {
	return m.add(ctx, key, value)
}

func (m *MemoryCache) Increment(key string, value int64) (int64, error) {
	return m.IncrementWithContext(context.Background(), key, value)
}

func (m *MemoryCache) DecrementWithContext(ctx context.Context, key string, value int64) (int64, error) {
	return m.add(ctx, key, -value)
}

func (m *MemoryCache) Decrement(key string, value int64) (int64, error) {
	return m.DecrementWithContext(context.Background(), key, value)
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

func (m *MemoryCache) PingWithContext(context.Context) error {
	return nil
}

func (m *MemoryCache) Ping() error {
	return m.PingWithContext(context.Background())
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

// Stubs for unsupported operations
func (m *MemoryCache) HashSetWithContext(ctx context.Context, key string, field string, value []byte) error {
	return ErrNotSupported
}
func (m *MemoryCache) HashSet(key string, field string, value []byte) error {
	return ErrNotSupported
}
func (m *MemoryCache) HashGetWithContext(ctx context.Context, key string, field string) ([]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) HashGet(key string, field string) ([]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) HashGetAllWithContext(ctx context.Context, key string) (map[string][]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) HashGetAll(key string) (map[string][]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) HashDeleteWithContext(ctx context.Context, key string, fields ...string) error {
	return ErrNotSupported
}
func (m *MemoryCache) HashDelete(key string, fields ...string) error {
	return ErrNotSupported
}
func (m *MemoryCache) ListPushWithContext(ctx context.Context, key string, values ...[]byte) error {
	return ErrNotSupported
}
func (m *MemoryCache) ListPush(key string, values ...[]byte) error {
	return ErrNotSupported
}
func (m *MemoryCache) ListPopWithContext(ctx context.Context, key string) ([]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) ListPop(key string) ([]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) ListRangeWithContext(ctx context.Context, key string, start, stop int64) ([][]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) ListRange(key string, start, stop int64) ([][]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) SetAddWithContext(ctx context.Context, key string, members ...[]byte) error {
	return ErrNotSupported
}
func (m *MemoryCache) SetAdd(key string, members ...[]byte) error {
	return ErrNotSupported
}
func (m *MemoryCache) SetMembersWithContext(ctx context.Context, key string) ([][]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) SetMembers(key string) ([][]byte, error) {
	return nil, ErrNotSupported
}
func (m *MemoryCache) SetIsMemberWithContext(ctx context.Context, key string, member []byte) (bool, error) {
	return false, ErrNotSupported
}
func (m *MemoryCache) SetIsMember(key string, member []byte) (bool, error) {
	return false, ErrNotSupported
}

var (
	_ Cache            = (*MemoryCache)(nil)
	_ CacheWithContext = (*MemoryCache)(nil)
)
