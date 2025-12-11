package gcache

import (
	"container/list"
	"sync"
)

// lruEntry is the type stored in the linked list.
type lruEntry struct {
	key   string
	value interface{}
}

// LRUCache is a non-thread-safe LRU (Least Recently Used) cache.
// It provides O(1) time complexity for both Get and Set operations.
type LRUCache struct {
	capacity int
	ll       *list.List
	cache    map[string]*list.Element
}

// NewLRUCache creates a new LRUCache with the specified capacity.
// The capacity must be greater than 0.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRUCache{
		capacity: capacity,
		ll:       list.New(),
		cache:    make(map[string]*list.Element),
	}
}

// Get retrieves a value from the cache for the given key.
// If the key is found, it marks the entry as recently used.
func (l *LRUCache) Get(key string) (interface{}, bool) {
	if elem, ok := l.cache[key]; ok {
		l.ll.MoveToFront(elem)
		return elem.Value.(*lruEntry).value, true
	}
	return nil, false
}

// Set adds or updates a key-value pair in the cache.
// If the key already exists, its value is updated and it's marked as recently used.
// If the key does not exist and the cache is full, the least recently used entry is evicted.
func (l *LRUCache) Set(key string, value interface{}) {
	// If the key already exists, update the value and move it to the front.
	if elem, ok := l.cache[key]; ok {
		l.ll.MoveToFront(elem)
		elem.Value.(*lruEntry).value = value
		return
	}

	// If the key doesn't exist, we need to add a new entry.
	// First, check if we need to evict the least recently used entry.
	if l.ll.Len() >= l.capacity {
		back := l.ll.Back()
		if back != nil {
			l.ll.Remove(back)
			delete(l.cache, back.Value.(*lruEntry).key)
		}
	}

	// Add the new entry to the front of the list and to the cache map.
	newElem := l.ll.PushFront(&lruEntry{key: key, value: value})
	l.cache[key] = newElem
}

// Len returns the current number of items in the cache.
func (l *LRUCache) Len() int {
	return l.ll.Len()
}

// --- Thread-Safe LRU Cache ---

// ThreadSafeLRUCache is a thread-safe wrapper around LRUCache.
type ThreadSafeLRUCache struct {
	lru  *LRUCache
	lock sync.RWMutex
}

// NewThreadSafeLRUCache creates a new thread-safe LRUCache with the specified capacity.
func NewThreadSafeLRUCache(capacity int) *ThreadSafeLRUCache {
	return &ThreadSafeLRUCache{
		lru: NewLRUCache(capacity),
	}
}

// Get retrieves a value from the cache in a thread-safe manner.
func (c *ThreadSafeLRUCache) Get(key string) (interface{}, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.lru.Get(key)
}

// Set adds or updates a key-value pair in the cache in a thread-safe manner.
func (c *ThreadSafeLRUCache) Set(key string, value interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.lru.Set(key, value)
}

// Len returns the current number of items in the cache in a thread-safe manner.
func (c *ThreadSafeLRUCache) Len() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.lru.Len()
}
