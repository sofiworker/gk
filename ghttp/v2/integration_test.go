package v2

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

func TestInputErrorsThroughServer(t *testing.T) {
	for _, tc := range []struct {
		body, ct string
		status   int
	}{
		{`{`, "application/json", 400},
		{`{} {}`, "application/json", 400},
		{`{}`, "application/xml", 415},
	} {
		t.Run(tc.body+tc.ct, func(t *testing.T) {
			s := root.New()
			r := Post("/", func(context.Context, contractInput) (contractOutput, error) {
				t.Fatal("handler called")
				return contractOutput{}, nil
			})
			if err := s.RawHandle(r.Method, r.Path, root.RawHandlerFunc(r.Serve)); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.ct)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRouteMiddleware(t *testing.T) {
	marker := errors.New("blocked")
	called := false
	mw := func(next root.Handler) root.Handler {
		return func(context.Context, *Request, *Response) error { return marker }
	}
	r := Get("/", func(context.Context, struct{}) (string, error) { called = true; return "", nil }, WithMiddleware(mw))
	err := r.Serve(context.Background(), &Request{Request: httptest.NewRequest("GET", "/", nil)}, &Response{ResponseWriter: httptest.NewRecorder()})
	if !errors.Is(err, marker) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestPathAndBodyBindingIntegration(t *testing.T) {
	type input struct {
		ID   int `path:"id"`
		Body struct {
			Name string `json:"name"`
		} `body:"json"`
	}
	r := Patch("/users/{id}", func(_ context.Context, in *input) (int, error) {
		if in.Body.Name != "alice" {
			t.Fatal(in)
		}
		return in.ID, nil
	})
	s := root.New()
	if err := r.Mount(s); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("PATCH", "/users/42", strings.NewReader(`{"name":"alice","ID":99}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "42\n" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
