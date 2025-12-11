package gcache

import (
	"fmt"
	"sync"
	"testing"
)

func TestLRUCache(t *testing.T) {
	t.Run("NewLRUCache", func(t *testing.T) {
		// Test with a valid capacity
		cache := NewLRUCache(10)
		if cache.capacity != 10 {
			t.Errorf("expected capacity 10, got %d", cache.capacity)
		}

		// Test with zero capacity, should default to 1
		cache = NewLRUCache(0)
		if cache.capacity != 1 {
			t.Errorf("expected capacity 1 for zero input, got %d", cache.capacity)
		}

		// Test with negative capacity, should default to 1
		cache = NewLRUCache(-5)
		if cache.capacity != 1 {
			t.Errorf("expected capacity 1 for negative input, got %d", cache.capacity)
		}
	})

	t.Run("SetAndGet", func(t *testing.T) {
		cache := NewLRUCache(2)

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

	t.Run("Eviction", func(t *testing.T) {
		cache := NewLRUCache(2)

		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		cache.Set("key3", "value3") // This should evict key1

		// key1 should be evicted
		_, ok := cache.Get("key1")
		if ok {
			t.Error("expected 'key1' to be evicted")
		}

		// key2 and key3 should still be present
		_, ok = cache.Get("key2")
		if !ok {
			t.Error("expected 'key2' to be present")
		}
		_, ok = cache.Get("key3")
		if !ok {
			t.Error("expected 'key3' to be present")
		}
	})

	t.Run("UpdateMovesToFront", func(t *testing.T) {
		cache := NewLRUCache(2)

		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		cache.Get("key1")           // Access key1 to make it recently used
		cache.Set("key3", "value3") // This should evict key2

		// key2 should be evicted
		_, ok := cache.Get("key2")
		if ok {
			t.Error("expected 'key2' to be evicted")
		}

		// key1 and key3 should be present
		_, ok = cache.Get("key1")
		if !ok {
			t.Error("expected 'key1' to be present")
		}
		_, ok = cache.Get("key3")
		if !ok {
			t.Error("expected 'key3' to be present")
		}
	})

	t.Run("Len", func(t *testing.T) {
		cache := NewLRUCache(3)
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
		cache.Set("key1", "new_value1") // Update
		if cache.Len() != 2 {
			t.Errorf("expected length 2 after update, got %d", cache.Len())
		}
		cache.Set("key3", "value3")
		cache.Set("key4", "value4") // Evict
		if cache.Len() != 3 {
			t.Errorf("expected length 3 after eviction, got %d", cache.Len())
		}
	})
}

func TestThreadSafeLRUCache(t *testing.T) {
	t.Run("ConcurrentSetAndGet", func(t *testing.T) {
		cache := NewThreadSafeLRUCache(100)
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

		// Check length
		if cache.Len() != numGoroutines {
			t.Errorf("expected cache length %d, got %d", numGoroutines, cache.Len())
		}

		// Concurrent reads
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				expectedValue := fmt.Sprintf("value%d", i)
				val, ok := cache.Get(key)
				if !ok || val != expectedValue {
					t.Errorf("failed to get correct value for %s", key)
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("ConcurrentEviction", func(t *testing.T) {
		capacity := 10
		cache := NewThreadSafeLRUCache(capacity)
		var wg sync.WaitGroup
		numItems := 20

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

		// The final length should be equal to the capacity
		if cache.Len() != capacity {
			t.Errorf("expected cache length to be %d after concurrent sets, got %d", capacity, cache.Len())
		}

		// We can't deterministically know which keys were evicted,
		// but we can check that the cache contains `capacity` number of items.
		// A more complex test could track access order to predict evicted keys,
		// but for a basic concurrency test, checking the final size is a good start.
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
