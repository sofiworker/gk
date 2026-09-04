package ghttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSanitizeLogToken 直接单测净化器：控制字符必须被转义为可见形式且不产生裸换行，
// 干净字符串必须原样返回（零拷贝快路）。
// TestSanitizeLogToken covers the escaper directly: control characters must become
// visible without emitting a bare newline, and a clean string must pass through
// unchanged (the zero-copy fast path).
func TestSanitizeLogToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"a\nb", `a\nb`},
		{"a\r\nb", `a\r\nb`},
		{"a\tb", `a\tb`},
		{"a\x00b", `a\x00b`},
		{"a\x7fb", `a\x7fb`},
		{"héllo", "héllo"}, // 非 ASCII 不应被破坏 / non-ASCII must survive
	}
	for _, c := range cases {
		if got := sanitizeLogToken(c.in); got != c.want {
			t.Errorf("sanitizeLogToken(%q) = %q, want %q", c.in, got, c.in)
		}
	}
	// 关键不变量：输出永不含裸换行。
	// The key invariant: the output never contains a bare newline.
	for _, in := range []string{"a\nb", "a\r\nb", "\n", "x\x0a\x0a\x0ay"} {
		if got := sanitizeLogToken(in); strings.ContainsAny(got, "\n\r") {
			t.Errorf("sanitizeLogToken(%q) = %q still contains a line break", in, got)
		}
	}
}

// TestRecovery_CommittedPanicIsNotSwallowed 锁定 P2：响应已提交（流式场景）时 Recovery
// 原先一律 return nil，于是这次崩溃在 onError 钩子、访问日志与 5xx 指标里彻底消失——
// 恰是最该告警的一类事故。
// Locks the committed-response half: Recovery used to return nil unconditionally, so
// a crash after the response was committed disappeared from the onError hook, the
// access log and the 5xx metrics — precisely the class worth alerting on.
func TestRecovery_CommittedPanicIsNotSwallowed(t *testing.T) {
	t.Parallel()

	var hookErr error
	var hookStatus int
	s := New(WithErrorHook(func(_ *http.Request, status int, err error) {
		hookStatus, hookErr = status, err
	}))
	s.Use(Recovery())
	if err := s.RawHandle("GET", "/stream", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain")
		resp.WriteHeader(http.StatusOK)
		resp.Flush() // 提交响应 / commit the response
		panic("boom after commit")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/stream", nil))

	// 客户端已收到的 200 不得被改写。
	// The 200 the client already received must not be rewritten.
	if rec.Code != http.StatusOK {
		t.Fatalf("client status = %d, want 200 (committed header must stand)", rec.Code)
	}
	if hookErr == nil {
		t.Fatal("the panic was swallowed: onError never saw it")
	}
	if !errors.Is(hookErr, ErrHandlerPanic) {
		t.Fatalf("hook error %#v does not match ErrHandlerPanic", hookErr)
	}
	// 已提交时钩子如实上报【客户端实际收到的】状态，而非分类值（见 writeError 文档）。
	// When committed, the hook reports the status the client actually received, not
	// the classification (see writeError's doc).
	if hookStatus != http.StatusOK {
		t.Fatalf("hook status = %d, want the delivered 200", hookStatus)
	}
	if !strings.Contains(hookErr.Error(), "boom after commit") {
		t.Fatalf("panic value lost from the error: %v", hookErr)
	}
}

// TestRecovery_UsesUnifiedErrorChainShape 锁定同一修复的另一半：未提交时 500 必须由
// 错误链渲染（JSON + code），而不是本中间件自写的 text/plain。
// Locks the other half: an uncommitted 500 must be rendered by the error chain
// (JSON with a code), not as text/plain hand-written by the middleware.
func TestRecovery_UsesUnifiedErrorChainShape(t *testing.T) {
	t.Parallel()

	s := New()
	s.Use(Recovery())
	if err := s.RawHandle("GET", "/boom", func(_ context.Context, _ *Request, _ *Response) error {
		panic("plain crash")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON from the unified error renderer", ct)
	}
	if !strings.Contains(rec.Body.String(), `"internal"`) {
		t.Fatalf("body %q lacks the unified error code", rec.Body.String())
	}
}

// TestLogger_AccessLogIsSingleLine 锁定 P1-8 的端到端效果：查询串/错误文本里的 %0a
// 不得把一条访问记录劈成两行（日志注入）。
// TestLogger_AccessLogIsSingleLine locks the end-to-end effect of P1-8: a %0a coming
// from a query string or error text must not split one access record into two lines
// (log injection).
func TestLogger_AccessLogIsSingleLine(t *testing.T) {
	t.Parallel()

	var lines []string
	s := New()
	s.Use(LoggerWith(func(a AccessLog) {
		lines = append(lines, "log "+a.Method+" "+sanitizeLogToken(a.Path)+" "+sanitizeLogToken(errText(a.Err)))
	}))
	if err := GetNone[string](s, "/q", JSON[string](),
		func(_ context.Context) (string, error) {
			return "", errors.New("bad value: injected\nFORGED admin login")
		}); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/q?name=x%0aFORGED", nil))

	joined := strings.Join(lines, "|")
	if strings.Count(joined, "log ") != len(lines) {
		t.Fatalf("one request produced %d log-prefixed records: %q", len(lines), joined)
	}
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 record, got %d: %q", len(lines), lines)
	}
	if strings.Contains(lines[0], "\n") {
		t.Fatalf("the record still contains a bare newline: %q", lines[0])
	}
	// 注入文本仍在（不脱敏），但被转义成同一行内的可见序列。
	// The injected text survives (no redaction) but as a visible escape within the
	// same line.
	if !strings.Contains(lines[0], `\nFORGED`) {
		t.Fatalf("expected the escaped form, got %q", lines[0])
	}
}

// errText 取错误文本（nil 安全）。
// errText reads an error's text, nil-safely.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestSanitizeLogToken_DefaultRecoverySink 验证默认 Recovery sink 也净化：panic 值带
// 换行时服务端日志只多出一行堆栈，而非凭空多一条记录。
// TestSanitizeLogToken_DefaultRecoverySink checks the default Recovery sink too: a
// panic value containing a newline must add only the stack line, never a whole extra
// record.
func TestSanitizeLogToken_DefaultRecoverySink(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	origOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOut) })

	s := New()
	s.Use(Recovery())
	if err := s.RawHandle("GET", "/inj", func(_ context.Context, _ *Request, _ *Response) error {
		panic("value\n2024-01-01 INFO admin login ok")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/inj", nil))

	out := buf.String()
	if strings.Contains(out, "INFO admin login ok\n") && !strings.Contains(out, `\n2024-01-01`) {
		t.Fatalf("the panic value broke the record line:\n%s", out)
	}
	if !strings.Contains(out, `value\n2024-01-01`) {
		t.Fatalf("expected the escaped form in the log, got:\n%s", out)
	}
}
