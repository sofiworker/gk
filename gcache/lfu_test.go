package gcache

import (
	"fmt"
	"sync"
	"testing"
)

func TestLFUCache(t *testing.T) {
	t.Run("NewLFUCache", func(t *testing.T) {
		cache := NewLFUCache[string, any](10)
		if cache.capacity != 10 {
			t.Errorf("expected capacity 10, got %d", cache.capacity)
		}
		cache = NewLFUCache[string, any](0)
		if cache.capacity != 1 {
			t.Errorf("expected capacity 1 for zero input, got %d", cache.capacity)
		}
	})

	t.Run("SetAndGet", func(t *testing.T) {
		cache := NewLFUCache[string, string](2)
		cache.Set("key1", "value1")
		val, ok := cache.Get("key1")
		if !ok || val != "value1" {
			t.Errorf("expected to get 'value1' for 'key1', got '%v'", val)
		}
	})

	t.Run("TypedValue", func(t *testing.T) {
		cache := NewLFUCache[int, string](2)
		cache.Set(1, "one")
		val, ok := cache.Get(1)
		if !ok || val != "one" {
			t.Errorf("expected 'one' for key 1, got %q", val)
		}
	})

	t.Run("Eviction", func(t *testing.T) {
		cache := NewLFUCache[string, string](2)
		cache.Set("key1", "value1") // 频率 1；freq 1.
		cache.Set("key2", "value2") // 频率 1；freq 1.

		// 访问 key1 以提升频率；access key1 to increase its frequency.
		cache.Get("key1") // key1 频率 2，key2 频率 1；key1 freq 2, key2 freq 1.

		cache.Set("key3", "value3") // 应淘汰 key2；should evict key2.

		if _, ok := cache.Get("key2"); ok {
			t.Error("expected 'key2' to be evicted")
		}
		if _, ok := cache.Get("key1"); !ok {
			t.Error("expected 'key1' to be present")
		}
		if _, ok := cache.Get("key3"); !ok {
			t.Error("expected 'key3' to be present")
		}
	})

	t.Run("EvictionWithSameFrequency", func(t *testing.T) {
		cache := NewLFUCache[string, string](2)
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		// 两者频率均为 1，key1 先加入（该频率内按 LRU）；both have freq 1, key1 added first.

		cache.Set("key3", "value3") // 应淘汰 key1；should evict key1.

		if _, ok := cache.Get("key1"); ok {
			t.Error("expected 'key1' to be evicted")
		}
		if _, ok := cache.Get("key2"); !ok {
			t.Error("expected 'key2' to be present")
		}
	})

	t.Run("UpdateValue", func(t *testing.T) {
		cache := NewLFUCache[string, string](1)
		cache.Set("key1", "value1")
		cache.Set("key1", "new_value")

		val, ok := cache.Get("key1")
		if !ok || val != "new_value" {
			t.Errorf("expected value to be updated to 'new_value', got '%v'", val)
		}
	})

	t.Run("Len", func(t *testing.T) {
		cache := NewLFUCache[string, string](2)
		if cache.Len() != 0 {
			t.Errorf("expected length 0, got %d", cache.Len())
		}
		cache.Set("key1", "value1")
		if cache.Len() != 1 {
			t.Errorf("expected length 1, got %d", cache.Len())
		}
		cache.Set("key2", "value2")
		if cache.Len() != 2 {
			t.Errorf("expected length 2, got %d", cache.Len())
		}
		cache.Set("key1", "new_value") // 更新；update.
		if cache.Len() != 2 {
			t.Errorf("expected length 2 after update, got %d", cache.Len())
		}
		cache.Set("key3", "value3") // 淘汰；evict.
		if cache.Len() != 2 {
			t.Errorf("expected length 2 after eviction, got %d", cache.Len())
		}
	})

	t.Run("ConcurrentSetAndGet", func(t *testing.T) {
		cache := NewLFUCache[string, string](100)
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				cache.Set(key, fmt.Sprintf("value%d", i))
			}(i)
		}
		wg.Wait()

		if cache.Len() != numGoroutines {
			t.Errorf("expected cache length %d, got %d", numGoroutines, cache.Len())
		}

		// Get 会提升频率（写共享状态），并发 Get 必须安全（配合 -race）。
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				want := fmt.Sprintf("value%d", i)
				if val, ok := cache.Get(key); !ok || val != want {
					t.Errorf("failed to get correct value for %s", key)
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("ConcurrentGetOnly", func(t *testing.T) {
		cache := NewLFUCache[int, int](256)
		for i := 0; i < 100; i++ {
			cache.Set(i, i)
		}

		var wg sync.WaitGroup
		for g := 0; g < 16; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					cache.Get(i)
				}
			}()
		}
		wg.Wait()
	})

	t.Run("ConcurrentEviction", func(t *testing.T) {
		capacity := 10
		cache := NewLFUCache[string, string](capacity)
		var wg sync.WaitGroup
		numItems := 20

		for i := 0; i < numItems; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				cache.Set(key, fmt.Sprintf("value%d", i))
			}(i)
		}
		wg.Wait()

		if cache.Len() != capacity {
			t.Errorf("expected cache length to be %d, got %d", capacity, cache.Len())
		}
	})
}
