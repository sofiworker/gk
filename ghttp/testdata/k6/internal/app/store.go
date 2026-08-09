package app

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	// ErrItemExists 表示命名空间中已存在相同 ID 的项目。
	// ErrItemExists indicates that an item with the same ID already exists in the namespace.
	ErrItemExists = errors.New("item already exists")
	// ErrVersionConflict 表示更新所需版本与当前版本不匹配。
	// ErrVersionConflict indicates that the expected update version does not match the current version.
	ErrVersionConflict = errors.New("version conflict")
	errItemNotFound    = errors.New("item not found")
)

// Item 表示命名空间内带版本的项目。
// Item represents a versioned item within a namespace.
type Item struct {
	ID        string    `json:"id"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Version   uint64    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store 提供并发安全的内存项目存储。
// Store provides concurrency-safe in-memory item storage.
type Store struct {
	mu    sync.RWMutex
	items map[string]map[string]Item
}

// NewStore 创建空存储。
// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{items: make(map[string]map[string]Item)}
}

// Create 在命名空间内创建项目。
// Create creates an item within a namespace.
func (s *Store) Create(namespace, id, name string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.items[namespace]
	if items == nil {
		items = make(map[string]Item)
		s.items[namespace] = items
	}
	if _, exists := items[id]; exists {
		return Item{}, ErrItemExists
	}
	item := Item{ID: id, Namespace: namespace, Name: name, Version: 1, UpdatedAt: time.Now()}
	items[id] = item
	return item, nil
}

// Get 返回命名空间内的项目。
// Get returns an item from a namespace.
func (s *Store) Get(namespace, id string) (Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[namespace][id]
	return item, ok
}

// List 按 ID 稳定顺序返回命名空间内的项目。
// List returns namespace items in stable ID order.
func (s *Store) List(namespace string) []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Item, 0, len(s.items[namespace]))
	for _, item := range s.items[namespace] {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// Update 在版本匹配时更新项目。
// Update updates an item when its version matches.
func (s *Store) Update(namespace, id, name string, expectedVersion uint64) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[namespace][id]
	if !ok {
		return Item{}, errItemNotFound
	}
	if item.Version != expectedVersion {
		return Item{}, ErrVersionConflict
	}
	item.Name = name
	item.Version++
	item.UpdatedAt = time.Now()
	s.items[namespace][id] = item
	return item, nil
}

// Delete 删除命名空间内的项目。
// Delete removes an item from a namespace.
func (s *Store) Delete(namespace, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.items[namespace]
	if _, ok := items[id]; !ok {
		return false
	}
	delete(items, id)
	if len(items) == 0 {
		delete(s.items, namespace)
	}
	return true
}

// Reset 删除所有项目。
// Reset removes all items.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]map[string]Item)
}
