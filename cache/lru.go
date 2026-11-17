package gcache

import (
	"container/list"
	"sync"
)

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

// LRUCache 是线程安全的 LRU（最近最少使用）缓存，Get/Set 均为 O(1)。
// 因为 Get 也会调整访问顺序（写共享状态），所有方法统一使用 sync.Mutex。
// LRUCache is a thread-safe LRU cache with O(1) Get/Set. Since Get also reorders entries
// (mutating shared state), every method takes a single sync.Mutex.
type LRUCache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	cache    map[K]*list.Element
}

// NewLRUCache 创建指定容量的 LRUCache；容量小于等于 0 时按 1 处理。
// NewLRUCache creates an LRU cache; a capacity <= 0 is treated as 1.
func NewLRUCache[K comparable, V any](capacity int) *LRUCache[K, V] {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRUCache[K, V]{
		capacity: capacity,
		ll:       list.New(),
		cache:    make(map[K]*list.Element),
	}
}

func (l *LRUCache[K, V]) Get(key K) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.cache[key]; ok {
		l.ll.MoveToFront(elem)
		return elem.Value.(*lruEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

func (l *LRUCache[K, V]) Set(key K, value V) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.cache[key]; ok {
		l.ll.MoveToFront(elem)
		elem.Value.(*lruEntry[K, V]).value = value
		return
	}

	if l.ll.Len() >= l.capacity {
		back := l.ll.Back()
		if back != nil {
			l.ll.Remove(back)
			delete(l.cache, back.Value.(*lruEntry[K, V]).key)
		}
	}

	newElem := l.ll.PushFront(&lruEntry[K, V]{key: key, value: value})
	l.cache[key] = newElem
}

func (l *LRUCache[K, V]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ll.Len()
}
