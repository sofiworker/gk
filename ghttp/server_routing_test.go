package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// textHandler 返回写出固定文本的处理器。
// textHandler returns a handler that writes a fixed text.
func textHandler(text string) RawHandlerFunc {
	return func(_ context.Context, _ *Request, resp *Response) error {
		_, err := resp.Write([]byte(text))
		return err
	}
}

// newRoutingServer 创建注册了给定路由的服务器。
// newRoutingServer creates a server with the given routes registered.
func newRoutingServer(t *testing.T, routes ...Route) *Server {
	t.Helper()
	s := NewServer()
	if err := s.Register(routes...); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	return s
}

// TestServerTrailingSlashRedirect 测试 TSR：GET/HEAD 用 301，其他方法用 308，并保留查询串。
// TestServerTrailingSlashRedirect tests TSR: 301 for GET/HEAD, 308 otherwise, keeping the query.
func TestServerTrailingSlashRedirect(t *testing.T) {
	s := newRoutingServer(t,
		Raw(http.MethodGet, "/users", textHandler("users")),
		Raw(http.MethodPost, "/users", textHandler("create")),
		Raw(http.MethodGet, "/docs/", textHandler("docs")),
		Raw(http.MethodGet, "/users/:id/", textHandler("user")),
	)

	tests := []struct {
		name     string
		method   string
		target   string
		status   int
		location string
	}{
		{"GET strip slash", http.MethodGet, "/users/", http.StatusMovedPermanently, "/users"},
		{"HEAD strip slash", http.MethodHead, "/users/", http.StatusMovedPermanently, "/users"},
		{"POST strip slash keeps method", http.MethodPost, "/users/", http.StatusPermanentRedirect, "/users"},
		{"GET add slash", http.MethodGet, "/docs", http.StatusMovedPermanently, "/docs/"},
		{"param add slash", http.MethodGet, "/users/42", http.StatusMovedPermanently, "/users/42/"},
		{"query preserved", http.MethodGet, "/users/?page=2&q=a", http.StatusMovedPermanently, "/users?page=2&q=a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(tt.method, tt.target, nil))
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if got := w.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
		})
	}
}

// TestServerTrailingSlashNoRedirect 测试不满足 TSR 条件时不重定向。
// TestServerTrailingSlashNoRedirect tests that no redirect happens when TSR does not apply.
func TestServerTrailingSlashNoRedirect(t *testing.T) {
	s := newRoutingServer(t,
		Raw(http.MethodGet, "/users", textHandler("users")),
		Raw(http.MethodGet, "/evil.com", textHandler("evil")),
	)

	tests := []struct {
		name   string
		method string
		target string
		status int
	}{
		{"exact match", http.MethodGet, "/users", http.StatusOK},
		{"unknown path", http.MethodGet, "/posts/", http.StatusNotFound},
		{"method without tree", http.MethodPut, "/users/", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(tt.method, tt.target, nil))
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			if loc := w.Header().Get("Location"); loc != "" {
				t.Errorf("unexpected Location %q", loc)
			}
		})
	}
}

// TestServerTrailingSlashOpenRedirect 测试 TSR 不会生成协议相对 URL（开放重定向）。
// TestServerTrailingSlashOpenRedirect tests that TSR never produces protocol-relative URLs.
func TestServerTrailingSlashOpenRedirect(t *testing.T) {
	s := newRoutingServer(t, Raw(http.MethodGet, "/:host/", textHandler("host")))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = "//evil.com"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if loc := w.Header().Get("Location"); strings.HasPrefix(loc, "//") {
		t.Fatalf("open redirect to %q", loc)
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestServerMethodNotAllowed 测试路径存在但方法不匹配时返回 405 与 Allow 头。
// TestServerMethodNotAllowed tests 405 with an Allow header when the path exists for other methods.
func TestServerMethodNotAllowed(t *testing.T) {
	s := newRoutingServer(t,
		Raw(http.MethodGet, "/users/:id", textHandler("get")),
		Raw(http.MethodDelete, "/users/:id", textHandler("delete")),
		Raw(http.MethodPost, "/items", textHandler("post")),
	)

	tests := []struct {
		name   string
		method string
		target string
		status int
		allow  string
	}{
		{"get and delete allowed", http.MethodPut, "/users/1", http.StatusMethodNotAllowed, "DELETE, GET, HEAD, OPTIONS"},
		{"post only", http.MethodGet, "/items", http.StatusMethodNotAllowed, "POST, OPTIONS"},
		{"missing path is 404", http.MethodPut, "/nothing", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(tt.method, tt.target, nil))
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if got := w.Header().Get("Allow"); got != tt.allow {
				t.Errorf("Allow = %q, want %q", got, tt.allow)
			}
		})
	}
}

// TestServerHeadFallback 测试 HEAD 回退到 GET，且显式 HEAD 路由优先。
// TestServerHeadFallback tests HEAD falling back to GET with explicit HEAD routes taking precedence.
func TestServerHeadFallback(t *testing.T) {
	var called string
	mark := func(name string) RawHandlerFunc {
		return func(_ context.Context, _ *Request, resp *Response) error {
			called = name
			resp.WriteHeader(http.StatusNoContent)
			return nil
		}
	}
	s := newRoutingServer(t,
		Raw(http.MethodGet, "/a", mark("get-a")),
		Raw(http.MethodGet, "/b", mark("get-b")),
		Raw(http.MethodHead, "/b", mark("head-b")),
	)

	tests := []struct {
		target string
		want   string
	}{
		{"/a", "get-a"},
		{"/b", "head-b"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			called = ""
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(http.MethodHead, tt.target, nil))
			if called != tt.want {
				t.Errorf("called %q, want %q", called, tt.want)
			}
		})
	}
}

// TestServerRouteParamsAndTemplate 测试服务器将参数与路由模板写入请求。
// TestServerRouteParamsAndTemplate tests that the server populates params and the route template.
func TestServerRouteParamsAndTemplate(t *testing.T) {
	var gotParams Params
	var gotMatched string
	s := newRoutingServer(t, Raw(http.MethodGet, "/users/:id/files/*path",
		func(_ context.Context, req *Request, _ *Response) error {
			gotParams, gotMatched = req.Params, req.matched
			return nil
		}))

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/7/files/a/b.txt", nil))

	want := Params{{Key: "id", Value: "7"}, {Key: "path", Value: "/a/b.txt"}}
	if !reflect.DeepEqual(gotParams, want) {
		t.Errorf("params = %v, want %v", gotParams, want)
	}
	if gotMatched != "/users/:id/files/*path" {
		t.Errorf("matched = %q", gotMatched)
	}
}

// TestRouterAddRouteErrors 测试注册错误可通过导出错误判断。
// TestRouterAddRouteErrors tests registration errors are matchable via exported errors.
func TestRouterAddRouteErrors(t *testing.T) {
	h := func(context.Context, *Request, *Response) error { return nil }

	tests := []struct {
		name   string
		routes []Route
		want   error
	}{
		{"invalid path", []Route{{Method: http.MethodGet, Path: "x", compiledHandler: h}}, ErrInvalidRoutePath},
		{"duplicate", []Route{
			{Method: http.MethodGet, Path: "/a", compiledHandler: h},
			{Method: http.MethodGet, Path: "/a", compiledHandler: h},
		}, ErrRouteConflict},
		{"unnamed wildcard", []Route{{Method: http.MethodGet, Path: "/a/:", compiledHandler: h}}, ErrRouteConflict},
		{"nil handler", []Route{{Method: http.MethodGet, Path: "/a"}}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter()
			var err error
			for _, route := range tt.routes {
				if err = r.addRoute(route); err != nil {
					break
				}
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("error %v does not wrap %v", err, tt.want)
			}
		})
	}
}

// TestRouterFailedInsertLeavesNoTree 测试首次注册失败不会留下空方法树（避免误报 405）。
// TestRouterFailedInsertLeavesNoTree tests a failed first insert leaves no empty tree (no bogus 405).
func TestRouterFailedInsertLeavesNoTree(t *testing.T) {
	r := newRouter()
	h := func(context.Context, *Request, *Response) error { return nil }
	if err := r.addRoute(Route{Method: http.MethodPut, Path: "/a/:", compiledHandler: h}); err == nil {
		t.Fatal("expected error")
	}
	if r.treeFor(http.MethodPut) != nil {
		t.Error("tree recorded after failed insert")
	}
}

// TestCountParams 测试参数计数。
// TestCountParams tests parameter counting.
func TestCountParams(t *testing.T) {
	tests := []struct {
		path string
		want uint16
	}{
		{"/", 0},
		{"/users/:id", 1},
		{"/users/:id/files/*path", 2},
	}
	for _, tt := range tests {
		if got := countParams(tt.path); got != tt.want {
			t.Errorf("countParams(%q) = %d, want %d", tt.path, got, tt.want)
		}
	}
	if got := countParams(strings.Repeat(":", 70000)); got != ^uint16(0) {
		t.Errorf("countParams overflow = %d", got)
	}
}

// TestTSRPath 测试重定向目标计算及开放重定向防护。
// TestTSRPath tests redirect target computation and open-redirect protection.
func TestTSRPath(t *testing.T) {
	tests := []struct {
		path   string
		want   string
		wantOK bool
	}{
		{"/users/", "/users", true},
		{"/users", "/users/", true},
		{"/", "//", false},
		{"//evil.com/", "", false},
		{"//evil.com", "", false},
		{"/\\evil.com/", "", false},
	}
	for _, tt := range tests {
		got, ok := tsrPath(tt.path)
		if ok != tt.wantOK || (ok && got != tt.want) {
			t.Errorf("tsrPath(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.wantOK)
		}
	}
}

// TestTSRStatus 测试重定向状态码选择。
// TestTSRStatus tests redirect status code selection.
func TestTSRStatus(t *testing.T) {
	tests := map[string]int{
		http.MethodGet:    http.StatusMovedPermanently,
		http.MethodHead:   http.StatusMovedPermanently,
		http.MethodPost:   http.StatusPermanentRedirect,
		http.MethodPut:    http.StatusPermanentRedirect,
		http.MethodDelete: http.StatusPermanentRedirect,
	}
	for method, want := range tests {
		if got := tsrStatus(method); got != want {
			t.Errorf("tsrStatus(%s) = %d, want %d", method, got, want)
		}
	}
}

// BenchmarkServerServeHTTPParam 测量 Server 实际路由路径（含参数）的开销。
// BenchmarkServerServeHTTPParam measures the server's real routing path with params.
func BenchmarkServerServeHTTPParam(b *testing.B) {
	s := NewServer()
	if err := s.Register(Raw(http.MethodGet, "/users/:id", func(context.Context, *Request, *Response) error {
		return nil
	})); err != nil {
		b.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		s.ServeHTTP(w, req)
	}
}
