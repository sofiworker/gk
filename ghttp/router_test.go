package ghttp

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type discardResponseWriter struct{}

func (discardResponseWriter) Header() http.Header {
	return http.Header{}
}

func (discardResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (discardResponseWriter) WriteHeader(int) {}

func TestRadixRouterStaticParamWildcard(t *testing.T) {
	r := NewRadixRouter()

	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	if err := r.Register("GET", "/users", dummy); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := r.Register("GET", "/users/{id}", dummy); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := r.Register("GET", "/assets/*path", dummy); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := r.Register("GET", "/articles/{category}/{id}", dummy); err != nil {
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

	var capturedParams pathParamList
	r.Register("GET", "/users/{id}", pathParamHandlerFunc(func(w http.ResponseWriter, req *http.Request, params pathParamList) {
		capturedParams = params
		w.WriteHeader(http.StatusOK)
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()

	http.Get(ts.URL + "/users/42")
	if capturedParams.Len() == 0 {
		t.Fatal("params should not be empty")
	}
	if capturedParams.Get("id") != "42" {
		t.Fatalf("expected id=42, got %s", capturedParams.Get("id"))
	}
}

func TestRadixRouterRejectsDuplicateRoute(t *testing.T) {
	r := NewRadixRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	if err := r.Register(http.MethodGet, "/users/{id}", dummy); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := r.Register(http.MethodGet, "/users/{id}", dummy); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Register error = %v, want ErrConflict", err)
	}
}

func TestRadixRouterMethodNotAllowedIncludesAllow(t *testing.T) {
	r := NewRadixRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	if err := r.Register(http.MethodGet, "/users/{id}", dummy); err != nil {
		t.Fatalf("Register GET failed: %v", err)
	}
	if err := r.Register(http.MethodPut, "/users/{id}", dummy); err != nil {
		t.Fatalf("Register PUT failed: %v", err)
	}
	if err := r.Register(http.MethodPost, "/other", dummy); err != nil {
		t.Fatalf("Register POST failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/42", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	allow := rec.Header().Values("Allow")
	if len(allow) != 1 {
		t.Fatalf("Allow values = %#v, want one header", allow)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut} {
		if !strings.Contains(allow[0], method) {
			t.Fatalf("Allow = %q, want method %s", allow[0], method)
		}
	}
}

func TestRadixRouterHEADFallsBackToGETWithoutBody(t *testing.T) {
	r := NewRadixRouter()
	if err := r.Register(http.MethodGet, "/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Health", "ok")
		_, _ = w.Write([]byte("healthy"))
	})); err != nil {
		t.Fatalf("Register GET failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/health", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Health"); got != "ok" {
		t.Fatalf("X-Health = %q, want ok", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0; body = %q", rec.Body.Len(), rec.Body.String())
	}
}

func TestRadixRouterMatchesTrailingSlash(t *testing.T) {
	r := NewRadixRouter()
	if err := r.Register(http.MethodGet, "/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Register GET failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRadixRouterParamRouteDoesNotAllocateOnHotPath(t *testing.T) {
	r := NewRadixRouter()
	if err := r.Register("GET", "/users/{id}", pathParamHandlerFunc(func(w http.ResponseWriter, req *http.Request, params pathParamList) {
		if got := params.Get("id"); got != "42" {
			t.Fatalf("id param = %q, want 42", got)
		}
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := discardResponseWriter{}
	r.ServeHTTP(w, req)

	allocs := testing.AllocsPerRun(1000, func() {
		r.ServeHTTP(w, req)
	})
	if allocs != 0 {
		t.Fatalf("allocs per param route = %v, want 0", allocs)
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

func TestRadixTreeKeepsSmallStaticFanoutInline(t *testing.T) {
	tree := newCompressedRadixTree()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for _, path := range []string{"/alpha", "/beta", "/gamma"} {
		tree.insert(newRouteEntry(path, dummy))
	}

	if got := len(tree.root.staticChildren); got != 3 {
		t.Fatalf("static children = %d, want 3", got)
	}
	if tree.root.staticIndex != nil {
		t.Fatal("small static fanout should stay inline without a map index")
	}

	var params pathParamList
	if entry := tree.lookup("/beta", &params); entry == nil {
		t.Fatal("lookup /beta returned nil")
	}
}

func TestRadixTreeBuildsStaticIndexForLargeFanout(t *testing.T) {
	tree := newCompressedRadixTree()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for i := 0; i < radixStaticIndexThreshold; i++ {
		tree.insert(newRouteEntry("/route-"+strconv.Itoa(i), dummy))
	}

	if len(tree.root.staticChildren) != radixStaticIndexThreshold {
		t.Fatalf("static children = %d, want %d", len(tree.root.staticChildren), radixStaticIndexThreshold)
	}
	if tree.root.staticIndex == nil {
		t.Fatal("large static fanout should build a map index")
	}

	var params pathParamList
	if entry := tree.lookup("/route-6", &params); entry == nil {
		t.Fatal("lookup /route-6 returned nil")
	}
}

func TestRadixTreeRemoveStaticChildFromIndexedFanout(t *testing.T) {
	tree := newCompressedRadixTree()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for i := 0; i < radixStaticIndexThreshold; i++ {
		tree.insert(newRouteEntry("/route-"+strconv.Itoa(i), dummy))
	}

	tree.remove("/route-3")

	if got := len(tree.root.staticChildren); got != radixStaticIndexThreshold-1 {
		t.Fatalf("static children = %d, want %d", got, radixStaticIndexThreshold-1)
	}
	if tree.root.staticIndex != nil {
		t.Fatal("static index should be dropped after fanout falls below threshold")
	}

	var params pathParamList
	if entry := tree.lookup("/route-3", &params); entry != nil {
		t.Fatal("lookup /route-3 returned removed route")
	}
	if entry := tree.lookup("/route-4", &params); entry == nil {
		t.Fatal("lookup /route-4 returned nil")
	}
}

func TestRadixTreeLookupBacktracksFromStaticDeadEnd(t *testing.T) {
	tree := newCompressedRadixTree()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	add := func(path string) {
		tree.insert(newRouteEntry(normalizeRoutePath(path), dummy))
	}

	add("/users/{id}")
	add("/users/{id}/profile")
	add("/users/me")
	add("/users/me/settings")
	add("/files/*path")
	add("/files/public/info")

	var params pathParamList
	entry := tree.lookup("/users/me", &params)
	if entry == nil || entry.path != "/users/me" {
		t.Fatalf("lookup /users/me route = %v, want static /users/me", entry)
	}
	if params.Len() != 0 {
		t.Fatalf("static route params len = %d, want 0", params.Len())
	}

	params.Reset()
	entry = tree.lookup("/users/me/profile", &params)
	if entry == nil || entry.path != "/users/:id/profile" {
		t.Fatalf("lookup /users/me/profile route = %v, want /users/:id/profile", entry)
	}
	if got := params.Get("id"); got != "me" {
		t.Fatalf("id param = %q, want me", got)
	}

	params.Reset()
	entry = tree.lookup("/files/public/app.css", &params)
	if entry == nil || entry.path != "/files/*path" {
		t.Fatalf("lookup /files/public/app.css route = %v, want /files/*path", entry)
	}
	if got := params.Get("path"); got != "public/app.css" {
		t.Fatalf("path wildcard = %q, want public/app.css", got)
	}
}

func TestMethodMatcherStoresParamRoutesInSingleTree(t *testing.T) {
	m := newMethodMatcher()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	if err := m.add(normalizeRoutePath("/users/{id}"), dummy); err != nil {
		t.Fatalf("add short param route failed: %v", err)
	}
	if err := m.add(normalizeRoutePath("/api/{version}/users/{id}"), dummy); err != nil {
		t.Fatalf("add deep param route failed: %v", err)
	}

	if m.paramTree == nil {
		t.Fatal("param routes should share a single param tree")
	}
	if got := m.paramTree.routeCount(); got != 2 {
		t.Fatalf("param tree route count = %d, want 2", got)
	}

	var params pathParamList
	if entry := m.lookup("/users/42", &params); entry == nil || params.Get("id") != "42" {
		t.Fatalf("lookup /users/42 entry=%v id=%q, want match id=42", entry, params.Get("id"))
	}
	if entry := m.lookup("/api/v1/users/42", &params); entry == nil || params.Get("version") != "v1" || params.Get("id") != "42" {
		t.Fatalf("lookup /api/v1/users/42 entry=%v version=%q id=%q, want match", entry, params.Get("version"), params.Get("id"))
	}
	if entry := m.lookup("/users/42/profile", &params); entry != nil {
		t.Fatal("lookup /users/42/profile returned a route, want no exact match")
	}
}

func TestStdRouterGo122(t *testing.T) {
	r := NewStdRouter()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if err := r.Register("GET", "/users/{id}", dummy); err != nil {
		t.Fatalf("StdRouter Register failed: %v", err)
	}
}

func TestStdRouterMatchesTrailingSlash(t *testing.T) {
	r := NewStdRouter()
	if err := r.Register(http.MethodGet, "/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Register GET failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRouteParamColonSyntaxIsCompatibleWithWarning(t *testing.T) {
	var buf bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldWriter)

	r := NewRadixRouter()
	var capturedParams pathParamList
	if err := r.Register("GET", "/users/:id", pathParamHandlerFunc(func(w http.ResponseWriter, req *http.Request, params pathParamList) {
		capturedParams = params
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedParams.Get("id") != "42" {
		t.Fatalf("id param = %q, want 42", capturedParams.Get("id"))
	}
	if got := buf.String(); !strings.Contains(got, "deprecated :param syntax") || !strings.Contains(got, "use {param}") {
		t.Fatalf("warning log = %q, want deprecated :param warning", got)
	}
}

func TestConvertPathParams(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/users/:id", "/users/{id}"},
		{"/users/{id}", "/users/{id}"},
		{"/assets/*path", "/assets/{path...}"},
		{"/assets/{path...}", "/assets/{path...}"},
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
