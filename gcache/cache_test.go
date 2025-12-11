package gcache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJSONSerializer(t *testing.T) {
	s := JSONSerializer{}

	// Test Serialize
	data, err := s.Serialize(map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil bytes, got %v", data)
	}

	// Test Deserialize
	var v map[string]string
	err = s.Deserialize([]byte(`{"foo":"bar"}`), &v)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}
	if v["foo"] != "bar" {
		t.Fatalf("expected foo to be bar, got %v", v)
	}
}

func TestTranslateRedisError(t *testing.T) {
	if err := translateRedisError(nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestTranslateValkeyError(t *testing.T) {
	if err := translateValkeyError(nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCloneBytes(t *testing.T) {
	b := []byte("hello")
	c := cloneBytes(b)
	if string(c) != string(b) {
		t.Errorf("expected %s, got %s", b, c)
	}
	// Modify original
	b[0] = 'H'
	if string(c) == string(b) {
		t.Error("expected copy to be independent")
	}

	if cloneBytes(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestRedisCache_New_Fail(t *testing.T) {
	_, err := NewRedisCache(WithAddress("invalid_address:6379"))
	if err == nil {
		t.Error("expected error for invalid address, got nil")
	}
}

func TestValkeyCache_New_Fail(t *testing.T) {
	_, err := NewValkeyCache(WithAddress("invalid_address:6379"))
	if err == nil {
		t.Error("expected error for invalid address, got nil")
	}
}

// runCacheTestSuite runs a suite of tests for a Cache implementation.
func runCacheTestSuite(t *testing.T, cache Cache) {
	cacheWithContext, ok := cache.(CacheWithContext)
	if !ok {
		t.Fatalf("cache does not implement CacheWithContext")
	}

	t.Run("Ping", func(t *testing.T) {
		if err := cache.Ping(); err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
		if err := cacheWithContext.PingWithContext(context.Background()); err != nil {
			t.Fatalf("PingWithContext failed: %v", err)
		}
	})

	t.Run("KeyValue", func(t *testing.T) {
		// Set
		if err := cache.Set("key1", []byte("value1"), 10*time.Second); err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		// Get
		val, err := cache.Get("key1")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if string(val) != "value1" {
			t.Errorf("Get: expected 'value1', got '%s'", string(val))
		}

		// Get non-existent
		_, err = cache.Get("non-existent-key")
		if !errors.Is(err, ErrCacheMiss) {
			t.Errorf("Get non-existent: expected ErrCacheMiss, got %v", err)
		}

		// Exists
		exists, err := cache.Exists("key1")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if !exists {
			t.Error("Exists: expected key1 to exist")
		}

		// Delete
		if err := cache.Delete("key1"); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		exists, err = cache.Exists("key1")
		if err != nil {
			t.Fatalf("Exists after delete failed: %v", err)
		}
		if exists {
			t.Error("Exists: expected key1 to be deleted")
		}
	})

	t.Run("Expiration", func(t *testing.T) {
		// Set with expiration
		if err := cache.Set("key2", []byte("value2"), 100*time.Millisecond); err != nil {
			t.Fatalf("Set with expiration failed: %v", err)
		}

		// TTL
		ttl, err := cache.TTL("key2")
		if err != nil {
			t.Fatalf("TTL failed: %v", err)
		}
		if ttl <= 0 || ttl > 100*time.Millisecond {
			t.Errorf("TTL: expected a positive duration less than or equal to 100ms, got %v", ttl)
		}

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)
		_, err = cache.Get("key2")
		if !errors.Is(err, ErrCacheMiss) {
			t.Errorf("Get after expiration: expected ErrCacheMiss, got %v", err)
		}

		// Expire
		if err := cache.Set("key3", []byte("value3"), 0); err != nil {
			t.Fatalf("Set for Expire failed: %v", err)
		}
		if err := cache.Expire("key3", 100*time.Millisecond); err != nil {
			t.Fatalf("Expire failed: %v", err)
		}
		ttl, err = cache.TTL("key3")
		if err != nil {
			t.Fatalf("TTL after Expire failed: %v", err)
		}
		if ttl <= 0 {
			t.Error("Expire: expected positive TTL")
		}
	})

	t.Run("Counter", func(t *testing.T) {
		// Increment
		newVal, err := cache.Increment("counter1", 1)
		if err != nil {
			t.Fatalf("Increment failed: %v", err)
		}
		if newVal != 1 {
			t.Errorf("Increment: expected 1, got %d", newVal)
		}
		newVal, err = cache.Increment("counter1", 4)
		if err != nil {
			t.Fatalf("Increment again failed: %v", err)
		}
		if newVal != 5 {
			t.Errorf("Increment again: expected 5, got %d", newVal)
		}

		// Decrement
		newVal, err = cache.Decrement("counter1", 2)
		if err != nil {
			t.Fatalf("Decrement failed: %v", err)
		}
		if newVal != 3 {
			t.Errorf("Decrement: expected 3, got %d", newVal)
		}
	})

	// Only run these tests if the cache is not MemoryCache
	if _, ok := cache.(*MemoryCache); !ok {
		t.Run("Hash", func(t *testing.T) {
			// HashSet
			if err := cache.HashSet("hash1", "field1", []byte("value1")); err != nil {
				t.Fatalf("HashSet failed: %v", err)
			}
			if err := cache.HashSet("hash1", "field2", []byte("value2")); err != nil {
				t.Fatalf("HashSet failed: %v", err)
			}

			// HashGet
			val, err := cache.HashGet("hash1", "field1")
			if err != nil {
				t.Fatalf("HashGet failed: %v", err)
			}
			if string(val) != "value1" {
				t.Errorf("HashGet: expected 'value1', got '%s'", string(val))
			}

			// HashGetAll
			all, err := cache.HashGetAll("hash1")
			if err != nil {
				t.Fatalf("HashGetAll failed: %v", err)
			}
			if len(all) != 2 || string(all["field1"]) != "value1" || string(all["field2"]) != "value2" {
				t.Errorf("HashGetAll: unexpected result: %v", all)
			}

			// HashDelete
			if err := cache.HashDelete("hash1", "field1"); err != nil {
				t.Fatalf("HashDelete failed: %v", err)
			}
			_, err = cache.HashGet("hash1", "field1")
			if !errors.Is(err, ErrCacheMiss) {
				t.Errorf("HashGet after delete: expected ErrCacheMiss, got %v", err)
			}
		})

		t.Run("List", func(t *testing.T) {
			// ListPush
			if err := cache.ListPush("list1", []byte("a"), []byte("b"), []byte("c")); err != nil {
				t.Fatalf("ListPush failed: %v", err)
			}

			// ListRange
			items, err := cache.ListRange("list1", 0, -1)
			if err != nil {
				t.Fatalf("ListRange failed: %v", err)
			}
			if len(items) != 3 || string(items[0]) != "a" || string(items[1]) != "b" || string(items[2]) != "c" {
				t.Errorf("ListRange: unexpected items: %v", items)
			}

			// ListPop
			item, err := cache.ListPop("list1")
			if err != nil {
				t.Fatalf("ListPop failed: %v", err)
			}
			if string(item) != "c" {
				t.Errorf("ListPop: expected 'c', got '%s'", string(item))
			}
		})

		t.Run("Set", func(t *testing.T) {
			// SetAdd
			if err := cache.SetAdd("set1", []byte("a"), []byte("b"), []byte("c")); err != nil {
				t.Fatalf("SetAdd failed: %v", err)
			}

			// SetIsMember
			isMember, err := cache.SetIsMember("set1", []byte("b"))
			if err != nil {
				t.Fatalf("SetIsMember failed: %v", err)
			}
			if !isMember {
				t.Error("SetIsMember: expected 'b' to be a member")
			}

			// SetMembers
			members, err := cache.SetMembers("set1")
			if err != nil {
				t.Fatalf("SetMembers failed: %v", err)
			}
			if len(members) != 3 {
				t.Errorf("SetMembers: expected 3 members, got %d", len(members))
			}
		})
	}
}
