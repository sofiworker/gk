package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorDetailsHiddenByDefault(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/users"), JSONBody[input](), JSONOutput[struct{}](), func(context.Context, input) (struct{}, error) {
		return struct{}{}, Err(http.StatusBadRequest, "internal secret detail")
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "unexpected EOF") {
		t.Fatalf("parse error details leaked: %s", w.Body.String())
	}
}

func TestPlainErrorDetailsHiddenByDefault(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/boom"), NoInput(), JSONOutput[struct{}](), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, errInternalSecret
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "internal secret detail") {
		t.Fatalf("error details leaked: %s", w.Body.String())
	}
}

func TestErrorDetailsExposedWhenEnabled(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithExposeErrorDetails())
	app.MustMount(Handle(Get("/boom"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, fmt.Errorf("raw internal secret detail")
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if !strings.Contains(w.Body.String(), "raw internal secret detail") {
		t.Fatalf("raw error details missing: %s", w.Body.String())
	}
}

func TestExplicitHTTPErrorMessageAlwaysReturned(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/users/{id}"), NoInput(), JSONOutput[struct{}](), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, Err(http.StatusNotFound, "user not found")
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/1", nil))

	if !strings.Contains(w.Body.String(), "user not found") {
		t.Fatalf("explicit error message missing: %s", w.Body.String())
	}
}

var errInternalSecret = &internalSecretError{}

type internalSecretError struct{}

func (e *internalSecretError) Error() string { return "internal secret detail" }
