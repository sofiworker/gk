package ghttp

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ——— Metrics ———

func TestMetricsMiddlewareRecordsHit(t *testing.T) {
	m := NewMetrics()
	s := New()
	s.Use(m.Middleware())
	s.RawHandle(http.MethodGet, "/hello", func(_ context.Context, _ *Request, resp *Response) error {
		_, _ = resp.WriteString("hi")
		return nil
	})
	s.RawHandle(http.MethodGet, "/metrics", m.Handler())

	// 发几个请求累积指标
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/hello", nil)
		s.ServeHTTP(httptest.NewRecorder(), r)
	}
	// 抓指标端点
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("metrics endpoint status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `http_requests_total{method="GET",route="/hello"}`) {
		t.Error("missing route label in metrics output")
	}
	if !strings.Contains(body, `code="200"`) {
		t.Error("missing status code label")
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Error("missing go runtime metrics")
	}
}

func TestMetricsMiddlewareRecordsError(t *testing.T) {
	m := NewMetrics()
	s := New()
	s.Use(m.Middleware())
	s.RawHandle(http.MethodGet, "/err", func(_ context.Context, _ *Request, _ *Response) error {
		return statusError(http.StatusTeapot)
	})
	s.RawHandle(http.MethodGet, "/metrics", m.Handler())

	r := httptest.NewRequest(http.MethodGet, "/err", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusTeapot {
		t.Fatalf("expected 418 got %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, `code="418"`) {
		t.Error("missing 418 status in metrics:", body)
	}
}

func TestHTTPStatusHelper(t *testing.T) {
	if s := HTTPStatus(nil); s != 200 {
		t.Errorf("nil err → %d want 200", s)
	}
	if s := HTTPStatus(ErrInvalidInput); s != 400 {
		t.Errorf("ErrInvalidInput → %d want 400", s)
	}
	if s := HTTPStatus(statusError(503)); s != 503 {
		t.Errorf("statusError(503) → %d want 503", s)
	}
}

func TestMetricsMissRoute(t *testing.T) {
	m := NewMetrics()
	s := New()
	s.Use(m.Middleware())
	s.RawHandle(http.MethodGet, "/metrics", m.Handler())
	r := httptest.NewRequest(http.MethodGet, "/nope", nil)
	s.ServeHTTP(httptest.NewRecorder(), r)
	r = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `route="no_route"`) {
		t.Error("miss route not labeled as no_route")
	}
}

// ——— Gzip ———

func TestGzipCompresses(t *testing.T) {
	s := New()
	s.Use(Gzip())
	s.RawHandle(http.MethodGet, "/body", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "application/json")
		payload := strings.Repeat("x", 500)
		_, _ = resp.WriteString(`{"data":"` + payload + `"}`)
		return nil
	})

	r := httptest.NewRequest(http.MethodGet, "/body", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding %q want gzip", w.Header().Get("Content-Encoding"))
	}
	body := w.Body.String()
	// 解压确认内容正确
	gr, err := gzip.NewReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	uncompressed, _ := io.ReadAll(gr)
	if !strings.Contains(string(uncompressed), `"data"`) {
		t.Errorf("decompressed body mismatch: %s", uncompressed)
	}
}

func TestGzipSkipsWhenNotRequested(t *testing.T) {
	s := New()
	s.Use(Gzip())
	s.RawHandle(http.MethodGet, "/body", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "application/json")
		_, _ = resp.WriteString("hello")
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/body", nil)
	// 不带 Accept-Encoding
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("compressed without Accept-Encoding")
	}
}

func TestGzipSkipsNonCompressibleCT(t *testing.T) {
	s := New()
	s.Use(Gzip(WithGzipContentTypes("application/json"))) // 仅 JSON 可压
	s.RawHandle(http.MethodGet, "/img", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "image/png")
		_, _ = resp.WriteString("pngbytes")
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/img", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("compressed image/png")
	}
}

func TestGzipHandles204(t *testing.T) {
	s := New()
	s.Use(Gzip())
	s.RawHandle(http.MethodGet, "/nodata", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusNoContent)
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/nodata", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("compressed 204 response")
	}
}

func TestGzipErrorResponseUncompressed(t *testing.T) {
	s := New()
	s.Use(Gzip())
	s.RawHandle(http.MethodGet, "/err", func(_ context.Context, _ *Request, _ *Response) error {
		return statusError(http.StatusTeapot)
	})
	r := httptest.NewRequest(http.MethodGet, "/err", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusTeapot {
		t.Fatalf("status %d", w.Code)
	}
	// 错误链写未压缩体;Content-Encoding 不应为 gzip
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("error response should not be gzip-encoded")
	}
}

func TestGzipAlreadyEncodedPassthrough(t *testing.T) {
	s := New()
	s.Use(Gzip())
	s.RawHandle(http.MethodGet, "/encoded", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Encoding", "br")
		resp.Header().Set("Content-Type", "text/plain")
		_, _ = resp.WriteString("brotlied")
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/encoded", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") != "br" {
		t.Errorf("overwrote existing Content-Encoding: got %q", w.Header().Get("Content-Encoding"))
	}
}

func TestGzipQZero(t *testing.T) {
	s := New()
	s.Use(Gzip())
	s.RawHandle(http.MethodGet, "/body", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain")
		_, _ = resp.WriteString("text")
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/body", nil)
	r.Header.Set("Accept-Encoding", "gzip;q=0")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Error("compressed with q=0")
	}
}

func TestGzipContentLengthRemoved(t *testing.T) {
	s := New()
	s.Use(Gzip())
	s.RawHandle(http.MethodGet, "/body", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain")
		resp.Header().Set("Content-Length", "100")
		_, _ = resp.WriteString("hello world")
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/body", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Header().Get("Content-Length") != "" {
		t.Errorf("Content-Length not removed: %q", w.Header().Get("Content-Length"))
	}
}
