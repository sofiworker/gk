package ghttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMatchitRouterStaticParamWildcard(t *testing.T) {
	r := NewMatchitRouter()
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

func TestMatchitRouterStaticBeatsParam(t *testing.T) {
	r := NewMatchitRouter()
	if err := r.Register(http.MethodGet, "/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("param"))
	})); err != nil {
		t.Fatalf("Register param failed: %v", err)
	}
	if err := r.Register(http.MethodGet, "/users/search", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("static"))
	})); err != nil {
		t.Fatalf("Register static failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/search", nil)
	r.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "static" {
		t.Fatalf("body = %q, want static", got)
	}
}

func TestMatchitRouterPathParams(t *testing.T) {
	r := NewMatchitRouter()
	var captured pathParamList
	if err := r.Register(http.MethodGet, "/orgs/{orgID}/users/{userID}", pathParamHandlerFunc(func(w http.ResponseWriter, req *http.Request, params pathParamList) {
		captured = params
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orgs/acme/users/alice", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Get("orgID") != "acme" || captured.Get("userID") != "alice" {
		t.Fatalf("params orgID=%q userID=%q, want acme/alice", captured.Get("orgID"), captured.Get("userID"))
	}
}

func TestMatchitRouterRejectsDuplicateRoute(t *testing.T) {
	r := NewMatchitRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if err := r.Register(http.MethodGet, "/users/{id}", dummy); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := r.Register(http.MethodGet, "/users/{id}", dummy); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Register error = %v, want ErrConflict", err)
	}
}
