package v2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

func TestRequestInputProvidesSources(t *testing.T) {
	route := Get("/users/{id}", func(_ context.Context, in *RequestInput) (string, error) {
		sources := in.Sources()
		if sources.Path("id") != "42" || in.QueryFirstValue("page") != "7" || sources.Header("X-Trace") != "trace" {
			t.Fatalf("unexpected request input: %#v", in)
		}
		return in.CookieValue("sid"), nil
	})
	s := root.New()
	if err := route.Mount(s); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/users/42?page=7", nil)
	req.Header.Set("X-Trace", "trace")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "cookie"})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "\"cookie\"\n" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}

func TestRequestInputValueHandler(t *testing.T) {
	route := Get("/", func(_ context.Context, in RequestInput) (string, error) { return in.URL.Path, nil })
	s := root.New()
	if err := route.Mount(s); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 200 || rec.Body.String() != "\"/\"\n" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}
