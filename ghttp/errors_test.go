package ghttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  HTTPError
		want string
	}{
		{
			name: "without cause",
			err:  HTTPError{Status: 400, Message: "bad request"},
			want: "HTTP 400: bad request",
		},
		{
			name: "with cause",
			err:  HTTPError{Status: 500, Message: "internal error", Cause: errors.New("db connection failed")},
			want: "HTTP 500: internal error (cause: db connection failed)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("HTTPError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHTTPError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := HTTPError{Status: 500, Message: "internal error", Cause: cause}

	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Errorf("HTTPError.Unwrap() = %v, want %v", unwrapped, cause)
	}

	// 测试 errors.Is
	// Test errors.Is
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false, want true")
	}
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    HTTPError
		status int
	}{
		{"ErrBadRequest", ErrBadRequest, http.StatusBadRequest},
		{"ErrUnauthorized", ErrUnauthorized, http.StatusUnauthorized},
		{"ErrForbidden", ErrForbidden, http.StatusForbidden},
		{"ErrNotFound", ErrNotFound, http.StatusNotFound},
		{"ErrMethodNotAllowed", ErrMethodNotAllowed, http.StatusMethodNotAllowed},
		{"ErrRequestTimeout", ErrRequestTimeout, http.StatusRequestTimeout},
		{"ErrRequestEntityTooLarge", ErrRequestEntityTooLarge, http.StatusRequestEntityTooLarge},
		{"ErrUnsupportedMediaType", ErrUnsupportedMediaType, http.StatusUnsupportedMediaType},
		{"ErrInternalServerError", ErrInternalServerError, http.StatusInternalServerError},
		{"ErrServiceUnavailable", ErrServiceUnavailable, http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Status != tt.status {
				t.Errorf("%s.Status = %d, want %d", tt.name, tt.err.Status, tt.status)
			}
			if tt.err.Message == "" {
				t.Errorf("%s.Message is empty", tt.name)
			}
		})
	}
}

func TestBadRequest(t *testing.T) {
	err := BadRequest("invalid input")
	if err.Status != http.StatusBadRequest {
		t.Errorf("BadRequest().Status = %d, want %d", err.Status, http.StatusBadRequest)
	}
	if err.Message != "invalid input" {
		t.Errorf("BadRequest().Message = %q, want %q", err.Message, "invalid input")
	}
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("missing token")
	if err.Status != http.StatusUnauthorized {
		t.Errorf("Unauthorized().Status = %d, want %d", err.Status, http.StatusUnauthorized)
	}
	if err.Message != "missing token" {
		t.Errorf("Unauthorized().Message = %q, want %q", err.Message, "missing token")
	}
}

func TestForbidden(t *testing.T) {
	err := Forbidden("access denied")
	if err.Status != http.StatusForbidden {
		t.Errorf("Forbidden().Status = %d, want %d", err.Status, http.StatusForbidden)
	}
	if err.Message != "access denied" {
		t.Errorf("Forbidden().Message = %q, want %q", err.Message, "access denied")
	}
}

func TestNotFound(t *testing.T) {
	err := NotFound("user not found")
	if err.Status != http.StatusNotFound {
		t.Errorf("NotFound().Status = %d, want %d", err.Status, http.StatusNotFound)
	}
	if err.Message != "user not found" {
		t.Errorf("NotFound().Message = %q, want %q", err.Message, "user not found")
	}
}

func TestInternalServerError(t *testing.T) {
	err := InternalServerError("database error")
	if err.Status != http.StatusInternalServerError {
		t.Errorf("InternalServerError().Status = %d, want %d", err.Status, http.StatusInternalServerError)
	}
	if err.Message != "database error" {
		t.Errorf("InternalServerError().Message = %q, want %q", err.Message, "database error")
	}
}

func TestHTTPError_IsComparable(t *testing.T) {
	// 测试预定义错误可以被比较
	// Test that predefined errors can be compared
	err := ErrBadRequest
	if err.Status != http.StatusBadRequest {
		t.Errorf("ErrBadRequest comparison failed")
	}

	// 测试 errors.Is 与自定义 HTTPError
	// Test errors.Is with custom HTTPError
	customErr := HTTPError{Status: 400, Message: "custom", Cause: ErrBadRequest}
	if !errors.Is(customErr, ErrBadRequest) {
		t.Errorf("errors.Is(customErr, ErrBadRequest) = false, want true")
	}
}

func TestHTTPError_ResponseWriting(t *testing.T) {
	// 测试 HTTPError 可以被正确写入响应
	// Test that HTTPError can be correctly written to response
	err := BadRequest("invalid parameter")

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	resp.WriteHeader(err.Status)
	resp.Write([]byte(err.Message))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	if rec.Body.String() != "invalid parameter" {
		t.Errorf("Response body = %q, want %q", rec.Body.String(), "invalid parameter")
	}
}
