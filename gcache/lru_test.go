package gcache

import (
	"fmt"
	"sync"
	"testing"
)

func TestLRUCache(t *testing.T) {
	t.Run("NewLRUCache", func(t *testing.T) {
		cache := NewLRUCache[string, any](10)
		if cache.capacity != 10 {
			t.Errorf("expected capacity 10, got %d", cache.capacity)
		}

		// 零容量应默认为 1；zero capacity defaults to 1.
		cache = NewLRUCache[string, any](0)
		if cache.capacity != 1 {
			t.Errorf("expected capacity 1 for zero input, got %d", cache.capacity)
		}

		// 负容量应默认为 1；negative capacity defaults to 1.
		cache = NewLRUCache[string, any](-5)
		if cache.capacity != 1 {
			t.Errorf("expected capacity 1 for negative input, got %d", cache.capacity)
		}
	})

	t.Run("SetAndGet", func(t *testing.T) {
		cache := NewLRUCache[string, any](2)

		cache.Set("key1", "value1")
		val, ok := cache.Get("key1")
		if !ok || val != "value1" {
			t.Errorf("expected to get 'value1' for 'key1', got '%v'", val)
		}

		cache.Set("key2", 123)
		val, ok = cache.Get("key2")
		if !ok || val != 123 {
			t.Errorf("expected to get 123 for 'key2', got '%v'", val)
		}
	})

	t.Run("TypedValue", func(t *testing.T) {
		// 泛型让值免去类型断言；generics remove the need for a type assertion on the value.
		cache := NewLRUCache[int, string](2)
		cache.Set(1, "one")
		val, ok := cache.Get(1)
		if !ok || val != "one" {
			t.Errorf("expected 'one' for key 1, got %q", val)
		}
	})

	t.Run("Eviction", func(t *testing.T) {
		cache := NewLRUCache[string, string](2)

		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		cache.Set("key3", "value3") // 应淘汰 key1；should evict key1.

		if _, ok := cache.Get("key1"); ok {
			t.Error("expected 'key1' to be evicted")
		}
		if _, ok := cache.Get("key2"); !ok {
			t.Error("expected 'key2' to be present")
		}
		if _, ok := cache.Get("key3"); !ok {
			t.Error("expected 'key3' to be present")
		}
	})

	t.Run("UpdateMovesToFront", func(t *testing.T) {
		cache := NewLRUCache[string, string](2)

		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		cache.Get("key1")           // 访问 key1 使其成为最近使用；mark key1 as recently used.
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

	t.Run("Len", func(t *testing.T) {
		cache := NewLRUCache[string, string](3)
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
		cache.Set("key1", "new_value1") // 更新；update.
		if cache.Len() != 2 {
			t.Errorf("expected length 2 after update, got %d", cache.Len())
		}
		cache.Set("key3", "value3")
		cache.Set("key4", "value4") // 淘汰；evict.
		if cache.Len() != 3 {
			t.Errorf("expected length 3 after eviction, got %d", cache.Len())
		}
	})

	// 并发读写：Get 会 MoveToFront 改动链表，因此内建锁下并发 Get 也必须安全。
	// 需配合 -race 才能捕捉回归。
	t.Run("ConcurrentSetAndGet", func(t *testing.T) {
		cache := NewLRUCache[string, string](100)
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

	// 全并发 Get 同一批键：直击「Get 修改共享链表」的竞争点。
	t.Run("ConcurrentGetOnly", func(t *testing.T) {
		cache := NewLRUCache[int, int](256)
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
		cache := NewLRUCache[string, string](capacity)
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
			t.Errorf("expected cache length to be %d after concurrent sets, got %d", capacity, cache.Len())
		}

		// 无法确定具体淘汰了哪些键，只断言条目数等于容量；
		// which keys are evicted is nondeterministic; only assert the final size.
		var presentCount int
		for i := 0; i < numItems; i++ {
			key := fmt.Sprintf("key%d", i)
			if _, ok := cache.Get(key); ok {
				presentCount++
			}
		}
		if presentCount != capacity {
			t.Errorf("expected %d items to be present in the cache, found %d", capacity, presentCount)
		}
	})
}
