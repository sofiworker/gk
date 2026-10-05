package ghttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/sofiworker/gk/gerr"
)

// r2Order 是带自校验方法（值接收者）的请求体。
// r2Order is a body with a value-receiver Validate method.
type r2Order struct {
	Qty int `json:"qty"`
}

func (o r2Order) Validate() error {
	if o.Qty <= 0 {
		return errors.New("qty must be positive")
	}
	return nil
}

// r2Login 是带 context 自校验方法（指针接收者）的请求体。
// r2Login is a body with a pointer-receiver, context-aware Validate method.
type r2Login struct {
	User string `json:"user"`
}

func (l *r2Login) Validate(context.Context) error {
	if l.User == "" {
		return Forbidden("user required")
	}
	return nil
}

// TestValidator 测试自校验方法、路由级/默认校验器、错误映射与 lazy 语义。
// TestValidator tests self-validation, route/default validators, error mapping and laziness.
func TestValidator(t *testing.T) {
	calls := 0
	maxQty := ValidatorFunc(func(_ context.Context, v any) error {
		// 默认校验器作用于所有路由，因此按类型分支
		// Default validators apply to every route, so switch on the type
		o, ok := v.(r2Order)
		if !ok {
			return nil
		}
		calls++
		if o.Qty > 10 {
			return errors.New("too many")
		}
		return nil
	})
	echo := func(ctx context.Context, req RequestOf[r2Order]) (int, error) {
		o, err := req.Data(ctx)
		return o.Qty, err
	}
	s := NewServer()
	s.With(WithValidator(maxQty))
	if err := s.Register(
		Post("/order", echo),
		Post("/login", func(ctx context.Context, req RequestOf[r2Login]) (string, error) {
			l, err := req.Data(ctx)
			return l.User, err
		}),
		Post("/login-ptr", func(ctx context.Context, req RequestOf[*r2Login]) (string, error) {
			l, err := req.Data(ctx)
			if err != nil {
				return "", err
			}
			return l.User, nil
		}),
		Post("/lazy", func(context.Context, RequestOf[r2Order]) (string, error) { return "skipped", nil }),
	); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path, body string
		status     int
	}{
		{"/order", `{"qty":3}`, 200},
		{"/order", `{"qty":0}`, 400},
		{"/order", `{"qty":11}`, 400},
		{"/login", `{"user":"a"}`, 200},
		{"/login", `{"user":""}`, 403},
		{"/login-ptr", `{"user":"b"}`, 200},
		{"/login-ptr", `null`, 400},
		{"/lazy", `{"qty":0}`, 200},
	}
	for _, tt := range tests {
		t.Run(tt.path+tt.body, func(t *testing.T) {
			rec := coreDo(s, http.MethodPost, tt.path, tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.status, rec.Body.String())
			}
		})
	}
	if calls != 2 {
		t.Errorf("default validator called %d times, want 2 (only valid self-checked orders)", calls)
	}
	if err := NewServer().Register(Post("/x", echo, WithValidator(nil))); err == nil {
		t.Error("nil validator should fail registration")
	}
}

// TestValidationErrorWrapping 测试校验错误的包装规则。
// TestValidationErrorWrapping tests validation error wrapping rules.
func TestValidationErrorWrapping(t *testing.T) {
	if validationError(nil) != nil {
		t.Error("nil should stay nil")
	}
	if err := validationError(errors.New("x")); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("plain error should wrap ErrInvalidInput: %v", err)
	}
	if err := validationError(Forbidden("no")); StatusFromError(err) != 403 {
		t.Errorf("HTTPError should keep its status: %v", err)
	}
}

// r2Logger 记录各级别的日志行。
// r2Logger records lines per level.
type r2Logger struct {
	mu    sync.Mutex
	lines map[string][]string
}

func (l *r2Logger) add(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lines == nil {
		l.lines = map[string][]string{}
	}
	l.lines[level] = append(l.lines[level], strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func (l *r2Logger) Debugf(f string, a ...any) { l.add("debug", f, a...) }
func (l *r2Logger) Infof(f string, a ...any)  { l.add("info", f, a...) }
func (l *r2Logger) Warnf(f string, a ...any)  { l.add("warn", f, a...) }
func (l *r2Logger) Errorf(f string, a ...any) { l.add("error", f, a...) }

func (l *r2Logger) get(level string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines[level]...)
}

// TestLoggerInjection 测试 WithLogger 接收框架告警，并转发 net/http 错误。
// TestLoggerInjection tests WithLogger receives framework warnings and net/http errors.
func TestLoggerInjection(t *testing.T) {
	l := &r2Logger{}
	s := coreNewServer(t, []ServerOption{WithLogger(l)}, Raw(http.MethodGet, "/", coreText("ok")))
	coreDo(s, http.MethodGet, "/", "")
	s.Use(AccessLog())
	if w := l.get("warn"); len(w) != 1 || !strings.Contains(w[0], "Use called after") {
		t.Fatalf("warn = %v", w)
	}

	el := errorLogFor(s.config)
	if el == nil {
		t.Fatal("errorLogFor should adapt the Logger")
	}
	el.Printf("tls: handshake error\n")
	if e := l.get("error"); len(e) != 1 || e[0] != "tls: handshake error" {
		t.Errorf("error = %v", e)
	}

	std := log.New(&bytes.Buffer{}, "", 0)
	if errorLogFor(serverConfig{errorLog: std, logger: l}) != std {
		t.Error("WithErrorLog should win over WithLogger")
	}
	if errorLogFor(serverConfig{}) != nil {
		t.Error("no logger configured should yield nil")
	}
}

// TestLoggerAdapters 测试 slog、标准库与 nop 适配器。
// TestLoggerAdapters tests the slog, stdlib and nop adapters.
func TestLoggerAdapters(t *testing.T) {
	var buf bytes.Buffer
	sl := NewSlogLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	sl.Debugf("hidden %d", 1)
	sl.Infof("info %d", 2)
	sl.Warnf("warn")
	sl.Errorf("err")
	out := buf.String()
	if strings.Contains(out, "hidden") || !strings.Contains(out, "info 2") || !strings.Contains(out, "level=WARN") || !strings.Contains(out, "level=ERROR") {
		t.Errorf("slog output = %q", out)
	}
	if NewSlogLogger(nil) == nil {
		t.Error("nil slog should use default")
	}

	buf.Reset()
	st := NewStdLogger(log.New(&buf, "", 0))
	st.Debugf("d")
	st.Infof("i")
	st.Warnf("w")
	st.Errorf("e")
	if buf.String() != "[DEBUG] d\n[INFO] i\n[WARN] w\n[ERROR] e\n" {
		t.Errorf("std output = %q", buf.String())
	}
	_ = NewStdLogger(nil)

	n := NopLogger()
	n.Debugf("x")
	n.Infof("x")
	n.Warnf("x")
	n.Errorf("x")
}

// TestAccessLogLevelsAndColor 测试访问日志按状态选级别、路由模板、颜色与跳过路径。
// TestAccessLogLevelsAndColor tests access log levels by status, route template, color and skips.
func TestAccessLogLevelsAndColor(t *testing.T) {
	l := &r2Logger{}
	s := NewServer()
	s.Use(AccessLogWithConfig(AccessLogConfig{Logger: l, SkipPaths: []string{"/skip"}}))
	if err := s.Register(
		Raw(http.MethodGet, "/ok/:id", coreText("ok")),
		Raw(http.MethodGet, "/skip", coreText("ok")),
		Raw(http.MethodGet, "/bad", func(context.Context, *Request, *Response) error { return ErrInternalServerError }),
	); err != nil {
		t.Fatal(err)
	}
	coreDo(s, http.MethodGet, "/ok/1", "")
	coreDo(s, http.MethodGet, "/missing", "")
	coreDo(s, http.MethodGet, "/bad", "")
	coreDo(s, http.MethodGet, "/skip", "")

	if got := l.get("info"); len(got) != 1 || !strings.Contains(got[0], " 200 ") || strings.Contains(got[0], "\033") {
		t.Errorf("info = %q", got)
	}
	if got := l.get("warn"); len(got) != 1 || !strings.Contains(got[0], " 404 ") {
		t.Errorf("warn = %q", got)
	}
	if got := l.get("error"); len(got) != 1 || !strings.Contains(got[0], " 500 ") {
		t.Errorf("error = %q", got)
	}

	var routes []string
	var buf bytes.Buffer
	s2 := NewServer()
	s2.Use(AccessLogWithConfig(AccessLogConfig{Output: &buf, Color: ColorAlways, Formatter: func(p AccessLogParams) string {
		routes = append(routes, p.Route)
		return defaultAccessLogFormatter(p)
	}}))
	_ = s2.Register(Raw(http.MethodPost, "/u/:id", coreText("x")))
	coreDo(s2, http.MethodPost, "/u/9", "")
	if !reflect.DeepEqual(routes, []string{"/u/:id"}) || !strings.Contains(buf.String(), "\033[") {
		t.Errorf("routes=%v colored=%q", routes, buf.String())
	}

	for _, tt := range []struct {
		mode ColorMode
		out  io.Writer
		want bool
	}{
		{ColorNever, os.Stdout, false},
		{ColorAlways, &buf, true},
		{ColorAuto, &buf, false},
	} {
		if got := useColor(tt.mode, tt.out); got != tt.want {
			t.Errorf("useColor(%v) = %v", tt.mode, got)
		}
	}
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE"} {
		_ = methodColorOf(m)
	}
	for _, c := range []int{200, 301, 404, 500} {
		if statusColorOf(c) == "" {
			t.Errorf("no color for %d", c)
		}
	}
	_ = AccessLogWithLogger(l)
}

// TestGerrInterop 测试 HTTPError 与 gerr 的互转。
// TestGerrInterop tests conversions between HTTPError and gerr.
func TestGerrInterop(t *testing.T) {
	ge := gerr.New("user missing", gerr.WithKind(gerr.KindNotFound), gerr.WithCode("U404"))
	he, ok := FromGerr(ge)
	if !ok || he.Status != 404 || he.Message != "user missing" || !errors.Is(he, ge) {
		t.Errorf("FromGerr = %+v %v", he, ok)
	}
	if he, ok := FromGerr(Forbidden("x")); !ok || he.Status != 403 {
		t.Errorf("FromGerr(HTTPError) = %+v %v", he, ok)
	}
	if _, ok := FromGerr(errors.New("plain")); ok {
		t.Error("plain error should not convert")
	}

	if ToGerr(nil) != nil {
		t.Error("ToGerr(nil) should be nil")
	}
	if ToGerr(ge) != ge {
		t.Error("existing gerr should be returned as-is")
	}
	g := ToGerr(Forbidden("nope"))
	if g.Kind != gerr.KindPermission || g.Code != "http_403" || g.Message != "nope" || g.Meta["http_status"] != 403 {
		t.Errorf("ToGerr = %+v", g)
	}
	if g := ToGerr(errors.New("x")); g.Kind != gerr.KindInternal || g.Message != "Internal Server Error" {
		t.Errorf("ToGerr(plain) = %+v", g)
	}

	kinds := map[int]gerr.Kind{
		404: gerr.KindNotFound, 410: gerr.KindNotFound, 409: gerr.KindConflict, 412: gerr.KindConflict,
		401: gerr.KindPermission, 403: gerr.KindPermission, 503: gerr.KindUnavailable, 429: gerr.KindUnavailable,
		502: gerr.KindUnavailable, 504: gerr.KindTimeout, 408: gerr.KindTimeout, 499: gerr.KindCanceled,
		400: gerr.KindInvalid, 422: gerr.KindInvalid, 500: gerr.KindInternal, 200: gerr.KindUnknown,
	}
	for status, want := range kinds {
		if got := KindFromStatus(status); got != want {
			t.Errorf("KindFromStatus(%d) = %q, want %q", status, got, want)
		}
	}
	if GerrStatus(gerr.KindConflict) != 409 {
		t.Error("GerrStatus mismatch")
	}
}

// TestRedirectFixedPath 测试路径修正重定向。
// TestRedirectFixedPath tests fixed-path redirects.
func TestRedirectFixedPath(t *testing.T) {
	routes := []Route{
		Raw(http.MethodGet, "/Users/:id", coreText("u")),
		Raw(http.MethodPost, "/Items", coreText("i")),
	}
	on := coreNewServer(t, []ServerOption{WithRedirectFixedPath()}, routes...)
	off := coreNewServer(t, nil, routes...)

	tests := []struct {
		s              *Server
		method, target string
		status         int
		location       string
	}{
		{on, http.MethodGet, "/users/7", 301, "/Users/7"},
		{on, http.MethodGet, "/users//7?x=1", 301, "/Users/7?x=1"},
		{on, http.MethodHead, "/USERS/7", 301, "/Users/7"},
		{on, http.MethodPost, "/items", 308, "/Items"},
		{on, http.MethodGet, "/nothing", 404, ""},
		{off, http.MethodGet, "/users/7", 404, ""},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			rec := coreDo(tt.s, tt.method, tt.target, "")
			if rec.Code != tt.status || rec.Header().Get("Location") != tt.location {
				t.Errorf("got %d %q, want %d %q", rec.Code, rec.Header().Get("Location"), tt.status, tt.location)
			}
		})
	}
	for _, p := range []string{"/a", "/", "//evil", "/\\evil", ""} {
		want := p == "/a" || p == "/"
		if safeRedirectPath(p) != want {
			t.Errorf("safeRedirectPath(%q) != %v", p, want)
		}
	}
	if _, ok := newRouter().fixedPath(http.MethodGet, "/x"); ok {
		t.Error("empty router should not fix")
	}
}

// TestOnShutdown 测试关闭钩子逆序执行一次并合并错误。
// TestOnShutdown tests shutdown hooks run once in reverse order and join errors.
func TestOnShutdown(t *testing.T) {
	var order []int
	boom := errors.New("boom")
	s := NewServer()
	s.OnShutdown(func(context.Context) error { order = append(order, 1); return nil }).
		OnShutdown(nil).
		OnShutdown(func(context.Context) error { order = append(order, 2); return boom })

	if err := s.Shutdown(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("Shutdown err = %v", err)
	}
	if err := s.Close(); !errors.Is(err, boom) {
		t.Errorf("Close err = %v", err)
	}
	if !reflect.DeepEqual(order, []int{2, 1}) {
		t.Errorf("order = %v, want [2 1] once", order)
	}
}

// TestProtocolOptions 测试协议选项传入底层 http.Server。
// TestProtocolOptions tests protocol options reach the underlying http.Server.
func TestProtocolOptions(t *testing.T) {
	h2 := &http.HTTP2Config{MaxConcurrentStreams: 7}
	s := NewServer(WithH2C(), WithHTTP2Config(h2))
	hs, err := s.prepareHTTPServer("127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	if hs.Protocols == nil || !hs.Protocols.UnencryptedHTTP2() || !hs.Protocols.HTTP1() || hs.HTTP2 != h2 {
		t.Errorf("protocols = %v http2 = %v", hs.Protocols, hs.HTTP2)
	}
	p := new(http.Protocols)
	p.SetHTTP1(true)
	s2 := NewServer(WithProtocols(p))
	hs2, _ := s2.prepareHTTPServer("", false)
	if hs2.Protocols != p {
		t.Error("WithProtocols not applied")
	}
}

// TestRoutesInfo 测试路由元数据与文档选项。
// TestRoutesInfo tests route metadata and doc options.
func TestRoutesInfo(t *testing.T) {
	s := NewServer()
	s.With(WithTags("base"))
	g := s.Group("/api", WithGroupOptions(WithTags("api")))
	if err := g.Register(
		Post("/users/{id}", func(context.Context, RequestOf[r2Order]) (Reply[coreUser], error) { return Reply[coreUser]{}, nil },
			WithDoc("Create", "creates"), WithOperationID("createUser"), WithDeprecated(), WithTags("users")),
		HandleAction(http.MethodPut, "/a", func(context.Context, RequestOf[r2Order]) error { return nil }),
		HandleProcedure(http.MethodDelete, "/p", func(context.Context) error { return nil }),
		Raw(http.MethodGet, "/raw", coreText("x")),
	); err != nil {
		t.Fatal(err)
	}
	_ = s.Register(Get("/z", apiNoData(1)))

	got := s.Routes()
	if len(got) != 5 {
		t.Fatalf("routes = %d", len(got))
	}
	byPath := map[string]RouteInfo{}
	for _, r := range got {
		byPath[r.Method+" "+r.Path] = r
	}
	u := byPath["POST /api/users/:id"]
	if u.Kind != RouteEndpoint || u.BodyType != reflect.TypeFor[r2Order]() || u.ResultType != reflect.TypeFor[Reply[coreUser]]() {
		t.Errorf("endpoint info = %+v", u)
	}
	if u.Doc.Summary != "Create" || u.Doc.OperationID != "createUser" || !u.Doc.Deprecated ||
		!reflect.DeepEqual(u.Doc.Tags, []string{"base", "api", "users"}) {
		t.Errorf("doc = %+v", u.Doc)
	}
	if a := byPath["PUT /api/a"]; a.Kind != RouteAction || a.ResultType != nil {
		t.Errorf("action = %+v", a)
	}
	if p := byPath["DELETE /api/p"]; p.Kind != RouteProcedure || p.BodyType != nil {
		t.Errorf("procedure = %+v", p)
	}
	if r := byPath["GET /api/raw"]; r.Kind != RouteRaw {
		t.Errorf("raw = %+v", r)
	}
	if z := byPath["GET /z"]; z.BodyType != nil || z.ResultType != reflect.TypeFor[int]() {
		t.Errorf("z = %+v", z)
	}
	got[0].Doc.Tags = append(got[0].Doc.Tags, "mutated")
	if reflect.DeepEqual(s.Routes()[0].Doc.Tags, got[0].Doc.Tags) {
		t.Error("Routes should return copies")
	}
}

// TestResponseHijack 测试 Hijack 标记已提交，之后的错误只记日志不写响应。
// TestResponseHijack tests Hijack marks the response committed so later errors are only logged.
func TestResponseHijack(t *testing.T) {
	l := &r2Logger{}
	s := coreNewServer(t, []ServerOption{WithLogger(l)}, Raw(http.MethodGet, "/up",
		func(_ context.Context, _ *Request, resp *Response) error {
			conn, _, err := http.NewResponseController(resp).Hijack()
			if err != nil {
				return err
			}
			if !resp.Hijacked() || !resp.Written() || resp.StatusCode() != http.StatusSwitchingProtocols {
				t.Errorf("hijacked=%v written=%v status=%d", resp.Hijacked(), resp.Written(), resp.StatusCode())
			}
			_, _ = conn.Write([]byte("HTTP/1.1 418 I'm a teapot\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			_ = conn.Close()
			return errors.New("after hijack")
		}))
	srv := httptest.NewServer(s)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/up")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d", res.StatusCode)
	}
	if w := l.get("warn"); len(w) != 1 || !strings.Contains(w[0], "hijacked") {
		t.Errorf("warn = %v", w)
	}

	type plain struct{ http.ResponseWriter }
	if _, _, err := (&Response{Writer: plain{httptest.NewRecorder()}}).Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Hijack on plain writer = %v", err)
	}
}
