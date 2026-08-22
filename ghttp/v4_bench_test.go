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
// v4 typed 入口基准：测量甲 -2 入口的端到端绑定+编码开销，含 alloc 归因。
// v4 typed entry benchmarks: measures the v4-style entries' end-to-end binding +
// encoding cost, with allocation attribution.
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

// BenchmarkV4GetParamsSmall 测量 1 path + 2 query 的绑定+输出编码。
func BenchmarkV4GetParamsSmall(b *testing.B) {
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

// BenchmarkV4GetParamsLarge 测量 1 path + 5 query + 3 header 的绑定+输出编码。
func BenchmarkV4GetParamsLarge(b *testing.B) {
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

// BenchmarkV4PostParamsBody 测量 1 path + body 解码 + 输出编码。
// 用预建 request + 每轮重置 body reader,剔除 httptest.NewRequest 的构造 alloc,
// 只测框架绑定/解码/编码开销。
func BenchmarkV4PostParamsBody(b *testing.B) {
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

// BenchmarkV4PostBody 测量仅 body 解码 + 输出编码（无 params）。
func BenchmarkV4PostBody(b *testing.B) {
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

// BenchmarkV4GetNone 测量无 params 无 body 的纯输出编码。
func BenchmarkV4GetNone(b *testing.B) {
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
