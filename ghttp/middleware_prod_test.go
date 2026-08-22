package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serveOne 用一条中间件包裹终端 handler 并执行一次请求，返回 recorder。
// serveOne wraps a terminal handler with one middleware and serves a single request.
func serveOne(mw Middleware, terminal Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	m := New()
	m.Use(mw)
	_ = m.RawHandle(method, path, RawHandlerFunc(terminal))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	m.ServeHTTP(rec, req)
	return rec
}

// --- Recovery ---------------------------------------------------------------

// TestRecovery_CatchesPanic 验证 panic 被捕获并写 500，且不向客户端泄露细节。
func TestRecovery_CatchesPanic(t *testing.T) {
	var captured PanicInfo
	mw := RecoveryWith(func(info PanicInfo) { captured = info })
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		panic("boom secret detail")
	}
	rec := serveOne(mw, terminal, http.MethodGet, "/panic", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("response body leaked panic detail: %q", rec.Body.String())
	}
	if captured.Value != "boom secret detail" {
		t.Errorf("captured.Value=%v, want the panic value", captured.Value)
	}
	if len(captured.Stack) == 0 {
		t.Error("captured.Stack should be non-empty")
	}
	if captured.Method != http.MethodGet || captured.Path != "/panic" {
		t.Errorf("captured method/path=%s %s, want GET /panic", captured.Method, captured.Path)
	}
}

// TestRecovery_NoPanicPassthrough 验证无 panic 时正常透传。
func TestRecovery_NoPanicPassthrough(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusTeapot)
		return nil
	}
	rec := serveOne(Recovery(), terminal, http.MethodGet, "/ok", nil)
	if rec.Code != http.StatusTeapot {
		t.Errorf("status=%d, want 418 (passthrough)", rec.Code)
	}
}

// TestRecovery_NoDoubleWriteWhenCommitted 验证下游已提交后 panic 不重写头。
func TestRecovery_NoDoubleWriteWhenCommitted(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write([]byte("partial"))
		panic("after commit")
	}
	rec := serveOne(Recovery(), terminal, http.MethodGet, "/p", nil)
	// 已提交 200，Recovery 不应改写为 500。
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200 (already committed)", rec.Code)
	}
}

// --- CORS -------------------------------------------------------------------

// TestCORS_SimpleRequestWildcard 验证通配来源的简单请求写 ACAO:*。
func TestCORS_SimpleRequestWildcard(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	rec := serveOne(CORS(CORSDefault()), terminal, http.MethodGet, "/api", map[string]string{
		"Origin": "https://example.com",
	})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ACAO=%q, want *", got)
	}
}

// TestCORS_Preflight 验证预检 OPTIONS 返回 204 且写方法头，不进下游。
func TestCORS_Preflight(t *testing.T) {
	reached := false
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		reached = true
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	rec := serveOne(CORS(CORSDefault()), terminal, http.MethodOptions, "/api", map[string]string{
		"Origin":                        "https://example.com",
		"Access-Control-Request-Method": "POST",
	})
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status=%d, want 204", rec.Code)
	}
	if reached {
		t.Error("preflight should not reach downstream handler")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("preflight should set Access-Control-Allow-Methods")
	}
}

// TestCORS_NoOriginPassthrough 验证无 Origin 的同源请求不写 CORS 头。
func TestCORS_NoOriginPassthrough(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	rec := serveOne(CORS(CORSDefault()), terminal, http.MethodGet, "/api", nil)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("same-origin request should not get ACAO header")
	}
}

// TestCORS_CredentialsEchoOrigin 验证带凭证时回显具体 Origin 而非 *。
func TestCORS_CredentialsEchoOrigin(t *testing.T) {
	cfg := CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
	}
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	rec := serveOne(CORS(cfg), terminal, http.MethodGet, "/api", map[string]string{
		"Origin": "https://app.example.com",
	})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("ACAO=%q, want echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("ACAC=%q, want true", got)
	}
}

// TestCORS_OriginNotAllowed 验证白名单外来源不写允许头。
func TestCORS_OriginNotAllowed(t *testing.T) {
	cfg := CORSConfig{AllowOrigins: []string{"https://trusted.com"}}
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	rec := serveOne(CORS(cfg), terminal, http.MethodGet, "/api", map[string]string{
		"Origin": "https://evil.com",
	})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin should not get ACAO header")
	}
}

// --- Timeout ----------------------------------------------------------------

// TestTimeout_FastHandlerPassthrough 验证快速 handler 正常返回，无 503。
func TestTimeout_FastHandlerPassthrough(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	rec := serveOne(Timeout(50*time.Millisecond), terminal, http.MethodGet, "/fast", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", rec.Code)
	}
}

// TestTimeout_SlowHandlerGets503 验证协作式超时：下游因 ctx 取消返回且未写响应 → 503。
func TestTimeout_SlowHandlerGets503(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		// 模拟一个协作良好的下游：监听 ctx 取消后返回，未写响应。
		<-ctx.Done()
		return ctx.Err()
	}
	rec := serveOne(Timeout(10*time.Millisecond), terminal, http.MethodGet, "/slow", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", rec.Code)
	}
}

// TestTimeout_CommittedNotOverwritten 验证下游超时前已提交响应则不被改写。
func TestTimeout_CommittedNotOverwritten(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusCreated)
		<-ctx.Done() // 提交后再阻塞到超时
		return ctx.Err()
	}
	rec := serveOne(Timeout(10*time.Millisecond), terminal, http.MethodGet, "/x", nil)
	if rec.Code != http.StatusCreated {
		t.Errorf("status=%d, want 201 (committed before timeout)", rec.Code)
	}
}

// TestTimeout_ZeroDisables 验证 d<=0 时透传。
func TestTimeout_ZeroDisables(t *testing.T) {
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		if _, ok := ctx.Deadline(); ok {
			t.Error("d<=0 should not set a deadline")
		}
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	rec := serveOne(Timeout(0), terminal, http.MethodGet, "/z", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", rec.Code)
	}
}

// TestTimeout_NonTimeoutErrorPreserved 验证非超时错误原样上冒（未被吞成 503）。
func TestTimeout_NonTimeoutErrorPreserved(t *testing.T) {
	sentinel := errors.New("business failure")
	var got error
	mw := func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			got = next(ctx, req, resp)
			return got
		}
	}
	terminal := func(ctx context.Context, req *Request, resp *Response) error {
		return sentinel
	}
	// 组合：外层捕获 err 的探针 + 内层 Timeout。
	m := New()
	m.Use(mw)
	m.Use(Timeout(time.Second))
	_ = m.RawHandle(http.MethodGet, "/e", RawHandlerFunc(terminal))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/e", nil))
	if !errors.Is(got, sentinel) {
		t.Errorf("got err=%v, want sentinel preserved", got)
	}
}

// --- gin 式全局中间件:命中/未命中/预检统一走链 --------------------------------
// --- gin-style global middleware: hits, misses, and preflight all go through it -

// TestGlobalMW_CORSPreflightUnregisteredOPTIONS 验证普通 Use(CORS) 即可应答未注册
// OPTIONS 路由的预检（204）。这是本次架构改造的核心目标:中间件在 ServeHTTP 期组装、
// 对未命中路由亦生效,无需任何路由前包装变通。
// TestGlobalMW_CORSPreflightUnregisteredOPTIONS verifies that a plain Use(CORS)
// answers preflight for an unregistered OPTIONS route (204). This is the core goal
// of the refactor: middleware is assembled at ServeHTTP time and applies to route
// misses, with no pre-routing wrapper workaround.
func TestGlobalMW_CORSPreflightUnregisteredOPTIONS(t *testing.T) {
	m := New()
	m.Use(CORS(CORSDefault()))
	// 仅注册 GET，不注册 OPTIONS。
	_ = m.RawHandle(http.MethodGet, "/api/items", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/items", nil)
	req.Header.Set("Origin", "https://x.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status=%d, want 204 (global CORS must answer before miss handling)", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("preflight should set Access-Control-Allow-Methods")
	}
}

// TestGlobalMW_RunsOnMiss 验证全局中间件在 404 未命中路径上也会运行(可观测/改写)。
// TestGlobalMW_RunsOnMiss verifies global middleware runs on the 404 miss path too.
func TestGlobalMW_RunsOnMiss(t *testing.T) {
	m := New()
	ran := false
	m.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			ran = true
			return next(ctx, req, resp)
		}
	})
	_ = m.RawHandle(http.MethodGet, "/exists", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))

	if !ran {
		t.Error("global middleware should run even on a 404 miss")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

// TestGlobalMW_RunsOn405 验证全局中间件在 405 路径上运行,且 Allow 头仍被写出。
// TestGlobalMW_RunsOn405 verifies global middleware runs on the 405 path and the
// Allow header is still written.
func TestGlobalMW_RunsOn405(t *testing.T) {
	m := New()
	ran := false
	m.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			ran = true
			return next(ctx, req, resp)
		}
	})
	_ = m.RawHandle(http.MethodGet, "/only-get", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/only-get", nil))

	if !ran {
		t.Error("global middleware should run on a 405")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") == "" {
		t.Error("405 should still set Allow header")
	}
}

// TestGlobalMW_OrderOutsideIn 验证多个全局中间件按 Use 声明顺序自外向内执行。
// TestGlobalMW_OrderOutsideIn verifies multiple globals run outside-in per Use order.
func TestGlobalMW_OrderOutsideIn(t *testing.T) {
	var order []string
	mk := func(tag string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				order = append(order, tag)
				return next(ctx, req, resp)
			}
		}
	}
	m := New()
	m.Use(mk("outer"), mk("inner"))
	_ = m.RawHandle(http.MethodGet, "/x", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		order = append(order, "handler")
		resp.WriteHeader(http.StatusOK)
		return nil
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	want := []string{"outer", "inner", "handler"}
	if len(order) != len(want) {
		t.Fatalf("order=%v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d]=%q, want %q", i, order[i], want[i])
		}
	}
}

// TestGlobalMW_ZeroOverheadWhenEmpty 验证无全局中间件时 globalChain 保持 nil(零开销
// 直连路径),且请求仍正常处理。
// TestGlobalMW_ZeroOverheadWhenEmpty verifies globalChain stays nil with no global
// middleware (zero-overhead direct path) and requests still work.
func TestGlobalMW_ZeroOverheadWhenEmpty(t *testing.T) {
	m := New()
	_ = m.RawHandle(http.MethodGet, "/x", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", rec.Code)
	}
	if m.globalChain != nil {
		t.Error("globalChain should stay nil with no global middleware (zero-overhead path)")
	}
}

// TestPanic_SafetyNetWithoutRecovery 验证未装 Recovery 中间件时,命中终端的 panic 被
// 最外层兜底收敛为 500 而非崩溃(无全局中间件走 dispatchRaw 的 m.serve 兜底)。
// TestPanic_SafetyNetWithoutRecovery verifies that without a Recovery middleware, a
// panic in the hit terminal is collapsed into a 500 by the safety net rather than
// crashing (no global middleware takes dispatchRaw's m.serve safety net).
func TestPanic_SafetyNetWithoutRecovery(t *testing.T) {
	m := New()
	_ = m.RawHandle(http.MethodGet, "/boom", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500 (safety net)", rec.Code)
	}
}

// TestPanic_SafetyNetWithGlobalMWButNoRecovery 验证装了非 Recovery 的全局中间件时,
// 终端 panic 冒泡穿过链,由 safeChain 最外层兜底为 500(不崩溃)。
// TestPanic_SafetyNetWithGlobalMWButNoRecovery verifies that with a non-Recovery
// global middleware installed, a terminal panic bubbles through the chain and is
// caught by safeChain's outermost net as a 500 (no crash).
func TestPanic_SafetyNetWithGlobalMWButNoRecovery(t *testing.T) {
	m := New()
	passed := false
	m.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			passed = true
			return next(ctx, req, resp) // 不 recover / does not recover
		}
	})
	_ = m.RawHandle(http.MethodGet, "/boom", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if !passed {
		t.Error("global middleware should have run before the panic")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500 (safeChain net)", rec.Code)
	}
}

// TestPanic_RecoveryCatchesThroughChain 验证 panic 能穿过 Recovery 之后的其它中间件
// 冒泡回 Recovery(顺序:Recovery 在外,内层中间件在中,终端 panic)。
// TestPanic_RecoveryCatchesThroughChain verifies a panic bubbles through other
// middleware inside Recovery back up to Recovery (order: Recovery outer, inner
// middleware, panicking terminal).
func TestPanic_RecoveryCatchesThroughChain(t *testing.T) {
	m := New()
	var got PanicInfo
	m.Use(
		RecoveryWith(func(info PanicInfo) { got = info }),
		func(next Handler) Handler { // 内层普通中间件 / inner plain middleware
			return func(ctx context.Context, req *Request, resp *Response) error {
				return next(ctx, req, resp)
			}
		},
	)
	_ = m.RawHandle(http.MethodGet, "/panic", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		panic("deep boom")
	}))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rec.Code)
	}
	if got.Value != "deep boom" {
		t.Errorf("captured=%v, want 'deep boom' (panic must bubble through inner MW to Recovery)", got.Value)
	}
}
