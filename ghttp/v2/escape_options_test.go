package v2

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEscapeOptionsAreIndependent(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	for _, tc := range []struct {
		name, body, media string
		opts              []Option
		status            int
	}{
		{"default media", `{}`, "text/plain", nil, 415},
		{"skip media", `{}`, "text/plain", []Option{WithoutContentTypeCheck()}, 200},
		{"skip media still validates JSON", `{`, "text/plain", []Option{WithoutContentTypeCheck()}, 400},
		{"limit", `{"name":"long"}`, "application/json", []Option{WithBodyLimit(2)}, 413},
		{"skip limit", `{"name":"long"}`, "application/json", []Option{WithBodyLimit(2), WithoutBodyLimit()}, 200},
		{"restore limit", `{"name":"long"}`, "application/json", []Option{WithoutBodyLimit(), WithBodyLimit(2)}, 413},
		{"skip limit retains media", `{}`, "text/plain", []Option{WithoutBodyLimit()}, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer()
			if err := s.Register(Post("/", func(context.Context, input) (string, error) { return "ok", nil }, tc.opts...)); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.media)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
