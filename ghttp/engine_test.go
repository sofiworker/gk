package ghttp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestEngine_RunAndShutdown 端到端验证 Engine.Run 一步式启动 + Shutdown 优雅关闭。
// 借 listenLocal 取一个真实空闲地址,关闭后立即交给 Run 复用(窗口极小,端口刚释放)。
// TestEngine_RunAndShutdown end-to-end verifies Engine.Run one-liner startup and
// graceful Shutdown. It borrows a real free address via listenLocal, closes it, and
// immediately reuses it in Run (tiny window; the port was just freed).
func TestEngine_RunAndShutdown(t *testing.T) {
	ln := listenLocal(t)
	addr := ln.Addr().String()
	_ = ln.Close()

	m := New()
	m.Use(CORS(CORSDefault()))
	_ = m.RawHandle(http.MethodGet, "/ping", RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error {
		_, _ = resp.WriteString("pong")
		return nil
	}))

	errCh := make(chan error, 1)
	go func() { errCh <- m.Run(addr) }()

	body, code := getWithRetry(t, "http://"+addr+"/ping", nil)
	if code != http.StatusOK || body != "pong" {
		t.Errorf("GET /ping = %q [%d], want pong [200]", body, code)
	}

	// 预检也应工作(全局 CORS 在未注册 OPTIONS 上生效),端到端复核架构修复。
	// Preflight should work too (global CORS on unregistered OPTIONS), an
	// end-to-end recheck of the architectural fix.
	preReq, _ := http.NewRequest(http.MethodOptions, "http://"+addr+"/ping", nil)
	preReq.Header.Set("Origin", "https://x.com")
	preReq.Header.Set("Access-Control-Request-Method", "GET")
	preResp, err := http.DefaultClient.Do(preReq)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_ = preResp.Body.Close()
	if preResp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status=%d, want 204", preResp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, ErrServerClosed) {
			t.Errorf("Run returned %v, want ErrServerClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Run did not return after Shutdown")
	}
}

// TestEngine_ShutdownBeforeRun 验证未经 Run 就 Shutdown 返回明确错误。
// TestEngine_ShutdownBeforeRun verifies Shutdown before Run returns a clear error.
func TestEngine_ShutdownBeforeRun(t *testing.T) {
	m := New()
	if err := m.Shutdown(context.Background()); err == nil {
		t.Error("Shutdown before Run should return an error")
	}
}
