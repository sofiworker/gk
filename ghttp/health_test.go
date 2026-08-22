package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// hitProbe 执行一次探针请求，返回状态码与解析后的 JSON body。
// hitProbe issues one probe request, returning the status code and parsed JSON body.
func hitProbe(t *testing.T, m *Server, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("probe %s: bad JSON %q: %v", path, rec.Body.String(), err)
		}
	}
	return rec.Code, body
}

// TestHealth_AlwaysOK 验证存活探针恒 200 且返回 status=ok。
func TestHealth_AlwaysOK(t *testing.T) {
	m := New()
	if err := Health(m, "/healthz"); err != nil {
		t.Fatal(err)
	}
	code, body := hitProbe(t, m, "/healthz")
	if code != http.StatusOK {
		t.Errorf("status=%d, want 200", code)
	}
	if body["status"] != "ok" {
		t.Errorf("status=%v, want ok", body["status"])
	}
}

// TestReady_AllPass 验证所有检查通过时返回 200。
func TestReady_AllPass(t *testing.T) {
	m := New()
	checks := []Checker{
		LivenessChecker("db"),
		{Name: "cache", Check: func(context.Context) error { return nil }},
	}
	if err := Ready(m, "/readyz", 0, checks...); err != nil {
		t.Fatal(err)
	}
	code, body := hitProbe(t, m, "/readyz")
	if code != http.StatusOK {
		t.Errorf("status=%d, want 200", code)
	}
	if body["status"] != "ok" {
		t.Errorf("status=%v, want ok", body["status"])
	}
	checksMap, _ := body["checks"].(map[string]any)
	if checksMap["db"] != "ok" || checksMap["cache"] != "ok" {
		t.Errorf("checks=%v, want all ok", checksMap)
	}
}

// TestReady_OneFails 验证任一检查失败时返回 503 且明细含错误消息。
func TestReady_OneFails(t *testing.T) {
	m := New()
	checks := []Checker{
		LivenessChecker("db"),
		{Name: "cache", Check: func(context.Context) error { return errors.New("connection refused") }},
	}
	if err := Ready(m, "/readyz", 0, checks...); err != nil {
		t.Fatal(err)
	}
	code, body := hitProbe(t, m, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", code)
	}
	if body["status"] != "unavailable" {
		t.Errorf("status=%v, want unavailable", body["status"])
	}
	checksMap, _ := body["checks"].(map[string]any)
	if checksMap["cache"] != "connection refused" {
		t.Errorf("cache check=%v, want error message", checksMap["cache"])
	}
	if checksMap["db"] != "ok" {
		t.Errorf("db check=%v, want ok", checksMap["db"])
	}
}

// TestReady_PerCheckTimeout 验证慢检查在 checkTimeout 下被判失败。
func TestReady_PerCheckTimeout(t *testing.T) {
	m := New()
	slow := Checker{Name: "slow", Check: func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			return nil
		}
	}}
	if err := Ready(m, "/readyz", 10*time.Millisecond, slow); err != nil {
		t.Fatal(err)
	}
	code, _ := hitProbe(t, m, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503 (slow check timed out)", code)
	}
}

// TestReadinessGate_Toggle 验证运行时就绪门闸开关。
func TestReadinessGate_Toggle(t *testing.T) {
	m := New()
	gate, checker := NewReadinessGate("startup")
	if err := Ready(m, "/readyz", 0, checker); err != nil {
		t.Fatal(err)
	}

	// 初始未就绪 → 503。
	if code, _ := hitProbe(t, m, "/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("initial status=%d, want 503", code)
	}

	// 置为就绪 → 200。
	gate.Set(true, nil)
	if code, _ := hitProbe(t, m, "/readyz"); code != http.StatusOK {
		t.Errorf("after ready status=%d, want 200", code)
	}

	// 排水（置为不就绪，带自定义原因）→ 503。
	gate.Set(false, errors.New("draining"))
	code, body := hitProbe(t, m, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Errorf("after drain status=%d, want 503", code)
	}
	checksMap, _ := body["checks"].(map[string]any)
	if checksMap["startup"] != "draining" {
		t.Errorf("startup check=%v, want draining", checksMap["startup"])
	}
}

// TestReadinessGate_DefaultCause 验证 Set(false,nil) 回退到 ErrNotReady。
func TestReadinessGate_DefaultCause(t *testing.T) {
	gate, checker := NewReadinessGate("g")
	gate.Set(false, nil)
	if err := checker.Check(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Errorf("check err=%v, want ErrNotReady", err)
	}
}

// TestHealth_HeadMethod 验证 HEAD 探针可用。
func TestHealth_HeadMethod(t *testing.T) {
	m := New()
	if err := Health(m, "/healthz"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD status=%d, want 200", rec.Code)
	}
}
