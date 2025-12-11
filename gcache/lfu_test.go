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
		cache.Set("key1", "value1") // freq 1
		cache.Set("key2", "value2") // freq 1

		// Access key1 to increase its frequency
		cache.Get("key1") // key1 freq 2, key2 freq 1

		cache.Set("key3", "value3") // Should evict key2

		// key2 should be evicted as it's the least frequently used
		_, ok := cache.Get("key2")
		if ok {
			t.Error("expected 'key2' to be evicted")
		}

		// key1 and key3 should still be present
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
		// Both have frequency 1, key1 was added first (LRU in this freq)

		cache.Set("key3", "value3") // Should evict key1

		// key1 should be evicted
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
		cache.Set("key1", "new_value") // Update
		if cache.Len() != 2 {
			t.Errorf("expected length 2 after update, got %d", cache.Len())
		}
		cache.Set("key3", "value3") // Evict
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

		// Concurrent writes
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

		// Concurrent reads and updates
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				expectedValue := fmt.Sprintf("value%d", i)
				val, ok := cache.Get(key) // This Get will increment the frequency
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

		// All will have freq 1, so it will evict based on LRU within the freq 1 list
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
