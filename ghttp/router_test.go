package ghttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRadixRouterStaticParamWildcard(t *testing.T) {
	r := NewRadixRouter()

	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	if err := r.Register("GET", "/users", dummy); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := r.Register("GET", "/users/:id", dummy); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := r.Register("GET", "/assets/*path", dummy); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := r.Register("GET", "/articles/:category/:id", dummy); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ts := httptest.NewServer(r)
	defer ts.Close()

	tests := []struct {
		path     string
		expected int
	}{
		{"/users", 200},
		{"/users/42", 200},
		{"/assets/img/logo.png", 200},
		{"/articles/tech/123", 200},
		{"/notfound", 404},
	}

	for _, tt := range tests {
		resp, err := http.Get(ts.URL + tt.path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", tt.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tt.expected {
			t.Errorf("GET %s: expected %d, got %d", tt.path, tt.expected, resp.StatusCode)
		}
	}
}

func TestRadixRouterPathParams(t *testing.T) {
	r := NewRadixRouter()

	var capturedParams map[string]string
	r.Register("GET", "/users/:id", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		capturedParams = Params(req)
		w.WriteHeader(http.StatusOK)
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()

	http.Get(ts.URL + "/users/42")
	if capturedParams == nil {
		t.Fatal("params should not be nil")
	}
	if capturedParams["id"] != "42" {
		t.Fatalf("expected id=42, got %s", capturedParams["id"])
	}
}

func TestRadixTreeRouteCount(t *testing.T) {
	tree := newCompressedRadixTree()
	if got := tree.routeCount(); got != 0 {
		t.Fatalf("expected 0 routes, got %d", got)
	}

	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	tree.insert(newRouteEntry("/static", dummy))

	if got := tree.routeCount(); got != 1 {
		t.Fatalf("after /static: expected 1 route, got %d", got)
	}

	tree.insert(newRouteEntry("/user/:id", dummy))

	if got := tree.routeCount(); got != 2 {
		t.Fatalf("after /user/:id: expected 2 routes, got %d", got)
	}

	// remove one and check again
	tree.remove("/static")
	if got := tree.routeCount(); got != 1 {
		t.Fatalf("after remove /static: expected 1 route, got %d", got)
	}
}

func TestStdRouterGo122(t *testing.T) {
	r := NewStdRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if err := r.Register("GET", "/users/{id}", dummy); err != nil {
		t.Fatalf("StdRouter Register failed: %v", err)
	}
}

func TestConvertPathParams(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/users/:id", "/users/{id}"},
		{"/assets/*path", "/assets/{path...}"},
		{"/articles/:category/:id", "/articles/{category}/{id}"},
		{"/static", "/static"},
	}
	for _, tt := range tests {
		got := convertPathParams(tt.input)
		if got != tt.expected {
			t.Errorf("convertPathParams(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
