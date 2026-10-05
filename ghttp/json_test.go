package ghttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sofiworker/gk/ghttp/wire"
)

func TestWriteJSON_Success(t *testing.T) {
	type User struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}

	user := User{ID: 1, Name: "Alice"}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := WriteJSON(resp, http.StatusOK, user); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	var got User
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if got.ID != user.ID || got.Name != user.Name {
		t.Errorf("Response body = %+v, want %+v", got, user)
	}
}

func TestWriteJSON_Nil(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := WriteJSON(resp, http.StatusNoContent, nil); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	if rec.Code != http.StatusNoContent {
		t.Errorf("Response status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestWriteJSON_Slice(t *testing.T) {
	users := []string{"Alice", "Bob", "Charlie"}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := WriteJSON(resp, http.StatusOK, users); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(got) != len(users) {
		t.Errorf("Response length = %d, want %d", len(got), len(users))
	}
}

func TestWriteJSON_Map(t *testing.T) {
	data := map[string]any{
		"status": "ok",
		"count":  42,
	}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := WriteJSON(resp, http.StatusOK, data); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if got["status"] != "ok" {
		t.Errorf("Response status field = %v, want 'ok'", got["status"])
	}
}

func TestReadJSON_Success(t *testing.T) {
	type CreateUserRequest struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ghttpReq := &Request{Raw: req}

	var result CreateUserRequest
	if err := ReadJSON(ghttpReq, &result); err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}

	if result.Name != "Alice" {
		t.Errorf("Name = %q, want %q", result.Name, "Alice")
	}
	if result.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", result.Email, "alice@example.com")
	}
}

func TestReadJSON_MissingBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/users", nil)
	ghttpReq := &Request{Raw: req}

	var result map[string]any
	err := ReadJSON(ghttpReq, &result)
	if err == nil {
		t.Fatal("ReadJSON() expected error, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusBadRequest {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusBadRequest)
	}
}

func TestReadJSON_EmptyBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/users", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")

	ghttpReq := &Request{Raw: req}

	var result map[string]any
	err := ReadJSON(ghttpReq, &result)
	if err == nil {
		t.Fatal("ReadJSON() expected error, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusBadRequest {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusBadRequest)
	}
}

func TestReadJSON_InvalidJSON(t *testing.T) {
	body := `{"name":"Alice","email":}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ghttpReq := &Request{Raw: req}

	var result map[string]any
	err := ReadJSON(ghttpReq, &result)
	if err == nil {
		t.Fatal("ReadJSON() expected error, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusBadRequest {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusBadRequest)
	}
}

func TestReadJSON_UnsupportedMediaType(t *testing.T) {
	body := `<user><name>Alice</name></user>`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")

	ghttpReq := &Request{Raw: req}

	var result map[string]any
	err := ReadJSON(ghttpReq, &result)
	if err == nil {
		t.Fatal("ReadJSON() expected error, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusUnsupportedMediaType {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusUnsupportedMediaType)
	}
}

func TestReadJSON_DisallowUnknownFields(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	body := `{"name":"Alice","unknown_field":"value"}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ghttpReq := &Request{Raw: req}

	var result User
	err := ReadJSON(ghttpReq, &result)
	if err == nil {
		t.Fatal("ReadJSON() expected error for unknown field, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusBadRequest {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusBadRequest)
	}
}

func TestReadJSON_MultipleObjects(t *testing.T) {
	body := `{"name":"Alice"}{"name":"Bob"}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ghttpReq := &Request{Raw: req}

	var result map[string]any
	err := ReadJSON(ghttpReq, &result)
	if err == nil {
		t.Fatal("ReadJSON() expected error for multiple objects, got nil")
	}

	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusBadRequest {
		t.Errorf("HTTPError.Status = %d, want %d", httpErr.Status, http.StatusBadRequest)
	}
}

func TestReadJSON_ContentTypeVariants(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		shouldPass  bool
	}{
		{"exact match", "application/json", true},
		{"with charset", "application/json; charset=utf-8", true},
		{"with charset and space", "application/json ; charset=utf-8", true},
		{"empty", "", true}, // 允许空 Content-Type
		{"text/plain", "text/plain", false},
		{"application/xml", "application/xml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"name":"Alice"}`
			req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			ghttpReq := &Request{Raw: req}

			var result map[string]any
			err := ReadJSON(ghttpReq, &result)

			if tt.shouldPass {
				if err != nil {
					t.Errorf("ReadJSON() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("ReadJSON() expected error, got nil")
				}
			}
		})
	}
}

// TestReadJSON_ErrorMapping 测试 wire 解码错误到 HTTPError 的映射及错误链。
// TestReadJSON_ErrorMapping tests mapping wire decode errors to HTTPError and the error chain.
func TestReadJSON_ErrorMapping(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name          string
		body          string
		limit         int64
		wantStatus    int
		wantFormatErr bool
	}{
		{"trailing top-level bracket", `{"name":"a"}]`, 0, http.StatusBadRequest, true},
		{"second value", `{"name":"a"}{"name":"b"}`, 0, http.StatusBadRequest, true},
		{"syntax error", `{"name":`, 0, http.StatusBadRequest, true},
		{"over limit", `{"name":"` + strings.Repeat("x", 64) + `"}`, 16, http.StatusRequestEntityTooLarge, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			raw.Header.Set("Content-Type", "application/json")
			if tt.limit > 0 {
				raw.Body = http.MaxBytesReader(httptest.NewRecorder(), raw.Body, tt.limit)
			}

			var p payload
			err := ReadJSON(&Request{Raw: raw}, &p)

			if got := wire.StatusOf(err); got != tt.wantStatus {
				t.Fatalf("status = %d, want %d (err=%v)", got, tt.wantStatus, err)
			}
			if got := errors.Is(err, wire.ErrInvalidFormat); got != tt.wantFormatErr {
				t.Errorf("errors.Is(ErrInvalidFormat) = %v, want %v", got, tt.wantFormatErr)
			}
		})
	}
}

// TestReadJSON_TruncatedBody 测试声明了长度却没有内容时判为 400。
// TestReadJSON_TruncatedBody tests that a declared length with no content is a 400.
func TestReadJSON_TruncatedBody(t *testing.T) {
	raw := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	raw.ContentLength = 10
	var v map[string]any
	err := ReadJSON(&Request{Raw: raw}, &v)
	if wire.StatusOf(err) != http.StatusBadRequest || !errors.Is(err, wire.ErrInvalidFormat) {
		t.Fatalf("err = %v, want 400 wrapping ErrInvalidFormat", err)
	}
}

func TestWriteJSON_ReadJSON_RoundTrip(t *testing.T) {
	type User struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	original := User{ID: 42, Name: "Alice", Email: "alice@example.com"}

	// 写入 JSON
	// Write JSON
	var buf bytes.Buffer
	rec := httptest.NewRecorder()
	rec.Body = &buf
	resp := &Response{Writer: rec}

	if err := WriteJSON(resp, http.StatusOK, original); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	// 读取 JSON
	// Read JSON
	req := httptest.NewRequest("POST", "/users", bytes.NewReader(rec.Body.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	ghttpReq := &Request{Raw: req}

	var result User
	if err := ReadJSON(ghttpReq, &result); err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}

	if result != original {
		t.Errorf("Round-trip failed: got %+v, want %+v", result, original)
	}
}
