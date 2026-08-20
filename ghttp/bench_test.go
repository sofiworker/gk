package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// discardWriter 是一个丢弃一切的 ResponseWriter,消除基准中的 I/O 噪声。
// discardWriter is a ResponseWriter that discards everything, removing I/O noise
// from benchmarks.
type discardWriter struct{ h http.Header }

func newDiscardWriter() *discardWriter { return &discardWriter{h: make(http.Header)} }

func (d *discardWriter) Header() http.Header         { return d.h }
func (d *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardWriter) WriteHeader(int)             {}

var benchNoop = func(context.Context, *Request, *Response) error { return nil }

// benchMux 构建一棵含常见形态路由的树。
// benchMux builds a tree with common route shapes.
func benchMux() *Mux {
	m := New()
	_ = m.RawHandle(http.MethodGet, "/ping", benchNoop)
	_ = m.RawHandle(http.MethodGet, "/users/{id}", benchNoop)
	_ = m.RawHandle(http.MethodGet, "/orgs/{o}/teams/{t}/members/{m}/roles/{r}/perms/{p}", benchNoop)
	_ = m.RawHandle(http.MethodGet, "/files/{path...}", benchNoop)
	return m
}

func benchServe(b *testing.B, method, path string) {
	m := benchMux()
	w := newDiscardWriter()
	req := httptest.NewRequest(method, path, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.ServeHTTP(w, req)
	}
}

func BenchmarkServeStatic(b *testing.B) { benchServe(b, http.MethodGet, "/ping") }
func BenchmarkServeParam1(b *testing.B) { benchServe(b, http.MethodGet, "/users/12345") }
func BenchmarkServeParam5(b *testing.B) {
	benchServe(b, http.MethodGet, "/orgs/o/teams/t/members/m/roles/r/perms/p")
}
func BenchmarkServeWildcard(b *testing.B) {
	benchServe(b, http.MethodGet, "/files/assets/css/site.css")
}
func BenchmarkServeMiss(b *testing.B) { benchServe(b, http.MethodGet, "/nope/nope") }

// BenchmarkMatchOnly 仅测树匹配(不含 ServeHTTP 的路径校验与池化),定位纯匹配成本。
// BenchmarkMatchOnly measures only tree matching (no ServeHTTP path validation or
// pooling), isolating the raw match cost.
func BenchmarkMatchOnly(b *testing.B) {
	m := benchMux()
	t := m.findTree(http.MethodGet)
	const path = "/orgs/o/teams/t/members/m/roles/r/perms/p"
	var p Params
	var skipped []skippedNode
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.reset()
		skipped = skipped[:0]
		t.root.getValue(path, &p, &skipped)
	}
}
