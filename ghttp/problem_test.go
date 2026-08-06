package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProblemDetailsOffByDefault(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Params, struct{}](app).GET("/users/{id}").To(func(context.Context, Params) (struct{}, error) {
		return struct{}{}, Err(http.StatusNotFound, "user not found")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/1", nil))

	if got := w.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("content type = %q, want %q", got, MIMEJSON)
	}
	if !strings.Contains(w.Body.String(), `"message"`) {
		t.Fatalf("body = %s, want default error body", w.Body.String())
	}
}

func TestProblemDetailsOn(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithProblemDetails())
	Route[Params, struct{}](app).GET("/users/{id}").To(func(context.Context, Params) (struct{}, error) {
		return struct{}{}, Err(http.StatusNotFound, "user not found")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	app.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type = %q, want application/problem+json", got)
	}
	body := w.Body.String()
	for _, want := range []string{`"status":404`, `"title":"Not Found"`, `"detail":"user not found"`, `"instance":"/users/1"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestProblemDetailsHidesPlainError(t *testing.T) {
	type input struct {
		Params
		Body struct {
			Name string `json:"name"`
		}
	}
	app := New(WithProduces(MIMEJSON), WithProblemDetails())
	Route[input, struct{}](app).POST("/users").To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), "unexpected EOF") {
		t.Fatalf("parse details leaked: %s", w.Body.String())
	}
}

func TestProblemDetailsWinsOverEnvelope(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithEnvelope(DefaultEnvelope), WithProblemDetails())
	Route[Params, struct{}](app).GET("/users/{id}").To(func(context.Context, Params) (struct{}, error) {
		return struct{}{}, Err(http.StatusNotFound, "user not found")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/1", nil))

	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type = %q, want application/problem+json", got)
	}
	if strings.Contains(w.Body.String(), `"code":404`) {
		t.Fatalf("envelope leaked into error body: %s", w.Body.String())
	}
}
