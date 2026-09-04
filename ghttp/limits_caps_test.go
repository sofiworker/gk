package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestFormBody_OversizedNonPOSTBodyIs413 锁定 P1-2：DELETE 等非 POST/PUT/PATCH 方法的
// 表单体由本包自行读取，过去完全无上限（LimitBody 是 opt-in），一条超大体即可打满内存。
// Locks P1-2: form bodies for methods other than POST/PUT/PATCH are read by this
// package itself and were previously uncapped (LimitBody is opt-in), so one oversized
// body could exhaust memory.
func TestFormBody_OversizedNonPOSTBodyIs413(t *testing.T) {
	t.Parallel()

	s := New()
	type body struct {
		A string `form:"a"`
	}
	var seen bool
	if err := DeleteBody(s, "/d", FormBody[body](), JSON[string](),
		func(_ context.Context, b body) (string, error) { seen = true; return b.A, nil }); err != nil {
		t.Fatalf("register: %v", err)
	}

	// 超过 defaultMaxFormBytes 的体（压缩不了，直接构造 11 MiB）。
	// A body larger than defaultMaxFormBytes (11 MiB, built directly).
	oversize := strings.Repeat("a", defaultMaxFormBytes+1<<20)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/d", strings.NewReader(oversize))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.ServeHTTP(rec, r)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %q)", rec.Code, rec.Body.String())
	}
	if seen {
		t.Fatal("handler must not run for an oversized body")
	}
}

// TestTextBody_OversizedBodyIs413 锁定同一策略覆盖 text/plain：textCodec 需把整块体读进
// 内存，故必须有帽。
// Locks the same policy for text/plain: textCodec must hold the whole body in memory,
// so it needs a cap.
func TestTextBody_OversizedBodyIs413(t *testing.T) {
	t.Parallel()

	s := New()
	var seen bool
	if err := PostBody(s, "/t", TextBody[string](), JSON[string](),
		func(_ context.Context, b string) (string, error) { seen = true; return b, nil }); err != nil {
		t.Fatalf("register: %v", err)
	}

	oversize := strings.Repeat("x", defaultMaxTextBodyBytes+1<<20)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/t", strings.NewReader(oversize))
	r.Header.Set("Content-Type", "text/plain")
	s.ServeHTTP(rec, r)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if seen {
		t.Fatal("handler must not run for an oversized text body")
	}
}

// TestFormBody_NormalSizeStillWorks 是反向保护：加帽不得误伤正常大小的体。
// TestFormBody_NormalSizeStillWorks is the reverse guard: the cap must not break
// normally sized bodies.
func TestFormBody_NormalSizeStillWorks(t *testing.T) {
	t.Parallel()

	s := New()
	type body struct {
		A string `form:"a"`
	}
	if err := DeleteBody(s, "/d", FormBody[body](), JSON[string](),
		func(_ context.Context, b body) (string, error) { return b.A, nil }); err != nil {
		t.Fatalf("register: %v", err)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/d", strings.NewReader("a=ok"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("got %d %q, want 200 with ok", rec.Code, rec.Body.String())
	}
}

// TestWSReadLimit_DefaultRejectsOversizedMessage 锁定 P1-3：过去从不 SetReadLimit，
// 任意已连接客户端可用持续大帧耗尽服务端内存。
// Locks P1-3: SetReadLimit was never called, so any connected client could exhaust
// server memory with ever-larger frames.
func TestWSReadLimit_DefaultRejectsOversizedMessage(t *testing.T) {
	t.Parallel()

	s := New()
	done := make(chan error, 1)
	up := NewWSUpgrader()
	if err := ServeWS(s, "/ws", up, func(_ context.Context, _ *Request, c *websocket.Conn) error {
		// 服务端只读：客户端发超大帧时应因默认上限被拒。
		// The server only reads: an oversized client frame must be rejected by the
		// default cap.
		_, _, err := c.ReadMessage()
		done <- err
		return err
	}); err != nil {
		t.Fatalf("ServeWS: %v", err)
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	big := make([]byte, defaultWSReadLimit+4096)
	if err := c.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetWriteDeadline: %v", err)
	}
	if err := c.WriteMessage(websocket.TextMessage, big); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("server read succeeded; the default read limit is not in effect")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server never rejected the oversized message")
	}
}
