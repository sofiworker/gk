package gcache

import (
	"os"
	"testing"
)

func TestRedisCache(t *testing.T) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	cache, err := NewRedisCache(WithAddress(redisAddr))
	if err != nil {
		t.Fatalf("Failed to create RedisCache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	runCacheTestSuite(t, cache)
}
