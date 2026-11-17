package ghttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

func staticTestServer(t *testing.T, opts ...StaticOption) *Server {
	t.Helper()
	mod := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	fsys := fstest.MapFS{
		"a.txt":             {Data: []byte("hello world"), ModTime: mod},
		"app.js":            {Data: []byte("plain js"), ModTime: mod},
		"app.js.gz":         {Data: []byte("GZDATA"), ModTime: mod},
		"app.js.br":         {Data: []byte("BRDATA"), ModTime: mod},
		"sub/index.html":    {Data: []byte("<h1>sub</h1>"), ModTime: mod},
		"empty/x.txt":       {Data: []byte("x"), ModTime: mod},
		"index.html":        {Data: []byte("<h1>root</h1>"), ModTime: mod},
		"secret/../oops":    {Data: []byte("x")},
		"unknown.zzzunk":    {Data: []byte("data"), ModTime: mod},
		"unknown.zzzunk.gz": {Data: []byte("gz"), ModTime: mod},
	}
	s := NewServer()
	if err := s.Register(Static("/static", fsys, opts...)...); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return s
}

func staticDo(s *Server, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestStaticServeFile(t *testing.T) {
	s := staticTestServer(t, WithStaticCacheControl("max-age=60"))
	rec := staticDo(s, http.MethodGet, "/static/a.txt", nil)
	if rec.Code != 200 || rec.Body.String() != "hello world" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "max-age=60" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestStaticHead(t *testing.T) {
	s := staticTestServer(t)
	rec := staticDo(s, http.MethodHead, "/static/a.txt", nil)
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "11" {
		t.Fatalf("got %d len=%d cl=%q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Length"))
	}
}

func TestStaticRange(t *testing.T) {
	s := staticTestServer(t)
	rec := staticDo(s, http.MethodGet, "/static/a.txt", map[string]string{"Range": "bytes=0-4"})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "hello" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestStaticIfModifiedSince(t *testing.T) {
	s := staticTestServer(t)
	lm := staticDo(s, http.MethodGet, "/static/a.txt", nil).Header().Get("Last-Modified")
	if lm == "" {
		t.Fatal("missing Last-Modified")
	}
	rec := staticDo(s, http.MethodGet, "/static/a.txt", map[string]string{"If-Modified-Since": lm})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("got %d, want 304", rec.Code)
	}
}

func TestStaticDirectory(t *testing.T) {
	s := staticTestServer(t)
	if rec := staticDo(s, http.MethodGet, "/static/empty/", nil); rec.Code != 404 {
		t.Errorf("dir without index: %d", rec.Code)
	}
	if rec := staticDo(s, http.MethodGet, "/static/sub/", nil); rec.Code != 200 || rec.Body.String() != "<h1>sub</h1>" {
		t.Errorf("dir with index: %d %q", rec.Code, rec.Body.String())
	}
	if rec := staticDo(s, http.MethodGet, "/static/sub", nil); rec.Code != 200 {
		t.Errorf("dir without slash: %d", rec.Code)
	}
	if rec := staticDo(s, http.MethodGet, "/static/", nil); rec.Code != 200 || rec.Body.String() != "<h1>root</h1>" {
		t.Errorf("root: %d %q", rec.Code, rec.Body.String())
	}
	off := staticTestServer(t, WithStaticIndex(false))
	if rec := staticDo(off, http.MethodGet, "/static/sub/", nil); rec.Code != 404 {
		t.Errorf("index disabled: %d", rec.Code)
	}
}

func TestStaticNotFoundAndTraversal(t *testing.T) {
	s := staticTestServer(t)
	for _, target := range []string{
		"/static/missing.txt",
		"/static/../a.txt",
		"/static/%2e%2e/a.txt",
		"/static/sub/../a.txt",
		"/static/..%2fa.txt",
		"/static/%5ca.txt",
	} {
		rec := staticDo(s, http.MethodGet, target, nil)
		if rec.Code == 200 {
			t.Errorf("%s served 200: %q", target, rec.Body.String())
		}
	}
	if rec := staticDo(s, http.MethodGet, "/static/missing.txt", nil); rec.Code != 404 {
		t.Errorf("missing: %d", rec.Code)
	}
}

func TestStaticName(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", ".", true}, {"/", ".", true}, {"/a/b.txt", "a/b.txt", true},
		{"a/", "a", true}, {"/../x", "", false}, {"a/./b", "", false},
		{"a\\b", "", false}, {"a//b", "", false},
	}
	for _, c := range cases {
		got, ok := staticName(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("staticName(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestStaticPrecompressed(t *testing.T) {
	s := staticTestServer(t, WithStaticPrecompressed())

	rec := staticDo(s, http.MethodGet, "/static/app.js", map[string]string{"Accept-Encoding": "gzip, br"})
	if rec.Body.String() != "BRDATA" || rec.Header().Get("Content-Encoding") != "br" {
		t.Errorf("br: %q enc=%q", rec.Body.String(), rec.Header().Get("Content-Encoding"))
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" || ct[:4] != "text" && ct[:11] != "application" {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Vary = %q", rec.Header().Get("Vary"))
	}

	rec = staticDo(s, http.MethodGet, "/static/app.js", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Body.String() != "GZDATA" || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("gzip: %q", rec.Body.String())
	}

	rec = staticDo(s, http.MethodGet, "/static/app.js", map[string]string{"Accept-Encoding": "br;q=0, gzip;q=0"})
	if rec.Body.String() != "plain js" || rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("rejected: %q", rec.Body.String())
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Vary on plain = %q", rec.Header().Get("Vary"))
	}

	rec = staticDo(s, http.MethodGet, "/static/unknown.zzzunk", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Header().Get("Content-Type") != "application/octet-stream" || rec.Body.String() != "gz" {
		t.Errorf("unknown ext: ct=%q body=%q", rec.Header().Get("Content-Type"), rec.Body.String())
	}

	// 未启用预压缩时忽略 .gz。
	plain := staticTestServer(t)
	rec = staticDo(plain, http.MethodGet, "/static/app.js", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Body.String() != "plain js" {
		t.Errorf("disabled precompressed served %q", rec.Body.String())
	}
}

func TestStaticAccepts(t *testing.T) {
	cases := []struct {
		h, c string
		want bool
	}{
		{"gzip, br", "br", true}, {"gzip;q=0", "gzip", false}, {"", "gzip", false},
		{"GZIP", "gzip", true}, {"br;q=0.5", "br", true}, {"deflate", "gzip", false},
	}
	for _, c := range cases {
		if got := staticAccepts(c.h, c.c); got != c.want {
			t.Errorf("staticAccepts(%q,%q)=%v", c.h, c.c, got)
		}
	}
}
