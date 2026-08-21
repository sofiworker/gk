package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// helper: 注册一个原始处理器并对 mux 发一个请求,返回 recorder。
// helper: register a raw handler, fire one request at the mux, return recorder.
func doRequest(t *testing.T, m *Mux, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// 中间件按 Use 追加顺序执行(先加先执行,洋葱最外层)。
func TestMiddlewareOrder(t *testing.T) {
	var order []string
	mark := func(tag string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				order = append(order, "pre-"+tag)
				err := next(ctx, req, resp)
				order = append(order, "post-"+tag)
				return err
			}
		}
	}
	m := New()
	m.Use(mark("a"), mark("b"))
	if err := m.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		order = append(order, "handler")
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	doRequest(t, m, http.MethodGet, "/x")

	want := "pre-a,pre-b,handler,post-b,post-a"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

// 中间件不调用 next 即短路,终端不执行。
func TestMiddlewareShortCircuit(t *testing.T) {
	handlerRan := false
	block := func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusForbidden)
			return nil // 不调用 next
		}
	}
	m := New()
	m.Use(block)
	if err := m.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		handlerRan = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(t, m, http.MethodGet, "/x")

	if handlerRan {
		t.Error("handler ran despite short-circuit")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// 后处理中间件能读到终端写下的最终状态码(依赖 Response.status 追踪)。
func TestMiddlewarePostReadsStatus(t *testing.T) {
	var seen int
	m := New()
	m.Use(LoggerWith(func(_, _ string, status int, _ time.Duration) { seen = status }))
	if err := m.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusTeapot)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	doRequest(t, m, http.MethodGet, "/x")

	if seen != http.StatusTeapot {
		t.Errorf("logger saw status %d, want 418", seen)
	}
}

// context 传值:RequestID 注入的 ID 下游可取,且回显到响应头。
func TestRequestIDContextAndHeader(t *testing.T) {
	var downstream string
	m := New()
	m.Use(RequestID())
	if err := m.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		downstream = RequestIDFromContext(ctx)
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(t, m, http.MethodGet, "/x")

	if downstream == "" {
		t.Error("downstream got empty request ID from context")
	}
	if h := rec.Header().Get(HeaderRequestID); h != downstream {
		t.Errorf("header X-Request-ID = %q, context = %q, want equal", h, downstream)
	}
}

// RequestID 回显合法的客户端 ID,替换超长 ID。
func TestRequestIDEchoAndReplace(t *testing.T) {
	m := New()
	m.Use(RequestID())
	if err := m.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 合法客户端 ID 被回显。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(HeaderRequestID, "client-123")
	m.ServeHTTP(rec, req)
	if got := rec.Header().Get(HeaderRequestID); got != "client-123" {
		t.Errorf("echo: X-Request-ID = %q, want client-123", got)
	}

	// 超长 ID 被替换。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(HeaderRequestID, strings.Repeat("z", DefaultMaxRequestIDLength+1))
	m.ServeHTTP(rec, req)
	if got := rec.Header().Get(HeaderRequestID); got == "" || len(got) > DefaultMaxRequestIDLength {
		t.Errorf("replace: X-Request-ID = %q (len %d), want fresh capped ID", got, len(got))
	}
}

// Group 快照语义:创建 Group 后再对父 Mux Use 的中间件不影响该 Group。
func TestGroupMiddlewareSnapshot(t *testing.T) {
	var hits []string
	mark := func(tag string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				hits = append(hits, tag)
				return next(ctx, req, resp)
			}
		}
	}
	m := New()
	m.Use(mark("global"))
	g := m.Group("/api", mark("group")) // 此刻快照 [global, group]
	m.Use(mark("late"))                 // 创建后追加,不应影响 g

	if err := g.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	doRequest(t, m, http.MethodGet, "/api/x")

	if got := strings.Join(hits, ","); got != "global,group" {
		t.Errorf("group chain = %q, want %q (late Use must not leak in)", got, "global,group")
	}
}

// Group 前缀拼接正确,嵌套 Group 前缀累加、中间件叠加。
func TestGroupNesting(t *testing.T) {
	var hits []string
	mark := func(tag string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				hits = append(hits, tag)
				return next(ctx, req, resp)
			}
		}
	}
	m := New()
	v1 := m.Group("/v1", mark("v1"))
	users := v1.Group("/users", mark("users"))
	if err := users.RawHandle(http.MethodGet, "/{id}", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(t, m, http.MethodGet, "/v1/users/42")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (prefix concatenation)", rec.Code)
	}
	if got := strings.Join(hits, ","); got != "v1,users" {
		t.Errorf("nested chain = %q, want v1,users", got)
	}
}
