package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProblemDetailsOffByDefault(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/users/{id}"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, Err(http.StatusNotFound, "user not found")
	}))

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
	app.MustMount(Handle(Get("/users/{id}"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, Err(http.StatusNotFound, "user not found")
	}))

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
	type payload struct {
		Name string `json:"name"`
	}
	app := New(WithProduces(MIMEJSON), WithProblemDetails())
	app.MustMount(Handle(Post("/users"), JSONBody[payload](), JSONOutput[EmptyInput](), func(context.Context, payload) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

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
	app.MustMount(Handle(Get("/users/{id}"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, Err(http.StatusNotFound, "user not found")
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/1", nil))

	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type = %q, want application/problem+json", got)
	}
	if strings.Contains(w.Body.String(), `"code":404`) {
		t.Fatalf("envelope leaked into error body: %s", w.Body.String())
	}
}

func TestRouteLevelProblemDetails(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/problem"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, Err(http.StatusNotFound, "user not found")
	}).WithProblemDetails())
	app.MustMount(Handle(Get("/plain"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, Err(http.StatusNotFound, "user not found")
	}))

	problemRec := httptest.NewRecorder()
	app.ServeHTTP(problemRec, httptest.NewRequest(http.MethodGet, "/problem", nil))
	if got := problemRec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("problem content type = %q", got)
	}

	plainRec := httptest.NewRecorder()
	app.ServeHTTP(plainRec, httptest.NewRequest(http.MethodGet, "/plain", nil))
	if got := plainRec.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("plain content type = %q, want %q", got, MIMEJSON)
	}
	if !strings.Contains(plainRec.Body.String(), `"message"`) {
		t.Fatalf("plain body = %s, want default error body", plainRec.Body.String())
	}
}

func TestChainErrorWriters(t *testing.T) {
	var logged string
	app := New(WithProduces(MIMEJSON), WithErrorWriter(ChainErrorWriters(
		func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
			logged = err.Error()
			return false
		},
		func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
			w.Header().Set("Content-Type", MIMEJSON)
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"custom":%q}`, err.Error())
			return true
		},
	)))
	app.MustMount(Handle(Get("/boom"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, Err(http.StatusBadRequest, "boom")
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if logged != "boom" {
		t.Fatalf("logged = %q, want boom", logged)
	}
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"custom":"boom"`) {
		t.Fatalf("status/body = %d %s", w.Code, w.Body.String())
	}
}

func TestRouteErrorWriterCustom(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/boom"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, Err(http.StatusBadRequest, "boom")
	}).WithErrorWriter(func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
		w.Header().Set("X-Custom-Error", "1")
		w.WriteHeader(status)
		_, _ = w.Write([]byte("custom-error"))
		return true
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusBadRequest || w.Header().Get("X-Custom-Error") != "1" || w.Body.String() != "custom-error" {
		t.Fatalf("status/header/body = %d %q %q", w.Code, w.Header().Get("X-Custom-Error"), w.Body.String())
	}
}

func TestWriteRouteErrorUsesRouteWriter(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	called := false
	writeRouteError(rec, req, app, func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
		called = true
		w.WriteHeader(http.StatusTeapot)
		return true
	}, nil, http.StatusBadRequest, fmt.Errorf("extraction boom"))

	if !called {
		t.Fatal("route error writer was not called")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
