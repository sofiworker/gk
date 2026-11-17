package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func healthServer(t *testing.T, h *Health) *Server {
	t.Helper()
	s := NewServer()
	if err := s.Register(h.Routes("/livez", "/readyz")...); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return s
}

func healthGet(s *Server, p string) (*httptest.ResponseRecorder, HealthReport) {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
	var rep HealthReport
	_ = json.Unmarshal(rec.Body.Bytes(), &rep)
	return rec, rep
}

func TestHealthLiveness(t *testing.T) {
	h := NewHealth()
	h.SetReady(false)
	rec, rep := healthGet(healthServer(t, h), "/livez")
	if rec.Code != 200 || rep.Status != "ok" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("got %d %+v cc=%q", rec.Code, rep, rec.Header().Get("Cache-Control"))
	}
}

func TestHealthReadiness(t *testing.T) {
	h := NewHealth()
	s := healthServer(t, h)
	if rec, rep := healthGet(s, "/readyz"); rec.Code != 200 || rep.Status != "ok" {
		t.Fatalf("empty: %d %+v", rec.Code, rep)
	}
	h.AddCheck("db", func(context.Context) error { return nil })
	rec, rep := healthGet(s, "/readyz")
	if rec.Code != 200 || rep.Checks["db"] != "ok" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("pass: %d %+v", rec.Code, rep)
	}
	h.AddCheck("cache", func(context.Context) error { return errors.New("secret dsn") })
	rec, rep = healthGet(s, "/readyz")
	if rec.Code != 503 || rep.Status != "unavailable" || rep.Checks["cache"] != "failed" || rep.Checks["db"] != "ok" {
		t.Fatalf("fail: %d %+v", rec.Code, rep)
	}
	if bytesContains(rec.Body.String(), "secret") {
		t.Error("error text leaked")
	}
}

func bytesContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestHealthVerbose(t *testing.T) {
	h := NewHealth(WithHealthVerbose(true))
	h.AddCheck("x", func(context.Context) error { return errors.New("boom") })
	_, rep := healthGet(healthServer(t, h), "/readyz")
	if rep.Checks["x"] != "boom" {
		t.Fatalf("%+v", rep)
	}
}

func TestHealthNotReady(t *testing.T) {
	h := NewHealth()
	h.AddCheck("ok", func(context.Context) error { return nil })
	s := healthServer(t, h)
	h.SetReady(false)
	rec, rep := healthGet(s, "/readyz")
	if rec.Code != 503 || rep.Status != "unavailable" || rep.Checks["ok"] != "ok" {
		t.Fatalf("%d %+v", rec.Code, rep)
	}
	h.SetReady(true)
	if rec, _ := healthGet(s, "/readyz"); rec.Code != 200 {
		t.Fatalf("restored: %d", rec.Code)
	}
}

func TestHealthTimeoutAndPanic(t *testing.T) {
	h := NewHealth(WithHealthCheckTimeout(50 * time.Millisecond))
	release := make(chan struct{})
	defer close(release)
	h.AddCheck("hang", func(context.Context) error { <-release; return nil })
	h.AddCheck("panic", func(context.Context) error { panic("x") })
	start := time.Now()
	rec, rep := healthGet(healthServer(t, h), "/readyz")
	if time.Since(start) > time.Second {
		t.Error("readiness blocked by hanging check")
	}
	if rec.Code != 503 || rep.Checks["hang"] != "failed" || rep.Checks["panic"] != "failed" {
		t.Fatalf("%d %+v", rec.Code, rep)
	}
}

func TestHealthChecksRunConcurrently(t *testing.T) {
	h := NewHealth(WithHealthCheckTimeout(time.Second))
	for _, n := range []string{"a", "b", "c"} {
		h.AddCheck(n, func(context.Context) error { time.Sleep(100 * time.Millisecond); return nil })
	}
	start := time.Now()
	if _, ok := h.Check(context.Background()); !ok {
		t.Fatal("not ok")
	}
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Errorf("checks seem sequential: %v", d)
	}
}

func TestHealthAddCheckConcurrent(t *testing.T) {
	h := NewHealth()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); h.AddCheck("c", func(context.Context) error { return nil }) }()
		go func() { defer wg.Done(); h.Check(context.Background()) }()
	}
	wg.Wait()
}

func TestHealthOptionsIgnoreInvalid(t *testing.T) {
	h := NewHealth(WithHealthCheckTimeout(0), nil)
	if h.timeout != DefaultHealthCheckTimeout {
		t.Errorf("timeout = %v", h.timeout)
	}
	h.AddCheck("nil", nil)
	if len(h.checks) != 0 {
		t.Error("nil check registered")
	}
}
