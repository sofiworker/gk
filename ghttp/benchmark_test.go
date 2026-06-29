package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		Path struct {
			ID    int    `path:"id"`
			OrgID string `path:"orgId"`
		}
		Query struct {
			Page int    `query:"page"`
			Sort string `query:"sort"`
		}
		Header struct {
			Auth   string `header:"Authorization"`
			Accept string `header:"Accept"`
		}
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
			ctx := &responseContext{w: w, r: r, codecMgr: cm}
			DefaultEnvelope(ctx, 200, &payload{ID: 1, Name: "Alice"}, nil, cm)
		}
	})

	b.Run("error", func(b *testing.B) {
		cm := NewCodecManager()
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			ctx := &responseContext{w: w, r: r, codecMgr: cm}
			DefaultEnvelope(ctx, 400, nil, Err(400, "bad request"), cm)
		}
	})
}

func BenchmarkFullServer(b *testing.B) {
	s := New()

	type benchInput struct {
		Path struct {
			ID int `path:"id"`
		}
		Body struct {
			Name string `json:"name"`
		}
	}

	type benchOutput struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	if err := Route[benchInput, benchOutput](s).POST("/bench/{id}").To(func(ctx context.Context, req *benchInput) (*benchOutput, error) {
		return &benchOutput{ID: req.Path.ID, Name: req.Body.Name}, nil
	}); err != nil {
		b.Fatalf("route registration failed: %v", err)
	}

	if err := Route[struct{ Body struct{} }, struct{ Pong string }](s).GET("/bench/ping").To(func(ctx context.Context, req *struct{ Body struct{} }) (*struct{ Pong string }, error) {
		return &struct{ Pong string }{"ok"}, nil
	}); err != nil {
		b.Fatalf("route registration failed: %v", err)
	}

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
