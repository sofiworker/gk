package ghttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exercise buffer reuse with large, empty and escaped values concurrently.
func TestJSONOutputMatchesMarshal(t *testing.T) {
	values := []struct {
		name string
		data any
	}{
		{"nil", nil},
		{"escaped", "<script>\n\u2028&"},
		{"map", map[string]any{"z": 2, "a": "first"}},
		{"large", strings.Repeat("x", 128<<10)},
		{"empty", ""},
	}
	for _, tc := range values {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want, err := json.Marshal(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			for range 20 {
				rec := httptest.NewRecorder()
				if err := WriteJSON(&Response{Writer: rec}, http.StatusCreated, tc.data); err != nil {
					t.Fatal(err)
				}
				if rec.Code != http.StatusCreated || rec.Body.String() != string(want) {
					t.Fatalf("status=%d, body differs from Marshal", rec.Code)
				}
			}
		})
	}
}

func TestJSONOutputEncodeFailureDoesNotCommit(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}
	// The failure follows a valid field so a partially encoded buffer must not leak.
	bad := struct {
		First string
		Bad   chan int
	}{First: "valid", Bad: make(chan int)}
	if err := WriteJSON(resp, http.StatusOK, bad); err == nil {
		t.Fatal("expected encoding error")
	}
	if resp.Written() || rec.Body.Len() != 0 || len(rec.Header()) != 0 {
		t.Fatal("encoding failure committed a partial response")
	}
	if err := WriteJSON(resp, http.StatusInternalServerError, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != "null" {
		t.Fatal("failed encoding corrupted the next response")
	}
}
