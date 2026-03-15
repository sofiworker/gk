package gclient

import (
	"bytes"
	"sort"
	"sync"
	"sync/atomic"
)

type MultiSizeBufferPool struct {
	pools []*sizeSpecificPool
	mu    sync.RWMutex
}

type sizeSpecificPool struct {
	size   int
	pool   sync.Pool
	allocs int64
	reuses int64
}

func NewMultiSizeBufferPool(sizes ...int) *MultiSizeBufferPool {
	if len(sizes) == 0 {
		sizes = []int{1024, 4096, 16384, 65536, 262144}
	}

	sort.Ints(sizes)
	mbp := &MultiSizeBufferPool{
		pools: make([]*sizeSpecificPool, len(sizes)),
	}

	for i, size := range sizes {
		finalSize := size
		poolRef := &sizeSpecificPool{size: finalSize}
		poolRef.pool = sync.Pool{
			New: func() interface{} {
				atomic.AddInt64(&poolRef.allocs, 1)
				return bytes.NewBuffer(make([]byte, 0, finalSize))
			},
		}
		mbp.pools[i] = poolRef
	}

	return mbp
}

func (mbp *MultiSizeBufferPool) Get(minSize int) *bytes.Buffer {
	mbp.mu.RLock()
	defer mbp.mu.RUnlock()

	for _, pool := range mbp.pools {
		if pool.size >= minSize {
			buf := pool.pool.Get().(*bytes.Buffer)
			buf.Reset()
			atomic.AddInt64(&pool.reuses, 1)
			return buf
		}
	}

	return bytes.NewBuffer(make([]byte, 0, minSize))
}

func (mbp *MultiSizeBufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	capacity := buf.Cap()
	buf.Reset()

	if capacity > 1024*1024 {
		return
	}

	mbp.mu.RLock()
	defer mbp.mu.RUnlock()

	for _, pool := range mbp.pools {
		if pool.size >= capacity {
			pool.pool.Put(buf)
			return
		}
	}
}

func (mbp *MultiSizeBufferPool) Stats() map[string]interface{} {
	mbp.mu.RLock()
	defer mbp.mu.RUnlock()

	stats := make(map[string]interface{})
	poolStats := make([]map[string]interface{}, len(mbp.pools))

	for i, pool := range mbp.pools {
		poolStats[i] = map[string]interface{}{
			"size":        pool.size,
			"allocations": atomic.LoadInt64(&pool.allocs),
			"reuses":      atomic.LoadInt64(&pool.reuses),
		}
	}

	stats["pools"] = poolStats
	return stats
}
