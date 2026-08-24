package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// errChainOut 与 errChainBody 是错误链测试用的最小类型。
type errChainOut struct {
	OK bool `json:"ok"`
}
type errChainBody struct {
	Name string `json:"name"`
}
type errChainPath struct {
	ID int `path:"id"`
}

// bizStatusErr 是实现 StatusCoder 的业务错误,携带自定义状态码与内部细节。
type bizStatusErr struct {
	status int
	detail string
}

func (e bizStatusErr) Error() string   { return e.detail }
func (e bizStatusErr) HTTPStatus() int { return e.status }

func newErrChainServer(t *testing.T, opts ...Option) *Server {
	t.Helper()
	s := New(opts...)
	GetParams(s, "/u/{id}", JSON[errChainOut](), func(_ context.Context, _ errChainPath) (errChainOut, error) {
		return errChainOut{}, bizStatusErr{status: http.StatusNotFound, detail: "user 42 missing in table users"}
	})
	GetParams(s, "/conflict/{id}", JSON[errChainOut](), func(_ context.Context, _ errChainPath) (errChainOut, error) {
		return errChainOut{}, bizStatusErr{status: http.StatusConflict, detail: "row already exists: secret"}
	})
	GetParams(s, "/sentinel/{id}", JSON[errChainOut](), func(_ context.Context, _ errChainPath) (errChainOut, error) {
		return errChainOut{}, ErrInvalidInput
	})
	PostBody(s, "/create", JSONBody(), JSON[errChainOut](), func(_ context.Context, _ errChainBody) (errChainOut, error) {
		return errChainOut{OK: true}, nil
	})
	s.RawHandle(http.MethodGet, "/boom", func(_ context.Context, _ *Request, _ *Response) error {
		panic("kaboom internal secret")
	})
	return s
}

func doReq(s *Server, method, path, ct, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// TestErrorChainStatusMapping 覆盖 StatusCoder 与框架哨兵的状态码映射。
func TestErrorChainStatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		ct         string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"biz statuscoder 404", "GET", "/u/7", "", "", http.StatusNotFound, "not_found"},
		{"biz statuscoder 409", "GET", "/conflict/7", "", "", http.StatusConflict, "conflict"},
		{"sentinel invalid input 400", "GET", "/sentinel/7", "", "", http.StatusBadRequest, "invalid_input"},
		{"path parse error 400", "GET", "/u/notint", "", "", http.StatusBadRequest, "invalid_input"},
		{"unsupported media type 415", "POST", "/create", "text/plain", `{"name":"x"}`, http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"bad json body 400", "POST", "/create", "application/json", `{bad`, http.StatusBadRequest, "invalid_input"},
		{"panic 500", "GET", "/boom", "", "", http.StatusInternalServerError, "internal"},
		{"not found 404", "GET", "/does-not-exist", "", "", http.StatusNotFound, "not_found"},
		{"method not allowed 405", "DELETE", "/create", "", "", http.StatusMethodNotAllowed, "method_not_allowed"},
	}
	s := newErrChainServer(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doReq(s, tt.method, tt.path, tt.ct, tt.body)
			if w.Code != tt.wantStatus {
				t.Fatalf("status: got %d want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("content-type: got %q want json", ct)
			}
			if !strings.Contains(w.Body.String(), `"code":"`+tt.wantCode+`"`) {
				t.Errorf("code: body %s does not contain code %q", w.Body.String(), tt.wantCode)
			}
		})
	}
}

// TestErrorChainSanitizeByDefault 验证默认脱敏:内部细节不出现在响应体。
func TestErrorChainSanitizeByDefault(t *testing.T) {
	s := newErrChainServer(t)
	leaks := []struct {
		name   string
		method string
		path   string
		ct     string
		body   string
		secret string
	}{
		{"biz detail", "GET", "/u/7", "", "", "table users"},
		{"conflict detail", "GET", "/conflict/7", "", "", "secret"},
		{"strconv detail", "GET", "/u/notint", "", "", "strconv"},
		{"json parser detail", "POST", "/create", "application/json", `{bad`, "invalid character"},
		{"panic detail", "GET", "/boom", "", "", "kaboom"},
	}
	for _, tt := range leaks {
		t.Run(tt.name, func(t *testing.T) {
			w := doReq(s, tt.method, tt.path, tt.ct, tt.body)
			if strings.Contains(w.Body.String(), tt.secret) {
				t.Errorf("LEAK: body %s contains secret %q", w.Body.String(), tt.secret)
			}
		})
	}
}

// TestErrorChainExposeDetails 验证 WithExposeErrorDetails(true) 时回传细节。
func TestErrorChainExposeDetails(t *testing.T) {
	s := newErrChainServer(t, WithExposeErrorDetails(true))
	w := doReq(s, "GET", "/u/7", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status got %d want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "table users") {
		t.Errorf("expose: body %s should contain detail", w.Body.String())
	}
}

// TestErrorChainStrictContentType 验证严格/宽松 Content-Type 两种模式。
func TestErrorChainStrictContentType(t *testing.T) {
	// 严格(默认):错误 CT → 415
	strict := newErrChainServer(t)
	if w := doReq(strict, "POST", "/create", "text/plain", `{"name":"x"}`); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("strict: got %d want 415", w.Code)
	}
	// 正确 CT → 200
	if w := doReq(strict, "POST", "/create", "application/json", `{"name":"x"}`); w.Code != http.StatusOK {
		t.Errorf("strict ok: got %d want 200", w.Code)
	}
	// 带 charset 参数的 CT 也应通过(media-type 比对)
	if w := doReq(strict, "POST", "/create", "application/json; charset=utf-8", `{"name":"x"}`); w.Code != http.StatusOK {
		t.Errorf("strict charset: got %d want 200", w.Code)
	}
	// 缺省 CT 放行(交解码器)
	if w := doReq(strict, "POST", "/create", "", `{"name":"x"}`); w.Code != http.StatusOK {
		t.Errorf("strict empty ct: got %d want 200", w.Code)
	}

	// 宽松:错误 CT 也放行解码
	lenient := New(WithStrictContentType(false))
	PostBody(lenient, "/create", JSONBody(), JSON[errChainOut](), func(_ context.Context, _ errChainBody) (errChainOut, error) {
		return errChainOut{OK: true}, nil
	})
	if w := doReq(lenient, "POST", "/create", "text/plain", `{"name":"x"}`); w.Code != http.StatusOK {
		t.Errorf("lenient: got %d want 200 (body=%s)", w.Code, w.Body.String())
	}
}

// TestErrorChainCustomRenderer 验证 WithErrorRenderer 可替换错误体。
func TestErrorChainCustomRenderer(t *testing.T) {
	s := New(WithErrorRenderer(rendererFunc(func(resp *Response, status int, code, message string) {
		resp.Header().Set("Content-Type", "text/plain")
		resp.WriteHeader(status)
		_, _ = resp.WriteString("ERR:" + code)
	})))
	GetParams(s, "/u/{id}", JSON[errChainOut](), func(_ context.Context, _ errChainPath) (errChainOut, error) {
		return errChainOut{}, ErrInvalidInput
	})
	w := doReq(s, "GET", "/u/1", "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status got %d want 400", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "ERR:invalid_input" {
		t.Errorf("custom renderer body got %q", got)
	}
}

type rendererFunc func(resp *Response, status int, code, message string)

func (f rendererFunc) RenderError(resp *Response, status int, code, message string) {
	f(resp, status, code, message)
}

// TestErrorChainErrorHook 验证 WithErrorHook 观测钩子被调用且状态码正确。
func TestErrorChainErrorHook(t *testing.T) {
	var gotStatus int
	var gotErr error
	s := New(WithErrorHook(func(_ *http.Request, status int, err error) {
		gotStatus = status
		gotErr = err
	}))
	GetParams(s, "/u/{id}", JSON[errChainOut](), func(_ context.Context, _ errChainPath) (errChainOut, error) {
		return errChainOut{}, ErrInvalidInput
	})
	_ = doReq(s, "GET", "/u/1", "", "")
	if gotStatus != http.StatusBadRequest {
		t.Errorf("hook status got %d want 400", gotStatus)
	}
	if !errors.Is(gotErr, ErrInvalidInput) {
		t.Errorf("hook err got %v want ErrInvalidInput", gotErr)
	}
}

// TestCustomNotFoundMethodNotAllowed 验证自定义 404/405 处理器。
func TestCustomNotFoundMethodNotAllowed(t *testing.T) {
	s := New(
		WithNotFoundHandler(func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusNotFound)
			_, _ = resp.WriteString("custom-404")
			return nil
		}),
		WithMethodNotAllowedHandler(func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = resp.WriteString("custom-405")
			return nil
		}),
	)
	PostBody(s, "/create", JSONBody(), JSON[errChainOut](), func(_ context.Context, _ errChainBody) (errChainOut, error) {
		return errChainOut{OK: true}, nil
	})
	if w := doReq(s, "GET", "/nope", "", ""); w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != "custom-404" {
		t.Errorf("custom 404: code=%d body=%q", w.Code, w.Body.String())
	}
	w := doReq(s, "DELETE", "/create", "", "")
	if w.Code != http.StatusMethodNotAllowed || strings.TrimSpace(w.Body.String()) != "custom-405" {
		t.Errorf("custom 405: code=%d body=%q", w.Code, w.Body.String())
	}
	if allow := w.Header().Get("Allow"); allow != "POST" {
		t.Errorf("custom 405 Allow header: got %q want POST", allow)
	}
}

// TestClassifyError 直接单测错误分类逻辑(不经 HTTP)。
func TestClassifyError(t *testing.T) {
	var maxBytes *http.MaxBytesError
	_ = maxBytes
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"statuscoder", bizStatusErr{status: 422, detail: "x"}, 422, "unprocessable_entity"},
		{"validation", ErrValidation, 400, "validation_failed"},
		{"missing required", ErrMissingRequired, 400, "missing_required"},
		{"invalid input", ErrInvalidInput, 400, "invalid_input"},
		{"invalid path", ErrInvalidRequestPath, 400, "invalid_input"},
		{"unsupported media", ErrUnsupportedMediaType, 415, "unsupported_media_type"},
		{"entity too large", ErrRequestEntityTooLarge, 413, "request_entity_too_large"},
		{"panic", ErrHandlerPanic, 500, "internal"},
		{"wrapped invalid input", errors.New("wrap: " + ErrInvalidInput.Error()), 500, "internal"},
		{"unknown", errors.New("random"), 500, "internal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code := classifyError(tt.err)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Errorf("classify(%v) = (%d,%q) want (%d,%q)", tt.err, status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

// TestAppendJSONStringMatchesStdlib 保护手写 JSON 转义:必须与标准库 json.Marshal 一致。
func TestAppendJSONStringMatchesStdlib(t *testing.T) {
	cases := []string{
		"", "simple", `with "quotes"`, `back\slash`, "tab\tnew\nline\r",
		"ctrl\x00\x01\x1f", `mix: "a"\b` + "\n", "unicode 世界 ok",
		ErrInvalidInput.Error(),
	}
	for _, s := range cases {
		got := string(appendJSONString(nil, s))
		want, _ := json.Marshal(s)
		if got != string(want) {
			t.Errorf("appendJSONString(%q) = %s want %s", s, got, want)
		}
	}
}
