package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestReady_OneFails 验证任一检查失败时返回 503 且默认报出原因。
// TestReady_OneFails verifies any failure yields 503 and reports the reason.
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
		t.Errorf("cache check=%v, want the reason", checksMap["cache"])
	}
	if checksMap["db"] != "ok" {
		t.Errorf("db check=%v, want ok", checksMap["db"])
	}
}

// TestReadyWith_DetailsExposesReason 验证显式开启后原因原文可见（排障逃生舱），且
// 长度受限。
// TestReadyWith_DetailsExposesReason checks the reason is visible once explicitly
// requested (the debugging escape hatch), and bounded.
func TestReadyWith_DetailsExposesReason(t *testing.T) {
	m := New()
	checks := []Checker{
		{Name: "db", Check: func(context.Context) error {
			return errors.New("dial tcp 10.0.3.7:5432: connection refused\nstack line 2")
		}},
	}
	if err := ReadyWith(m, "/readyz", 0, true, checks...); err != nil {
		t.Fatal(err)
	}
	code, body := hitProbe(t, m, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", code)
	}
	checksMap, _ := body["checks"].(map[string]any)
	got, _ := checksMap["db"].(string)
	if !strings.Contains(got, "10.0.3.7:5432") {
		t.Fatalf("db check=%q, want the raw reason when details are on", got)
	}
	// 多行原因必须被折叠为单行：JSON 里塞整段堆栈无益且放大响应体。
	if strings.Contains(got, "\n") {
		t.Fatalf("reason must be collapsed to one line, got %q", got)
	}
	if len(got) > maxHealthDetailLen+len("…") {
		t.Fatalf("reason length = %d, want bounded by %d", len(got), maxHealthDetailLen)
	}

	// 反向：details=false 不得出现任何原因文本。
	// Reverse: details=false must surface no reason text at all.
	m2 := New()
	if err := ReadyWith(m2, "/readyz", 0, false, checks...); err != nil {
		t.Fatal(err)
	}
	code2, body2 := hitProbe(t, m2, "/readyz")
	if code2 != http.StatusServiceUnavailable {
		t.Errorf("masked status=%d, want 503", code2)
	}
	checksMap2, _ := body2["checks"].(map[string]any)
	if checksMap2["db"] != "fail" {
		t.Fatalf("db check=%v, want the masked token \"fail\"", checksMap2["db"])
	}
}

// TestReady_RejectsUnusableCheckers 锁定注册期校验：nil Check 会让请求期空指针 panic
// （探针通常是无鉴权公开端点，一次坏注册永久挂死该路径）；空名或重名会让结果 map 互相
// 覆盖，运维看到的"某依赖 ok"实际来自另一个依赖。
// TestReady_RejectsUnusableCheckers locks registration-time validation: a nil Check
// panics at request time (probes are usually unauthenticated public endpoints, so one
// bad registration wedges the path forever), while an empty or duplicate name lets the
// result map overwrite entries, so "dependency X is ok" comes from another dependency.
func TestReady_RejectsUnusableCheckers(t *testing.T) {
	cases := []struct {
		name   string
		checks []Checker
		want   string
	}{
		{"nil check func", []Checker{{Name: "db"}}, "nil Check"},
		{"empty name", []Checker{{Check: func(context.Context) error { return nil }}}, "empty Name"},
		{"duplicate name", []Checker{
			{Name: "db", Check: func(context.Context) error { return nil }},
			{Name: "db", Check: func(context.Context) error { return nil }},
		}, "duplicate checker name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New()
			err := Ready(m, "/readyz", 0, c.checks...)
			if err == nil {
				t.Fatal("want a registration error")
			}
			if !errors.Is(err, ErrInvalidParam) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrInvalidParam mentioning %q", err, c.want)
			}
		})
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
