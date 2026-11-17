package gcache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTimedCache(t *testing.T) {
	t.Run("CloseIsIdempotent", func(t *testing.T) {
		cache := NewTimedCache[string, string](10 * time.Millisecond)
		cache.Close()
		cache.Close() // 二次 Close 不得 panic；a second Close must not panic.

		// 零间隔不启动后台清理，但 Close 仍安全。
		zero := NewTimedCache[string, string](0)
		zero.Close()
	})

	t.Run("SetAndGet", func(t *testing.T) {
		cache := NewTimedCache[string, any](0)
		defer cache.Close()

		cache.Set("key1", "value1", 0)
		val, ok := cache.Get("key1")
		if !ok || val != "value1" {
			t.Errorf("expected to get 'value1', got '%v'", val)
		}

		cache.Set("key2", 123, 100*time.Millisecond)
		val, ok = cache.Get("key2")
		if !ok || val != 123 {
			t.Errorf("expected to get 123, got '%v'", val)
		}
	})

	t.Run("TypedValue", func(t *testing.T) {
		cache := NewTimedCache[int, string](0)
		defer cache.Close()

		cache.Set(1, "one", 0)
		val, ok := cache.Get(1)
		if !ok || val != "one" {
			t.Errorf("expected 'one' for key 1, got %q", val)
		}
	})

	t.Run("GetExpired", func(t *testing.T) {
		cache := NewTimedCache[string, string](0)
		defer cache.Close()

		cache.Set("key1", "value1", 5*time.Millisecond)
		time.Sleep(10 * time.Millisecond)

		if _, ok := cache.Get("key1"); ok {
			t.Error("expected item to be expired and not found")
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		cache := NewTimedCache[string, string](5 * time.Millisecond)
		defer cache.Close()

		cache.Set("key1", "value1", 1*time.Millisecond)
		cache.Set("key2", "value2", 500*time.Millisecond)

		// 轮询等待后台清理，避免慢机器上的偶发失败。
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, ok := cache.Get("key1"); !ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("expected 'key1' to be evicted by the cleanup goroutine")
			}
			time.Sleep(2 * time.Millisecond)
		}

		if _, ok := cache.Get("key2"); !ok {
			t.Error("expected 'key2' to still be present")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		cache := NewTimedCache[string, string](0)
		defer cache.Close()

		cache.Set("key1", "value1", 0)
		cache.Delete("key1")

		if cache.Len() != 0 {
			t.Errorf("expected length 0 after delete, got %d", cache.Len())
		}
		if _, ok := cache.Get("key1"); ok {
			t.Error("expected item to be deleted")
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		cache := NewTimedCache[string, string](10 * time.Millisecond)
		defer cache.Close()
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				cache.Set(key, fmt.Sprintf("value%d", i), time.Duration(i+10)*time.Millisecond)
			}(i)
		}
		wg.Wait()

		if cache.Len() != numGoroutines {
			t.Errorf("expected cache length %d, got %d", numGoroutines, cache.Len())
		}

		// 等待部分条目过期；wait for some items to expire.
		time.Sleep(50 * time.Millisecond)

		var foundCount int
		var mu sync.Mutex
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				if _, ok := cache.Get(key); ok {
					mu.Lock()
					foundCount++
					mu.Unlock()
					cache.Delete(key)
				}
			}(i)
		}
		wg.Wait()

		if foundCount == 0 {
			t.Error("expected some items to be found before deletion")
		}
		// 长 TTL 或清理未完成时缓存可能非空，这里只确保数量已减少；
		// long TTLs or pending cleanup may keep items; only assert the count decreased.
		if cache.Len() == numGoroutines {
			t.Errorf("expected cache to have fewer than %d items after concurrent operations, got len %d", numGoroutines, cache.Len())
		}
	})
}
