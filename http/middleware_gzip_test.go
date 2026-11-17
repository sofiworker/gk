package ghttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func gzDo(t testing.TB, mw Middleware, method string, hdr map[string]string, h Handler) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/x", nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	if err := mw(h)(r.Context(), &Request{Raw: r}, &Response{Writer: rec}); err != nil {
		t.Errorf("err: %v", err)
	}
	return rec
}

func gzBody(t testing.TB, rec *httptest.ResponseRecorder) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Errorf("not gzip: %v", err)
		return ""
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Errorf("read: %v", err)
	}
	return string(b)
}

var gzAccept = map[string]string{"Accept-Encoding": "gzip"}

func gzText(body, ct string) Handler {
	return func(_ context.Context, _ *Request, resp *Response) error {
		if ct != "" {
			resp.Header().Set("Content-Type", ct)
		}
		_, err := resp.Write([]byte(body))
		return err
	}
}

func TestGzipLargeAndSmall(t *testing.T) {
	big := strings.Repeat("hello gzip ", 500)
	rec := gzDo(t, Gzip(), "GET", gzAccept, gzText(big, "text/plain"))
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Content-Length") != "" {
		t.Fatalf("headers: %v", rec.Header())
	}
	if gzBody(t, rec) != big {
		t.Fatal("content mismatch")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("missing Vary")
	}

	rec = gzDo(t, Gzip(), "GET", gzAccept, gzText("tiny", "text/plain"))
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "tiny" {
		t.Fatalf("small should be plain: %v %q", rec.Header(), rec.Body.String())
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Error("Vary missing on small")
	}
}

func TestGzipMultiWriteCrossThreshold(t *testing.T) {
	h := func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "application/json")
		for i := 0; i < 20; i++ {
			_, _ = resp.Write([]byte(strings.Repeat("a", 100)))
		}
		return nil
	}
	rec := gzDo(t, Gzip(), "GET", gzAccept, h)
	if got := gzBody(t, rec); len(got) != 2000 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestGzipSniffedType(t *testing.T) {
	big := strings.Repeat("plain text ", 300)
	rec := gzDo(t, Gzip(), "GET", gzAccept, gzText(big, ""))
	if rec.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("headers: %v", rec.Header())
	}
	rec = gzDo(t, Gzip(), "GET", gzAccept, gzText(strings.Repeat("\x89PNG\r\n\x1a\n", 300), ""))
	if rec.Header().Get("Content-Encoding") != "" {
		t.Error("binary sniffed content must not be compressed")
	}
}

func TestGzipSkips(t *testing.T) {
	big := strings.Repeat("x", 4096)
	cases := []struct {
		name   string
		method string
		hdr    map[string]string
		h      Handler
		enc    string
	}{
		{"no accept", "GET", nil, gzText(big, "text/plain"), ""},
		{"q=0", "GET", map[string]string{"Accept-Encoding": "gzip;q=0, deflate"}, gzText(big, "text/plain"), ""},
		{"other only", "GET", map[string]string{"Accept-Encoding": "br"}, gzText(big, "text/plain"), ""},
		{"HEAD", "HEAD", gzAccept, gzText(big, "text/plain"), ""},
		{"Range", "GET", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-9"}, gzText(big, "text/plain"), ""},
		{"image type", "GET", gzAccept, gzText(big, "image/png"), ""},
		{"already encoded", "GET", gzAccept, func(_ context.Context, _ *Request, resp *Response) error {
			resp.Header().Set("Content-Type", "text/plain")
			resp.Header().Set("Content-Encoding", "br")
			_, err := resp.Write([]byte(big))
			return err
		}, "br"},
		{"content-range", "GET", gzAccept, func(_ context.Context, _ *Request, resp *Response) error {
			resp.Header().Set("Content-Type", "text/plain")
			resp.Header().Set("Content-Range", "bytes 0-4095/9999")
			resp.WriteHeader(206)
			_, err := resp.Write([]byte(big))
			return err
		}, ""},
		{"204", "GET", gzAccept, func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(204)
			return nil
		}, ""},
		{"304", "GET", gzAccept, func(_ context.Context, _ *Request, resp *Response) error {
			resp.Header().Set("Content-Type", "text/plain")
			resp.WriteHeader(304)
			return nil
		}, ""},
		{"small content-length", "GET", gzAccept, func(_ context.Context, _ *Request, resp *Response) error {
			resp.Header().Set("Content-Type", "text/plain")
			resp.Header().Set("Content-Length", "10")
			_, err := resp.Write([]byte("0123456789"))
			return err
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := gzDo(t, Gzip(), c.method, c.hdr, c.h)
			if got := rec.Header().Get("Content-Encoding"); got != c.enc {
				t.Fatalf("Content-Encoding = %q, want %q", got, c.enc)
			}
			if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
				t.Error("Vary missing")
			}
		})
	}
}

func TestGzipAcceptParsing(t *testing.T) {
	cases := map[string]bool{
		"gzip":                  true,
		"deflate, gzip;q=0.5":   true,
		"GZIP":                  true,
		"gzip;q=0":              false,
		"*":                     true,
		"*;q=0":                 false,
		"gzip;q=0, *":           false,
		"identity":              false,
		"":                      false,
		"gzip;q=0.0, deflate":   false,
		"x-gzip":                true,
		"br;q=1.0, gzip;q=0.1 ": true,
	}
	for in, want := range cases {
		if got := gzAcceptsGzip([]string{in}); got != want {
			t.Errorf("%q = %v, want %v", in, got, want)
		}
	}
}

func TestGzipContentLengthAndETag(t *testing.T) {
	big := strings.Repeat("e", 3000)
	h := func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain")
		resp.Header().Set("Content-Length", "3000")
		resp.Header().Set("ETag", `"abc"`)
		_, err := resp.Write([]byte(big))
		return err
	}
	rec := gzDo(t, Gzip(), "GET", gzAccept, h)
	if rec.Header().Get("Content-Length") != "" || rec.Header().Get("Etag") != `W/"abc"` || gzBody(t, rec) != big {
		t.Fatalf("headers: %v", rec.Header())
	}
}

func TestGzipOptions(t *testing.T) {
	mw := Gzip(WithGzipMinSize(0), WithGzipLevel(gzip.BestSpeed), WithGzipContentTypes("image/*"))
	rec := gzDo(t, mw, "GET", gzAccept, gzText("abc", "image/png"))
	if gzBody(t, rec) != "abc" {
		t.Fatal("custom type not compressed")
	}
	rec = gzDo(t, mw, "GET", gzAccept, gzText("abc", "text/plain"))
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("text should be excluded by custom types")
	}
	rec = gzDo(t, Gzip(WithGzipLevel(99), WithGzipMinSize(1)), "GET", gzAccept, gzText("abc", "text/plain"))
	if gzBody(t, rec) != "abc" {
		t.Fatal("invalid level should fall back to default")
	}
}

func TestGzipFlushStreaming(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	resp := &Response{Writer: rec}
	var flushedLen int
	h := func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/event-stream")
		_, _ = resp.Write([]byte("data: 1\n\n"))
		if err := http.NewResponseController(resp).Flush(); err != nil {
			t.Errorf("flush: %v", err)
		}
		flushedLen = rec.Body.Len()
		_, _ = resp.Write([]byte("data: 2\n\n"))
		return nil
	}
	if err := Gzip()(h)(r.Context(), &Request{Raw: r}, resp); err != nil {
		t.Fatal(err)
	}
	if flushedLen == 0 {
		t.Error("flush did not push data")
	}
	if !rec.Flushed {
		t.Error("underlying not flushed")
	}
	if got := gzBody(t, rec); got != "data: 1\n\ndata: 2\n\n" {
		t.Fatalf("body %q", got)
	}
	if _, ok := resp.Writer.(*gzWriter); ok {
		t.Error("writer not restored")
	}
}

func TestGzipFileReplyRange(t *testing.T) {
	content := strings.Repeat("0123456789", 500)
	serve := func(_ context.Context, req *Request, resp *Response) error {
		f := &FileReply{Reader: strings.NewReader(content), Name: "a.txt", ContentType: "text/plain"}
		return f.ServeFile(resp, req)
	}
	rec := gzDo(t, Gzip(), "GET", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=10-19"}, serve)
	if rec.Code != 206 || rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != content[10:20] {
		t.Fatalf("range: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
	rec = gzDo(t, Gzip(), "GET", gzAccept, serve)
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || gzBody(t, rec) != content {
		t.Fatalf("full: %d %v", rec.Code, rec.Header())
	}
}

func TestGzipNothingWritten(t *testing.T) {
	rec := gzDo(t, Gzip(), "GET", gzAccept, func(context.Context, *Request, *Response) error { return nil })
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("unexpected output")
	}
}

func TestGzipConcurrent(t *testing.T) {
	mw := Gzip()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := strings.Repeat(string(rune('a'+i%26)), 2000+i)
			rec := gzDo(t, mw, "GET", gzAccept, gzText(body, "text/plain"))
			if gzBody(t, rec) != body {
				t.Error("mismatch")
			}
		}(i)
	}
	wg.Wait()
}

func TestGzipWithServer(t *testing.T) {
	s := NewServer()
	s.Use(Gzip())
	big := strings.Repeat("srv ", 1000)
	h := gzText(big, "text/plain")
	if err := s.Register(Raw("GET", "/g", RawHandlerFunc(h))); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/g", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "gzip" || gzBody(t, rec) != big {
		t.Fatalf("server: %v", rec.Header())
	}
}

func BenchmarkGzip(b *testing.B) {
	body := strings.Repeat("benchmark payload ", 600)
	h := Gzip()(gzText(body, "application/json"))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		_ = h(r.Context(), &Request{Raw: r}, &Response{Writer: httptest.NewRecorder()})
	}
}
