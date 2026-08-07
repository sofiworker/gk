package gcache

import (
	"container/list"
	"sync"
)

type lruEntry struct {
	key   string
	value interface{}
}

// LRUCache 是非线程安全的 LRU（最近最少使用）缓存，Get/Set 均为 O(1)。
// LRUCache is a non-thread-safe LRU cache with O(1) Get/Set.
type LRUCache struct {
	capacity int
	ll       *list.List
	cache    map[string]*list.Element
}

// NewLRUCache 创建指定容量的 LRUCache；容量必须大于 0。
// NewLRUCache creates an LRU cache; capacity must be positive.
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

func (l *LRUCache) Get(key string) (interface{}, bool) {
	if elem, ok := l.cache[key]; ok {
		l.ll.MoveToFront(elem)
		return elem.Value.(*lruEntry).value, true
	}
	return nil, false
}

func (l *LRUCache) Set(key string, value interface{}) {
	if elem, ok := l.cache[key]; ok {
		l.ll.MoveToFront(elem)
		elem.Value.(*lruEntry).value = value
		return
	}

	if l.ll.Len() >= l.capacity {
		back := l.ll.Back()
		if back != nil {
			l.ll.Remove(back)
			delete(l.cache, back.Value.(*lruEntry).key)
		}
	}

	newElem := l.ll.PushFront(&lruEntry{key: key, value: value})
	l.cache[key] = newElem
}

func (l *LRUCache) Len() int {
	return l.ll.Len()
}

// ThreadSafeLRUCache 是 LRUCache 的线程安全包装。
// ThreadSafeLRUCache is a thread-safe wrapper around LRUCache.
type ThreadSafeLRUCache struct {
	lru  *LRUCache
	lock sync.RWMutex
}

// NewThreadSafeLRUCache 创建指定容量的线程安全 LRUCache；容量必须大于 0。
// NewThreadSafeLRUCache creates a thread-safe LRU cache; capacity must be positive.
func NewThreadSafeLRUCache(capacity int) *ThreadSafeLRUCache {
	return &ThreadSafeLRUCache{
		lru: NewLRUCache(capacity),
	}
}

func (c *ThreadSafeLRUCache) Get(key string) (interface{}, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.lru.Get(key)
}

func (c *ThreadSafeLRUCache) Set(key string, value interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.lru.Set(key, value)
}

func (c *ThreadSafeLRUCache) Len() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.lru.Len()
}
