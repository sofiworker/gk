package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 全局中间件改写 URL.Path 后,终端必须按新路径重新解析,否则 strip-prefix 网关、
// URL 重写中间件全部失效,且会产出"GET 收到 405 + Allow: GET"的自相矛盾响应。
// After global middleware rewrites URL.Path the terminal must re-resolve with the
// new path; otherwise strip-prefix gateways and URL-rewrite middleware all break,
// and self-contradictory responses like "GET gets 405 + Allow: GET" appear.
func TestMiddlewareURLRewrite(t *testing.T) {
	t.Run("strip prefix gateway", func(t *testing.T) {
		s := New()
		s.Use(func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				if p, ok := strings.CutPrefix(req.URL.Path, "/api/v1"); ok && p != "" {
					req.URL.Path = p
				}
				return next(ctx, req, resp)
			}
		})
		if err := s.RawHandle(http.MethodGet, "/users", func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			_, _ = resp.Write([]byte("users"))
			return nil
		}); err != nil {
			t.Fatalf("RawHandle: %v", err)
		}

		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
		if w.Code != http.StatusOK || w.Body.String() != "users" {
			t.Fatalf("rewritten path not re-resolved: status=%d body=%q", w.Code, w.Body.String())
		}

		// 未被重写的路径仍正常命中/未命中。
		// Un-rewritten paths still hit/miss normally.
		w2 := httptest.NewRecorder()
		s.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/users", nil))
		if w2.Code != http.StatusOK {
			t.Fatalf("direct path broken: status=%d", w2.Code)
		}
		w3 := httptest.NewRecorder()
		s.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/nope", nil))
		if w3.Code != http.StatusNotFound {
			t.Fatalf("miss path broken: status=%d", w3.Code)
		}
	})

	t.Run("rewrite updates params and matched route", func(t *testing.T) {
		s := New()
		s.Use(func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				if req.URL.Path == "/legacy/42" {
					req.URL.Path = "/users/42"
				}
				return next(ctx, req, resp)
			}
		})
		var gotID, gotRoute string
		if err := s.RawHandle(http.MethodGet, "/users/{id}", func(ctx context.Context, req *Request, resp *Response) error {
			gotID = req.Params.Get("id")
			gotRoute = req.MatchedRoute()
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatalf("RawHandle: %v", err)
		}

		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/legacy/42", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("rewrite to param route failed: status=%d", w.Code)
		}
		if gotID != "42" {
			t.Fatalf("params not re-bound after rewrite: id=%q", gotID)
		}
		if gotRoute != "/users/:id" {
			t.Fatalf("matched route not refreshed after rewrite: %q", gotRoute)
		}
	})

	t.Run("rewrite to missing path yields 404 not stale hit", func(t *testing.T) {
		s := New()
		s.Use(func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				req.URL.Path = "/gone"
				return next(ctx, req, resp)
			}
		})
		if err := s.RawHandle(http.MethodGet, "/present", func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatalf("RawHandle: %v", err)
		}

		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/present", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("stale hit executed after rewrite to missing path: status=%d", w.Code)
		}
	})

	t.Run("method override re-resolves", func(t *testing.T) {
		s := New()
		s.Use(func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				if ov := req.Header.Get("X-HTTP-Method-Override"); ov != "" {
					req.Method = ov
				}
				return next(ctx, req, resp)
			}
		})
		if err := s.RawHandle(http.MethodDelete, "/things/1", func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusNoContent)
			return nil
		}); err != nil {
			t.Fatalf("RawHandle: %v", err)
		}

		r := httptest.NewRequest(http.MethodPost, "/things/1", nil)
		r.Header.Set("X-HTTP-Method-Override", http.MethodDelete)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("method override not re-resolved: status=%d", w.Code)
		}
	})

	t.Run("no contradictory 405 with Allow of own method", func(t *testing.T) {
		s := New()
		s.Use(func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				req.URL.Path = "/new"
				return next(ctx, req, resp)
			}
		})
		if err := s.RawHandle(http.MethodGet, "/new", func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatalf("RawHandle: %v", err)
		}

		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/old", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("rewrite lost: status=%d Allow=%q", w.Code, w.Header().Get("Allow"))
		}
	})

	t.Run("metrics style middleware still sees pre-rewrite matched route", func(t *testing.T) {
		// 匹配先于链完成的初衷:按路由聚合的中间件在【进入下游前】就能读到模板。
		// 重写发生前读到的是原路径的匹配,属预期。
		// The point of resolving before the chain: route-aggregating middleware can
		// read the template BEFORE calling downstream. Reading the original path's
		// match before any rewrite happens is expected.
		s := New()
		var seenBefore string
		s.Use(func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				seenBefore = req.MatchedRoute()
				return next(ctx, req, resp)
			}
		})
		if err := s.RawHandle(http.MethodGet, "/direct", func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatalf("RawHandle: %v", err)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/direct", nil))
		if seenBefore != "/direct" {
			t.Fatalf("middleware lost pre-resolved MatchedRoute: %q", seenBefore)
		}
	})
}
