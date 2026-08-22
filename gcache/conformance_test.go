package gcache_test

import (
	"testing"
	"time"

	"github.com/sofiworker/gk/gcache"
	"github.com/sofiworker/gk/gcache/cachetest"
)

func TestMemoryCacheConformance(t *testing.T) {
	cache, err := gcache.NewMemoryCache(gcache.WithCleanupInterval(10 * time.Millisecond))
	if err != nil {
		t.Fatalf("NewMemoryCache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	t.Run("KV", func(t *testing.T) { cachetest.RunKV(t, cache) })
	t.Run("TTL", func(t *testing.T) { cachetest.RunTTL(t, cache) })
	t.Run("Counter", func(t *testing.T) { cachetest.RunCounter(t, cache) })
}

// 用 Funcs 包一层 MemoryCache，模拟用户注入的后端：验证套件对注入实现同样成立，
// 且只声明 KV 的实现会自动跳过 TTLReader/Expirer 断言。
func TestInjectedFuncsConformance(t *testing.T) {
	backing, err := gcache.NewMemoryCache(gcache.WithCleanupInterval(10 * time.Millisecond))
	if err != nil {
		t.Fatalf("NewMemoryCache: %v", err)
	}
	t.Cleanup(func() { _ = backing.Close() })

	injected := gcache.Funcs{
		GetFunc:    backing.Get,
		SetFunc:    backing.Set,
		DeleteFunc: backing.Delete,
	}

	t.Run("KV", func(t *testing.T) { cachetest.RunKV(t, injected) })
	t.Run("TTL", func(t *testing.T) { cachetest.RunTTL(t, injected) })
}
