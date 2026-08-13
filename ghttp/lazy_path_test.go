package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLazyPathParamsDecodeOnAccess(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	var gotID string
	Route[Params, map[string]string](server).GET("/users/{id}").To(func(_ context.Context, p Params) (map[string]string, error) {
		gotID = p.Path("id")
		return map[string]string{"id": gotID}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	server.ServeHTTP(rec, req)
	if gotID != "42" {
		t.Fatalf("Path(id) = %q, want 42", gotID)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLazyPathParamsCatchAllJoinsRawSegments(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	var got string
	Route[Params, map[string]string](server).GET("/files/{path...}").To(func(_ context.Context, p Params) (map[string]string, error) {
		got = p.Path("path")
		return map[string]string{"path": got}, nil
	})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a/b%2Fc/d", nil))
	if got != "a/b/c/d" {
		t.Fatalf("catch-all = %q, want a/b/c/d", got)
	}
}

func TestLazyPathParamsEncodedValues(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	var id, slug string
	Route[Params, map[string]string](server).GET("/u/{id}/{slug}").To(func(_ context.Context, p Params) (map[string]string, error) {
		id = p.Path("id")
		slug = p.Path("slug")
		return map[string]string{}, nil
	})

	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/u/a%2fb/caf%C3%A9", nil))
	if id != "a/b" {
		t.Fatalf("id = %q, want a/b (encoded slash kept inside the param)", id)
	}
	if slug != "café" {
		t.Fatalf("slug = %q, want café", slug)
	}
}

func TestLazyPathParamsStaticEscapedMatch(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	Route[struct{}, map[string]string](server).GET("/caf%C3%A9/ok").To(func(context.Context, struct{}) (map[string]string, error) {
		return map[string]string{"hit": "1"}, nil
	})

	for _, target := range []string{"/caf%C3%A9/ok", "/café/ok"} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", target, rec.Code)
		}
	}
}

func TestLazyPathParamsDetachMaterializes(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	var detached Params
	Route[Params, struct{}](server).GET("/a/{x}/b/{y}").To(func(_ context.Context, p Params) (struct{}, error) {
		_ = p.Path("x") // 只访问一个;detach 后另一个也必须物化。
		detached = p.Detach()
		return struct{}{}, nil
	})

	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/a/1/b/2", nil))
	if detached.Path("x") != "1" || detached.Path("y") != "2" {
		t.Fatalf("detached params = %q/%q, want 1/2", detached.Path("x"), detached.Path("y"))
	}
}

func TestRawSegmentMatches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw      string
		expected string
		want     bool
	}{
		{raw: "users", expected: "users", want: true},
		{raw: "%2F", expected: "/", want: true},
		{raw: "%2f", expected: "/", want: true}, // 十六进制大小写不敏感。
		{raw: "caf%C3%A9", expected: "café", want: true},
		{raw: "a%20b", expected: "a b", want: true},
		{raw: "a%2Fb", expected: "a/b", want: true},
		{raw: "a", expected: "ab", want: false},
		{raw: "ab", expected: "a", want: false},
		{raw: "%", expected: "%", want: false},
	}
	for _, tc := range cases {
		if got := rawSegmentMatches(tc.raw, tc.expected); got != tc.want {
			t.Errorf("rawSegmentMatches(%q, %q) = %v, want %v", tc.raw, tc.expected, got, tc.want)
		}
	}
}

func TestValidateRawSegmentDotForms(t *testing.T) {
	t.Parallel()

	rejected := []string{".", "..", "%2e", "%2E", "%2e%2e", ".%2e", "%2e.", "a%2Fb%2Fc"}
	for _, raw := range rejected {
		if raw == "a%2Fb%2Fc" {
			if err := validateRawSegment(raw); err != nil {
				t.Errorf("validateRawSegment(%q) unexpected error: %v", raw, err)
			}
			continue
		}
		if err := validateRawSegment(raw); err == nil {
			t.Errorf("validateRawSegment(%q) = nil, want dot/invalid escape error", raw)
		}
	}
	if err := validateRawSegment("%zz"); err == nil {
		t.Error("validateRawSegment(%zz) = nil, want invalid escape error")
	}
}
