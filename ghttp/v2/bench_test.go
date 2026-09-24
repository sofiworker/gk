package v2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkRouteExecution(b *testing.B) {
	r := Get("/", func(context.Context, struct{}) (string, error) { return "ok", nil }, WithOutput(TextOutput[string]()))
	req := &Request{Request: httptest.NewRequest(http.MethodGet, "/", nil)}
	b.ReportAllocs()
	for b.Loop() {
		resp := &Response{ResponseWriter: httptest.NewRecorder()}
		if err := r.Serve(context.Background(), req, resp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRouteRegistration(b *testing.B) {
	h := func(context.Context, struct{}) (string, error) { return "ok", nil }
	b.ReportAllocs()
	for b.Loop() {
		if err := Get("/", h).Err(); err != nil {
			b.Fatal(err)
		}
	}
}
