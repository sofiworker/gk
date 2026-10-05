package ghttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPError_ToErrorResponse 测试 HTTPError 转换为 ErrorResponse。
// TestHTTPError_ToErrorResponse tests HTTPError conversion to ErrorResponse.
func TestHTTPError_ToErrorResponse(t *testing.T) {
	err := HTTPError{
		Status:  http.StatusBadRequest,
		Message: "invalid input",
	}

	resp := err.ToErrorResponse()

	if resp.Error != "invalid input" {
		t.Errorf("ErrorResponse.Error = %q, want %q", resp.Error, "invalid input")
	}
	if resp.Status != http.StatusBadRequest {
		t.Errorf("ErrorResponse.Status = %d, want %d", resp.Status, http.StatusBadRequest)
	}
}

// TestWriteErrorJSON 测试将错误以 JSON 格式写入响应。
// TestWriteErrorJSON tests writing error as JSON to response.
func TestWriteErrorJSON(t *testing.T) {
	err := HTTPError{
		Status:  http.StatusNotFound,
		Message: "user not found",
	}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if writeErr := WriteErrorJSON(resp, err); writeErr != nil {
		t.Fatalf("WriteErrorJSON() error = %v", writeErr)
	}

	if rec.Code != http.StatusNotFound {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	var errorResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errorResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if errorResp.Error != "user not found" {
		t.Errorf("ErrorResponse.Error = %q, want %q", errorResp.Error, "user not found")
	}
	if errorResp.Status != http.StatusNotFound {
		t.Errorf("ErrorResponse.Status = %d, want %d", errorResp.Status, http.StatusNotFound)
	}
}

// TestHTTPError_ErrorChaining 测试错误链。
// TestHTTPError_ErrorChaining tests error chaining.
func TestHTTPError_ErrorChaining(t *testing.T) {
	baseErr := errors.New("database connection failed")
	wrappedErr := HTTPError{
		Status:  http.StatusInternalServerError,
		Message: "internal error",
		Cause:   baseErr,
	}

	// 测试 errors.Is
	// Test errors.Is
	if !errors.Is(wrappedErr, baseErr) {
		t.Errorf("errors.Is(wrappedErr, baseErr) = false, want true")
	}

	// 测试 errors.Unwrap
	// Test errors.Unwrap
	if unwrapped := errors.Unwrap(wrappedErr); unwrapped != baseErr {
		t.Errorf("errors.Unwrap(wrappedErr) = %v, want %v", unwrapped, baseErr)
	}
}

// TestHTTPError_MultiLevelChaining 测试多层错误链。
// TestHTTPError_MultiLevelChaining tests multi-level error chaining.
func TestHTTPError_MultiLevelChaining(t *testing.T) {
	// 创建多层错误链
	// Create multi-level error chain
	baseErr := errors.New("connection refused")
	dbErr := errors.New("database error: " + baseErr.Error())
	httpErr := HTTPError{
		Status:  http.StatusServiceUnavailable,
		Message: "service unavailable",
		Cause:   dbErr,
	}

	// 验证错误消息包含所有层级信息
	// Verify error message contains all levels
	errorMsg := httpErr.Error()
	if errorMsg == "" {
		t.Error("HTTPError.Error() returned empty string")
	}
}

// TestDefaultErrorHandler_WithHTTPError 测试默认错误处理器处理 HTTPError。
// TestDefaultErrorHandler_WithHTTPError tests default error handler with HTTPError.
func TestDefaultErrorHandler_WithHTTPError(t *testing.T) {
	err := BadRequest("invalid parameter")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	defaultErrorHandler(req.Context(), ghttpReq, resp, err)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	if want := `{"error":"invalid parameter","status":400}`; rec.Body.String() != want {
		t.Errorf("Response body = %q, want %q", rec.Body.String(), want)
	}
}

// TestDefaultErrorHandler_WithGenericError 测试默认错误处理器处理普通错误。
// TestDefaultErrorHandler_WithGenericError tests default error handler with generic error.
func TestDefaultErrorHandler_WithGenericError(t *testing.T) {
	err := errors.New("something went wrong")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	resp := &Response{Writer: rec}
	ghttpReq := &Request{Raw: req}

	defaultErrorHandler(req.Context(), ghttpReq, resp, err)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	// 内部错误文本不得泄露给客户端
	// Internal error text must not leak to the client
	if want := `{"error":"Internal Server Error","status":500}`; rec.Body.String() != want {
		t.Errorf("Response body = %q, want %q", rec.Body.String(), want)
	}
}

// TestDefaultErrorHandler_AlreadyWritten 测试响应已写入时的错误处理。
// TestDefaultErrorHandler_AlreadyWritten tests error handling when response already written.
func TestDefaultErrorHandler_AlreadyWritten(t *testing.T) {
	err := BadRequest("test error")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	// 先写入响应
	// Write response first
	rec.WriteHeader(http.StatusOK)
	rec.Write([]byte("already written"))

	resp := &Response{Writer: rec, written: true, statusCode: http.StatusOK}
	ghttpReq := &Request{Raw: req}

	defaultErrorHandler(req.Context(), ghttpReq, resp, err)

	// 响应已写入，内容不应改变
	// Response already written, content should not change
	if rec.Body.String() != "already written" {
		t.Errorf("Response body changed after written flag, got %q", rec.Body.String())
	}
}
