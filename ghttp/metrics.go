package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
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

// normalizeMetricsMethod 把请求方法归一为有界集合,未识别的方法统一记作 "OTHER"。
//
// 未命中路由时 route 已归一为 no_route,但方法此前原样进标签:任意客户端发
// `FOO /x`、`BAR /x`… 即可无限新建 series,把 MetricsRegistry 的 map 撑爆(内存与基数
// 双重 DoS,且抓取端也会被拖垮)。标准方法集是封闭的,超出即视为噪声。
// normalizeMetricsMethod maps a request method into a bounded set, recording any
// unrecognized method as "OTHER".
//
// On a miss the route is already normalized to no_route, but the method used to enter
// the label verbatim: any client could send `FOO /x`, `BAR /x`, … and mint unbounded
// series, exhausting the MetricsRegistry map (a memory and cardinality DoS that also
// drags down the scraper). The standard method set is closed; anything beyond is noise.
func normalizeMetricsMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
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
			key := metricsRouteKey{method: normalizeMetricsMethod(req.Method), route: route}
			r := m.statsFor(key)
			r.inflight.Add(1)
			start := time.Now()

			// 全部记账放进 defer:handler panic 时 panic 会穿过本中间件向上传播,若在
			// panic 后才递减,inflight 会永久 +1 且该请求不进 total——监控从此长期虚高,
			// 成为假告警源。defer 保证无论正常返回、返回 error 还是 panic 都恰好记一次。
			// All accounting goes in a defer: a handler panic propagates up through this
			// middleware, and decrementing after it would leave inflight permanently +1
			// with the request missing from total — the monitor stays inflated forever
			// and becomes a false-alarm source. A defer guarantees exactly one
			// accounting pass whether the call returns, errors, or panics.
			var err error
			completed := false
			defer func() {
				r.inflight.Add(-1)
				status := resp.Status()
				if status == 0 {
					// writeError 在中间件链外执行,此时 resp 可能尚未写入;
					// 如果 handler 返回了 error,用 error 推断终态码;否则按 200。
					// writeError runs outside the middleware chain; resp may not be
					// committed yet. If the handler returned an error, infer the final
					// status from it; otherwise default to 200.
					switch {
					case err != nil:
						status = HTTPStatus(err)
					case !completed:
						// 未走到正常返回即离开 ⇒ panic 正在向上传播,响应最终被写成 500。
						// Leaving without reaching the normal return means a panic is
						// propagating, and the response ends up written as 500.
						status = http.StatusInternalServerError
					default:
						status = http.StatusOK
					}
				}
				r.total.Add(1)
				r.countFor(status).Add(1)
				r.durSumNS.Add(uint64(time.Since(start)))
				r.respBytes.Add(uint64(resp.BytesOut()))
			}()

			err = next(ctx, req, resp)
			completed = true
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

// writeHTTPMetrics 按 Prometheus 文本暴露格式输出 HTTP 指标。
//
// 格式约束(此前均被违反,标准解析器会直接报错):
//   - 每个 metric family 的 HELP/TYPE 只能出现一次,且必须在该 family 的样本之前;
//     放进按路由的循环里会重复输出。
//   - 同一 family 的样本必须连续,不能被别的 family 打断。
//   - 同一 family 的所有样本必须有相同的标签键集合。原实现让 http_requests_total 同时
//     产出带 code 与不带 code 两种标签集,这是非法的;因此按状态码的计数独立成
//     http_requests_by_code_total,而 http_requests_total 只保留总数。
//
// 因此这里按 family 分组遍历(每个 family 一次 HELP/TYPE + 连续样本),而不是按路由。
// writeHTTPMetrics emits HTTP metrics in the Prometheus text exposition format.
//
// Format constraints (all previously violated, making standard parsers fail):
//   - A metric family's HELP/TYPE may appear only once and must precede that family's
//     samples; placing them in the per-route loop repeats them.
//   - A family's samples must be contiguous and not interleaved with another family.
//   - Every sample of one family must carry the same label key set. The original code
//     emitted http_requests_total both with and without a code label, which is
//     illegal; per-status counts therefore move to http_requests_by_code_total while
//     http_requests_total keeps only the aggregate.
//
// Iteration is therefore grouped by family (one HELP/TYPE plus contiguous samples
// each) rather than by route.
func (m *MetricsRegistry) writeHTTPMetrics(w *Response) {
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

	labelsOf := func(k metricsRouteKey) string {
		return fmt.Sprintf(`method="%s",route="%s"`, escapePromLabel(k.method), escapePromLabel(k.route))
	}

	writePromLine(w, "# HELP http_requests_total Total number of HTTP requests.")
	writePromLine(w, "# TYPE http_requests_total counter")
	for _, k := range keys {
		writePromLine(w, fmt.Sprintf(`http_requests_total{%s} %d`, labelsOf(k), m.routes[k].total.Load()))
	}

	writePromLine(w, "# HELP http_requests_in_flight Currently in-flight requests.")
	writePromLine(w, "# TYPE http_requests_in_flight gauge")
	for _, k := range keys {
		writePromLine(w, fmt.Sprintf(`http_requests_in_flight{%s} %d`, labelsOf(k), m.routes[k].inflight.Load()))
	}

	// _count 与 _sum 配对暴露,抓取端才能算平均延迟(sum/count)。只有 _sum 时均值无从计算。
	// _count is exposed alongside _sum so a scraper can compute mean latency
	// (sum/count); with only _sum the mean is not derivable.
	writePromLine(w, "# HELP http_request_duration_seconds_count Total number of observed requests.")
	writePromLine(w, "# TYPE http_request_duration_seconds_count counter")
	for _, k := range keys {
		writePromLine(w, fmt.Sprintf(`http_request_duration_seconds_count{%s} %d`, labelsOf(k), m.routes[k].total.Load()))
	}

	writePromLine(w, "# HELP http_request_duration_seconds_sum Cumulative request duration in seconds.")
	writePromLine(w, "# TYPE http_request_duration_seconds_sum counter")
	for _, k := range keys {
		writePromLine(w, fmt.Sprintf(`http_request_duration_seconds_sum{%s} %s`, labelsOf(k), formatSeconds(m.routes[k].durSumNS.Load())))
	}

	writePromLine(w, "# HELP http_response_size_bytes_sum Cumulative response body bytes.")
	writePromLine(w, "# TYPE http_response_size_bytes_sum counter")
	for _, k := range keys {
		writePromLine(w, fmt.Sprintf(`http_response_size_bytes_sum{%s} %d`, labelsOf(k), m.routes[k].respBytes.Load()))
	}

	writePromLine(w, "# HELP http_requests_by_code_total Total number of HTTP requests by response status code.")
	writePromLine(w, "# TYPE http_requests_by_code_total counter")
	for _, k := range keys {
		s := m.routes[k]
		s.mu.Lock()
		statuses := make([]int, 0, len(s.counts))
		for st := range s.counts {
			statuses = append(statuses, st)
		}
		sort.Ints(statuses)
		lines := make([]string, 0, len(statuses))
		for _, st := range statuses {
			lines = append(lines, fmt.Sprintf(`http_requests_by_code_total{%s,code="%d"} %d`, labelsOf(k), st, s.counts[st].Load()))
		}
		s.mu.Unlock()
		for _, ln := range lines {
			writePromLine(w, ln)
		}
	}
}

// escapePromLabel 转义 Prometheus 标签值中的 \、" 与换行。
//
// 标签值是带引号的字符串,未转义的引号或换行会破坏整行语法。route 来自注册的路由模板
// (可控),但 method 来自请求;虽已归一为有界集合,仍在此转义作为纵深防御——一旦将来
// 新增用户可控标签,这里不必再改。
// escapePromLabel escapes \, " and newlines inside a Prometheus label value.
//
// A label value is a quoted string, so an unescaped quote or newline breaks the line's
// syntax. route comes from registered route templates (controlled), but method comes
// from the request; although it is already normalized to a bounded set, escaping here
// is defense in depth so a future user-controlled label needs no change.
func escapePromLabel(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	var b strings.Builder
	b.Grow(len(v) + 8)
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(v[i])
		}
	}
	return b.String()
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
