package gcache

import "sync"

type MemoryCache struct {
	data        *sync.Map
	expirations *sync.Map
	mu          sync.RWMutex
	stopCleanup chan struct{}
}
