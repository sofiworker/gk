package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareOrder(t *testing.T) {
	app := New()

	var order []string

	app.UseFunc(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "a_before")
			next(w, r)
			order = append(order, "a_after")
		}
	})

	app.UseFunc(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "b_before")
			next(w, r)
			order = append(order, "b_after")
		}
	})

	var handlerCalled bool
	if err := Route[struct{ Body struct{} }, struct{ Body struct{} }](app).GET("/test").To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
		order = append(order, "handler")
		handlerCalled = true
		return &struct{ Body struct{} }{}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	app.ServeHTTP(w, r)

	if !handlerCalled {
		t.Fatal("handler was not called")
	}
	if len(order) != 5 {
		t.Fatalf("expected 5 order entries, got %d: %v", len(order), order)
	}
}

func TestHandlerMiddlewareFuncServerGroupAndRoute(t *testing.T) {
	app := New()
	var calls []string

	app.UseFunc(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "server-before")
			next(w, r)
			calls = append(calls, "server-after")
		}
	})

	group := app.Group("/api").UseFunc(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "group-before")
			next(w, r)
			calls = append(calls, "group-after")
		}
	})

	if err := Route[struct{ Body struct{} }, struct{ Body struct{} }](group).
		GET("/test").
		UseFunc(func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, "route-before")
				next(w, r)
				calls = append(calls, "route-after")
			}
		}).
		To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
			calls = append(calls, "handler")
			return &struct{ Body struct{} }{}, nil
		}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	app.ServeHTTP(w, r)

	want := []string{
		"server-before",
		"group-before",
		"route-before",
		"handler",
		"route-after",
		"group-after",
		"server-after",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestBuiltinMiddlewareRequestID(t *testing.T) {
	app := New()
	app.Use(RequestID())

	if err := Route[struct{ Body struct{} }, struct{ Body struct{} }](app).GET("/test").To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
		return &struct{ Body struct{} }{}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	app.ServeHTTP(w, r)

	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header")
	}
}

func TestBuiltinMiddlewareCORSUsesConfiguredHeaders(t *testing.T) {
	app := New()
	app.Use(CORS(CORSConfig{
		AllowOrigins: []string{"https://example.test"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
		AllowHeaders: []string{"Content-Type", "X-Request-ID"},
		MaxAge:       60,
	}))

	if err := Route[struct{}, struct{}](app).GET("/cors").To(func(ctx context.Context, req *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cors", nil)
	r.Header.Set("Origin", "https://example.test")
	app.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.test" {
		t.Fatalf("allow origin = %q, want https://example.test", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Fatalf("allow methods = %q, want GET, POST", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Request-ID" {
		t.Fatalf("allow headers = %q, want Content-Type, X-Request-ID", got)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "60" {
		t.Fatalf("max age = %q, want 60", got)
	}
}

func TestBuiltinMiddlewareRecovery(t *testing.T) {
	app := New()
	app.Use(Recoverer())

	if err := Route[struct{ Body struct{} }, struct{ Body struct{} }](app).GET("/panic").To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
		panic("test panic")
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/panic", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after panic, got %d", w.Code)
	}
}
