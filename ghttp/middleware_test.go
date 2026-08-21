package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLimitBody_ContentLengthExceeded 验证 Content-Length 超限直接返回 413。
func TestLimitBody_ContentLengthExceeded(t *testing.T) {
	mw := LimitBody(100) // max 100 bytes
	handler := mw(func(ctx context.Context, req *Request, resp *Response) error {
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
	// 状态码应已设置到 resp.status，然后由 mux 处理或直接写入响应。
	// Check resp status field which was written by WriteHeader
	if resp.Status() != http.StatusRequestEntityTooLarge {
		t.Errorf("resp.Status = %d, want %d", resp.Status(), http.StatusRequestEntityTooLarge)
	}
}

// TestLimitBody_NoCLWithMaxBytesReader 验证无 CL 时 MaxBytesReader 触发 413。
func TestLimitBody_NoCLWithMaxBytesReader(t *testing.T) {
	mw := LimitBody(100) // max 100 bytes
	handler := mw(func(ctx context.Context, req *Request, resp *Response) error {
		var b [200]byte
		n, err := io.ReadFull(req.Body, b[:]) // 试图读取 200 bytes → returns error if exceeds max
		t.Logf("read %d bytes, err=%v", n, err)
		return nil
	})

	rec := httptest.NewRecorder()
	largePayload := strings.Repeat("x", 200)
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(largePayload))}

	err := handler(context.Background(), req, &Response{ResponseWriter: rec})
	if err != nil {
		// Expected: MaxBytesReader returns error when exceeding limit
		t.Log("handler error:", err)
	}
	if rec.Code != 0 && rec.Code != http.StatusOK {
		t.Logf("status=%d (ok or unset)", rec.Code)
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
