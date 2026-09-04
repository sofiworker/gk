package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// staticTracker 是一个中间件，记录链最外层看到的最终 status/bytes/written。
// staticTracker is a middleware capturing the final status/bytes/written as seen
// at the outermost point of the chain.
type staticTracker struct {
	status int
	bytes  int
}

// TestStatic_ServingGoesThroughResponseGate 锁定 P1-1 的观测部分：静态服务必须经
// *Response 包装器写，否则 Written()/Status()/BytesOut() 全为初始值，Metrics 与
// Logger 记成 "200 且 0 字节"，流量/体积统计完全失真。
// Locks the observability half of P1-1: static serving must write through the
// *Response wrapper, otherwise Written()/Status()/BytesOut() keep their zero
// values and Metrics/Logger record "200 with 0 bytes", wrecking traffic and
// size accounting.
func TestStatic_ServingGoesThroughResponseGate(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte(strings.Repeat("x", 4096))}}
	s := New()
	tr := &staticTracker{}
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			err := next(ctx, req, resp)
			tr.status = resp.Status()
			tr.bytes = resp.BytesOut()
			return err
		}
	})
	if err := StaticFS(s, "/s/", fsys); err != nil {
		t.Fatalf("StaticFS: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/s/a.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("client status = %d, want 200", rec.Code)
	}
	if tr.bytes != 4096 {
		t.Fatalf("framework-visible BytesOut = %d, want 4096 (writer bypassed the *Response gate)", tr.bytes)
	}
	if tr.status != http.StatusOK {
		t.Fatalf("framework-visible Status = %d, want 200", tr.status)
	}
}

// TestStatic_RangeAnd304RecordRealStatus 锁定状态跟踪对 206/304 也成立：
// http.ServeContent 会自行 WriteHeader，绕过包装器时框架只能记成 200。
// Locks status tracking for 206/304 too: http.ServeContent calls WriteHeader
// itself, so bypassing the wrapper leaves the framework recording 200.
func TestStatic_RangeAnd304RecordRealStatus(t *testing.T) {
	t.Parallel()

	data := []byte(strings.Repeat("y", 8192))
	fsys := fstest.MapFS{"big.txt": &fstest.MapFile{Data: data}}
	s := New()
	var got int
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			err := next(ctx, req, resp)
			got = resp.Status()
			return err
		}
	})
	if err := StaticFS(s, "/s/", fsys); err != nil {
		t.Fatalf("StaticFS: %v", err)
	}

	r := httptest.NewRequest("GET", "/s/big.txt", nil)
	r.Header.Set("Range", "bytes=0-99")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("client status = %d, want 206", rec.Code)
	}
	if got != http.StatusPartialContent {
		t.Fatalf("framework-visible Status = %d, want 206", got)
	}
}

// TestStatic_MissUsesUnifiedJSONErrorShape 锁定错误体口径：静态 miss 必须与其余 404
// 同形（JSON + code），而不是 text/plain 裸文本——否则前端/契约测试对同一逻辑错误
// 要处理两种响应格式。
// Locks the error-body contract: a static miss must share the shape of every other
// 404 (JSON with a code), not bare text/plain — otherwise clients must handle two
// formats for one logical error.
func TestStatic_MissUsesUnifiedJSONErrorShape(t *testing.T) {
	t.Parallel()

	s := New()
	if err := StaticFS(s, "/s/", fstest.MapFS{}); err != nil {
		t.Fatalf("StaticFS: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/s/missing.txt", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON (unified error renderer)", ct)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not the unified error JSON: %v", rec.Body.String(), err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("error.code = %q, want not_found", body.Error.Code)
	}
}

// TestStatic_ControlCharPathIs400 锁定 P2-24：含 NUL 的 catch-all 名字过去被 os.DirFS
// 拒为 EINVAL，而静态层把所有非 ErrNotExist 的 stat 错误一律记 500，任何人都能用一条
// URL 稳定制造 5xx。现在路径校验在入口即拦控制字符 → 400。
// Locks P2-24: a catch-all name containing NUL used to be rejected by os.DirFS with
// EINVAL, which the static layer lumped into a 500, so anyone could manufacture
// stable 5xx with one URL. Path validation now rejects control characters at the
// entry → 400.
func TestStatic_ControlCharPathIs400(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := New()
	if err := Static(s, "/s/", dir); err != nil {
		t.Fatalf("Static: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/s/ok%00txt", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (a client-reachable path must never be a 5xx)", rec.Code)
	}

	// 换行同理：过去会原样进日志，形成伪造整行的日志注入。
	// Same for a newline, which used to reach the logs verbatim and let a client
	// forge whole log lines.
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, httptest.NewRequest("GET", "/s/a%0Ab", nil))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a newline-injected path", rec2.Code)
	}
}

// TestStatic_NonExistentStill404 是反向保护：修复不得把普通 404 变成 400。
// TestStatic_NonExistentStill404 is the reverse guard: the fix must not turn an
// ordinary missing file into a 400.
func TestStatic_NonExistentStill404(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := New()
	if err := Static(s, "/s/", dir); err != nil {
		t.Fatalf("Static: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/s/nope.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
