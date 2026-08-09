package app

import (
	"runtime"
	"strconv"
	"sync"
)

// MetricsSnapshot 表示应用与 Go runtime 指标的时间点快照。
// MetricsSnapshot represents a point-in-time snapshot of application and Go runtime metrics.
type MetricsSnapshot struct {
	ActiveRequests uint64            `json:"active_requests"`
	PeakRequests   uint64            `json:"peak_requests"`
	TotalRequests  uint64            `json:"total_requests"`
	SSEConnections uint64            `json:"sse_connections"`
	WSConnections  uint64            `json:"ws_connections"`
	RequestBytes   uint64            `json:"request_bytes"`
	ResponseBytes  uint64            `json:"response_bytes"`
	Statuses       map[string]uint64 `json:"statuses"`
	HeapAlloc      uint64            `json:"heap_alloc"`
	HeapInUse      uint64            `json:"heap_in_use"`
	TotalAlloc     uint64            `json:"total_alloc"`
	NumGC          uint32            `json:"num_gc"`
	GCPauseTotalNS uint64            `json:"gc_pause_total_ns"`
	Goroutines     int               `json:"goroutines"`
}

// RuntimeMetrics 并发安全地跟踪应用运行指标。
// RuntimeMetrics tracks application runtime metrics safely across concurrent callers.
type RuntimeMetrics struct {
	mu             sync.Mutex
	generation     uint64
	activeRequests uint64
	peakRequests   uint64
	totalRequests  uint64
	sseConnections uint64
	wsConnections  uint64
	requestBytes   uint64
	responseBytes  uint64
	statuses       map[string]uint64
}

// NewRuntimeMetrics 创建空的运行指标收集器。
// NewRuntimeMetrics creates an empty runtime metrics collector.
func NewRuntimeMetrics() *RuntimeMetrics {
	return &RuntimeMetrics{statuses: make(map[string]uint64)}
}

// BeginRequest 记录请求开始，并返回记录完成结果的函数。
// BeginRequest records a request start and returns a function that records its completion.
func (m *RuntimeMetrics) BeginRequest() func(status int, requestBytes, responseBytes uint64) {
	m.mu.Lock()
	generation := m.generation
	m.activeRequests++
	m.totalRequests++
	if m.activeRequests > m.peakRequests {
		m.peakRequests = m.activeRequests
	}
	m.mu.Unlock()

	var once sync.Once
	return func(status int, requestBytes, responseBytes uint64) {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if generation != m.generation {
				return
			}
			if m.activeRequests > 0 {
				m.activeRequests--
			}
			m.requestBytes += requestBytes
			m.responseBytes += responseBytes
			m.statuses[strconv.Itoa(status)]++
		})
	}
}

// OpenSSE 记录一个打开的 SSE 连接，并返回同 generation 的幂等关闭函数。
// OpenSSE records an opened SSE connection and returns an idempotent same-generation closer.
func (m *RuntimeMetrics) OpenSSE() func() {
	m.mu.Lock()
	generation := m.generation
	m.sseConnections++
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { m.closeSSE(generation) })
	}
}

// CloseSSE 记录一个关闭的 SSE 连接。
// CloseSSE records a closed SSE connection.
func (m *RuntimeMetrics) CloseSSE() {
	m.closeSSE(m.currentGeneration())
}

func (m *RuntimeMetrics) closeSSE(generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if generation != m.generation {
		return
	}
	if m.sseConnections > 0 {
		m.sseConnections--
	}
}

// OpenWebSocket 记录一个打开的 WebSocket 连接，并返回同 generation 的幂等关闭函数。
// OpenWebSocket records an opened WebSocket connection and returns an idempotent same-generation closer.
func (m *RuntimeMetrics) OpenWebSocket() func() {
	m.mu.Lock()
	generation := m.generation
	m.wsConnections++
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { m.closeWebSocket(generation) })
	}
}

// CloseWebSocket 记录一个关闭的 WebSocket 连接。
// CloseWebSocket records a closed WebSocket connection.
func (m *RuntimeMetrics) CloseWebSocket() {
	m.closeWebSocket(m.currentGeneration())
}

func (m *RuntimeMetrics) closeWebSocket(generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if generation != m.generation {
		return
	}
	if m.wsConnections > 0 {
		m.wsConnections--
	}
}

func (m *RuntimeMetrics) currentGeneration() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation
}

// Snapshot 返回应用与 Go runtime 指标的独立快照。
// Snapshot returns an independent snapshot of application and Go runtime metrics.
func (m *RuntimeMetrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	snapshot := MetricsSnapshot{
		ActiveRequests: m.activeRequests,
		PeakRequests:   m.peakRequests,
		TotalRequests:  m.totalRequests,
		SSEConnections: m.sseConnections,
		WSConnections:  m.wsConnections,
		RequestBytes:   m.requestBytes,
		ResponseBytes:  m.responseBytes,
		Statuses:       make(map[string]uint64, len(m.statuses)),
	}
	for status, count := range m.statuses {
		snapshot.Statuses[status] = count
	}
	m.mu.Unlock()

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	snapshot.HeapAlloc = memory.HeapAlloc
	snapshot.HeapInUse = memory.HeapInuse
	snapshot.TotalAlloc = memory.TotalAlloc
	snapshot.NumGC = memory.NumGC
	snapshot.GCPauseTotalNS = memory.PauseTotalNs
	snapshot.Goroutines = runtime.NumGoroutine()
	return snapshot
}

// Reset 清除所有应用指标。
// Reset clears all application metrics.
func (m *RuntimeMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generation++
	m.activeRequests = 0
	m.peakRequests = 0
	m.totalRequests = 0
	m.sseConnections = 0
	m.wsConnections = 0
	m.requestBytes = 0
	m.responseBytes = 0
	m.statuses = make(map[string]uint64)
}
