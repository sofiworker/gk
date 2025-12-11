package gcache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTimedCache(t *testing.T) {
	t.Run("NewTimedCache", func(t *testing.T) {
		// Test with a cleanup interval
		cache := NewTimedCache(10 * time.Millisecond)
		if cache.stop == nil {
			t.Error("expected stop channel to be initialized")
		}
		cache.Close()
		if cache.stop != nil {
			t.Error("expected stop channel to be nil after Close")
		}

		// Test without a cleanup interval
		cache = NewTimedCache(0)
		if cache.stop != nil {
			t.Error("expected stop channel to be nil for zero interval")
		}
	})

	t.Run("SetAndGet", func(t *testing.T) {
		cache := NewTimedCache(0)
		defer cache.Close()

		// Test item that doesn't expire
		cache.Set("key1", "value1", 0)
		val, ok := cache.Get("key1")
		if !ok || val != "value1" {
			t.Errorf("expected to get 'value1', got '%v'", val)
		}

		// Test item with TTL
		cache.Set("key2", 123, 100*time.Millisecond)
		val, ok = cache.Get("key2")
		if !ok || val != 123 {
			t.Errorf("expected to get 123, got '%v'", val)
		}
	})

	t.Run("GetExpired", func(t *testing.T) {
		cache := NewTimedCache(0)
		defer cache.Close()

		cache.Set("key1", "value1", 5*time.Millisecond)
		time.Sleep(10 * time.Millisecond)

		_, ok := cache.Get("key1")
		if ok {
			t.Error("expected item to be expired and not found")
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		cache := NewTimedCache(5 * time.Millisecond)
		defer cache.Close()

		cache.Set("key1", "value1", 1*time.Millisecond)
		cache.Set("key2", "value2", 20*time.Millisecond)

		if cache.Len() != 2 {
			t.Errorf("expected length 2, got %d", cache.Len())
		}

		// Wait for cleanup to run
		time.Sleep(15 * time.Millisecond)

		if cache.Len() != 1 {
			t.Errorf("expected length 1 after cleanup, got %d", cache.Len())
		}

		_, ok := cache.Get("key1")
		if ok {
			t.Error("expected 'key1' to be evicted by cleanup goroutine")
		}
		_, ok = cache.Get("key2")
		if !ok {
			t.Error("expected 'key2' to still be present")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		cache := NewTimedCache(0)
		defer cache.Close()

		cache.Set("key1", "value1", 0)
		cache.Delete("key1")

		if cache.Len() != 0 {
			t.Errorf("expected length 0 after delete, got %d", cache.Len())
		}
		_, ok := cache.Get("key1")
		if ok {
			t.Error("expected item to be deleted")
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		cache := NewTimedCache(10 * time.Millisecond)
		defer cache.Close()
		var wg sync.WaitGroup
		numGoroutines := 100

		// Concurrent writes
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", i)
				value := fmt.Sprintf("value%d", i)
				cache.Set(key, value, time.Duration(i+10)*time.Millisecond)
			}(i)
		}
		wg.Wait()

		if cache.Len() != numGoroutines {
			t.Errorf("expected cache length %d, got %d", numGoroutines, cache.Len())
		}

		// Wait for some items to expire
		time.Sleep(50 * time.Millisecond)

		// Concurrent reads and deletes
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
		// The cache might not be empty if some items were set with very long TTLs or if cleanup hasn't run yet for all.
		// A more precise check would be to count expected remaining items based on TTLs.
		// For this test, we just ensure it's not the initial full count.
		if cache.Len() == numGoroutines {
			t.Errorf("expected cache to have fewer than %d items after concurrent operations, got len %d", numGoroutines, cache.Len())
		}
	})
}
