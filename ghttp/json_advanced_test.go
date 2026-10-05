package ghttp

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

// TestDecodeJSON_Generic 测试泛型 JSON 解码。
// TestDecodeJSON_Generic tests generic JSON decoding.
func TestDecodeJSON_Generic(t *testing.T) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	ghttpReq := &Request{Raw: req}

	user, err := DecodeJSON[User](ghttpReq)
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}

	if user.Name != "Alice" {
		t.Errorf("User.Name = %q, want %q", user.Name, "Alice")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("User.Email = %q, want %q", user.Email, "alice@example.com")
	}
}

// TestDecodeJSON_Pointer 测试指针类型的解码。
// TestDecodeJSON_Pointer tests decoding pointer types.
func TestDecodeJSON_Pointer(t *testing.T) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := `{"name":"Bob","email":"bob@example.com"}`
	req := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	ghttpReq := &Request{Raw: req}

	user, err := DecodeJSON[*User](ghttpReq)
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}

	if user == nil {
		t.Fatal("DecodeJSON() returned nil")
	}

	if user.Name != "Bob" {
		t.Errorf("User.Name = %q, want %q", user.Name, "Bob")
	}
}

// TestEncodeJSON_Generic 测试泛型 JSON 编码。
// TestEncodeJSON_Generic tests generic JSON encoding.
func TestEncodeJSON_Generic(t *testing.T) {
	type User struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}

	user := User{ID: 1, Name: "Alice"}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := EncodeJSON(resp, user); err != nil {
		t.Fatalf("EncodeJSON() error = %v", err)
	}

	if rec.Code != 200 {
		t.Errorf("Response status = %d, want 200", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}
}

// TestEncodeJSONWithStatus 测试带状态码的 JSON 编码。
// TestEncodeJSONWithStatus tests JSON encoding with status code.
func TestEncodeJSONWithStatus(t *testing.T) {
	type ResponseData struct {
		Message string `json:"message"`
	}

	data := ResponseData{Message: "created"}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := EncodeJSONWithStatus(resp, 201, data); err != nil {
		t.Fatalf("EncodeJSONWithStatus() error = %v", err)
	}

	if rec.Code != 201 {
		t.Errorf("Response status = %d, want 201", rec.Code)
	}
}

// TestEncodeJSON_Slice 测试编码切片。
// TestEncodeJSON_Slice tests encoding slices.
func TestEncodeJSON_Slice(t *testing.T) {
	users := []string{"Alice", "Bob", "Charlie"}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := EncodeJSON(resp, users); err != nil {
		t.Fatalf("EncodeJSON() error = %v", err)
	}

	if rec.Code != 200 {
		t.Errorf("Response status = %d, want 200", rec.Code)
	}
}

// TestEncodeJSON_Map 测试编码 Map。
// TestEncodeJSON_Map tests encoding maps.
func TestEncodeJSON_Map(t *testing.T) {
	data := map[string]interface{}{
		"status": "ok",
		"count":  42,
	}

	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}

	if err := EncodeJSON(resp, data); err != nil {
		t.Fatalf("EncodeJSON() error = %v", err)
	}

	if rec.Code != 200 {
		t.Errorf("Response status = %d, want 200", rec.Code)
	}
}
