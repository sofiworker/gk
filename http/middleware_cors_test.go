package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func corsDo(t *testing.T, cfg CORSConfig, method string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	s := NewServer()
	s.Use(CORS(cfg))
	h := func(_ context.Context, _ *Request, resp *Response) error { _, _ = resp.Write([]byte("OK")); return nil }
	if err := s.Register(Route{Method: http.MethodGet, Path: "/t", compiledHandler: h}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/t", nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	s.ServeHTTP(w, r)
	return w
}

func TestCORSSimpleRequest(t *testing.T) {
	cfg := CORSConfig{AllowOrigins: []string{"https://a.com"}, ExposeHeaders: []string{"X-A", "X-B"}, AllowCredentials: true}
	w := corsDo(t, cfg, "GET", map[string]string{"Origin": "https://a.com"})
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != "https://a.com" || h.Get("Access-Control-Allow-Credentials") != "true" ||
		h.Get("Access-Control-Expose-Headers") != "X-A, X-B" || h.Get("Vary") != "Origin" {
		t.Fatalf("headers: %v", h)
	}
	// 未命中来源：无 ACAO 但有 Vary / not matched: no ACAO but Vary
	w = corsDo(t, cfg, "GET", map[string]string{"Origin": "https://evil.com"})
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "Origin" || w.Code != 200 {
		t.Fatalf("headers: %v", w.Header())
	}
	// 无 Origin / no Origin
	w = corsDo(t, cfg, "GET", nil)
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "Origin" {
		t.Fatalf("headers: %v", w.Header())
	}
}

func TestCORSWildcardAndFunc(t *testing.T) {
	w := corsDo(t, CORSConfig{AllowOrigins: []string{"*"}}, "GET", map[string]string{"Origin": "https://x.com"})
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("headers: %v", w.Header())
	}
	cfg := CORSConfig{AllowOriginFunc: func(o string) bool { return o == "https://fn.com" }}
	if corsDo(t, cfg, "GET", map[string]string{"Origin": "https://fn.com"}).Header().Get("Access-Control-Allow-Origin") != "https://fn.com" {
		t.Fatal("func origin not allowed")
	}
	if corsDo(t, cfg, "GET", map[string]string{"Origin": "https://no.com"}).Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("func origin wrongly allowed")
	}
}

func TestCORSPreflight(t *testing.T) {
	cfg := CORSConfig{AllowOrigins: []string{"https://a.com"}, AllowMethods: []string{"GET", "POST"}, MaxAge: time.Hour}
	pre := map[string]string{"Origin": "https://a.com", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "X-Foo"}
	w := corsDo(t, cfg, "OPTIONS", pre)
	h := w.Header()
	if w.Code != 204 || h.Get("Access-Control-Allow-Methods") != "GET, POST" || h.Get("Access-Control-Allow-Headers") != "X-Foo" ||
		h.Get("Access-Control-Max-Age") != "3600" || h.Get("Access-Control-Allow-Origin") != "https://a.com" {
		t.Fatalf("code %d headers: %v", w.Code, h)
	}
	cfg.AllowHeaders = []string{"Content-Type"}
	if got := corsDo(t, cfg, "OPTIONS", pre).Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
		t.Fatalf("allow headers %q", got)
	}
	// 普通 OPTIONS（无 Request-Method）交给服务器自动应答并带 Allow
	// plain OPTIONS (no Request-Method) goes to the server's automatic answer with Allow
	w = corsDo(t, cfg, "OPTIONS", map[string]string{"Origin": "https://a.com"})
	if w.Header().Get("Allow") == "" || w.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatalf("code %d headers: %v", w.Code, w.Header())
	}
	// 来源未命中的预检不由 CORS 应答
	// a preflight from a disallowed origin is not answered by CORS
	pre["Origin"] = "https://evil.com"
	w = corsDo(t, cfg, "OPTIONS", pre)
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatalf("headers: %v", w.Header())
	}
}

func TestNewCORSWildcardCredentials(t *testing.T) {
	_, err := NewCORS(CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true})
	if !errors.Is(err, ErrCORSWildcardCredentials) {
		t.Fatalf("err: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("CORS should panic")
		}
	}()
	CORS(CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true})
}
