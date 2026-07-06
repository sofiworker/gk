package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

var (
	benchmarkRouteEntrySink *routeEntry
	benchmarkParamLenSink   int
)

// BenchmarkRadixRouter benchmarks route lookup performance.
func BenchmarkRadixRouter(b *testing.B) {
	router := NewRadixRouter()

	// Register routes matching common patterns
	routes := []struct{ method, path string }{
		{"GET", "/"},
		{"GET", "/users"},
		{"GET", "/users/{id}"},
		{"POST", "/users"},
		{"PUT", "/users/{id}"},
		{"DELETE", "/users/{id}"},
		{"GET", "/users/{id}/posts"},
		{"GET", "/users/{id}/posts/{postId}"},
		{"POST", "/users/{id}/posts"},
		{"GET", "/api/v1/products"},
		{"GET", "/api/v1/products/{id}"},
		{"GET", "/api/v1/products/{id}/reviews"},
		{"POST", "/api/v1/products"},
		{"GET", "/api/v1/orders"},
		{"GET", "/api/v1/orders/{id}"},
		{"GET", "/health"},
		{"GET", "/metrics"},
		{"GET", "/static/*"},
	}

	for _, r := range routes {
		router.Register(r.method, r.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	}

	benchPaths := []struct{ name, method, path string }{
		{"root", "GET", "/"},
		{"static", "GET", "/users"},
		{"param", "GET", "/users/42"},
		{"deep", "GET", "/users/42/posts/99"},
		{"api", "GET", "/api/v1/products/123"},
		{"deep-api", "GET", "/api/v1/products/456/reviews"},
		{"wildcard", "GET", "/static/css/main.css"},
	}

	b.ResetTimer()
	for _, bp := range benchPaths {
		b.Run(bp.name, func(b *testing.B) {
			req, _ := http.NewRequest(bp.method, bp.path, nil)
			for i := 0; i < b.N; i++ {
				router.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}

func BenchmarkRadixRouterLookup(b *testing.B) {
	for _, routeCount := range []int{16, 128, 1024, 8192} {
		router := newBenchmarkRadixRouter(routeCount)
		benchPaths := benchmarkLookupPaths(routeCount)

		for _, bp := range benchPaths {
			b.Run(fmt.Sprintf("routes=%d/%s", routeCount, bp.name), func(b *testing.B) {
				var params pathParamList
				entry := router.lookup(http.MethodGet, bp.path, &params)
				if bp.wantMatch && entry == nil {
					b.Fatalf("lookup(%q) returned nil", bp.path)
				}
				if !bp.wantMatch && entry != nil {
					b.Fatalf("lookup(%q) returned a route", bp.path)
				}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					entry = router.lookup(http.MethodGet, bp.path, &params)
					benchmarkRouteEntrySink = entry
					benchmarkParamLenSink += params.Len()
				}
			})
		}
	}
}

func BenchmarkRadixRouterServeHTTPNoopWriter(b *testing.B) {
	for _, routeCount := range []int{16, 128, 1024, 8192} {
		router := newBenchmarkRadixRouter(routeCount)
		benchPaths := benchmarkLookupPaths(routeCount)
		writer := discardResponseWriter{}

		for _, bp := range benchPaths {
			if !bp.wantMatch {
				continue
			}
			b.Run(fmt.Sprintf("routes=%d/%s", routeCount, bp.name), func(b *testing.B) {
				req := httptest.NewRequest(http.MethodGet, bp.path, nil)

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					router.ServeHTTP(writer, req)
				}
			})
		}
	}
}

func BenchmarkRadixRouterParallel(b *testing.B) {
	router := NewRadixRouter()

	routes := []struct{ method, path string }{
		{"GET", "/"},
		{"GET", "/users"},
		{"GET", "/users/{id}"},
		{"GET", "/users/{id}/posts"},
		{"GET", "/api/v1/products/{id}"},
		{"GET", "/health"},
	}

	for _, r := range routes {
		router.Register(r.method, r.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	}

	paths := []string{"/", "/users", "/users/42", "/users/42/posts", "/api/v1/products/123", "/health"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := paths[i%len(paths)]
			req, _ := http.NewRequest("GET", path, nil)
			router.ServeHTTP(httptest.NewRecorder(), req)
			i++
		}
	})
}

type benchmarkLookupPath struct {
	name      string
	path      string
	wantMatch bool
}

func newBenchmarkRadixRouter(routeCount int) *RadixRouter {
	router := NewRadixRouter()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for i := 0; i < routeCount; i++ {
		path := benchmarkRoutePattern(i)
		if err := router.Register(http.MethodGet, path, handler); err != nil {
			panic(err)
		}
	}

	return router
}

func benchmarkLookupPaths(routeCount int) []benchmarkLookupPath {
	return []benchmarkLookupPath{
		{name: "static", path: benchmarkRouteRequestPath(routeCount, 0), wantMatch: true},
		{name: "param", path: benchmarkRouteRequestPath(routeCount, 1), wantMatch: true},
		{name: "deep-param", path: benchmarkRouteRequestPath(routeCount, 2), wantMatch: true},
		{name: "wildcard", path: benchmarkRouteRequestPath(routeCount, 3), wantMatch: true},
		{name: "miss", path: "/missing/not-found", wantMatch: false},
	}
}

func benchmarkRoutePattern(index int) string {
	switch index % 4 {
	case 0:
		return fmt.Sprintf("/static/%04d", index)
	case 1:
		return fmt.Sprintf("/users/%04d/{id}", index)
	case 2:
		return fmt.Sprintf("/api/%04d/orgs/{orgID}/users/{userID}/posts/{postID}", index)
	default:
		return fmt.Sprintf("/assets/%04d/*path", index)
	}
}

func benchmarkRouteRequestPath(routeCount, kind int) string {
	index := benchmarkRouteIndex(routeCount, kind)
	switch kind {
	case 0:
		return fmt.Sprintf("/static/%04d", index)
	case 1:
		return fmt.Sprintf("/users/%04d/42", index)
	case 2:
		return fmt.Sprintf("/api/%04d/orgs/acme/users/alice/posts/99", index)
	default:
		return fmt.Sprintf("/assets/%04d/css/app.css", index)
	}
}

func benchmarkRouteIndex(routeCount, kind int) int {
	if routeCount <= 0 {
		return kind
	}

	index := routeCount / 2
	for index < routeCount && index%4 != kind {
		index++
	}
	if index < routeCount {
		return index
	}

	index = routeCount/2 - 1
	for index >= 0 && index%4 != kind {
		index--
	}
	if index >= 0 {
		return index
	}

	return kind
}

func BenchmarkStdRouter(b *testing.B) {
	router := NewStdRouter()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	routes := []struct{ method, path string }{
		{"GET", "/"},
		{"GET", "/users"},
		{"GET", "/users/{id}"},
		{"GET", "/users/{id}/posts"},
		{"GET", "/users/{id}/posts/{postId}"},
		{"GET", "/api/v1/products"},
		{"GET", "/api/v1/products/{id}"},
		{"GET", "/health"},
	}

	for _, r := range routes {
		router.Register(r.method, r.path, handler)
	}

	benchPaths := []struct{ name, path string }{
		{"root", "/"},
		{"static", "/users"},
		{"param", "/users/42"},
		{"deep", "/users/42/posts/99"},
		{"api", "/api/v1/products/123"},
	}

	b.ResetTimer()
	for _, bp := range benchPaths {
		b.Run(bp.name, func(b *testing.B) {
			req, _ := http.NewRequest("GET", bp.path, nil)
			for i := 0; i < b.N; i++ {
				router.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}

func BenchmarkJSONCodec(b *testing.B) {
	codec := &JSONCodec{}

	type testStruct struct {
		Name   string   `json:"name"`
		Age    int      `json:"age"`
		Email  string   `json:"email"`
		Active bool     `json:"active"`
		Score  float64  `json:"score"`
		Tags   []string `json:"tags"`
	}

	obj := &testStruct{
		Name:   "Alice",
		Age:    30,
		Email:  "alice@example.com",
		Active: true,
		Score:  99.5,
		Tags:   []string{"go", "http", "framework"},
	}

	b.Run("marshal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			codec.Marshal(w, obj)
		}
	})

	b.Run("unmarshal", func(b *testing.B) {
		data := strings.NewReader(`{"name":"Alice","age":30,"email":"alice@example.com","active":true,"score":99.5,"tags":["go","http","framework"]}`)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var result testStruct
			data.Seek(0, 0)
			codec.Unmarshal(data, &result)
		}
	})
}

func BenchmarkParseInput(b *testing.B) {
	type input struct {
		Params `json:"-"`

		Body struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Age   int    `json:"age"`
		}
	}

	b.Run("json-body", func(b *testing.B) {
		body := `{"name":"Alice","email":"alice@example.com","age":30}`
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("POST", "/users/42?page=1&sort=asc", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/json")

			ctx := context.WithValue(req.Context(), pathParamsKey, map[string]string{
				"id":    "42",
				"orgId": "org-1",
			})
			req = req.WithContext(ctx)

			var in input
			_ = ParseInput(req, &in)
		}
	})

	b.Run("empty-body", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/users/42", nil)
			ctx := context.WithValue(req.Context(), pathParamsKey, map[string]string{"id": "42"})
			req = req.WithContext(ctx)

			var in input
			_ = ParseInput(req, &in)
		}
	})
}

func BenchmarkEnvelope(b *testing.B) {
	type payload struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	b.Run("success", func(b *testing.B) {
		cm := NewCodecManager()
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			DefaultEnvelope(w, r, 200, &payload{ID: 1, Name: "Alice"}, nil, cm)
		}
	})

	b.Run("error", func(b *testing.B) {
		cm := NewCodecManager()
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			DefaultEnvelope(w, r, 400, nil, Err(400, "bad request"), cm)
		}
	})
}

func BenchmarkFullServer(b *testing.B) {
	s := New(WithProduces(MIMEJSON))

	type benchInput struct {
		Params `json:"-"`

		Body struct {
			Name string `json:"name"`
		}
	}

	type benchOutput struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	Route[benchInput, benchOutput](s).POST("/bench/{id}").To(func(ctx context.Context, req benchInput) (benchOutput, error) {
		id, _ := strconv.Atoi(req.Path("id"))
		return benchOutput{ID: id, Name: req.Body.Name}, nil
	})

	Route[struct{ Body struct{} }, struct{ Pong string }](s).GET("/bench/ping").To(func(ctx context.Context, req struct{ Body struct{} }) (struct{ Pong string }, error) {
		return struct{ Pong string }{"ok"}, nil
	})

	ts := httptest.NewServer(s)
	defer ts.Close()

	b.Run("ping", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, _ := http.Get(ts.URL + "/bench/ping")
			resp.Body.Close()
		}
	})

	b.Run("post-with-body", func(b *testing.B) {
		body := `{"name":"Bench"}`
		for i := 0; i < b.N; i++ {
			resp, _ := http.Post(ts.URL+"/bench/42", "application/json", strings.NewReader(body))
			resp.Body.Close()
		}
	})

	b.Run("parallel-ping", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := http.Get(ts.URL + "/bench/ping")
				if err == nil {
					resp.Body.Close()
				}
			}
		})
	})
}
