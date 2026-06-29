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

	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "a_before")
			next.ServeHTTP(w, r)
			order = append(order, "a_after")
		})
	})

	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "b_before")
			next.ServeHTTP(w, r)
			order = append(order, "b_after")
		})
	})

	var handlerCalled bool
	Route[struct{ Body struct{} }, struct{ Body struct{} }](app, "/test").GET("").To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
		order = append(order, "handler")
		handlerCalled = true
		return &struct{ Body struct{} }{}, nil
	})

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

func TestBuiltinMiddlewareRequestID(t *testing.T) {
	app := New()
	app.Use(RequestID())

	Route[struct{ Body struct{} }, struct{ Body struct{} }](app, "/test").GET("").To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
		return &struct{ Body struct{} }{}, nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	app.ServeHTTP(w, r)

	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID header")
	}
}

func TestBuiltinMiddlewareRecovery(t *testing.T) {
	app := New()
	app.Use(Recoverer())

	Route[struct{ Body struct{} }, struct{ Body struct{} }](app, "/panic").GET("").To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Body struct{} }, error) {
		panic("test panic")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/panic", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after panic, got %d", w.Code)
	}
}
