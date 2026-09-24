package v2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runRoute(t *testing.T, r Route) {
	t.Helper()
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	req := &Request{Request: httptest.NewRequest(r.Method, "/", strings.NewReader(`{"Name":"alice"}`))}
	rec := httptest.NewRecorder()
	if err := r.Serve(context.Background(), req, &Response{ResponseWriter: rec}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "{\"Name\":\"alice\"}\n" {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestRoutePointerMatrix(t *testing.T) {
	runRoute(t, Post("/", func(_ context.Context, in contractInput) (contractOutput, error) {
		return contractOutput{Name: in.Name}, nil
	}))
	runRoute(t, Post("/", func(_ context.Context, in *contractInput) (contractOutput, error) {
		return contractOutput{Name: in.Name}, nil
	}))
	runRoute(t, Post("/", func(_ context.Context, in contractInput) (*contractOutput, error) {
		return &contractOutput{Name: in.Name}, nil
	}))
	runRoute(t, Post("/", func(_ context.Context, in *contractInput) (*contractOutput, error) {
		return &contractOutput{Name: in.Name}, nil
	}))
}

func TestRouteCodecMismatch(t *testing.T) {
	r := Post("/", func(context.Context, contractInput) (contractOutput, error) { return contractOutput{}, nil }, WithInput(TextInput[string]()))
	if r.Err() == nil {
		t.Fatal("expected registration error")
	}
}

func TestRouteNoBodyPointer(t *testing.T) {
	r := Get("/", func(_ context.Context, in *contractInput) (string, error) {
		if in == nil {
			t.Fatal("nil input")
		}
		return "ok", nil
	}, WithOutput(TextOutput[string]()))
	rec := httptest.NewRecorder()
	if err := r.Serve(context.Background(), &Request{Request: httptest.NewRequest(http.MethodGet, "/", nil)}, &Response{ResponseWriter: rec}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "ok" {
		t.Fatal(rec.Body.String())
	}
}

func TestFormPointerBodyOnly(t *testing.T) {
	r := Post("/", func(_ context.Context, in *contractInput) (contractOutput, error) {
		return contractOutput{Name: in.Name}, nil
	}, WithInput(FormInput[*contractInput]()))
	req := httptest.NewRequest(http.MethodPost, "/?Name=query", strings.NewReader("Name=body"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	if err := r.Serve(context.Background(), &Request{Request: req}, &Response{ResponseWriter: rec}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "{\"Name\":\"body\"}\n" {
		t.Fatal(rec.Body.String())
	}
}

func TestHeadSuppressesBody(t *testing.T) {
	r := Head("/", func(context.Context, struct{}) (string, error) { return "hello", nil }, WithOutput(TextOutput[string]().WithStatus(202)))
	rec := httptest.NewRecorder()
	if err := r.Serve(context.Background(), &Request{Request: httptest.NewRequest(http.MethodHead, "/", nil)}, &Response{ResponseWriter: rec}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != 0 || rec.Code != 202 || rec.Result().Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("response: %+v", rec.Result())
	}
}
