package ghttp

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// S1:TSR 重定向的开放重定向
// ---------------------------------------------------------------------------

// rawRequest 用裸 HTTP 报文构造请求,绕过 httptest.NewRequest 对 URL 的规范化。
// 开放重定向只有在客户端能发出 `GET //evil.com/` 这类原始请求行时才可复现。
func rawRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	raw := "GET " + target + " HTTP/1.1\r\nHost: svc.example\r\n\r\n"
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadRequest(%q): %v", target, err)
	}
	return req
}

// TestTSR_RejectsProtocolRelativeTarget 锁定开放重定向被拒。
//
// 路由树按 / 分段匹配且忽略空段,因此 `//evil.com/` 能命中 /{a}/{b} 并触发 TSR 去尾斜杠。
// 若原样回写,Location: //evil.com 是协议相对 URL,浏览器会跳到 https://evil.com。
// `/\evil.com` 同理:浏览器把反斜杠等同斜杠。两者都必须拒绝重定向。
func TestTSR_RejectsProtocolRelativeTarget(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		target  string
	}{
		{"double slash", "/{a}/{b}", "//evil.com/"},
		{"backslash", "/{name}", "/\\evil.com/"},
		{"double slash with path", "/{a}/{b}/{c}", "//evil.com/x/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if err := s.RawHandle(http.MethodGet, tc.pattern, func(_ context.Context, _ *Request, resp *Response) error {
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, rawRequest(t, tc.target))

			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("Location=%q, want none: an off-site redirect target must be refused", loc)
			}
			if rec.Code == http.StatusMovedPermanently || rec.Code == http.StatusPermanentRedirect {
				t.Errorf("status=%d, want a non-redirect (open redirect)", rec.Code)
			}
			if rec.Code != http.StatusNotFound {
				t.Errorf("status=%d, want 404", rec.Code)
			}
		})
	}
}

// TestTSR_ReEncodesPathInLocation 锁定 Location 里的路径被重新转义。
//
// r.URL.Path 已被标准库解码,直接拼进 Location 会让 %3F/%23 还原成裸 ? 与 #,
// 把路径的一部分变成查询串或 fragment(/a%3Fx=1/b/ → /a?x=1/b),重定向指向的资源就变了。
func TestTSR_ReEncodesPathInLocation(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		wantLoc    string
		notContain string
	}{
		{"encoded question mark", "/a%3Fx=1/b/", "/a%3Fx=1/b", "?"},
		{"encoded hash", "/a%23frag/b/", "/a%23frag/b", "#"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if err := s.RawHandle(http.MethodGet, "/{a}/{b}", func(_ context.Context, _ *Request, resp *Response) error {
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, rawRequest(t, tc.target))

			loc := rec.Header().Get("Location")
			if loc != tc.wantLoc {
				t.Errorf("Location=%q, want %q", loc, tc.wantLoc)
			}
			if strings.Contains(loc, tc.notContain) {
				t.Errorf("Location=%q must not contain a bare %q: decoding changed the target's meaning",
					loc, tc.notContain)
			}
		})
	}
}

// TestTSR_NormalRedirectStillWorks 确认安全加固没有破坏正常的尾斜杠重定向。
func TestTSR_NormalRedirectStillWorks(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		target   string
		wantCode int
		wantLoc  string
	}{
		{"strip trailing slash", "/users", "/users/", http.StatusMovedPermanently, "/users"},
		{"append trailing slash", "/users/", "/users", http.StatusMovedPermanently, "/users/"},
		{"keeps query string", "/users", "/users/?page=2", http.StatusMovedPermanently, "/users?page=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if err := s.RawHandle(http.MethodGet, tc.pattern, func(_ context.Context, _ *Request, resp *Response) error {
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if rec.Code != tc.wantCode {
				t.Errorf("status=%d, want %d", rec.Code, tc.wantCode)
			}
			if got := rec.Header().Get("Location"); got != tc.wantLoc {
				t.Errorf("Location=%q, want %q", got, tc.wantLoc)
			}
		})
	}
}

// TestIsSafeRedirectTarget 直测判定函数的边界。
func TestIsSafeRedirectTarget(t *testing.T) {
	cases := []struct {
		target string
		want   bool
	}{
		{"/users", true},
		{"/", true},
		{"/a/b/c", true},
		{"/users?x=1", true},
		{"//evil.com", false},
		{"//evil.com/path", false},
		{"/\\evil.com", false},
		{"/\\\\evil.com", false},
		{"users", false}, // 无前导斜杠不是站内绝对路径
		{"", false},      // 空目标
		{"http://x", false},
	}
	for _, tc := range cases {
		if got := isSafeRedirectTarget(tc.target); got != tc.want {
			t.Errorf("isSafeRedirectTarget(%q)=%v, want %v", tc.target, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// S4:StatusCoder 返回非法状态码导致 panic 击穿 ServeHTTP
// ---------------------------------------------------------------------------

// badStatusError 是一个实现不当的业务错误:HTTPStatus 返回越界值。
type badStatusError struct{ status int }

func (e badStatusError) Error() string   { return "business failure" }
func (e badStatusError) HTTPStatus() int { return e.status }

// TestClassifyError_RejectsIllegalStatusCode 锁定非法状态码不会击穿 ServeHTTP。
//
// writeError 运行在所有 recover 之外(它本身就是 panic 的善后出口),因此
// WriteHeader(0) 的 panic 会直接逃出 ServeHTTP 并由 net/http 断连——一个实现不当的
// 业务错误类型即可打挂连接。非法值必须回退 500。
func TestClassifyError_RejectsIllegalStatusCode(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   int
	}{
		{"zero", 0, http.StatusInternalServerError},
		{"below range", 99, http.StatusInternalServerError},
		{"negative", -1, http.StatusInternalServerError},
		{"above range", 600, http.StatusInternalServerError},
		{"far above range", 700, http.StatusInternalServerError},
		{"valid 4xx passes through", http.StatusTeapot, http.StatusTeapot},
		{"valid 1xx boundary", 100, 100},
		{"valid 5xx boundary", 599, 599},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code := classifyError(badStatusError{tc.status})
			if status != tc.want {
				t.Errorf("classifyError status=%d, want %d", status, tc.want)
			}
			if code == "" {
				t.Error("code must never be empty")
			}
		})
	}
}

// TestServeHTTP_IllegalStatusCoderDoesNotPanic 端到端确认连接不被打挂。
func TestServeHTTP_IllegalStatusCoderDoesNotPanic(t *testing.T) {
	for _, status := range []int{0, 99, 700} {
		s := New()
		if err := s.RawHandle(http.MethodGet, "/x", func(context.Context, *Request, *Response) error {
			return badStatusError{status}
		}); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Errorf("HTTPStatus()=%d made a panic escape ServeHTTP: %v", status, rec)
				}
			}()
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("HTTPStatus()=%d yielded status %d, want 500", status, rec.Code)
			}
		}()
	}
}

// TestIsValidHTTPStatus 直测状态码校验边界。
func TestIsValidHTTPStatus(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{-1, false}, {0, false}, {99, false},
		{100, true}, {200, true}, {404, true}, {599, true},
		{600, false}, {1000, false},
	}
	for _, tc := range cases {
		if got := isValidHTTPStatus(tc.status); got != tc.want {
			t.Errorf("isValidHTTPStatus(%d)=%v, want %v", tc.status, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// S7:http.ErrAbortHandler 被吞成 500 / panic 值被丢弃
// ---------------------------------------------------------------------------

// TestErrAbortHandler_PropagatesThroughSafetyNet 锁定兜底 recover 放行 ErrAbortHandler。
//
// 标准库约定 panic(http.ErrAbortHandler) 表示"静默放弃这个响应",net/http 据此断连且
// 不打日志(ReverseProxy 依赖该约定)。收敛成 500 既篡改语义又刷屏。
func TestErrAbortHandler_PropagatesThroughSafetyNet(t *testing.T) {
	s := New()
	if err := s.RawHandle(http.MethodGet, "/a", func(context.Context, *Request, *Response) error {
		panic(http.ErrAbortHandler)
	}); err != nil {
		t.Fatal(err)
	}
	propagated := false
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				err, _ := rec.(error)
				propagated = errors.Is(err, http.ErrAbortHandler)
			}
		}()
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a", nil))
		t.Errorf("ErrAbortHandler was swallowed into status %d", rec.Code)
	}()
	if !propagated {
		t.Error("panic(http.ErrAbortHandler) must reach net/http unchanged")
	}
}

// TestErrAbortHandler_PropagatesThroughRecoveryMiddleware 锁定 Recovery 中间件同样放行。
// 它既不该把 ErrAbortHandler 记成 panic 日志,也不该写 500 覆盖调用方意图。
func TestErrAbortHandler_PropagatesThroughRecoveryMiddleware(t *testing.T) {
	onPanicCalled := false
	s := New()
	s.Use(RecoveryWith(func(PanicInfo) { onPanicCalled = true }))
	if err := s.RawHandle(http.MethodGet, "/a", func(context.Context, *Request, *Response) error {
		panic(http.ErrAbortHandler)
	}); err != nil {
		t.Fatal(err)
	}
	propagated := false
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				err, _ := rec.(error)
				propagated = errors.Is(err, http.ErrAbortHandler)
			}
		}()
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/a", nil))
	}()
	if !propagated {
		t.Error("Recovery middleware must let ErrAbortHandler through")
	}
	if onPanicCalled {
		t.Error("ErrAbortHandler is not a failure: it must not be reported to onPanic")
	}
}

// TestOrdinaryPanicStillBecomes500 确认对 ErrAbortHandler 的特判没有放过普通 panic。
func TestOrdinaryPanicStillBecomes500(t *testing.T) {
	var hookErr error
	s := New(WithErrorHook(func(_ *http.Request, _ int, err error) { hookErr = err }))
	if err := s.RawHandle(http.MethodGet, "/p", func(context.Context, *Request, *Response) error {
		panic("boom")
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rec.Code)
	}
	if !errors.Is(hookErr, ErrHandlerPanic) {
		t.Errorf("hook err=%v, want errors.Is(err, ErrHandlerPanic)", hookErr)
	}
}

// TestPanicValueReachesErrorHook 锁定 panic 原因不再被丢弃。
//
// 兜底 recover 此前写 `_ = rec` 直接丢弃 panic 值,不挂 Recovery 中间件时 onError 只见
// 一句 "handler panicked",既无原因也无类型,是生产排障黑洞。
func TestPanicValueReachesErrorHook(t *testing.T) {
	sentinel := errors.New("db connection lost")
	cases := []struct {
		name       string
		panicValue any
		wantInText string
	}{
		{"string panic", "boom", "boom"},
		{"error panic", sentinel, "db connection lost"},
		{"int panic", 42, "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hookErr error
			s := New(WithErrorHook(func(_ *http.Request, _ int, err error) { hookErr = err }))
			if err := s.RawHandle(http.MethodGet, "/p", func(context.Context, *Request, *Response) error {
				panic(tc.panicValue)
			}); err != nil {
				t.Fatal(err)
			}
			s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))

			if !errors.Is(hookErr, ErrHandlerPanic) {
				t.Fatalf("hook err=%v, want errors.Is(err, ErrHandlerPanic)", hookErr)
			}
			if !strings.Contains(hookErr.Error(), tc.wantInText) {
				t.Errorf("hook err=%q must carry the panic cause %q", hookErr.Error(), tc.wantInText)
			}
			value, ok := PanicValueOf(hookErr)
			if !ok {
				t.Fatal("PanicValueOf must extract the panic value")
			}
			if value != tc.panicValue {
				t.Errorf("PanicValueOf=%v, want %v", value, tc.panicValue)
			}
		})
	}
}

// TestPanicValueOf_NonPanicError 确认 PanicValueOf 不误报普通错误。
func TestPanicValueOf_NonPanicError(t *testing.T) {
	if _, ok := PanicValueOf(errors.New("ordinary")); ok {
		t.Error("PanicValueOf must report false for an ordinary error")
	}
	if _, ok := PanicValueOf(nil); ok {
		t.Error("PanicValueOf must report false for nil")
	}
	// 裸哨兵不携带值。
	if _, ok := PanicValueOf(ErrHandlerPanic); ok {
		t.Error("the bare sentinel carries no panic value")
	}
}
