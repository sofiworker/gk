package v2

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

func TestValidatorContract(t *testing.T) {
	type input struct {
		N int `query:"n"`
	}
	marker := errors.New("negative input")
	called := 0
	validate := func(ctx context.Context, in *input) error {
		if in.N < 0 {
			return marker
		}
		return nil
	}
	r := Get("/", func(_ context.Context, in *input) (int, error) { called++; return in.N, nil }, WithValidator(validate))
	for _, query := range []string{"?n=2", "?n=-1", "?n=invalid"} {
		err := r.Serve(context.Background(), &Request{Request: httptest.NewRequest("GET", "/"+query, nil)}, &Response{ResponseWriter: httptest.NewRecorder()})
		if query == "?n=2" && err != nil {
			t.Fatal(err)
		}
		if query != "?n=2" && !errors.Is(err, root.ErrInvalidInput) {
			t.Fatalf("invalid input: %v", err)
		}
		if query == "?n=-1" && !errors.Is(err, marker) {
			t.Fatalf("lost cause: %v", err)
		}
	}
	if called != 1 {
		t.Fatalf("handler calls: %d", called)
	}
	if Get("/", func(context.Context, input) (string, error) { return "", nil }, WithValidator(validate)).Err() == nil {
		t.Fatal("validator type mismatch accepted")
	}
	if Get("/", func(context.Context, *input) (string, error) { return "", nil }, WithValidator[*input](nil)).Err() == nil {
		t.Fatal("nil validator accepted")
	}
}

func TestValidatorGroupOverride(t *testing.T) {
	s := root.New()
	a := New(s).With(WithValidator(func(context.Context, RequestInput) error { return errors.New("blocked") }))
	r := Get("/", func(context.Context, RequestInput) (string, error) { return "ok", nil }, WithValidator(func(context.Context, RequestInput) error { return nil }))
	if err := a.Register(r); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}
}
