package ghttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLimitBody_ContentLengthExceeded 验证 Content-Length 超限时中间件返回 413 哨兵错误。
//
// 断言从原先的 resp.Status()==413 改为"不提交响应 + 返回 ErrRequestEntityTooLarge":
// 旧行为在中间件里手动 WriteHeader(413) 并返回 ErrInvalidInput(被分类成 400),
// 客户端、onError 钩子、响应体三方不一致。中间件现在只返回错误、不碰响应,
// 状态码由统一错误链单点决定,故这里改断言错误语义与"响应未提交"。
func TestLimitBody_ContentLengthExceeded(t *testing.T) {
	mw := LimitBody(100) // max 100 bytes
	inner := false
	handler := mw(func(ctx context.Context, req *Request, resp *Response) error {
		inner = true
		return nil
	})

	rec := httptest.NewRecorder()
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))}
	req.Header.Set("Content-Length", "200")
	resp := &Response{ResponseWriter: rec}

	err := handler(context.Background(), req, resp)
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	if !errors.Is(err, ErrRequestEntityTooLarge) {
		t.Errorf("err = %v, want errors.Is(err, ErrRequestEntityTooLarge)", err)
	}
	if errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, must NOT be ErrInvalidInput (that classified to 400)", err)
	}
	// 中间件必须把响应完全交给错误链:任何提前提交都会绕过统一 JSON 错误体。
	if resp.Written() {
		t.Errorf("resp.Written() = true, want false (middleware must not commit the response)")
	}
	if status, code := classifyError(err); status != http.StatusRequestEntityTooLarge || code != "request_entity_too_large" {
		t.Errorf("classifyError = (%d,%q), want (413,%q)", status, code, "request_entity_too_large")
	}
	if inner {
		t.Error("terminal handler must not run for an oversized body")
	}
}

// TestLimitBody_NoCLWithMaxBytesReader 验证无 Content-Length 时 Body 被包成
// MaxBytesReader，读超限返回 *http.MaxBytesError,且该错误可被分类成 413。
//
// 原用例全是 t.Log 无任何断言(REVIEW 已指出),这里补成真断言:既守住
// MaxBytesReader 确实被挂上,也守住其错误类型仍是错误链 413 分支的输入。
func TestLimitBody_NoCLWithMaxBytesReader(t *testing.T) {
	mw := LimitBody(100) // max 100 bytes
	var readErr error
	var readN int
	handler := mw(func(ctx context.Context, req *Request, resp *Response) error {
		var b [200]byte
		readN, readErr = io.ReadFull(req.Body, b[:])
		return readErr
	})

	rec := httptest.NewRecorder()
	largePayload := strings.Repeat("x", 200)
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(largePayload))}
	// httptest.NewRequest 只填 req.ContentLength、不设 Content-Length 头,故 CL 分支
	// 不会命中,请求必然走到 MaxBytesReader；显式删头是为了不依赖该实现细节。
	req.Header.Del("Content-Length")

	err := handler(context.Background(), req, &Response{ResponseWriter: rec})
	if err == nil {
		t.Fatal("expected error: reading 200 bytes through a 100-byte MaxBytesReader must fail")
	}
	var mbe *http.MaxBytesError
	if !errors.As(err, &mbe) {
		t.Fatalf("err = %v (%T), want an *http.MaxBytesError in the chain", err, err)
	}
	if mbe.Limit != 100 {
		t.Errorf("MaxBytesError.Limit = %d, want 100", mbe.Limit)
	}
	// 读到的字节数不得超过上限:证明 Body 真的被截断,而非原样透传。
	if readN > 100 {
		t.Errorf("read %d bytes, want <= 100 (body must be capped)", readN)
	}
	if status, code := classifyError(err); status != http.StatusRequestEntityTooLarge || code != "request_entity_too_large" {
		t.Errorf("classifyError = (%d,%q), want (413,%q)", status, code, "request_entity_too_large")
	}
}

// TestLimitBody_ValidSize 验证未超限时正常通过。
func TestLimitBody_ValidSize(t *testing.T) {
	mw := LimitBody(200)
	handler := mw(func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	})

	rec := httptest.NewRecorder()
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small payload"))}
	req.Header.Set("Content-Length", "5")

	err := handler(context.Background(), req, &Response{ResponseWriter: rec})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != 0 && rec.Code != http.StatusOK {
		t.Errorf("expected ok, got %d", rec.Code)
	}
}
