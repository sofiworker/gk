package gcache

import (
	"container/list"
	"sync"
)

type lfuEntry[K comparable, V any] struct {
	key   K
	value V
	freq  int
}

// LFUCache 是线程安全的 LFU（最不经常使用）缓存，Get/Set 均为 O(1)。
// 因为 Get 也会提升频率（写共享状态），所有方法统一使用 sync.Mutex。
// LFUCache is a thread-safe LFU cache with O(1) Get/Set. Since Get also bumps the frequency
// (mutating shared state), every method takes a single sync.Mutex.
type LFUCache[K comparable, V any] struct {
	mu         sync.Mutex
	capacity   int
	minFreq    int
	cache      map[K]*list.Element
	freqToList map[int]*list.List
}

// NewLFUCache 创建指定容量的 LFUCache；容量小于等于 0 时按 1 处理。
// NewLFUCache creates an LFU cache; a capacity <= 0 is treated as 1.
func NewLFUCache[K comparable, V any](capacity int) *LFUCache[K, V] {
	if capacity <= 0 {
		capacity = 1
	}
	return &LFUCache[K, V]{
		capacity:   capacity,
		minFreq:    0,
		cache:      make(map[K]*list.Element),
		freqToList: make(map[int]*list.List),
	}
}

func (l *LFUCache[K, V]) Get(key K) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	elem, ok := l.cache[key]
	if !ok {
		var zero V
		return zero, false
	}

	l.incrementFrequency(elem)
	return elem.Value.(*lfuEntry[K, V]).value, true
}

func (l *LFUCache[K, V]) Set(key K, value V) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.cache[key]; ok {
		elem.Value.(*lfuEntry[K, V]).value = value
		l.incrementFrequency(elem)
		return
	}

	if len(l.cache) >= l.capacity {
		l.evict()
	}

	entry := &lfuEntry[K, V]{key: key, value: value, freq: 1}
	if _, ok := l.freqToList[1]; !ok {
		l.freqToList[1] = list.New()
	}
	newElem := l.freqToList[1].PushFront(entry)
	l.cache[key] = newElem
	l.minFreq = 1
}

func (l *LFUCache[K, V]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.cache)
}

// incrementFrequency 将元素移动到下一更高频率的链表。
// incrementFrequency moves an element to the next-higher frequency list.
func (l *LFUCache[K, V]) incrementFrequency(elem *list.Element) {
	entry := elem.Value.(*lfuEntry[K, V])
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
func (l *LFUCache[K, V]) evict() {
	listToEvict, ok := l.freqToList[l.minFreq]
	if !ok || listToEvict.Len() == 0 {
		return
	}

	elemToEvict := listToEvict.Back()
	if elemToEvict == nil {
		return
	}

	listToEvict.Remove(elemToEvict)
	delete(l.cache, elemToEvict.Value.(*lfuEntry[K, V]).key)
}
