package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===========================================================================
// Typed 入口基准：测量端到端绑定+编码开销，含 alloc 归因。
// Typed entry benchmarks: measures end-to-end binding + encoding cost, with
// allocation attribution.
// ===========================================================================

type benchParams struct {
	ID     int64  `path:"id"`
	Page   int    `query:"page"`
	Filter string `query:"filter"`
}

type benchLargeParams struct {
	ID     int64  `path:"id"`
	Page   int    `query:"page"`
	Size   int    `query:"size"`
	Sort   string `query:"sort"`
	Filter string `query:"filter"`
	Order  string `query:"order"`
	Trace  string `header:"X-Trace"`
	Auth   string `header:"Authorization"`
	Accept string `header:"Accept"`
}

type benchBody struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

type benchOut struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// BenchmarkTypedGetParamsSmall 测量 1 path + 2 query 的绑定+输出编码。
func BenchmarkTypedGetParamsSmall(b *testing.B) {
	m := New()
	_ = GetParams(m, "/users/{id}", JSON[benchOut](), func(ctx context.Context, p benchParams) (benchOut, error) {
		return benchOut{ID: p.ID, Name: p.Filter}, nil
	})
	w := newDiscardWriter()
	req := httptest.NewRequest(http.MethodGet, "/users/12345?page=2&filter=golang", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ServeHTTP(w, req)
	}
}

// BenchmarkTypedGetParamsLarge 测量 1 path + 5 query + 3 header 的绑定+输出编码。
func BenchmarkTypedGetParamsLarge(b *testing.B) {
	m := New()
	_ = GetParams(m, "/users/{id}", JSON[benchOut](), func(ctx context.Context, p benchLargeParams) (benchOut, error) {
		return benchOut{ID: p.ID, Name: p.Filter}, nil
	})
	w := newDiscardWriter()
	req := httptest.NewRequest(http.MethodGet, "/users/12345?page=2&size=50&sort=name&filter=go&order=desc", nil)
	req.Header.Set("X-Trace", "trace-abc-123")
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Accept", "application/json")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ServeHTTP(w, req)
	}
}

// BenchmarkTypedPostParamsBody 测量 1 path + body 解码 + 输出编码。
// 用预建 request + 每轮重置 body reader,剔除 httptest.NewRequest 的构造 alloc,
// 只测框架绑定/解码/编码开销。
func BenchmarkTypedPostParamsBody(b *testing.B) {
	m := New()
	_ = PostParamsBody(m, "/users/{id}", JSONBody(), JSON[benchOut](), func(ctx context.Context, p benchParams, body benchBody) (benchOut, error) {
		return benchOut{ID: p.ID, Name: body.Name}, nil
	})
	w := newDiscardWriter()
	payload := `{"name":"alice","email":"a@example.com","age":30}`
	req := httptest.NewRequest(http.MethodPost, "/users/7", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Body = io.NopCloser(strings.NewReader(payload))
		m.ServeHTTP(w, req)
	}
}

// BenchmarkTypedPostBody 测量仅 body 解码 + 输出编码（无 params）。
func BenchmarkTypedPostBody(b *testing.B) {
	m := New()
	_ = PostBody(m, "/register", JSONBody(), JSON[benchOut](), func(ctx context.Context, body benchBody) (benchOut, error) {
		return benchOut{Name: body.Name}, nil
	})
	w := newDiscardWriter()
	payload := `{"name":"alice","email":"a@example.com","age":30}`
	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Body = io.NopCloser(strings.NewReader(payload))
		m.ServeHTTP(w, req)
	}
}

// BenchmarkTypedGetNone 测量无 params 无 body 的纯输出编码。
func BenchmarkTypedGetNone(b *testing.B) {
	m := New()
	_ = GetNone(m, "/health", JSON[benchOut](), func(ctx context.Context) (benchOut, error) {
		return benchOut{ID: 1, Name: "ok"}, nil
	})
	w := newDiscardWriter()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ServeHTTP(w, req)
	}
}
