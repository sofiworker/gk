package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// 健康检查状态字面量。
// Health status literals.
const (
	HealthStatusOK          = "ok"
	HealthStatusUnavailable = "unavailable"
	// HealthCheckFailed 是非 verbose 模式下单项失败的摘要。
	// HealthCheckFailed is the per-check summary for failures when not verbose.
	HealthCheckFailed = "failed"
)

// DefaultHealthCheckTimeout 是单项检查的默认超时。
// DefaultHealthCheckTimeout is the default per-check timeout.
const DefaultHealthCheckTimeout = 2 * time.Second

// HealthOption 配置 Health（WithFunc 风格）。
// HealthOption configures Health (WithFunc style).
type HealthOption func(*Health)

// WithHealthCheckTimeout 设置单项检查超时；d<=0 时忽略。
// WithHealthCheckTimeout sets the per-check timeout; ignored when d<=0.
func WithHealthCheckTimeout(d time.Duration) HealthOption {
	return func(h *Health) {
		if d > 0 {
			h.timeout = d
		}
	}
}

// WithHealthVerbose 控制 readiness 响应是否暴露错误文本；默认 false，仅返回 "failed"。
// WithHealthVerbose controls whether readiness exposes error text; default false (only "failed").
func WithHealthVerbose(v bool) HealthOption {
	return func(h *Health) { h.verbose = v }
}

type healthCheck struct {
	name string
	fn   func(ctx context.Context) error
}

// Health 提供 liveness / readiness 探针。零值不可用，请用 NewHealth。
// Health provides liveness / readiness probes. Use NewHealth; the zero value is unusable.
type Health struct {
	timeout time.Duration
	verbose bool
	notRdy  atomic.Bool

	mu     sync.RWMutex
	checks []healthCheck
}

// HealthReport 是 readiness 响应体。
// HealthReport is the readiness response body.
type HealthReport struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// NewHealth 创建 Health，默认 ready。
// NewHealth creates a Health, ready by default.
func NewHealth(opts ...HealthOption) *Health {
	h := &Health{timeout: DefaultHealthCheckTimeout}
	for _, o := range opts {
		if o != nil {
			o(h)
		}
	}
	return h
}

// AddCheck 注册一项就绪检查，并发安全。同名检查结果以后者为准。fn 为 nil 时忽略。
// AddCheck registers a readiness check; concurrency-safe. With duplicate names the later
// result wins. A nil fn is ignored.
func (h *Health) AddCheck(name string, fn func(ctx context.Context) error) {
	if fn == nil {
		return
	}
	h.mu.Lock()
	h.checks = append(h.checks, healthCheck{name: name, fn: fn})
	h.mu.Unlock()
}

// SetReady 设置是否就绪；优雅下线时先 SetReady(false) 摘流量。
// SetReady sets readiness; call SetReady(false) first on graceful shutdown to drain traffic.
func (h *Health) SetReady(ready bool) { h.notRdy.Store(!ready) }

// Ready 报告当前 ready 开关（不执行检查）。
// Ready reports the ready switch (without running checks).
func (h *Health) Ready() bool { return !h.notRdy.Load() }

// Check 并发执行所有检查并返回报告与是否整体健康。
// Check runs all checks concurrently and returns the report and overall health.
func (h *Health) Check(ctx context.Context) (HealthReport, bool) {
	h.mu.RLock()
	checks := append([]healthCheck(nil), h.checks...)
	h.mu.RUnlock()

	results := make([]string, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = h.runOne(ctx, c)
		}()
	}
	wg.Wait()

	ok := h.Ready()
	rep := HealthReport{}
	if len(checks) > 0 {
		rep.Checks = make(map[string]string, len(checks))
	}
	for i, c := range checks {
		rep.Checks[c.name] = results[i]
		if results[i] != HealthStatusOK {
			ok = false
		}
	}
	rep.Status = HealthStatusOK
	if !ok {
		rep.Status = HealthStatusUnavailable
	}
	return rep, ok
}

// runOne 执行单项检查：带超时，panic 视为失败，不服从 ctx 的检查也不会阻塞响应。
// runOne runs one check with a timeout; a panic counts as failure and a check ignoring its
// ctx cannot block the response.
func (h *Health) runOne(ctx context.Context, c healthCheck) string {
	cctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- c.fn(cctx)
	}()
	var err error
	select {
	case err = <-done:
	case <-cctx.Done():
		err = cctx.Err()
	}
	if err == nil {
		return HealthStatusOK
	}
	if h.verbose {
		return err.Error()
	}
	return HealthCheckFailed
}

// Routes 返回 liveness 与 readiness 的 GET 路由。liveness 恒 200 {"status":"ok"}；
// readiness 全部通过且 ready 时 200，否则 503。响应带 Cache-Control: no-store。
// Routes returns the GET routes for liveness and readiness. Liveness always answers 200
// {"status":"ok"}; readiness answers 200 only when all checks pass and ready, else 503.
// Responses carry Cache-Control: no-store.
func (h *Health) Routes(livePath, readyPath string) []Route {
	live := func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Cache-Control", "no-store")
		return writeJSON(resp, http.StatusOK, HealthReport{Status: HealthStatusOK})
	}
	ready := func(ctx context.Context, _ *Request, resp *Response) error {
		rep, ok := h.Check(ctx)
		status := http.StatusOK
		if !ok {
			status = http.StatusServiceUnavailable
		}
		resp.Header().Set("Cache-Control", "no-store")
		return writeJSON(resp, status, rep)
	}
	return []Route{
		Raw(http.MethodGet, livePath, live),
		Raw(http.MethodGet, readyPath, ready),
	}
}
