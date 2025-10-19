package gcache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCacheBasic(t *testing.T) {
	cache := NewMemoryCache(10 * time.Millisecond)
	t.Cleanup(func() { _ = cache.Close() })

	ctx := context.Background()

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

	ttl, err := cache.TTL(ctx, "foo")
	if err != nil {
		t.Fatalf("TTL failed: %v", err)
	}
	if ttl != -1 {
		t.Fatalf("expected ttl -1 for persistent key, got %v", ttl)
	}

	if err := cache.Expire(ctx, "foo", 30*time.Millisecond); err != nil {
		t.Fatalf("Expire failed: %v", err)
	}

	ttl, err = cache.TTL(ctx, "foo")
	if err != nil {
		t.Fatalf("TTL after expire failed: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("expected positive ttl, got %v", ttl)
	}

	time.Sleep(40 * time.Millisecond)
	if _, err := cache.Get(ctx, "foo"); err != ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss after expiration, got %v", err)
	}

	if _, err := cache.Increment(ctx, "counter", 5); err != nil {
		t.Fatalf("Increment failed: %v", err)
	}
	if value, err := cache.Get(ctx, "counter"); err != nil || string(value) != "5" {
		t.Fatalf("unexpected counter value %q, err=%v", value, err)
	}

	if _, err := cache.Decrement(ctx, "counter", 2); err != nil {
		t.Fatalf("Decrement failed: %v", err)
	}

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

	if err := cache.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}
