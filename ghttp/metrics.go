package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ===========================================================================
// 请求指标收集与暴露 / Request metrics collection & exposition
//
// Prometheus 文本格式,零第三方依赖,按 MatchedRoute 低基数标签聚合。路由命中后自动
// 归总到模板维度(如 /users/:id),未命中归 "no_route",避免高基数 path 爆炸。
// 挂载中间件后,每个请求后按 method+route+status 三元组记录计数、延迟、响应体大小、
// 在途请求数。/metrics 端点以 Prometheus 文本格式暴露,可被 Prometheus/Grafana 抓取。
//
// Prometheus text format, zero third-party deps, aggregated by the low-cardinality
// MatchedRoute label. Hits roll up to the template dimension (e.g. /users/:id);
// misses go to "no_route" to avoid high-cardinality path explosion. Once the
// middleware is mounted, each request records count, duration, response size, and
// in-flight count per (method, route, status) triple. The /metrics endpoint
// exposes results in Prometheus text format, scrapable by Prometheus/Grafana.
// ===========================================================================

// MetricsRegistry 并发安全地收集按 HTTP method + 路由 + 状态码聚合的请求指标。
// MetricsRegistry concurrently collects request metrics aggregated by HTTP method,
// route, and status code.
type MetricsRegistry struct {
	mu     sync.RWMutex
	routes map[metricsRouteKey]*metricsRouteStats
}

// metricsRouteKey 是 method + route 模板组成的内存键;route 是低基数模板(如 /users/:id)。
// metricsRouteKey is a method + route template in-memory key; route is a
// low-cardinality template (e.g. /users/:id).
type metricsRouteKey struct {
	method string
	route  string
}

// metricsRouteStats 是一条路由上的累计指标,内部字段全用原子操作,响应路径无锁。
// metricsRouteStats holds cumulative metrics for one route; inner fields are all
// atomic, making the response path lock-free.
type metricsRouteStats struct {
	total     atomic.Uint64
	inflight  atomic.Int64
	durSumNS  atomic.Uint64
	respBytes atomic.Uint64

	mu     sync.Mutex
	counts map[int]*atomic.Uint64 // 按状态码计数 / per-status counters
}

func (s *metricsRouteStats) countFor(status int) *atomic.Uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.counts[status]; ok {
		return c
	}
	c := new(atomic.Uint64)
	s.counts[status] = c
	return c
}

// NewMetrics 创建空的指标注册器,挂载 Middleware 后开始收集,用 Handler 暴露端点。
// NewMetrics creates an empty metrics registry; mount Middleware to start
// collecting, and use Handler to expose the endpoint.
func NewMetrics() *MetricsRegistry {
	return &MetricsRegistry{routes: make(map[metricsRouteKey]*metricsRouteStats)}
}

// statsFor 读取或创建 route 的指标对象。首次创建时需写锁,后续读锁。
// statsFor gets or creates the stats for a route; first creation needs a write
// lock, subsequent reads are lock-free.
func (m *MetricsRegistry) statsFor(key metricsRouteKey) *metricsRouteStats {
	m.mu.RLock()
	if s, ok := m.routes[key]; ok {
		m.mu.RUnlock()
		return s
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.routes[key]; ok {
		return s
	}
	s := &metricsRouteStats{counts: make(map[int]*atomic.Uint64)}
	m.routes[key] = s
	return s
}

// Middleware 返回一个记录请求指标到本注册器的中间件。挂载后,每个请求在 next 返回后
// 记录计数、延迟、响应大小与在途连接数。未挂载此中间件的路由不受影响。
// Middleware returns a middleware that records request metrics into this registry.
// After mounting, each request records count, duration, response size, and
// in-flight count after next returns. Routes that don't mount it are unaffected.
func (m *MetricsRegistry) Middleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			route := req.MatchedRoute()
			if route == "" {
				route = "no_route"
			}
			key := metricsRouteKey{method: req.Method, route: route}
			r := m.statsFor(key)
			r.inflight.Add(1)
			start := time.Now()
			err := next(ctx, req, resp)
			r.inflight.Add(-1)

			status := resp.Status()
			if status == 0 {
				// writeError 在中间件链外执行,此时 resp 可能尚未写入;
				// 如果 handler 返回了 error,用 error 推断终态码;否则按 200。
				// writeError runs outside the middleware chain; resp may not be
				// committed yet. If the handler returned an error, infer the final
				// status from it; otherwise default to 200.
				if err != nil {
					status = HTTPStatus(err)
				} else {
					status = http.StatusOK
				}
			}
			r.total.Add(1)
			r.countFor(status).Add(1)
			r.durSumNS.Add(uint64(time.Since(start)))
			r.respBytes.Add(uint64(resp.BytesOut()))
			return err
		}
	}
}

// Handler 返回一个 Prometheus 文本格式的 /metrics 端点处理器。它同时输出按路由聚合
// 的 HTTP 指标与 Go runtime 基础指标(goroutine 数、堆分配),满足生产抓取。
// Handler returns a /metrics endpoint handler in Prometheus text format. It
// outputs both per-route aggregated HTTP metrics and basic Go runtime metrics
// (goroutine count, heap allocation) for production scraping.
func (m *MetricsRegistry) Handler() RawHandlerFunc {
	return func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		resp.WriteHeader(http.StatusOK)
		m.writeHTTPMetrics(resp)
		m.writeGoMetrics(resp)
		return nil
	}
}

func (m *MetricsRegistry) writeHTTPMetrics(w *Response) {
	writePromLine(w, "# HELP http_requests_total Total number of HTTP requests.")
	writePromLine(w, "# TYPE http_requests_total counter")
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 按 method+route 排序,保证输出稳定可 diff。
	// Sort by method+route for stable, diffable output.
	keys := make([]metricsRouteKey, 0, len(m.routes))
	for k := range m.routes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].route < keys[j].route
	})

	for _, k := range keys {
		s := m.routes[k]
		routeLabels := fmt.Sprintf(`method="%s",route="%s"`, k.method, k.route)
		// 请求总数 / total requests
		writePromLine(w, fmt.Sprintf(`http_requests_total{%s} %d`, routeLabels, s.total.Load()))
		// 在途请求 / in-flight
		writePromLine(w, fmt.Sprintf("# HELP http_requests_in_flight Currently in-flight requests."))
		writePromLine(w, fmt.Sprintf("# TYPE http_requests_in_flight gauge"))
		writePromLine(w, fmt.Sprintf(`http_requests_in_flight{%s} %d`, routeLabels, s.inflight.Load()))
		// 延迟 / duration
		durSum := s.durSumNS.Load()
		writePromLine(w, fmt.Sprintf("# HELP http_request_duration_seconds_sum Cumulative request duration in seconds."))
		writePromLine(w, fmt.Sprintf("# TYPE http_request_duration_seconds_sum counter"))
		writePromLine(w, fmt.Sprintf(`http_request_duration_seconds_sum{%s} %s`, routeLabels, formatSeconds(durSum)))
		// 响应大小 / response size
		writePromLine(w, fmt.Sprintf("# HELP http_response_size_bytes_sum Cumulative response body bytes."))
		writePromLine(w, fmt.Sprintf("# TYPE http_response_size_bytes_sum counter"))
		writePromLine(w, fmt.Sprintf(`http_response_size_bytes_sum{%s} %d`, routeLabels, s.respBytes.Load()))
		// 按状态码 / per-status
		s.mu.Lock()
		statuses := make([]int, 0, len(s.counts))
		for st := range s.counts {
			statuses = append(statuses, st)
		}
		sort.Ints(statuses)
		for _, st := range statuses {
			c := s.counts[st]
			writePromLine(w, fmt.Sprintf(`http_requests_total{%s,code="%d"} %d`, routeLabels, st, c.Load()))
		}
		s.mu.Unlock()
	}
}

func (m *MetricsRegistry) writeGoMetrics(w *Response) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	writePromLine(w, "# HELP go_goroutines Number of goroutines.")
	writePromLine(w, "# TYPE go_goroutines gauge")
	writePromLine(w, fmt.Sprintf("go_goroutines %d", runtime.NumGoroutine()))
	writePromLine(w, "# HELP go_memstats_alloc_bytes Number of bytes allocated and still in use.")
	writePromLine(w, "# TYPE go_memstats_alloc_bytes gauge")
	writePromLine(w, fmt.Sprintf("go_memstats_alloc_bytes %d", mem.HeapAlloc))
}

// writePromLine 写一个 Prometheus 文本行,末尾自动加换行。
// writePromLine writes a Prometheus text line with a trailing newline.
func writePromLine(w *Response, line string) {
	_, _ = w.WriteString(line + "\n")
}

// formatSeconds 把纳秒转为包含小数秒的 Prometheus 兼容字符串(如 "0.0042")。
// formatSeconds converts nanoseconds to a Prometheus-compatible fractional-second
// string (e.g. "0.0042").
func formatSeconds(ns uint64) string {
	const precision = 6
	sec := ns / 1e9
	frac := ns % 1e9
	return strconv.FormatUint(sec, 10) + "." + fmt.Sprintf("%09d", frac)[:precision]
}

// HTTPStatus 返回 err 经错误链分类后应得的 HTTP 状态码,供中间件/观测层在响应提交前
// 推断终态码。err 为 nil 时返回 200。
// HTTPStatus returns the HTTP status code that the error chain would classify err
// into, for middleware/observability to infer the final status before the response
// is committed. Returns 200 when err is nil.
func HTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	status, _ := classifyError(err)
	return status
}
