package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrictContentNegotiationReturns406(t *testing.T) {
	type pingResp struct {
		Name string `json:"name"`
	}
	app := New(WithProduces(MIMEJSON), WithStrictContentNegotiation())
	Route[Params, pingResp](app).GET("/ping").To(func(context.Context, Params) (pingResp, error) {
		return pingResp{Name: "pong"}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "text/html")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotAcceptable, w.Body.String())
	}
}

func TestLenientContentNegotiationFallsBack(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Params, map[string]string](app).GET("/ping").To(func(context.Context, Params) (map[string]string, error) {
		return map[string]string{"name": "pong"}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "text/html")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("content type = %q, want %q", got, MIMEJSON)
	}
}

func TestStrictContentTypeReturns415(t *testing.T) {
	type input struct {
		Params
		Body struct {
			Name string `json:"name"`
		}
	}
	app := New(WithProduces(MIMEJSON), WithStrictContentType())
	Route[input, struct{}](app).POST("/users").To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Content-Type", "application/octet-stream")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusUnsupportedMediaType, w.Body.String())
	}
}
