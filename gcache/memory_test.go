package gcache

import (
	"testing"
	"time"
)

func TestMemoryCache(t *testing.T) {
	cache, err := NewMemoryCache(WithCleanupInterval(10 * time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create MemoryCache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	runCacheTestSuite(t, cache)
}
