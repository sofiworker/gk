package v2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

func TestGroupPathAndMiddlewareInheritance(t *testing.T) {
	s := root.New()
	var order []string
	mw := func(name string) root.Middleware {
		return func(next root.Handler) root.Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				order = append(order, name)
				return next(ctx, req, resp)
			}
		}
	}
	parent := New().Group("/api", GroupBodyLimit(64<<10)).Use(mw("parent"))
	child := parent.Group("users").Use(mw("child"))
	route, err := GetIn(child, s, "/{id}", func(context.Context, struct{}) (string, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	if route.Path != "/api/users/{id}" {
		t.Fatalf("path %q", route.Path)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/users/7", nil))
	if !reflect.DeepEqual(order, []string{"parent", "child"}) {
		t.Fatal(order)
	}
}

func TestGroupCodecInheritanceAndRouteOverride(t *testing.T) {
	api := New()
	g := api.Group("/x", GroupInput(JSONInput[contractInput]()), GroupOutput(TextOutput[string]()))
	s := root.New()
	r, err := Handle(g, http.MethodPost, "/default", s, func(_ context.Context, in contractInput) (string, error) { return in.Name, nil })
	if err != nil {
		t.Fatal(err)
	}
	override, err := Handle(g, http.MethodPost, "/override", s, func(_ context.Context, in contractInput) (string, error) { return in.Name, nil }, WithOutput(JSONOutput[string]()))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x/default", stringsReader(`{"Name":"alice"}`)))
	if rec.Body.String() != "alice" || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("%s %v", rec.Body.String(), rec.Header())
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x/override", stringsReader(`{"Name":"bob"}`)))
	if rec.Body.String() != "\"bob\"\n" {
		t.Fatal(rec.Body.String())
	}
	if r.Path == override.Path {
		t.Fatal("route paths should remain distinct")
	}
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }

func TestGroupBodyLimitAndOverride(t *testing.T) {
	g := New().Group("/api", GroupBodyLimit(2))
	s := root.New()
	h := func(context.Context, contractInput) (string, error) { return "ok", nil }
	if _, err := PostIn(g, s, "/limited", h); err != nil {
		t.Fatal(err)
	}
	if _, err := PostIn(g, s, "/allowed", h, WithBodyLimit(1024)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/limited", 413}, {"/api/allowed", 200}} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("POST", tc.path, strings.NewReader(`{"Name":"alice"}`)))
		if rec.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, rec.Code)
		}
	}
}
