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
		t.Skipf("skip redis integration test: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	runCacheTestSuite(t, cache)
}
