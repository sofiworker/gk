package v2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

func TestRegisterInheritedDefaultsAndOverride(t *testing.T) {
	s := root.New()
	api := New(s).With(WithBodyLimit(2))
	group := api.Group("/api").Group("/users").With(WithOutput(TextOutput[string]()))
	h := func(_ context.Context, in contractInput) (string, error) { return in.Name, nil }
	if err := group.Register(Post("", h, WithBodyLimit(100)), Post("/limited", h)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/users", 200}, {"/api/users/limited", 413}} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(`{"Name":"alice"}`))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, rec.Code, rec.Body.String())
		}
		if tc.status == 200 && rec.Body.String() != "alice" {
			t.Fatal(rec.Body.String())
		}
	}
}

func TestRegisterValidationBeforeInstall(t *testing.T) {
	s := root.New()
	api := New(s)
	valid := Get("/ok", func(context.Context, struct{}) (string, error) { return "ok", nil })
	invalid := Get("/bad", func(context.Context, struct{}) (string, error) { return "bad", nil }, WithBodyLimit(-1))
	if err := api.Register(valid, invalid); err == nil || !strings.Contains(err.Error(), "/bad") {
		t.Fatalf("error %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
}

func TestRegisterSameDefinitionMultipleGroups(t *testing.T) {
	s := root.New()
	api := New(s)
	r := Get("/item", func(context.Context, struct{}) (string, error) { return "ok", nil })
	for _, prefix := range []string{"/a", "/b"} {
		if err := api.Group(prefix).Register(r); err != nil {
			t.Fatal(err)
		}
	}
	if r.Path != "/item" {
		t.Fatal(r.Path)
	}
	for _, prefix := range []string{"/a", "/b"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", prefix+"/item", nil))
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
	}
}
