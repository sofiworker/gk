package gcache

import (
	"container/list"
	"sync"
)

// lfuEntry stores the key, value, and frequency of a cache entry.
type lfuEntry struct {
	key   string
	value interface{}
	freq  int
}

// LFUCache is a non-thread-safe LFU (Least Frequently Used) cache.
// It provides O(1) time complexity for both Get and Set operations.
type LFUCache struct {
	capacity   int
	minFreq    int
	cache      map[string]*list.Element
	freqToList map[int]*list.List
}

// NewLFUCache creates a new LFUCache with the specified capacity.
// The capacity must be greater than 0.
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

// Get retrieves a value from the cache for the given key.
// If the key is found, its frequency is incremented.
func (l *LFUCache) Get(key string) (interface{}, bool) {
	elem, ok := l.cache[key]
	if !ok {
		return nil, false
	}

	// Increment frequency
	l.incrementFrequency(elem)
	return elem.Value.(*lfuEntry).value, true
}

// Set adds or updates a key-value pair in the cache.
func (l *LFUCache) Set(key string, value interface{}) {
	if l.capacity <= 0 {
		return
	}

	// If the key already exists, update the value and increment frequency.
	if elem, ok := l.cache[key]; ok {
		elem.Value.(*lfuEntry).value = value
		l.incrementFrequency(elem)
		return
	}

	// If the cache is full, evict the least frequently used item.
	if len(l.cache) >= l.capacity {
		l.evict()
	}

	// Add the new entry.
	entry := &lfuEntry{key: key, value: value, freq: 1}
	if _, ok := l.freqToList[1]; !ok {
		l.freqToList[1] = list.New()
	}
	newElem := l.freqToList[1].PushFront(entry)
	l.cache[key] = newElem
	l.minFreq = 1
}

// Len returns the current number of items in the cache.
func (l *LFUCache) Len() int {
	return len(l.cache)
}

// incrementFrequency moves an element to the list of the next higher frequency.
func (l *LFUCache) incrementFrequency(elem *list.Element) {
	entry := elem.Value.(*lfuEntry)
	currentFreq := entry.freq
	currentList := l.freqToList[currentFreq]

	// Remove from current frequency list
	currentList.Remove(elem)

	// If the current frequency list is now empty and it was the minimum, update minFreq.
	if currentList.Len() == 0 && currentFreq == l.minFreq {
		l.minFreq++
	}

	// Increment frequency and add to the new list
	entry.freq++
	newFreq := entry.freq
	if _, ok := l.freqToList[newFreq]; !ok {
		l.freqToList[newFreq] = list.New()
	}
	newElem := l.freqToList[newFreq].PushFront(entry)
	l.cache[entry.key] = newElem
}

// evict removes the least frequently and least recently used item from the cache.
func (l *LFUCache) evict() {
	listToEvict, ok := l.freqToList[l.minFreq]
	if !ok || listToEvict.Len() == 0 {
		return
	}

	// Get the element to evict (the last one in the list, which is the LRU).
	elemToEvict := listToEvict.Back()
	if elemToEvict == nil {
		return
	}

	// Remove from list and cache map.
	listToEvict.Remove(elemToEvict)
	delete(l.cache, elemToEvict.Value.(*lfuEntry).key)
}

// --- Thread-Safe LFU Cache ---

// ThreadSafeLFUCache is a thread-safe wrapper around LFUCache.
type ThreadSafeLFUCache struct {
	lfu  *LFUCache
	lock sync.RWMutex
}

// NewThreadSafeLFUCache creates a new thread-safe LFUCache with the specified capacity.
func NewThreadSafeLFUCache(capacity int) *ThreadSafeLFUCache {
	return &ThreadSafeLFUCache{
		lfu: NewLFUCache(capacity),
	}
}

// Get retrieves a value from the cache in a thread-safe manner.
func (c *ThreadSafeLFUCache) Get(key string) (interface{}, bool) {
	c.lock.Lock() // Use a write lock because frequency is modified
	defer c.lock.Unlock()
	return c.lfu.Get(key)
}

// Set adds or updates a key-value pair in the cache in a thread-safe manner.
func (c *ThreadSafeLFUCache) Set(key string, value interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.lfu.Set(key, value)
}

// Len returns the current number of items in the cache in a thread-safe manner.
func (c *ThreadSafeLFUCache) Len() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.lfu.Len()
}
