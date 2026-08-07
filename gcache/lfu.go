package gcache

import (
	"container/list"
	"sync"
)

type lfuEntry struct {
	key   string
	value interface{}
	freq  int
}

// LFUCache 是非线程安全的 LFU（最不经常使用）缓存，Get/Set 均为 O(1)。
// LFUCache is a non-thread-safe LFU cache with O(1) Get/Set.
type LFUCache struct {
	capacity   int
	minFreq    int
	cache      map[string]*list.Element
	freqToList map[int]*list.List
}

// NewLFUCache 创建指定容量的 LFUCache；容量必须大于 0。
func NewLFUCache(capacity int) *LFUCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &LFUCache{
		capacity:   capacity,
		minFreq:    0,
		cache:      make(map[string]*list.Element),
		freqToList: make(map[int]*list.List),
	}
}

func (l *LFUCache) Get(key string) (interface{}, bool) {
	elem, ok := l.cache[key]
	if !ok {
		return nil, false
	}

	l.incrementFrequency(elem)
	return elem.Value.(*lfuEntry).value, true
}

func (l *LFUCache) Set(key string, value interface{}) {
	if l.capacity <= 0 {
		return
	}

	if elem, ok := l.cache[key]; ok {
		elem.Value.(*lfuEntry).value = value
		l.incrementFrequency(elem)
		return
	}

	if len(l.cache) >= l.capacity {
		l.evict()
	}

	entry := &lfuEntry{key: key, value: value, freq: 1}
	if _, ok := l.freqToList[1]; !ok {
		l.freqToList[1] = list.New()
	}
	newElem := l.freqToList[1].PushFront(entry)
	l.cache[key] = newElem
	l.minFreq = 1
}

func (l *LFUCache) Len() int {
	return len(l.cache)
}

// incrementFrequency 将元素移动到下一更高频率的链表。
// incrementFrequency moves an element to the next-higher frequency list.
func (l *LFUCache) incrementFrequency(elem *list.Element) {
	entry := elem.Value.(*lfuEntry)
	currentFreq := entry.freq
	currentList := l.freqToList[currentFreq]

	currentList.Remove(elem)

	if currentList.Len() == 0 && currentFreq == l.minFreq {
		l.minFreq++
	}

	entry.freq++
	newFreq := entry.freq
	if _, ok := l.freqToList[newFreq]; !ok {
		l.freqToList[newFreq] = list.New()
	}
	newElem := l.freqToList[newFreq].PushFront(entry)
	l.cache[entry.key] = newElem
}

// evict 淘汰最不经常且最久未使用的条目。
// evict removes the least-frequently and least-recently used entry.
func (l *LFUCache) evict() {
	listToEvict, ok := l.freqToList[l.minFreq]
	if !ok || listToEvict.Len() == 0 {
		return
	}

	elemToEvict := listToEvict.Back()
	if elemToEvict == nil {
		return
	}

	listToEvict.Remove(elemToEvict)
	delete(l.cache, elemToEvict.Value.(*lfuEntry).key)
}

// ThreadSafeLFUCache 是 LFUCache 的线程安全包装。
// ThreadSafeLFUCache is a thread-safe wrapper around LFUCache.
type ThreadSafeLFUCache struct {
	lfu  *LFUCache
	lock sync.RWMutex
}

// NewThreadSafeLFUCache 创建指定容量的线程安全 LFUCache；容量必须大于 0。
// NewThreadSafeLFUCache creates a thread-safe LFU cache; capacity must be positive.
func NewThreadSafeLFUCache(capacity int) *ThreadSafeLFUCache {
	return &ThreadSafeLFUCache{
		lfu: NewLFUCache(capacity),
	}
}

func (c *ThreadSafeLFUCache) Get(key string) (interface{}, bool) {
	c.lock.Lock() // 频率会变更，使用写锁；frequency changes, so use a write lock.
	defer c.lock.Unlock()
	return c.lfu.Get(key)
}

func (c *ThreadSafeLFUCache) Set(key string, value interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.lfu.Set(key, value)
}

func (c *ThreadSafeLFUCache) Len() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.lfu.Len()
}
