package gcache

import (
	"os"
	"testing"
)

func TestValkeyCache(t *testing.T) {
	valkeyAddr := os.Getenv("VALKEY_ADDR")
	if valkeyAddr == "" {
		valkeyAddr = "localhost:6379"
	}

	cache, err := NewValkeyCache(WithAddress(valkeyAddr))
	if err != nil {
		t.Skipf("skip valkey integration test: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	runCacheTestSuite(t, cache)
}
