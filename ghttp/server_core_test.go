package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// coreUser 是核心测试使用的请求/响应体。
// coreUser is the request/response body used by the core tests.
type coreUser struct {
	Name string `json:"name" xml:"name"`
}

// coreWait 是核心测试中异步等待的统一上限。
// coreWait is the common async wait limit in the core tests.
const coreWait = 5 * time.Second

// coreLogBuffer 是并发安全的日志缓冲，用于断言 WithErrorLog 输出。
// coreLogBuffer is a concurrency-safe log buffer used to assert WithErrorLog output.
type coreLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write 实现 io.Writer。
// Write implements io.Writer.
func (b *coreLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String 返回已记录的日志。
// String returns the recorded log.
func (b *coreLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// coreNewLogger 返回写入缓冲的日志器及其缓冲。
// coreNewLogger returns a logger writing into a buffer, and the buffer.
func coreNewLogger() (*log.Logger, *coreLogBuffer) {
	buf := &coreLogBuffer{}
	return log.New(buf, "", 0), buf
}

// coreNewServer 创建服务器并注册路由，注册失败时终止测试。
// coreNewServer creates a server and registers routes, failing the test on error.
func coreNewServer(t *testing.T, opts []ServerOption, routes ...Route) *Server {
	t.Helper()
	s := NewServer(opts...)
	if len(routes) > 0 {
		if err := s.Register(routes...); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}
	return s
}

// coreDo 通过 httptest 发送请求；header 以键值对形式给出。
// coreDo sends a request through httptest; header is given as key/value pairs.
func coreDo(h http.Handler, method, target, body string, header ...string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// coreServe 用单个路由创建服务器并发送一次请求。
// coreServe creates a server with a single route and sends one request.
func coreServe(t *testing.T, route Route, method, target, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	return coreDo(coreNewServer(t, nil, route), method, target, body, header...)
}

// coreErrBody 把响应体解析为 ErrorResponse。
// coreErrBody parses the response body as ErrorResponse.
func coreErrBody(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var er ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &er); err != nil {
		t.Fatalf("body %q is not an ErrorResponse: %v", rec.Body.String(), err)
	}
	return er
}

// coreText 返回写出固定文本的原始处理器。
// coreText returns a raw handler writing a fixed text.
func coreText(text string) RawHandlerFunc {
	return func(_ context.Context, _ *Request, resp *Response) error {
		_, err := resp.Write([]byte(text))
		return err
	}
}

// coreReadAll 返回读取整个请求体并回显长度的原始处理器。
// coreReadAll returns a raw handler that reads the whole body and echoes its length.
func coreReadAll() RawHandlerFunc {
	return func(_ context.Context, req *Request, resp *Response) error {
		n, err := io.Copy(io.Discard, req.Raw.Body)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(resp, "%d", n)
		return err
	}
}

// coreTrace 返回在调用 next 前记录 name 的中间件。
// coreTrace returns middleware recording name before calling next.
func coreTrace(name string, trace *[]string) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			*trace = append(*trace, name)
			return next(ctx, req, resp)
		}
	}
}

// coreFreeAddr 获取一个空闲的本地地址。
// coreFreeAddr obtains a free local address.
func coreFreeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// coreClient 返回不复用连接的 HTTP 客户端。
// coreClient returns an HTTP client that does not reuse connections.
func coreClient() *http.Client {
	return &http.Client{
		Timeout:   coreWait,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

// TestCoreServerDefaults 测试服务器默认配置。
// TestCoreServerDefaults tests the default server configuration.
func TestCoreServerDefaults(t *testing.T) {
	s := NewServer(nil) // nil ServerOption 被忽略 / a nil ServerOption is ignored
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"maxBodyBytes", s.config.maxBodyBytes, DefaultMaxBodyBytes},
		{"readHeaderTimeout", s.config.readHeaderTimeout, DefaultReadHeaderTimeout},
		{"shutdownTimeout", s.config.shutdownTimeout, DefaultShutdownTimeout},
		{"addr", s.config.addr, ":8080"},
		{"strictPath", s.config.strictPath, false},
		{"errorHandler set", s.config.errorHandler != nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
	if DefaultMaxBodyBytes != 32<<20 {
		t.Errorf("DefaultMaxBodyBytes = %d, want 32MiB", DefaultMaxBodyBytes)
	}

	// WithErrorHandler(nil) 回退到默认错误处理器
	// WithErrorHandler(nil) falls back to the default error handler
	s2 := NewServer(WithErrorHandler(nil))
	if s2.config.errorHandler == nil {
		t.Error("nil error handler should fall back to default")
	}
}

// TestCoreRegisterValidation 测试注册期校验：method、handler、路径、选项与输入输出。
// TestCoreRegisterValidation tests registration-time validation: method, handler, path,
// options and input/output.
func TestCoreRegisterValidation(t *testing.T) {
	okEndpoint := func(context.Context, RequestOf[coreUser]) (string, error) { return "", nil }
	noDataEndpoint := func(context.Context, RequestOf[NoDataType]) (string, error) { return "", nil }
	passMW := func(next Handler) Handler { return next }
	nilMW := func(Handler) Handler { return nil }

	tests := []struct {
		name    string
		setup   func(s *Server)
		route   Route
		wantIs  error
		wantSub string
	}{
		{name: "非法 method token / invalid method token", route: Raw("GE T", "/x", coreText("x")), wantSub: "invalid HTTP method"},
		{name: "空 method / empty method", route: Method("", "/x", noDataEndpoint), wantSub: "invalid HTTP method"},
		{name: "method 含控制字符 / control char in method", route: Raw("GET\n", "/x", coreText("x")), wantSub: "invalid HTTP method"},
		{name: "Route 字面量非法 method / literal route with bad method", route: Route{Method: "B@D", Path: "/x", compiledHandler: Handler(coreText("x"))}, wantSub: "invalid HTTP method"},
		{name: "typed nil handler", route: Get[NoDataType, string]("/x", nil), wantSub: "handler cannot be nil"},
		{name: "Raw nil handler", route: Raw(http.MethodGet, "/x", nil), wantSub: "handler cannot be nil"},
		{name: "HandleAction nil handler", route: HandleAction[NoDataType](http.MethodPost, "/x", nil), wantSub: "handler cannot be nil"},
		{name: "HandleProcedure nil handler", route: HandleProcedure(http.MethodPost, "/x", nil), wantSub: "handler cannot be nil"},
		{name: "Route 字面量无 handler / literal route without handler", route: Route{Method: http.MethodGet, Path: "/x"}, wantSub: "nil handler"},
		{name: "空路径 / empty path", route: Get("", noDataEndpoint), wantSub: "path cannot be empty"},
		{name: "路径缺少前导斜杠 / path without leading slash", route: Get("users", noDataEndpoint), wantIs: ErrInvalidRoutePath},
		{name: "非法模板 / invalid template", route: Get("/users/{id", noDataEndpoint), wantIs: ErrInvalidRoutePath},
		{name: "WithMiddleware(nil)", route: Get("/x", noDataEndpoint, WithMiddleware(nil)), wantSub: "nil middleware"},
		{name: "WithMiddleware 含 nil / WithMiddleware with a nil entry", route: Get("/x", noDataEndpoint, WithMiddleware(passMW, nil)), wantSub: "nil middleware"},
		{name: "nil Option", route: Get("/x", noDataEndpoint, nil), wantSub: "nil route option"},
		{name: "路由中间件返回 nil / route middleware returns nil", route: Get("/x", noDataEndpoint, WithMiddleware(nilMW)), wantSub: "middleware returned a nil handler"},
		{
			name:    "Server.With(WithInput)",
			setup:   func(s *Server) { s.With(WithInput(JSONInput[coreUser]())) },
			route:   Post("/x", okEndpoint),
			wantSub: "route-level",
		},
		{
			name:    "Server.With(WithOutput)",
			setup:   func(s *Server) { s.With(WithOutput(TextOutput())) },
			route:   Get("/x", noDataEndpoint),
			wantSub: "route-level",
		},
		{
			name:    "Server.With(nil)",
			setup:   func(s *Server) { s.With(nil) },
			route:   Get("/x", noDataEndpoint),
			wantSub: "nil route option",
		},
		{name: "WithInput 类型不匹配 / WithInput type mismatch", route: Post("/x", okEndpoint, WithInput(JSONInput[NoDataType]())), wantSub: "does not match body type"},
		{name: "WithOutput 类型不匹配 / WithOutput type mismatch", route: Get("/x", noDataEndpoint, WithOutput(JSONOutput[int]())), wantSub: "does not match result type"},
		{name: "WithInput(nil)", route: Post("/x", okEndpoint, WithInput[coreUser](nil)), wantSub: "nil Input"},
		{name: "WithOutput(nil)", route: Get("/x", noDataEndpoint, WithOutput[string](nil)), wantSub: "nil Output"},
		{name: "Raw + WithInput", route: Raw(http.MethodPost, "/x", coreText("x"), WithInput(JSONInput[coreUser]())), wantSub: "not applicable to Raw"},
		{name: "Raw + WithOutput", route: Raw(http.MethodGet, "/x", coreText("x"), WithOutput(TextOutput())), wantSub: "not applicable to Raw"},
		{
			name:    "HandleAction + WithOutput",
			route:   HandleAction(http.MethodPost, "/x", func(context.Context, RequestOf[coreUser]) error { return nil }, WithOutput(TextOutput())),
			wantSub: "not applicable to actions",
		},
		{
			name:    "HandleAction WithInput 类型不匹配 / HandleAction input mismatch",
			route:   HandleAction(http.MethodPost, "/x", func(context.Context, RequestOf[coreUser]) error { return nil }, WithInput(JSONInput[NoDataType]())),
			wantSub: "does not match body type",
		},
		{name: "非法 body 类型 / invalid body type", route: Post("/x", func(context.Context, RequestOf[int]) (string, error) { return "", nil }), wantSub: "invalid body type"},
		{name: "非法响应类型 / invalid response type", route: Get("/x", func(context.Context, RequestOf[NoDataType]) (chan int, error) { return nil, nil }), wantSub: "invalid response type"},
		{
			name:    "HandleAction 非法 body 类型 / HandleAction invalid body type",
			route:   HandleAction(http.MethodPost, "/x", func(context.Context, RequestOf[string]) error { return nil }),
			wantSub: "invalid body type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer()
			if tt.setup != nil {
				tt.setup(s)
			}
			err := s.Register(tt.route)
			if err == nil {
				t.Fatal("expected registration error, got nil")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantIs)
			}
			if tt.wantSub != "" && !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", err, tt.wantSub)
			}
		})
	}
}

// TestCoreRegisterBatchPrevalidation 测试整批预校验：任一条非法时整批都不安装。
// TestCoreRegisterBatchPrevalidation tests whole-batch prevalidation: if any route is
// invalid, none of the batch is installed.
func TestCoreRegisterBatchPrevalidation(t *testing.T) {
	s := NewServer()
	err := s.Register(
		Raw(http.MethodGet, "/first", coreText("first")),
		Raw(http.MethodGet, "/second", coreText("second"), WithMiddleware(nil)),
	)
	if err == nil {
		t.Fatal("expected error for the invalid second route")
	}
	if !strings.Contains(err.Error(), "/second") {
		t.Errorf("error %q should name the failing route", err)
	}
	// 第一条也未安装：可再次注册且不冲突
	// The first route was not installed either: it can be registered again without conflict
	if err := s.Register(Raw(http.MethodGet, "/first", coreText("first"))); err != nil {
		t.Fatalf("re-register /first: %v", err)
	}
	if rec := coreDo(s, http.MethodGet, "/second", ""); rec.Code != http.StatusNotFound {
		t.Errorf("/second status = %d, want 404", rec.Code)
	}
	if rec := coreDo(s, http.MethodGet, "/first", ""); rec.Code != http.StatusOK || rec.Body.String() != "first" {
		t.Errorf("/first = %d %q", rec.Code, rec.Body.String())
	}
}

// TestCoreRegisterConflict 测试安装期冲突返回 ErrRouteConflict，且不回滚已安装路由。
// TestCoreRegisterConflict tests install-time conflicts return ErrRouteConflict without
// rolling back installed routes.
func TestCoreRegisterConflict(t *testing.T) {
	tests := []struct {
		name    string
		first   []Route
		second  []Route
		reach   string // 冲突后仍应可访问的路径 / path still reachable after the conflict
		missing string // 不应存在的路径 / path that must not exist
	}{
		{
			name:   "重复注册 / duplicate",
			first:  []Route{Raw(http.MethodGet, "/dup", coreText("a"))},
			second: []Route{Raw(http.MethodGet, "/dup", coreText("b"))},
			reach:  "/dup",
		},
		{
			name:   "模板与 Gin 写法等价冲突 / template equals gin syntax",
			first:  []Route{Raw(http.MethodGet, "/u/:id", coreText("a"))},
			second: []Route{Raw(http.MethodGet, "/u/{id}", coreText("b"))},
			reach:  "/u/1",
		},
		{
			name:   "参数名冲突 / param name conflict",
			first:  []Route{Raw(http.MethodGet, "/p/:id", coreText("a"))},
			second: []Route{Raw(http.MethodGet, "/p/:name", coreText("b"))},
			reach:  "/p/1",
		},
		{
			name:    "同批次后一条冲突不回滚前一条 / in-batch conflict keeps earlier routes",
			second:  []Route{Raw(http.MethodGet, "/ok", coreText("ok")), Raw(http.MethodGet, "/ok", coreText("again")), Raw(http.MethodGet, "/after", coreText("x"))},
			reach:   "/ok",
			missing: "/after",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer()
			if len(tt.first) > 0 {
				if err := s.Register(tt.first...); err != nil {
					t.Fatalf("first Register: %v", err)
				}
			}
			err := s.Register(tt.second...)
			if !errors.Is(err, ErrRouteConflict) {
				t.Fatalf("err = %v, want ErrRouteConflict", err)
			}
			if rec := coreDo(s, http.MethodGet, tt.reach, ""); rec.Code != http.StatusOK {
				t.Errorf("%s status = %d, want 200", tt.reach, rec.Code)
			}
			if tt.missing != "" {
				if rec := coreDo(s, http.MethodGet, tt.missing, ""); rec.Code != http.StatusNotFound {
					t.Errorf("%s status = %d, want 404", tt.missing, rec.Code)
				}
			}
		})
	}
}

// TestCoreUseNilMiddlewareWarns 测试 Server.Use(nil) 被忽略并经 WithErrorLog 告警。
// TestCoreUseNilMiddlewareWarns tests Server.Use(nil) is ignored and warned via WithErrorLog.
func TestCoreUseNilMiddlewareWarns(t *testing.T) {
	logger, buf := coreNewLogger()
	var trace []string
	s := NewServer(WithErrorLog(logger))
	s.Use(nil, coreTrace("ok", &trace), nil)
	if len(s.middleware) != 1 {
		t.Fatalf("middleware count = %d, want 1", len(s.middleware))
	}
	if got := strings.Count(buf.String(), "nil middleware passed to Use"); got != 2 {
		t.Errorf("warning count = %d, want 2; log: %q", got, buf.String())
	}
	if err := s.Register(Raw(http.MethodGet, "/x", coreText("x"))); err != nil {
		t.Fatal(err)
	}
	if rec := coreDo(s, http.MethodGet, "/x", ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(trace) != 1 {
		t.Errorf("trace = %v, want [ok]", trace)
	}
}

// TestCoreGlobalMiddlewareReturningNil 复现：全局中间件返回 nil handler 时首个请求 panic。
// TestCoreGlobalMiddlewareReturningNil reproduces: a global middleware returning a nil
// handler makes the first request panic.
func TestCoreGlobalMiddlewareReturningNil(t *testing.T) {
	s := coreNewServer(t, nil, Raw(http.MethodGet, "/x", coreText("x")))
	s.Use(func(Handler) Handler { return nil })
	var rec *httptest.ResponseRecorder
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ServeHTTP panicked: %v", r)
			}
		}()
		rec = coreDo(s, http.MethodGet, "/x", "")
	}()
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestCoreAfterStart 测试首个请求后 Use/With 被忽略并告警、注册返回 ErrRegistrationAfterStart。
// TestCoreAfterStart tests that after the first request Use/With are ignored with a
// warning and registration returns ErrRegistrationAfterStart.
func TestCoreAfterStart(t *testing.T) {
	logger, buf := coreNewLogger()
	var trace []string
	s := coreNewServer(t, []ServerOption{WithErrorLog(logger)}, Raw(http.MethodGet, "/x", coreText("x")))
	g := s.Group("/g")

	if rec := coreDo(s, http.MethodGet, "/x", ""); rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d", rec.Code)
	}
	if !s.started.Load() {
		t.Fatal("server should be started after first request")
	}

	s.Use(coreTrace("late-use", &trace))
	s.With(WithMiddleware(coreTrace("late-with", &trace)))

	tests := []struct {
		name string
		err  error
	}{
		{"Server.Register", s.Register(Raw(http.MethodGet, "/y", coreText("y")))},
		{"Group.Register", g.Register(Raw(http.MethodGet, "/y", coreText("y")))},
		{"Server.Group.Register", s.Group("/h").Register(Raw(http.MethodGet, "/y", coreText("y")))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, ErrRegistrationAfterStart) {
				t.Errorf("err = %v, want ErrRegistrationAfterStart", tt.err)
			}
		})
	}

	if rec := coreDo(s, http.MethodGet, "/x", ""); rec.Code != http.StatusOK {
		t.Fatalf("second request status = %d", rec.Code)
	}
	if len(trace) != 0 {
		t.Errorf("late middleware ran: %v", trace)
	}
	logs := buf.String()
	for _, want := range []string{"Use called after the server started", "With called after the server started"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log %q missing %q", logs, want)
		}
	}
}

// TestCoreDispatchThroughGlobalMiddleware 测试 404/405/TSR/OPTIONS/HEAD/非法路径都经过全局中间件，
// 且中间件在 next 前可读到 Request.Route()。
// TestCoreDispatchThroughGlobalMiddleware tests 404/405/TSR/OPTIONS/HEAD/bad paths all pass
// through global middleware, which can read Request.Route() before calling next.
func TestCoreDispatchThroughGlobalMiddleware(t *testing.T) {
	var (
		calls int
		route string
	)
	s := NewServer()
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			calls++
			route = req.Route()
			return next(ctx, req, resp)
		}
	})
	if err := s.Register(
		Raw(http.MethodGet, "/users/{id}", coreText("user")),
		Raw(http.MethodGet, "/docs", coreText("docs")),
	); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantRoute  string
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{name: "命中 / matched", method: http.MethodGet, target: "/users/7", wantStatus: 200, wantRoute: "/users/:id"},
		{name: "HEAD 回退 GET / HEAD falls back to GET", method: http.MethodHead, target: "/docs", wantStatus: 200, wantRoute: "/docs"},
		{
			name: "404", method: http.MethodGet, target: "/nope", wantStatus: 404,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if er := coreErrBody(t, rec); er.Status != 404 || er.Error != "not found" {
					t.Errorf("body = %+v", er)
				}
			},
		},
		{
			name: "405 带 Allow / 405 with Allow", method: http.MethodPost, target: "/users/7", wantStatus: 405,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if got := rec.Header().Get("Allow"); got != "GET, HEAD, OPTIONS" {
					t.Errorf("Allow = %q", got)
				}
				if er := coreErrBody(t, rec); er.Status != 405 {
					t.Errorf("body = %+v", er)
				}
			},
		},
		{
			name: "OPTIONS 自动 204 / automatic OPTIONS 204", method: http.MethodOptions, target: "/docs", wantStatus: 204,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if got := rec.Header().Get("Allow"); !strings.Contains(got, "OPTIONS") || !strings.Contains(got, "GET") {
					t.Errorf("Allow = %q", got)
				}
				if rec.Body.Len() != 0 {
					t.Errorf("body = %q, want empty", rec.Body.String())
				}
			},
		},
		{
			name: "TSR 301", method: http.MethodGet, target: "/docs/?q=1", wantStatus: 301,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if got := rec.Header().Get("Location"); got != "/docs?q=1" {
					t.Errorf("Location = %q", got)
				}
			},
		},
		{
			name: "非法路径 400 / invalid path 400", method: http.MethodGet, target: "/users/../docs", wantStatus: 400,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				if er := coreErrBody(t, rec); er.Error != "invalid request path" {
					t.Errorf("body = %+v", er)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, route = 0, "unset"
			rec := coreDo(s, tt.method, tt.target, "")
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if calls != 1 {
				t.Errorf("global middleware calls = %d, want 1", calls)
			}
			if tt.wantRoute != "" && route != tt.wantRoute {
				t.Errorf("Route() = %q, want %q", route, tt.wantRoute)
			}
			if tt.check != nil {
				tt.check(t, rec)
			}
		})
	}
}

// TestCoreBodyLimit 测试 Server 级与路由级请求体限额及 lazy 语义。
// TestCoreBodyLimit tests server- and route-level body limits and laziness.
func TestCoreBodyLimit(t *testing.T) {
	ignoreBody := Raw(http.MethodPost, "/x", coreText("ignored"))
	small := strings.Repeat("a", 4)
	big := strings.Repeat("a", 64)

	tests := []struct {
		name       string
		opts       []ServerOption
		setup      func(s *Server)
		route      Route
		body       string
		wantStatus int
	}{
		{name: "Server 限额内 / within server limit", opts: []ServerOption{WithMaxBodyBytes(8)}, route: Raw(http.MethodPost, "/x", coreReadAll()), body: small, wantStatus: 200},
		{name: "Server 超限 413 / over server limit 413", opts: []ServerOption{WithMaxBodyBytes(8)}, route: Raw(http.MethodPost, "/x", coreReadAll()), body: big, wantStatus: 413},
		{name: "路由放宽 / route raises limit", opts: []ServerOption{WithMaxBodyBytes(8)}, route: Raw(http.MethodPost, "/x", coreReadAll(), WithBodyLimit(128)), body: big, wantStatus: 200},
		{name: "路由收紧 / route lowers limit", route: Raw(http.MethodPost, "/x", coreReadAll(), WithBodyLimit(4)), body: big, wantStatus: 413},
		{name: "路由 -1 不限 / route -1 unlimited", opts: []ServerOption{WithMaxBodyBytes(8)}, route: Raw(http.MethodPost, "/x", coreReadAll(), WithBodyLimit(-1)), body: big, wantStatus: 200},
		{name: "路由 0 沿用 Server / route 0 inherits", opts: []ServerOption{WithMaxBodyBytes(8)}, route: Raw(http.MethodPost, "/x", coreReadAll(), WithBodyLimit(0)), body: big, wantStatus: 413},
		{name: "WithMaxBodyBytes(0) 不限 / unlimited", opts: []ServerOption{WithMaxBodyBytes(0)}, route: Raw(http.MethodPost, "/x", coreReadAll()), body: strings.Repeat("b", 64<<10), wantStatus: 200},
		{name: "WithMaxBodyBytes(-1) 不限 / unlimited", opts: []ServerOption{WithMaxBodyBytes(-1)}, route: Raw(http.MethodPost, "/x", coreReadAll()), body: big, wantStatus: 200},
		{name: "Server.With 默认限额 / Server.With default limit", setup: func(s *Server) { s.With(WithBodyLimit(4)) }, route: Raw(http.MethodPost, "/x", coreReadAll()), body: big, wantStatus: 413},
		{
			name: "typed JSON 超限 413 / typed JSON over limit",
			opts: []ServerOption{WithMaxBodyBytes(8)},
			route: Post("/x", func(ctx context.Context, r RequestOf[coreUser]) (string, error) {
				u, err := r.Data(ctx)
				return u.Name, err
			}),
			body: `{"name":"` + big + `"}`, wantStatus: 413,
		},
		{name: "未读 body 不受影响 / unread body unaffected", opts: []ServerOption{WithMaxBodyBytes(8)}, route: ignoreBody, body: big, wantStatus: 200},
		{
			name:  "NoData 不读 body / NoData never reads body",
			opts:  []ServerOption{WithMaxBodyBytes(1)},
			route: Post("/x", func(context.Context, RequestOf[NoDataType]) (string, error) { return "ok", nil }),
			body:  big, wantStatus: 200,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer(tt.opts...)
			if tt.setup != nil {
				tt.setup(s)
			}
			if err := s.Register(tt.route); err != nil {
				t.Fatal(err)
			}
			rec := coreDo(s, http.MethodPost, "/x", tt.body, "Content-Type", "application/json")
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body %q", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// coreZeroReader 是无限输出 'a' 的读取器。
// coreZeroReader is a reader yielding endless 'a' bytes.
type coreZeroReader struct{}

// Read 实现 io.Reader。
// Read implements io.Reader.
func (coreZeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// TestCoreBodyLimitDefault32MiB 测试默认 32MiB 上限：恰好 32MiB 通过，多 1 字节返回 413。
// TestCoreBodyLimitDefault32MiB tests the default 32MiB limit: exactly 32MiB passes and one
// more byte yields 413.
func TestCoreBodyLimitDefault32MiB(t *testing.T) {
	if testing.Short() {
		t.Skip("skip 32MiB body in -short mode")
	}
	s := coreNewServer(t, nil, Raw(http.MethodPost, "/x", coreReadAll()))
	tests := []struct {
		name       string
		size       int64
		wantStatus int
	}{
		{"恰好上限 / exactly at limit", DefaultMaxBodyBytes, 200},
		{"超出 1 字节 / one byte over", DefaultMaxBodyBytes + 1, 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/x", io.LimitReader(coreZeroReader{}, tt.size))
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestCoreErrorAfterCommitLogged 测试响应已提交后返回的错误只记录日志，不改写响应。
// TestCoreErrorAfterCommitLogged tests that an error returned after the response is
// committed is only logged and does not rewrite the response.
func TestCoreErrorAfterCommitLogged(t *testing.T) {
	logger, buf := coreNewLogger()
	s := coreNewServer(t, []ServerOption{WithErrorLog(logger)},
		Raw(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusAccepted)
			_, _ = resp.Write([]byte("partial"))
			return errors.New("secret failure")
		}))
	rec := coreDo(s, http.MethodGet, "/x", "")
	if rec.Code != http.StatusAccepted || rec.Body.String() != "partial" {
		t.Errorf("response = %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(buf.String(), "error after response was committed") {
		t.Errorf("log = %q", buf.String())
	}
}

// TestCoreCustomErrorHandler 测试 WithErrorHandler 替换默认错误响应，且 404 也走它。
// TestCoreCustomErrorHandler tests WithErrorHandler replaces the default error response,
// including for 404.
func TestCoreCustomErrorHandler(t *testing.T) {
	var got []error
	s := coreNewServer(t, []ServerOption{WithErrorHandler(func(_ context.Context, _ *Request, resp *Response, err error) {
		got = append(got, err)
		resp.WriteHeader(StatusFromError(err))
		_, _ = resp.Write([]byte("custom"))
	})}, Raw(http.MethodGet, "/x", func(context.Context, *Request, *Response) error { return ErrForbidden }))

	tests := []struct {
		target     string
		wantStatus int
		wantErr    error
	}{
		{"/x", 403, ErrForbidden},
		{"/missing", 404, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got = nil
			rec := coreDo(s, http.MethodGet, tt.target, "")
			if rec.Code != tt.wantStatus || rec.Body.String() != "custom" {
				t.Errorf("response = %d %q", rec.Code, rec.Body.String())
			}
			if len(got) != 1 || !errors.Is(got[0], tt.wantErr) {
				t.Errorf("handler errors = %v", got)
			}
		})
	}
}

// TestCoreHTTPServerConfig 测试 WithAddr/WithReadHeaderTimeout/WithBaseContext/WithErrorLog 等传入底层
// http.Server；Run("") 使用 WithAddr，非法地址返回错误。
// TestCoreHTTPServerConfig tests that WithAddr/WithReadHeaderTimeout/WithBaseContext/
// WithErrorLog etc. reach the underlying http.Server; Run("") uses WithAddr and an invalid
// address returns an error.
func TestCoreHTTPServerConfig(t *testing.T) {
	type ctxKey struct{}
	logger, _ := coreNewLogger()
	baseCtx := context.WithValue(context.Background(), ctxKey{}, "base")
	const badAddr = "127.0.0.1:not-a-port"

	tests := []struct {
		name string
		run  func(s *Server) error
	}{
		{"Run", func(s *Server) error { return s.Run("") }},
		{"RunContext", func(s *Server) error { return s.RunContext(context.Background(), "") }},
		{"RunTLS", func(s *Server) error { return s.RunTLS("", "missing.crt", "missing.key") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer(
				WithAddr(badAddr),
				WithReadHeaderTimeout(3*time.Second),
				WithReadTimeout(4*time.Second),
				WithWriteTimeout(5*time.Second),
				WithIdleTimeout(6*time.Second),
				WithMaxHeaderBytes(4096),
				WithBaseContext(func(net.Listener) context.Context { return baseCtx }),
				WithErrorLog(logger),
			)
			err := tt.run(s)
			if err == nil || errors.Is(err, ErrServerClosed) {
				t.Fatalf("err = %v, want a listen error", err)
			}
			hs := s.httpServer
			if hs == nil {
				t.Fatal("httpServer not created")
			}
			if hs.Addr != badAddr {
				t.Errorf("Addr = %q, want %q", hs.Addr, badAddr)
			}
			if hs.ReadHeaderTimeout != 3*time.Second || hs.ReadTimeout != 4*time.Second ||
				hs.WriteTimeout != 5*time.Second || hs.IdleTimeout != 6*time.Second || hs.MaxHeaderBytes != 4096 {
				t.Errorf("timeouts/limits not propagated: %+v", hs)
			}
			if hs.ErrorLog != logger {
				t.Error("ErrorLog not propagated")
			}
			if hs.BaseContext == nil || hs.BaseContext(nil).Value(ctxKey{}) != "base" {
				t.Error("BaseContext not propagated")
			}
			if hs.Handler != s {
				t.Error("Handler should be the Server")
			}
			if !s.started.Load() {
				t.Error("Run should freeze the chain")
			}
		})
	}

	// 默认 ReadHeaderTimeout 也会传入
	// The default ReadHeaderTimeout is propagated too
	s := NewServer(WithAddr(badAddr))
	_ = s.Run("")
	if s.httpServer.ReadHeaderTimeout != DefaultReadHeaderTimeout {
		t.Errorf("default ReadHeaderTimeout = %v", s.httpServer.ReadHeaderTimeout)
	}
}

// TestCoreRunContextUsesWithAddr 测试 RunContext(ctx, "") 监听 WithAddr，ctx 取消后返回 nil。
// TestCoreRunContextUsesWithAddr tests RunContext(ctx, "") listens on WithAddr and returns
// nil after ctx is canceled.
func TestCoreRunContextUsesWithAddr(t *testing.T) {
	addr := coreFreeAddr(t)
	s := coreNewServer(t, []ServerOption{WithAddr(addr)}, Raw(http.MethodGet, "/ping", coreText("pong")))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- s.RunContext(ctx, "") }()

	client := coreClient()
	deadline := time.Now().Add(coreWait)
	for {
		resp, err := client.Get("http://" + addr + "/ping")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(body) != "pong" {
				t.Fatalf("body = %q", body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("RunContext = %v, want nil", err)
		}
	case <-time.After(coreWait):
		t.Fatal("RunContext did not return")
	}
}

// coreStartSlow 启动一个带阻塞路由的 ServeContext，返回 ctx 取消函数、结果通道等。
// coreStartSlow starts ServeContext with a blocking route and returns the cancel func,
// result channels, etc.
func coreStartSlow(t *testing.T, opts ...ServerOption) (s *Server, cancel context.CancelFunc, release chan struct{}, serveErr chan error, bodyCh chan string) {
	t.Helper()
	started := make(chan struct{})
	release = make(chan struct{})
	s = coreNewServer(t, opts, Raw(http.MethodGet, "/slow", func(_ context.Context, _ *Request, resp *Response) error {
		close(started)
		<-release
		_, err := resp.Write([]byte("done"))
		return err
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	serveErr = make(chan error, 1)
	go func() { serveErr <- s.ServeContext(ctx, ln) }()

	bodyCh = make(chan string, 1)
	go func() {
		resp, err := coreClient().Get("http://" + ln.Addr().String() + "/slow")
		if err != nil {
			bodyCh <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		bodyCh <- string(b)
	}()
	select {
	case <-started:
	case <-time.After(coreWait):
		t.Fatal("slow handler never started")
	}
	return s, cancel, release, serveErr, bodyCh
}

// TestCoreServeContextGraceful 测试 ctx 取消后优雅关闭：等待在途请求完成并返回 nil。
// TestCoreServeContextGraceful tests graceful shutdown on ctx cancel: in-flight requests
// complete and nil is returned.
func TestCoreServeContextGraceful(t *testing.T) {
	_, cancel, release, serveErr, bodyCh := coreStartSlow(t, WithShutdownTimeout(coreWait))
	defer cancel()

	cancel()
	select {
	case err := <-serveErr:
		t.Fatalf("ServeContext returned before in-flight request finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)

	select {
	case body := <-bodyCh:
		if body != "done" {
			t.Errorf("in-flight body = %q, want done", body)
		}
	case <-time.After(coreWait):
		t.Fatal("in-flight request never completed")
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Errorf("ServeContext = %v, want nil", err)
		}
	case <-time.After(coreWait):
		t.Fatal("ServeContext did not return")
	}
}

// TestCoreServeContextShutdownTimeout 测试关闭超时：在途长请求导致返回 context.DeadlineExceeded。
// TestCoreServeContextShutdownTimeout tests the shutdown timeout: a long in-flight request
// makes ServeContext return context.DeadlineExceeded.
func TestCoreServeContextShutdownTimeout(t *testing.T) {
	s, cancel, release, serveErr, bodyCh := coreStartSlow(t, WithShutdownTimeout(50*time.Millisecond))
	defer func() {
		close(release)
		<-bodyCh
		_ = s.Close()
	}()

	cancel()
	select {
	case err := <-serveErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("ServeContext = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(coreWait):
		t.Fatal("ServeContext did not return after shutdown timeout")
	}
}

// TestCoreConcurrentServeAndRegister 测试首个请求与 Register/Use/With/Group 并发时无数据竞争，
// 且成功注册的路由都可访问。
// TestCoreConcurrentServeAndRegister tests no data race when the first request races with
// Register/Use/With/Group, and every successfully registered route is reachable.
func TestCoreConcurrentServeAndRegister(t *testing.T) {
	logger, _ := coreNewLogger()
	for iter := 0; iter < 20; iter++ {
		s := coreNewServer(t, []ServerOption{WithErrorLog(logger)}, Raw(http.MethodGet, "/a", coreText("a")))
		start := make(chan struct{})
		var wg sync.WaitGroup
		var regMu sync.Mutex
		registered := map[string]bool{}

		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if rec := coreDo(s, http.MethodGet, "/a?x=1", ""); rec.Code != http.StatusOK {
					t.Errorf("status = %d", rec.Code)
				}
			}()
		}
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				path := fmt.Sprintf("/r%d", i)
				var err error
				if i%2 == 0 {
					err = s.Register(Raw(http.MethodGet, path, coreText("r")))
				} else {
					err = s.Group("/g").Register(Raw(http.MethodGet, path, coreText("r")))
					path = "/g" + path
				}
				switch {
				case err == nil:
					regMu.Lock()
					registered[path] = true
					regMu.Unlock()
				case !errors.Is(err, ErrRegistrationAfterStart):
					t.Errorf("Register: %v", err)
				}
			}(i)
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			s.Use(func(next Handler) Handler { return next })
		}()
		go func() {
			defer wg.Done()
			<-start
			s.With(WithBodyLimit(1 << 10))
		}()

		close(start)
		wg.Wait()
		for path := range registered {
			if rec := coreDo(s, http.MethodGet, path, ""); rec.Code != http.StatusOK {
				t.Errorf("registered route %s status = %d", path, rec.Code)
			}
		}
	}
}
