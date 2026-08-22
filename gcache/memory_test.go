package gcache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCloneBytes(t *testing.T) {
	b := []byte("hello")
	c := cloneBytes(b)
	if string(c) != string(b) {
		t.Errorf("expected %s, got %s", b, c)
	}

	b[0] = 'H'
	if string(c) == string(b) {
		t.Error("expected copy to be independent")
	}

	if cloneBytes(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func newTestMemoryCache(t *testing.T) *MemoryCache {
	t.Helper()
	cache, err := NewMemoryCache(WithCleanupInterval(10 * time.Millisecond))
	if err != nil {
		t.Fatalf("NewMemoryCache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	return cache
}

func TestMemoryCacheExpireClearsExpirationOnNonPositiveTTL(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()

	if err := cache.Set(ctx, "key", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cache.Expire(ctx, "key", 0); err != nil {
		t.Fatalf("Expire: %v", err)
	}

	ttl, err := cache.TTL(ctx, "key")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl >= 0 {
		t.Fatalf("TTL = %v, want a negative value meaning no expiration", ttl)
	}
}

func TestMemoryCacheExpireOnAbsentKey(t *testing.T) {
	cache := newTestMemoryCache(t)

	if err := cache.Expire(context.Background(), "absent", time.Minute); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Expire on absent key = %v, want ErrCacheMiss", err)
	}
}

func TestMemoryCacheAddRejectsNonNumericValue(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()

	if err := cache.Set(ctx, "text", []byte("not a number"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := cache.Add(ctx, "text", 1); err == nil {
		t.Fatal("Add on a non-numeric value should fail")
	}
}

func TestMemoryCacheAddPreservesExpiration(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()

	if _, err := cache.Add(ctx, "counter", 1); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := cache.Expire(ctx, "counter", time.Minute); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if _, err := cache.Add(ctx, "counter", 1); err != nil {
		t.Fatalf("second Add: %v", err)
	}

	ttl, err := cache.TTL(ctx, "counter")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("TTL = %v, want the expiration to survive Add", ttl)
	}
}

func TestMemoryCacheBackgroundCleanupEvictsExpiredItems(t *testing.T) {
	cache := newTestMemoryCache(t)
	ctx := context.Background()

	if err := cache.Set(ctx, "gone", []byte("v"), 5*time.Millisecond); err != nil {
		t.Fatalf("Set expiring: %v", err)
	}
	if err := cache.Set(ctx, "stays", []byte("v"), 0); err != nil {
		t.Fatalf("Set permanent: %v", err)
	}

	// 轮询而不是固定 sleep，避免慢机器上的偶发失败。
	deadline := time.Now().Add(2 * time.Second)
	for {
		cache.mu.RLock()
		_, present := cache.items["gone"]
		cache.mu.RUnlock()
		if !present {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background cleanup did not evict the expired item")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cache.mu.RLock()
	_, kept := cache.items["stays"]
	cache.mu.RUnlock()
	if !kept {
		t.Fatal("background cleanup evicted an item that never expires")
	}
}

func TestMemoryCacheNonPositiveCleanupIntervalStartsNoTicker(t *testing.T) {
	cache, err := NewMemoryCache(WithCleanupInterval(0))
	if err != nil {
		t.Fatalf("NewMemoryCache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	ctx := context.Background()
	if err := cache.Set(ctx, "key", []byte("v"), 10*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	// 没有后台清理，过期项仍必须在读取时被剔除。
	if _, err := cache.Get(ctx, "key"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get after expiry = %v, want ErrCacheMiss", err)
	}
}

func TestMemoryCacheCloseIsIdempotent(t *testing.T) {
	cache, err := NewMemoryCache()
	if err != nil {
		t.Fatalf("NewMemoryCache: %v", err)
	}
	if err := cache.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cache.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestMemoryCachePing(t *testing.T) {
	cache := newTestMemoryCache(t)
	if err := cache.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
