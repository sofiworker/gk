package gclient

import (
	"bytes"
	"sort"
	"sync"
	"sync/atomic"
)

// MultiSizeBufferPool 多尺寸缓冲区池
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

// NewMultiSizeBufferPool 创建多尺寸缓冲区池
func NewMultiSizeBufferPool(sizes ...int) *MultiSizeBufferPool {
	if len(sizes) == 0 {
		// 默认尺寸：1KB, 4KB, 16KB, 64KB, 256KB
		sizes = []int{1024, 4096, 16384, 65536, 262144}
	}

	sort.Ints(sizes)

	mbp := &MultiSizeBufferPool{
		pools: make([]*sizeSpecificPool, len(sizes)),
	}

	for i, size := range sizes {
		finalSize := size
		mbp.pools[i] = &sizeSpecificPool{
			size: finalSize,
			pool: sync.Pool{
				New: func() interface{} {
					atomic.AddInt64(&mbp.pools[i].allocs, 1)
					return bytes.NewBuffer(make([]byte, 0, finalSize))
				},
			},
		}
	}

	return mbp
}

// Get 获取合适大小的缓冲区
func (mbp *MultiSizeBufferPool) Get(minSize int) *bytes.Buffer {
	mbp.mu.RLock()
	defer mbp.mu.RUnlock()

	// 寻找第一个大于等于 minSize 的池
	for _, pool := range mbp.pools {
		if pool.size >= minSize {
			buf := pool.pool.Get().(*bytes.Buffer)
			atomic.AddInt64(&pool.reuses, 1)
			return buf
		}
	}

	// 如果没有找到合适的池，使用最大的池
	if len(mbp.pools) > 0 {
		lastPool := mbp.pools[len(mbp.pools)-1]
		buf := lastPool.pool.Get().(*bytes.Buffer)
		atomic.AddInt64(&lastPool.reuses, 1)
		return buf
	}

	// 如果没有配置任何池，创建新的缓冲区
	return bytes.NewBuffer(make([]byte, 0, minSize))
}

// Put 将缓冲区放回池中
func (mbp *MultiSizeBufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	capacity := buf.Cap()
	buf.Reset()

	// 如果缓冲区过大，直接丢弃
	if capacity > 1024*1024 { // 1MB
		return
	}

	mbp.mu.RLock()
	defer mbp.mu.RUnlock()

	// 寻找最适合的池
	for _, pool := range mbp.pools {
		if pool.size >= capacity {
			pool.pool.Put(buf)
			return
		}
	}

	// 如果没有找到合适的池，使用最大的池
	if len(mbp.pools) > 0 {
		lastPool := mbp.pools[len(mbp.pools)-1]
		lastPool.pool.Put(buf)
	}
}

// Stats 获取统计信息
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
