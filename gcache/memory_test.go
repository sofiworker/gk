package gcache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryCacheBasic(t *testing.T) {
	cache := NewMemoryCache(10 * time.Millisecond)
	t.Cleanup(func() { _ = cache.Close() })

	ctx := context.Background()

	// Test Set and Get
	if err := cache.Set(ctx, "foo", []byte("bar"), 0); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := cache.Get(ctx, "foo")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got) != "bar" {
		t.Fatalf("unexpected value: %s", got)
	}

	// Test Expiration defaults
	ttl, err := cache.TTL(ctx, "foo")
	if err != nil {
		t.Fatalf("TTL failed: %v", err)
	}
	if ttl != -1 {
		t.Fatalf("expected ttl -1 for persistent key, got %v", ttl)
	}

	// Test Expire
	if err := cache.Expire(ctx, "foo", 50*time.Millisecond); err != nil {
		t.Fatalf("Expire failed: %v", err)
	}

	ttl, err = cache.TTL(ctx, "foo")
	if err != nil {
		t.Fatalf("TTL after expire failed: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("expected positive ttl, got %v", ttl)
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)
	if _, err := cache.Get(ctx, "foo"); err != ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss after expiration, got %v", err)
	}

	// Test Increment
	if _, err := cache.Increment(ctx, "counter", 5); err != nil {
		t.Fatalf("Increment failed: %v", err)
	}
	if value, err := cache.Get(ctx, "counter"); err != nil || string(value) != "5" {
		t.Fatalf("unexpected counter value %q, err=%v", value, err)
	}

	// Test Increment on existing
	if _, err := cache.Increment(ctx, "counter", 2); err != nil {
		t.Fatalf("Increment existing failed: %v", err)
	}
	if value, err := cache.Get(ctx, "counter"); err != nil || string(value) != "7" {
		t.Fatalf("unexpected counter value %q, err=%v", value, err)
	}

	// Test Decrement
	if _, err := cache.Decrement(ctx, "counter", 2); err != nil {
		t.Fatalf("Decrement failed: %v", err)
	}
	if value, err := cache.Get(ctx, "counter"); err != nil || string(value) != "5" {
		t.Fatalf("unexpected counter value %q, err=%v", value, err)
	}

	// Test Delete
	if err := cache.Delete(ctx, "counter"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	exists, err := cache.Exists(ctx, "counter")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Fatalf("expected counter to be deleted")
	}

	// Test Ping
	if err := cache.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestMemoryCache_Boundaries(t *testing.T) {
	// Test NewMemoryCache with invalid cleanup interval
	cache := NewMemoryCache(0) // Should default to 1 min
	t.Cleanup(func() { _ = cache.Close() })
	
	ctx := context.Background()

	// Test Get non-existent
	if _, err := cache.Get(ctx, "missing"); err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss for missing key, got %v", err)
	}

	// Test Expire non-existent
	if err := cache.Expire(ctx, "missing", time.Minute); err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss for expiring missing key, got %v", err)
	}
	
	// Test Expire with <= 0
	_ = cache.Set(ctx, "k", []byte("v"), 0)
	if err := cache.Expire(ctx, "k", 0); err != nil {
		t.Errorf("Expire with 0 should return nil, got %v", err)
	}

	// Test TTL non-existent
	if _, err := cache.TTL(ctx, "missing"); err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss for TTL missing key, got %v", err)
	}

	// Test Increment invalid value
	_ = cache.Set(ctx, "str", []byte("abc"), 0)
	if _, err := cache.Increment(ctx, "str", 1); err == nil {
		t.Errorf("expected error incrementing non-int value, got nil")
	}

	// Test Close idempotency (indirectly via once)
	if err := cache.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if err := cache.Close(); err != nil {
		t.Errorf("Close again failed: %v", err)
	}
}

func TestMemoryCache_Concurrent(t *testing.T) {
	cache := NewMemoryCache(time.Minute)
	t.Cleanup(func() { _ = cache.Close() })
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "key"
			_ = cache.Set(ctx, key, []byte("val"), 0)
			_, _ = cache.Get(ctx, key)
			_, _ = cache.Increment(ctx, "cnt", 1)
		}(i)
	}
	wg.Wait()
	
	val, err := cache.Get(ctx, "cnt")
	if err != nil {
		t.Fatalf("failed to get cnt: %v", err)
	}
	if string(val) != "100" {
		t.Errorf("expected 100, got %s", val)
	}
}

func TestMemoryCache_Cleanup(t *testing.T) {
	// Short cleanup interval
	cache := NewMemoryCache(10 * time.Millisecond)
	t.Cleanup(func() { _ = cache.Close() })
	ctx := context.Background()

	_ = cache.Set(ctx, "k1", []byte("v1"), 20*time.Millisecond)
	_ = cache.Set(ctx, "k2", []byte("v2"), 0) // persistent

	time.Sleep(50 * time.Millisecond)

	// k1 should be removed by cleanup loop or Get check
	// We want to verify cleanup loop removed it without calling Get first if possible,
	// but Get is the only way to observe unless we inspect internals.
	// However, if we access the map directly (using reflection or unsafe, or just trusting Get)
	// But let's just use Exists
	
	exists, _ := cache.Exists(ctx, "k1")
	if exists {
		t.Error("k1 should have expired")
	}
	exists, _ = cache.Exists(ctx, "k2")
	if !exists {
		t.Error("k2 should exist")
	}
}