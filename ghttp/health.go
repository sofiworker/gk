package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// ===========================================================================
// 健康检查：为容器编排（K8s liveness/readiness probe）与负载均衡探测提供标准端点。
//   - Health：存活探针，恒 200，表明进程在运行；
//   - Ready：就绪探针，逐一运行注册的 Checker，任一失败即 503，表明暂不可接流量。
// 响应体为 JSON，便于运维观测各依赖项状态。
// Health checks: standard endpoints for container orchestration (K8s liveness/
// readiness probes) and load-balancer health polling.
//   - Health: a liveness probe, always 200, signaling the process is running;
//   - Ready: a readiness probe, running each registered Checker; any failure
//     yields 503, signaling "not ready to receive traffic".
// The body is JSON for operational observability of each dependency's status.
// ===========================================================================

// Checker 是一个具名健康检查项：Check 返回 nil 表示健康，非 nil 表示不健康。
// Checker is a named health check: Check returns nil when healthy, non-nil otherwise.
type Checker struct {
	// Name 是检查项名称，出现在就绪响应的 JSON 中。
	// Name is the check's name, surfaced in the readiness JSON.
	Name string
	// Check 执行一次健康探测，应尊重 ctx 取消/超时。
	// Check runs one probe and should honor ctx cancellation/timeout.
	Check func(ctx context.Context) error
}

// Health 在 path 注册存活探针（GET/HEAD），恒返回 200 与 {"status":"ok"}。
// 用于 K8s livenessProbe：只表明进程存活，不检查依赖。
// Health registers a liveness probe (GET/HEAD) at path, always returning 200 with
// {"status":"ok"}. For a K8s livenessProbe: it only signals the process is alive
// and does not check dependencies.
func Health(m *Server, path string) error {
	handler := func(ctx context.Context, req *Request, resp *Response) error {
		writeJSON(resp, http.StatusOK, map[string]string{"status": "ok"})
		return nil
	}
	return registerProbe(m, path, handler)
}

// Ready 在 path 注册就绪探针（GET/HEAD）：依次运行 checks，全部通过返回 200，
// 任一失败返回 503。响应 JSON 含各检查项状态。用于 K8s readinessProbe。
// checkTimeout<=0 时对每个 check 不加超时。
// Ready registers a readiness probe (GET/HEAD) at path: it runs checks in order,
// returning 200 if all pass and 503 if any fails. The JSON body reports each
// check's status. For a K8s readinessProbe. A checkTimeout<=0 imposes no per-check
// timeout.
func Ready(m *Server, path string, checkTimeout time.Duration, checks ...Checker) error {
	handler := func(ctx context.Context, req *Request, resp *Response) error {
		result := runChecks(ctx, checkTimeout, checks)
		code := http.StatusOK
		if !result.healthy {
			code = http.StatusServiceUnavailable
		}
		writeJSON(resp, code, result.body())
		return nil
	}
	return registerProbe(m, path, handler)
}

// readyResult 汇总一次就绪检查的整体健康与各项明细。
// readyResult aggregates the overall health and per-check details of one readiness run.
type readyResult struct {
	healthy bool
	checks  map[string]string // name -> "ok" 或错误消息 / "ok" or the error message
}

// body 构造就绪响应的 JSON 结构。
// body builds the readiness response's JSON structure.
func (r readyResult) body() map[string]any {
	status := "ok"
	if !r.healthy {
		status = "unavailable"
	}
	return map[string]any{"status": status, "checks": r.checks}
}

// runChecks 顺序运行所有检查项，收集结果；每项可选加 timeout。
// runChecks runs all checks sequentially, collecting results; each may get a timeout.
func runChecks(ctx context.Context, timeout time.Duration, checks []Checker) readyResult {
	res := readyResult{healthy: true, checks: make(map[string]string, len(checks))}
	for _, c := range checks {
		cctx := ctx
		var cancel context.CancelFunc
		if timeout > 0 {
			cctx, cancel = context.WithTimeout(ctx, timeout)
		}
		err := c.Check(cctx)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			res.healthy = false
			res.checks[c.Name] = err.Error()
		} else {
			res.checks[c.Name] = "ok"
		}
	}
	return res
}

// registerProbe 为探针路径注册 GET 与 HEAD。
// registerProbe registers GET and HEAD for a probe path.
func registerProbe(m *Server, path string, h Handler) error {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if err := m.RawHandle(method, path, RawHandlerFunc(h)); err != nil {
			return err
		}
	}
	return nil
}

// writeJSON 写状态码与 JSON 响应体（探针专用的小工具，避免依赖 typed 输出层）。
// writeJSON writes a status code and JSON body (a small probe-only helper, avoiding
// the typed output layer).
func writeJSON(resp *Response, code int, v any) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.WriteHeader(code)
	_ = json.NewEncoder(resp).Encode(v)
}

// LivenessChecker 是一个恒健康的检查项构造器，便于把无依赖的组件登记进就绪列表。
// LivenessChecker builds an always-healthy Checker, handy for registering a
// dependency-free component in the readiness list.
func LivenessChecker(name string) Checker {
	return Checker{Name: name, Check: func(context.Context) error { return nil }}
}

// ReadinessGate 是一个可运行时切换的就绪门闸：适合“启动完成/开始排水”场景。
// 它以 Checker 形式接入 Ready，通过 Set 在运行时开关。
// ReadinessGate is a runtime-toggleable readiness gate for "startup complete / begin
// draining" scenarios. It plugs into Ready as a Checker and toggles via Set.
type ReadinessGate struct {
	mu    sync.RWMutex
	ready bool
	err   error
}

// NewReadinessGate 返回一个初始未就绪的门闸与其 Checker。调用 Set(true,nil) 置为就绪。
// NewReadinessGate returns an initially-not-ready gate and its Checker. Call
// Set(true, nil) to mark ready.
func NewReadinessGate(name string) (*ReadinessGate, Checker) {
	g := &ReadinessGate{err: ErrNotReady}
	c := Checker{Name: name, Check: func(context.Context) error {
		g.mu.RLock()
		defer g.mu.RUnlock()
		if g.ready {
			return nil
		}
		return g.err
	}}
	return g, c
}

// Set 切换门闸状态：ready=true 表示就绪；ready=false 时用 cause 作为不就绪原因
// （nil 时回退到 ErrNotReady）。
// Set toggles the gate: ready=true marks ready; when ready=false, cause is the
// not-ready reason (falling back to ErrNotReady when nil).
func (g *ReadinessGate) Set(ready bool, cause error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ready = ready
	if ready {
		g.err = nil
	} else if cause != nil {
		g.err = cause
	} else {
		g.err = ErrNotReady
	}
}
