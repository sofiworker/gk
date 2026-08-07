package gcache

import (
	"fmt"
	"sync"
	"testing"
)

func TestLFUCache(t *testing.T) {
	t.Run("NewLFUCache", func(t *testing.T) {
		cache := NewLFUCache(10)
		if cache.capacity != 10 {
			t.Errorf("expected capacity 10, got %d", cache.capacity)
		}
		cache = NewLFUCache(0)
		if cache.capacity != 1 {
			t.Errorf("expected capacity 1 for zero input, got %d", cache.capacity)
		}
	})

	t.Run("SetAndGet", func(t *testing.T) {
		cache := NewLFUCache(2)
		cache.Set("key1", "value1")
		val, ok := cache.Get("key1")
		if !ok || val != "value1" {
			t.Errorf("expected to get 'value1' for 'key1', got '%v'", val)
		}
	})

	t.Run("Eviction", func(t *testing.T) {
		cache := NewLFUCache(2)
		cache.Set("key1", "value1") // 频率 1；freq 1.
		cache.Set("key2", "value2") // 频率 1；freq 1.

		// 访问 key1 以提升频率；access key1 to increase its frequency.
		cache.Get("key1") // key1 频率 2，key2 频率 1；key1 freq 2, key2 freq 1.

		cache.Set("key3", "value3") // 应淘汰 key2；should evict key2.

		// key2 频率最低，应被淘汰；key2 is least frequently used and should be evicted.
		_, ok := cache.Get("key2")
		if ok {
			t.Error("expected 'key2' to be evicted")
		}

		// key1 与 key3 应仍存在；key1 and key3 should remain.
		_, ok = cache.Get("key1")
		if !ok {
			t.Error("expected 'key1' to be present")
		}
		_, ok = cache.Get("key3")
		if !ok {
			t.Error("expected 'key3' to be present")
		}
	})

	t.Run("EvictionWithSameFrequency", func(t *testing.T) {
		cache := NewLFUCache(2)
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		// 两者频率均为 1，key1 先加入（该频率内按 LRU）；both have freq 1, key1 added first.

		cache.Set("key3", "value3") // 应淘汰 key1；should evict key1.

		// key1 应被淘汰；key1 should be evicted.
		_, ok := cache.Get("key1")
		if ok {
			t.Error("expected 'key1' to be evicted")
		}
		_, ok = cache.Get("key2")
		if !ok {
			t.Error("expected 'key2' to be present")
		}
	})

	t.Run("UpdateValue", func(t *testing.T) {
		cache := NewLFUCache(1)
		cache.Set("key1", "value1")
		cache.Set("key1", "new_value")

		val, ok := cache.Get("key1")
		if !ok || val != "new_value" {
			t.Errorf("expected value to be updated to 'new_value', got '%v'", val)
		}
	})

	t.Run("Len", func(t *testing.T) {
		cache := NewLFUCache(2)
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
}

func TestThreadSafeLFUCache(t *testing.T) {
	t.Run("ConcurrentSetAndGet", func(t *testing.T) {
		cache := NewThreadSafeLFUCache(100)
		var wg sync.WaitGroup
		numGoroutines := 50

		// 并发写入；concurrent writes.
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				value := fmt.Sprintf("value%d", i)
				cache.Set(key, value)
			}(i)
		}
		wg.Wait()

		if cache.Len() != numGoroutines {
			t.Errorf("expected cache length %d, got %d", numGoroutines, cache.Len())
		}

		// 并发读取与更新；concurrent reads and updates.
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				expectedValue := fmt.Sprintf("value%d", i)
				val, ok := cache.Get(key) // 该 Get 会提升频率；this Get increments the frequency.
				if !ok || val != expectedValue {
					t.Errorf("failed to get correct value for %s", key)
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("ConcurrentEviction", func(t *testing.T) {
		capacity := 10
		cache := NewThreadSafeLFUCache(capacity)
		var wg sync.WaitGroup
		numItems := 20

		// 全部频率为 1，将按频率 1 链表内的 LRU 顺序淘汰；
		// all have freq 1, so eviction follows LRU within the freq-1 list.
		for i := 0; i < numItems; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				value := fmt.Sprintf("value%d", i)
				cache.Set(key, value)
			}(i)
		}
		wg.Wait()

		if cache.Len() != capacity {
			t.Errorf("expected cache length to be %d, got %d", capacity, cache.Len())
		}
	})
}
