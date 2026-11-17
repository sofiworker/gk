package ghttp

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

// BenchmarkWriteJSON 基准测试 JSON 编码性能。
// BenchmarkWriteJSON benchmarks JSON encoding performance.
func BenchmarkWriteJSON(b *testing.B) {
	type User struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	user := User{ID: 1, Name: "Alice", Email: "alice@example.com"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		resp := &Response{Writer: rec}
		_ = WriteJSON(resp, 200, user)
	}
}

// BenchmarkReadJSON 基准测试 JSON 解码性能。
// BenchmarkReadJSON benchmarks JSON decoding performance.
func BenchmarkReadJSON(b *testing.B) {
	type User struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := `{"id":1,"name":"Alice","email":"alice@example.com"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		ghttpReq := &Request{Raw: req}

		var user User
		_ = ReadJSON(ghttpReq, &user)
	}
}

// BenchmarkEncodeJSON 基准测试泛型 JSON 编码性能。
// BenchmarkEncodeJSON benchmarks generic JSON encoding performance.
func BenchmarkEncodeJSON(b *testing.B) {
	type User struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	user := User{ID: 1, Name: "Alice", Email: "alice@example.com"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		resp := &Response{Writer: rec}
		_ = EncodeJSON(resp, user)
	}
}

// BenchmarkDecodeJSON 基准测试泛型 JSON 解码性能。
// BenchmarkDecodeJSON benchmarks generic JSON decoding performance.
func BenchmarkDecodeJSON(b *testing.B) {
	type User struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := `{"id":1,"name":"Alice","email":"alice@example.com"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		ghttpReq := &Request{Raw: req}

		_, _ = DecodeJSON[User](ghttpReq)
	}
}
