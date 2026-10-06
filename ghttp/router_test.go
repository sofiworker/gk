package ghttp

import (
	"context"
	"net/http/httptest"
	"testing"
)

// TestRouterStaticLookupWithoutScratch guards the allocation-free static lookup,
// including misses and trailing-slash recommendations.
func TestRouterStaticLookupWithoutScratch(t *testing.T) {
	r := newRouter()
	h := func(context.Context, *Request, *Response) error { return nil }
	for _, path := range []string{"/", "/users", "/users/list", "/files/"} {
		if err := r.addRoute(Route{Method: "GET", Path: path, compiledHandler: h}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		path     string
		fullPath string
		tsr      bool
	}{
		{"/", "/", false},
		{"/users", "/users", false},
		{"/users/list", "/users/list", false},
		{"/users/", "", true},
		{"/files", "", true},
		{"/users/missing", "", false},
		{"/missing", "", false},
	} {
		t.Run(tt.path, func(t *testing.T) {
			got := r.lookup("GET", tt.path)
			if got.fullPath != tt.fullPath || got.tsr != tt.tsr || (got.handler != nil) != (tt.fullPath != "") || len(got.params) != 0 {
				t.Fatalf("lookup(%q) = %+v", tt.path, got)
			}
			if allocs := testing.AllocsPerRun(100, func() { got = r.lookup("GET", tt.path) }); allocs != 0 {
				t.Errorf("lookup allocated %g times, want 0", allocs)
			}
		})
	}
}

// TestRouter_StaticRoutes 测试静态路由匹配
// TestRouter_StaticRoutes tests static route matching
func TestRouter_StaticRoutes(t *testing.T) {
	r := newRouter()

	handler1 := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("handler1"))
		return nil
	}

	handler2 := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("handler2"))
		return nil
	}

	handler3 := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("handler3"))
		return nil
	}

	routes := []Route{
		{Method: "GET", Path: "/", compiledHandler: handler1},
		{Method: "GET", Path: "/users", compiledHandler: handler2},
		{Method: "GET", Path: "/users/list", compiledHandler: handler3},
	}

	for _, route := range routes {
		if err := r.addRoute(route); err != nil {
			t.Fatalf("failed to add route %s: %v", route.Path, err)
		}
	}

	tests := []struct {
		name        string
		method      string
		path        string
		shouldMatch bool
	}{
		{"root path", "GET", "/", true},
		{"exact match", "GET", "/users", true},
		{"nested path", "GET", "/users/list", true},
		{"no match", "GET", "/posts", false},
		{"method mismatch", "POST", "/users", false},
		{"trailing slash", "GET", "/users/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, _ := r.match(tt.method, tt.path)
			matched := handler != nil

			if matched != tt.shouldMatch {
				t.Errorf("expected match=%v, got match=%v", tt.shouldMatch, matched)
			}
		})
	}
}

// TestRouter_ParamRoutes 测试参数路由匹配
// TestRouter_ParamRoutes tests parameter route matching
func TestRouter_ParamRoutes(t *testing.T) {
	r := newRouter()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	routes := []Route{
		{Method: "GET", Path: "/users/:id", compiledHandler: handler},
		{Method: "GET", Path: "/users/:id/posts/:postId", compiledHandler: handler},
		{Method: "GET", Path: "/files/:name", compiledHandler: handler},
	}

	for _, route := range routes {
		if err := r.addRoute(route); err != nil {
			t.Fatalf("failed to add route %s: %v", route.Path, err)
		}
	}

	tests := []struct {
		name           string
		path           string
		shouldMatch    bool
		expectedParams map[string]string
	}{
		{
			name:        "single param",
			path:        "/users/123",
			shouldMatch: true,
			expectedParams: map[string]string{
				"id": "123",
			},
		},
		{
			name:        "multiple params",
			path:        "/users/456/posts/789",
			shouldMatch: true,
			expectedParams: map[string]string{
				"id":     "456",
				"postId": "789",
			},
		},
		{
			name:        "param with special chars",
			path:        "/files/test.txt",
			shouldMatch: true,
			expectedParams: map[string]string{
				"name": "test.txt",
			},
		},
		{
			name:        "no match - missing param",
			path:        "/users/",
			shouldMatch: false,
		},
		{
			name:        "no match - extra segment",
			path:        "/users/123/extra",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, params, _ := r.match("GET", tt.path)
			matched := handler != nil

			if matched != tt.shouldMatch {
				t.Errorf("expected match=%v, got match=%v", tt.shouldMatch, matched)
				return
			}

			if matched {
				for key, expectedValue := range tt.expectedParams {
					found := false
					for _, param := range params {
						if param.Key == key {
							found = true
							if param.Value != expectedValue {
								t.Errorf("param %s: expected %q, got %q", key, expectedValue, param.Value)
							}
							break
						}
					}
					if !found {
						t.Errorf("param %s not found", key)
					}
				}
			}
		})
	}
}

// TestRouter_CatchAllRoutes 测试通配路由匹配
// TestRouter_CatchAllRoutes tests catch-all route matching
func TestRouter_CatchAllRoutes(t *testing.T) {
	r := newRouter()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	routes := []Route{
		{Method: "GET", Path: "/static/*filepath", compiledHandler: handler},
		{Method: "GET", Path: "/files/*path", compiledHandler: handler},
	}

	for _, route := range routes {
		if err := r.addRoute(route); err != nil {
			t.Fatalf("failed to add route %s: %v", route.Path, err)
		}
	}

	tests := []struct {
		name          string
		path          string
		shouldMatch   bool
		paramName     string
		expectedValue string
	}{
		{
			name:          "catch single segment",
			path:          "/static/css",
			shouldMatch:   true,
			paramName:     "filepath",
			expectedValue: "/css",
		},
		{
			name:          "catch multiple segments",
			path:          "/static/css/main.css",
			shouldMatch:   true,
			paramName:     "filepath",
			expectedValue: "/css/main.css",
		},
		{
			name:          "catch deep path",
			path:          "/files/documents/2024/report.pdf",
			shouldMatch:   true,
			paramName:     "path",
			expectedValue: "/documents/2024/report.pdf",
		},
		{
			name:        "no match - prefix only",
			path:        "/static",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, params, _ := r.match("GET", tt.path)
			matched := handler != nil

			if matched != tt.shouldMatch {
				t.Errorf("expected match=%v, got match=%v", tt.shouldMatch, matched)
				return
			}

			if matched {
				if len(params) != 1 {
					t.Errorf("expected 1 param, got %d", len(params))
					return
				}

				if params[0].Key != tt.paramName {
					t.Errorf("expected param name %q, got %q", tt.paramName, params[0].Key)
				}

				if params[0].Value != tt.expectedValue {
					t.Errorf("expected param value %q, got %q", tt.expectedValue, params[0].Value)
				}
			}
		})
	}
}

// TestRouter_RoutePriority 测试路由优先级（静态 > 参数 > 通配）
// TestRouter_RoutePriority tests route priority (static > param > catch-all)
func TestRouter_RoutePriority(t *testing.T) {
	r := newRouter()

	staticHandler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("static"))
		return nil
	}

	paramHandler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("param"))
		return nil
	}

	catchAllHandler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("catchall"))
		return nil
	}

	routes := []Route{
		{Method: "GET", Path: "/users/:id", compiledHandler: paramHandler},
		{Method: "GET", Path: "/users/new", compiledHandler: staticHandler},
		{Method: "GET", Path: "/files/*path", compiledHandler: catchAllHandler},
	}

	for _, route := range routes {
		if err := r.addRoute(route); err != nil {
			t.Fatalf("failed to add route %s: %v", route.Path, err)
		}
	}

	tests := []struct {
		name         string
		path         string
		expectedType string
	}{
		{"static wins over param", "/users/new", "static"},
		{"param matches", "/users/123", "param"},
		{"catch-all matches", "/files/documents/file.txt", "catchall"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, _ := r.match("GET", tt.path)
			if handler == nil {
				t.Fatal("expected handler, got nil")
			}

			// 通过写入响应来判断调用了哪个 handler
			// Determine which handler was called by writing response
			rec := httptest.NewRecorder()
			if err := handler(context.Background(), &Request{}, &Response{Writer: rec}); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if got := rec.Body.String(); got != tt.expectedType {
				t.Errorf("expected %q handler, got %q", tt.expectedType, got)
			}
		})
	}
}

// TestRouter_RouteConflicts 测试路由冲突
// TestRouter_RouteConflicts tests route conflicts
func TestRouter_RouteConflicts(t *testing.T) {
	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	tests := []struct {
		name      string
		routes    []Route
		expectErr bool
	}{
		{
			name: "duplicate static route",
			routes: []Route{
				{Method: "GET", Path: "/users", compiledHandler: handler},
				{Method: "GET", Path: "/users", compiledHandler: handler},
			},
			expectErr: true,
		},
		{
			name: "duplicate param route",
			routes: []Route{
				{Method: "GET", Path: "/users/:id", compiledHandler: handler},
				{Method: "GET", Path: "/users/:userId", compiledHandler: handler},
			},
			expectErr: true,
		},
		{
			name: "different methods - no conflict",
			routes: []Route{
				{Method: "GET", Path: "/users", compiledHandler: handler},
				{Method: "POST", Path: "/users", compiledHandler: handler},
			},
			expectErr: false,
		},
		{
			name: "static sibling of catch-all (Gin semantics)",
			routes: []Route{
				{Method: "GET", Path: "/files/*path", compiledHandler: handler},
				{Method: "GET", Path: "/files/public", compiledHandler: handler},
			},
			expectErr: true,
		},
		{
			name: "catch-all position error",
			routes: []Route{
				{Method: "GET", Path: "/files/*path/extra", compiledHandler: handler},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter()
			var lastErr error

			for _, route := range tt.routes {
				err := r.addRoute(route)
				if err != nil {
					lastErr = err
				}
			}

			if tt.expectErr && lastErr == nil {
				t.Error("expected error, got nil")
			}

			if !tt.expectErr && lastErr != nil {
				t.Errorf("expected no error, got: %v", lastErr)
			}
		})
	}
}

// TestRouter_PathParameterExtraction 测试路径参数提取的准确性
// TestRouter_PathParameterExtraction tests path parameter extraction accuracy
func TestRouter_PathParameterExtraction(t *testing.T) {
	r := newRouter()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	route := Route{
		Method:          "GET",
		Path:            "/api/v1/users/:userId/posts/:postId/comments/:commentId",
		compiledHandler: handler,
	}

	if err := r.addRoute(route); err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	tests := []struct {
		name   string
		path   string
		params map[string]string
	}{
		{
			name: "all numeric params",
			path: "/api/v1/users/123/posts/456/comments/789",
			params: map[string]string{
				"userId":    "123",
				"postId":    "456",
				"commentId": "789",
			},
		},
		{
			name: "mixed params",
			path: "/api/v1/users/user-abc/posts/post-def/comments/comment-ghi",
			params: map[string]string{
				"userId":    "user-abc",
				"postId":    "post-def",
				"commentId": "comment-ghi",
			},
		},
		{
			name: "params with special chars",
			path: "/api/v1/users/user_123/posts/post-456/comments/comment.789",
			params: map[string]string{
				"userId":    "user_123",
				"postId":    "post-456",
				"commentId": "comment.789",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, params, _ := r.match("GET", tt.path)

			if len(params) != len(tt.params) {
				t.Fatalf("expected %d params, got %d", len(tt.params), len(params))
			}

			for key, expectedValue := range tt.params {
				found := false
				for _, param := range params {
					if param.Key == key {
						found = true
						if param.Value != expectedValue {
							t.Errorf("param %s: expected %q, got %q", key, expectedValue, param.Value)
						}
						break
					}
				}
				if !found {
					t.Errorf("param %s not found in result", key)
				}
			}
		})
	}
}

// TestRouter_EmptyPath 测试空路径处理
// TestRouter_EmptyPath tests empty path handling
func TestRouter_EmptyPath(t *testing.T) {
	r := newRouter()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	tests := []struct {
		name      string
		path      string
		expectErr bool
	}{
		{"empty path", "", true},
		{"no leading slash", "users", true},
		{"valid root", "/", false},
		{"valid path", "/users", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := Route{
				Method:          "GET",
				Path:            tt.path,
				compiledHandler: handler,
			}

			err := r.addRoute(route)
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

// TestRouter_MethodSeparation 测试不同 HTTP 方法的路由分离
// TestRouter_MethodSeparation tests route separation by HTTP method
func TestRouter_MethodSeparation(t *testing.T) {
	r := newRouter()

	getHandler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("GET"))
		return nil
	}

	postHandler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("POST"))
		return nil
	}

	putHandler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("PUT"))
		return nil
	}

	deleteHandler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("DELETE"))
		return nil
	}

	routes := []Route{
		{Method: "GET", Path: "/users", compiledHandler: getHandler},
		{Method: "POST", Path: "/users", compiledHandler: postHandler},
		{Method: "PUT", Path: "/users/:id", compiledHandler: putHandler},
		{Method: "DELETE", Path: "/users/:id", compiledHandler: deleteHandler},
	}

	for _, route := range routes {
		if err := r.addRoute(route); err != nil {
			t.Fatalf("failed to add route %s %s: %v", route.Method, route.Path, err)
		}
	}

	tests := []struct {
		method      string
		path        string
		shouldMatch bool
	}{
		{"GET", "/users", true},
		{"POST", "/users", true},
		{"PUT", "/users/123", true},
		{"DELETE", "/users/123", true},
		{"PATCH", "/users", false},
		{"GET", "/users/123", false}, // GET /users/:id not registered
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			handler, _, _ := r.match(tt.method, tt.path)
			matched := handler != nil

			if matched != tt.shouldMatch {
				t.Errorf("expected match=%v, got match=%v", tt.shouldMatch, matched)
			}
		})
	}
}

// TestRouter_ComplexPaths 测试复杂路径
// TestRouter_ComplexPaths tests complex paths
func TestRouter_ComplexPaths(t *testing.T) {
	r := newRouter()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	routes := []Route{
		{Method: "GET", Path: "/api/v1/users", compiledHandler: handler},
		{Method: "GET", Path: "/api/v2/users", compiledHandler: handler},
		{Method: "GET", Path: "/api/v1/users/:id/profile", compiledHandler: handler},
		{Method: "GET", Path: "/api/v1/organizations/:orgId/users/:userId", compiledHandler: handler},
	}

	for _, route := range routes {
		if err := r.addRoute(route); err != nil {
			t.Fatalf("failed to add route %s: %v", route.Path, err)
		}
	}

	tests := []struct {
		name        string
		path        string
		shouldMatch bool
		params      map[string]string
	}{
		{
			name:        "version 1",
			path:        "/api/v1/users",
			shouldMatch: true,
		},
		{
			name:        "version 2",
			path:        "/api/v2/users",
			shouldMatch: true,
		},
		{
			name:        "nested with param",
			path:        "/api/v1/users/123/profile",
			shouldMatch: true,
			params:      map[string]string{"id": "123"},
		},
		{
			name:        "multiple params",
			path:        "/api/v1/organizations/org-456/users/user-789",
			shouldMatch: true,
			params: map[string]string{
				"orgId":  "org-456",
				"userId": "user-789",
			},
		},
		{
			name:        "wrong version",
			path:        "/api/v3/users",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, params, _ := r.match("GET", tt.path)
			matched := handler != nil

			if matched != tt.shouldMatch {
				t.Errorf("expected match=%v, got match=%v", tt.shouldMatch, matched)
				return
			}

			if matched && tt.params != nil {
				for key, expectedValue := range tt.params {
					found := false
					for _, param := range params {
						if param.Key == key {
							found = true
							if param.Value != expectedValue {
								t.Errorf("param %s: expected %q, got %q", key, expectedValue, param.Value)
							}
							break
						}
					}
					if !found {
						t.Errorf("param %s not found", key)
					}
				}
			}
		})
	}
}
