package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 快路径只对“raw 终结器 + 无中间件 + 无错误模型 + 无 body + 非 HEAD”的路由
// 生效,跳过 requestState 注入;其余场景必须保持慢路径语义。
// the fast path applies only to raw terminals with no middleware, no error
// model, no body and non-HEAD; everything else keeps the slow-path semantics.

func TestFastRawRouteSkipsRequestState(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/ping").ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st := requestStateFromRequest(r); st != nil {
			t.Errorf("fast route should have no requestState, got %+v", st)
		}
		w.Header().Set("Content-Type", MIMEJSON)
		_, _ = io.WriteString(w, `{"message":"pong"}`)
	}))

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pong") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestRawRouteWithBodyKeepsRequestState(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).POST("/post").ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st := requestStateFromRequest(r); st == nil {
			t.Error("route with body should keep requestState")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/post", strings.NewReader("x"))
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRawRouteWithMiddlewareKeepsRequestState(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/mw").Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if st := requestStateFromRequest(r); st == nil {
				t.Error("middleware route should keep requestState")
			}
			next.ServeHTTP(w, r)
		})
	}).ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mw", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHeadRawRouteKeepsRequestStateAndSuppressesBody(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/ping").ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st := requestStateFromRequest(r); st == nil {
			t.Error("HEAD route should keep requestState for body suppression")
		}
		_, _ = io.WriteString(w, "should-not-appear")
	}))

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/ping", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body should be suppressed, got %q", rec.Body.String())
	}
}

func TestFastRoutePanicMatchesSlowPathErrorBody(t *testing.T) {
	run := func(withMiddleware bool) string {
		app := New(WithProduces(MIMEJSON))
		b := Route[struct{}, struct{}](app).GET("/boom")
		if withMiddleware {
			b.Use(func(next http.Handler) http.Handler { return next })
		}
		b.ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			panic("boom")
		}))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
		return rec.Body.String()
	}

	fast := run(false)
	slow := run(true)
	if fast != slow {
		t.Errorf("fast path panic body = %q, slow path = %q", fast, slow)
	}
	if !strings.Contains(fast, `"code":500`) {
		t.Errorf("panic body = %q", fast)
	}
}

func TestTypedRouteKeepsRequestState(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/typed").Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if st := requestStateFromRequest(r); st == nil {
				t.Error("typed route should keep requestState")
			}
			next.ServeHTTP(w, r)
		})
	}).To(func(_ context.Context, _ struct{}) (struct{}, error) {
		return struct{}{}, nil
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/typed", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTypedInputReuseDoesNotLeakAcrossRequests(t *testing.T) {
	type in struct {
		Q string `query:"q"`
	}
	app := New(WithProduces(MIMEJSON))
	Route[in, map[string]string](app).GET("/r").To(func(_ context.Context, req in) (map[string]string, error) {
		return map[string]string{"q": req.Q}, nil
	})

	get := func(query string) string {
		rec := httptest.NewRecorder()
		u := "/r"
		if query != "" {
			u += "?q=" + query
		}
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		return rec.Body.String()
	}

	if body := get("first"); !strings.Contains(body, "first") {
		t.Fatalf("first request body = %q", body)
	}
	if body := get("second"); !strings.Contains(body, "second") {
		t.Fatalf("second request body = %q", body)
	}
	if body := get(""); strings.Contains(body, "first") || strings.Contains(body, "second") {
		t.Fatalf("empty request leaked previous input: %q", body)
	}
}

func TestWriteOutcomeErrorWithoutRequestState(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	state := compileState(server, nil, nil, nil, false, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	state.writeOutcomeError(rec, req, http.StatusNotFound, Err(http.StatusNotFound, http.StatusText(http.StatusNotFound)))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), `"code":404`) {
		t.Fatalf("body = %q, want error envelope", rec.Body.String())
	}
}

func TestNotFoundHeadSuppressesBody(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD 404 body = %q, want empty", rec.Body.String())
	}
}
