package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ===========================================================================
// MatchedRoute 的可见性与池化隔离。
//
// 两条不变量:
//  1. 全局中间件运行时就必须能读到路由模板——按路由聚合的中间件(metrics、按路由限流、
//     tracing span 命名)都在自身执行期读它,若留到终端才写入,它们只能读到空串。
//  2. 池化的 Request 绝不能把上一个请求的路由模板带给下一个请求,否则未命中的请求会被
//     记到别人的路由维度上。
//
// MatchedRoute visibility and pool isolation.
//
// Two invariants:
//  1. The route template must be readable while global middleware runs — metrics,
//     per-route rate limiting, and tracing span naming all read it during their own
//     execution, and would see "" if it were written only at the terminal.
//  2. A pooled Request must never hand the previous request's route template to the
//     next one, or a miss gets recorded under someone else's route dimension.
// ===========================================================================

func TestMatchedRoute_VisibleToGlobalMiddleware(t *testing.T) {
	s := New()
	var seenBefore, seenAfter string
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			// 进入 next 之前就要可读:限流/鉴权类中间件在放行前就需要路由维度。
			seenBefore = req.MatchedRoute()
			err := next(ctx, req, resp)
			seenAfter = req.MatchedRoute()
			return err
		}
	})
	if err := s.RawHandle(http.MethodGet, "/users/{id}", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	const want = "/users/:id"
	if seenBefore != want {
		t.Errorf("MatchedRoute before next = %q, want %q", seenBefore, want)
	}
	if seenAfter != want {
		t.Errorf("MatchedRoute after next = %q, want %q", seenAfter, want)
	}
}

func TestMatchedRoute_EmptyOnMissNotStale(t *testing.T) {
	s := New()
	var seen []string
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			err := next(ctx, req, resp)
			seen = append(seen, req.MatchedRoute())
			return err
		}
	})
	if err := s.RawHandle(http.MethodGet, "/hit", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 先命中再未命中:两个请求复用同一个池化 Request,第二次必须报告空串。
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hit", nil))
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))

	if len(seen) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(seen))
	}
	if seen[0] != "/hit" {
		t.Errorf("hit should report its template, got %q", seen[0])
	}
	if seen[1] != "" {
		t.Errorf("miss must report an empty route, got stale %q", seen[1])
	}
}

func TestMatchedRoute_NoCrossContaminationBetweenRoutes(t *testing.T) {
	s := New()
	var seen []string
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			seen = append(seen, req.MatchedRoute())
			return next(ctx, req, resp)
		}
	})
	for _, p := range []string{"/a", "/b/{id}", "/c"} {
		if err := s.RawHandle(http.MethodGet, p, func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	for _, target := range []string{"/a", "/b/7", "/c", "/a"} {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	}
	want := []string{"/a", "/b/:id", "/c", "/a"}
	if len(seen) != len(want) {
		t.Fatalf("seen=%q", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("request %d reported %q, want %q (full=%q)", i, seen[i], want[i], seen)
		}
	}
}

func TestMatchedRoute_RawPathStillMatches(t *testing.T) {
	// 无全局中间件时走 dispatchRaw 快路径,MatchedRoute 同样必须正确写入。
	s := New()
	var seen string
	if err := s.RawHandle(http.MethodGet, "/raw/{id}", func(ctx context.Context, req *Request, resp *Response) error {
		seen = req.MatchedRoute()
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/raw/9", nil))
	if seen != "/raw/:id" {
		t.Errorf("raw fast path MatchedRoute=%q", seen)
	}
}

func TestMatchedRoute_ParamsAvailableToGlobalMiddleware(t *testing.T) {
	// 匹配提前到链之前的副产物:路径参数同样在中间件里已就绪,便于按资源维度做鉴权。
	s := New()
	var seenID string
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			seenID = req.Params.Get("id")
			return next(ctx, req, resp)
		}
	})
	if err := s.RawHandle(http.MethodGet, "/items/{id}", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/abc", nil))
	if seenID != "abc" {
		t.Errorf("Params.Get(id) in middleware = %q, want %q", seenID, "abc")
	}
}

func TestMatchedRoute_InvalidPathStillReachesErrorChain(t *testing.T) {
	// 路径校验错误在 resolve 阶段产生,必须仍经中间件链传到统一错误出口(400)。
	s := New(WithStrictPath(true))
	var ran bool
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			ran = true
			return next(ctx, req, resp)
		}
	})
	if err := s.RawHandle(http.MethodGet, "/ok", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.URL.Path = "//ok" // 非规范路径:严格模式下应 400
	s.ServeHTTP(rec, r)
	if !ran {
		t.Error("middleware should still run for an invalid path")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code=%d want 400 body=%s", rec.Code, rec.Body.String())
	}
}

func TestMatchedRoute_MethodNotAllowedReportsNoRoute(t *testing.T) {
	s := New()
	var seen string
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			err := next(ctx, req, resp)
			seen = req.MatchedRoute()
			return err
		}
	})
	if err := s.RawHandle(http.MethodGet, "/only-get", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/only-get", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d want 405", rec.Code)
	}
	// 405 不算命中任何路由,不能借用同路径 GET 的模板。
	if seen != "" {
		t.Errorf("405 should report an empty route, got %q", seen)
	}
}
