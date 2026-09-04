package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// 挂载 Gzip 中间件后,同一 URL 响应体随 AE 而变(压缩/未压缩两种),必须声明 Vary。
// 本测试固化该契约并验证大小写组合与多值场景下不产生重复头。
// After mounting the Gzip middleware, one URL's body varies by AE (compressed vs.
// uncompressed), so Vary must be declared. This test codifies that contract and
// verifies no duplicates across case-insensitive combinations and multi-value cases.
func TestGzip_VaryHeader(t *testing.T) {
	t.Run("compressed response includes Vary", func(t *testing.T) {
		s := New()
		s.Use(Gzip())
		if err := s.RawHandle(http.MethodGet, "/g", func(ctx context.Context, req *Request, resp *Response) error {
			resp.Header().Set("Content-Type", "application/json")
			resp.WriteHeader(http.StatusOK)
			_, _ = resp.Write([]byte(`{"a":"` + strings.Repeat("x", 2000) + `"}`))
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		r := httptest.NewRequest(http.MethodGet, "/g", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		vary := w.Header().Get("Vary")
		if !strings.Contains(vary, "Accept-Encoding") {
			t.Errorf("Vary missing Accept-Encoding; want it present on compressed path; got %q", vary)
		}
	})

	t.Run("non-gzip client still sees Vary", func(t *testing.T) {
		s := New()
		s.Use(Gzip())
		if err := s.RawHandle(http.MethodGet, "/g", func(ctx context.Context, req *Request, resp *Response) error {
			resp.Header().Set("Content-Type", "text/plain")
			resp.WriteHeader(http.StatusOK)
			_, _ = resp.Write([]byte(strings.Repeat("line\n", 100)))
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		r := httptest.NewRequest(http.MethodGet, "/g", nil) // 不接受 gzip
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		vary := w.Header().Get("Vary")
		if !strings.Contains(vary, "Accept-Encoding") {
			t.Errorf("Vary missing on non-gzip client; want it always set when middleware is mounted; got %q", vary)
		}
	})

	t.Run("case insensitivity does not produce duplicates", func(t *testing.T) {
		h := make(http.Header)
		h.Add("Vary", "accept-encoding") // lowercase
		ensureVary(h, "Accept-Encoding") // capitalized
		v := h.Get("Vary")
		// normalize by splitting comma and trimming spaces
		items := strings.Split(v, ",")
		n := 0
		for _, p := range items {
			if strings.EqualFold(strings.TrimSpace(p), "accept-encoding") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("case-insensitive duplication of Vary; found %d occurrences in %q", n, v)
		}
	})

	t.Run("existing multiple values handled without dup", func(t *testing.T) {
		h := make(http.Header)
		h.Add("Vary", "Accept-Language")
		h.Add("Vary", "Cache-Control")
		ensureVary(h, "Accept-Encoding")
		exists := false
		for _, v := range h.Values("Vary") {
			for _, item := range strings.Split(v, ",") {
				if strings.EqualFold(strings.TrimSpace(item), "Accept-Encoding") {
					exists = true
				}
			}
		}
		if !exists {
			t.Errorf("Vary Accept-Encoding not added to existing multi-value; got %v", h.Values("Vary"))
		}
		// 再次追加不产生重复。
		// A second call adds no duplicate.
		ensureVary(h, "Accept-Encoding")
		n := 0
		for _, v := range h.Values("Vary") {
			for _, item := range strings.Split(v, ",") {
				if strings.EqualFold(strings.TrimSpace(item), "Accept-Encoding") {
					n++
				}
			}
		}
		if n != 1 {
			t.Errorf("ensureVary not idempotent; found %d occurrences in %v", n, h.Values("Vary"))
		}
	})

	t.Run("gzip middleware plus static precompression yields one Vary", func(t *testing.T) {
		// Gzip 中间件与静态预压缩路径都会声明 Vary: Accept-Encoding;二者同时生效时
		// 必须合并为一个声明,而不是叠加出重复项。
		// Both the Gzip middleware and the static pre-compression path declare
		// Vary: Accept-Encoding; when both are active they must merge into one
		// declaration rather than stack duplicates.
		s := New()
		s.Use(Gzip())
		fsys := fstest.MapFS{
			"app.js":    {Data: []byte("console.log('plain');")},
			"app.js.br": {Data: []byte("BR-BYTES")},
		}
		if err := StaticFS(s, "/assets/", fsys, WithPrecompressed()); err != nil {
			t.Fatalf("StaticFS: %v", err)
		}

		r := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		r.Header.Set("Accept-Encoding", "br")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)

		n := 0
		for _, v := range w.Header().Values("Vary") {
			for _, item := range strings.Split(v, ",") {
				if strings.EqualFold(strings.TrimSpace(item), "Accept-Encoding") {
					n++
				}
			}
		}
		if n != 1 {
			t.Errorf("Vary Accept-Encoding declared %d times, want exactly 1; Vary=%v", n, w.Header().Values("Vary"))
		}
	})
}
