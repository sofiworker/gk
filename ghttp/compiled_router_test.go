package ghttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompiledRouterStaticParamWildcard(t *testing.T) {
	r := NewCompiledRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, route := range []string{
		"/users",
		"/users/{id}",
		"/assets/*path",
		"/articles/{category}/{id}",
	} {
		if err := r.Register(http.MethodGet, route, dummy); err != nil {
			t.Fatalf("Register(%q) failed: %v", route, err)
		}
	}

	tests := []struct {
		path string
		want int
	}{
		{"/users", http.StatusOK},
		{"/users/42", http.StatusOK},
		{"/assets/img/logo.png", http.StatusOK},
		{"/articles/tech/123", http.StatusOK},
		{"/notfound", http.StatusNotFound},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Fatalf("GET %s status = %d, want %d", tt.path, rec.Code, tt.want)
		}
	}
}

func TestCompiledRouterPathParams(t *testing.T) {
	r := NewCompiledRouter()
	var captured pathParamList
	if err := r.Register(http.MethodGet, "/users/{id}", pathParamHandlerFunc(func(w http.ResponseWriter, req *http.Request, params pathParamList) {
		captured = params
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Get("id") != "42" {
		t.Fatalf("id = %q, want 42", captured.Get("id"))
	}
}

func TestCompiledRouterHEADFallsBackToGETWithoutBody(t *testing.T) {
	r := NewCompiledRouter()
	if err := r.Register(http.MethodGet, "/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Health", "ok")
		_, _ = w.Write([]byte("healthy"))
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/health", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Health") != "ok" {
		t.Fatalf("X-Health = %q, want ok", rec.Header().Get("X-Health"))
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0", rec.Body.Len())
	}
}

func TestCompiledRouterMethodNotAllowedIncludesAllow(t *testing.T) {
	r := NewCompiledRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if err := r.Register(http.MethodGet, "/users/{id}", dummy); err != nil {
		t.Fatalf("Register GET failed: %v", err)
	}
	if err := r.Register(http.MethodPut, "/users/{id}", dummy); err != nil {
		t.Fatalf("Register PUT failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/42", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	allow := rec.Header().Get("Allow")
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut} {
		if !strings.Contains(allow, method) {
			t.Fatalf("Allow = %q, want method %s", allow, method)
		}
	}
}
