package ghttp

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func FuzzRoutePathParserAndMatcher(f *testing.F) {
	for _, seed := range []string{
		"/users/%",
		"/users/%zz",
		"/users//active",
		"/users/.",
		"/users/..",
		"/users/%2E",
		"/users/%2e%2e",
		"/users/%E4%BD%A0%E5%A5%BD",
		"/users/你好",
		"/users/%2F",
		"/users/" + strings.Repeat("a", 64*1024),
		"/users/{id}",
		"/files",
		"/files/a/b/c",
		"/files/{path...}",
	} {
		f.Add(seed)
	}

	matcher := newFuzzRouteMatcher(f)
	f.Fuzz(func(t *testing.T, rawPath string) {
		pattern, patternErr := parseRoutePattern(rawPath, false)
		if patternErr != nil && !errors.Is(patternErr, ErrRoutePathInvalid) {
			t.Fatalf("parseRoutePattern(%q) error = %v, want ErrRoutePathInvalid", rawPath, patternErr)
		}

		path, pathErr := parseRequestPath(rawPath, false)
		if pathErr != nil && !errors.Is(pathErr, ErrInvalidRequestPath) {
			t.Fatalf("parseRequestPath(%q) error = %v, want ErrInvalidRequestPath", rawPath, pathErr)
		}
		if pathErr != nil {
			return
		}

		assertFuzzRouteMatchConsistent(t, matcher, path)
		if patternErr == nil {
			assertFuzzRouteMatchConsistent(t, newRouteMux([]routeDefinition{{
				method:  http.MethodGet,
				pattern: pattern,
			}}), path)
		}
	})
}

func newFuzzRouteMatcher(f *testing.F) *routeMux {
	f.Helper()

	definitions := make([]routeDefinition, 0, 2)
	for _, rawPattern := range []string{"/users/{id}", "/files/{path...}"} {
		pattern, err := parseRoutePattern(rawPattern, false)
		if err != nil {
			f.Fatalf("parseRoutePattern(%q) error = %v", rawPattern, err)
		}
		definitions = append(definitions, routeDefinition{
			method:  http.MethodGet,
			pattern: pattern,
		})
	}
	return newRouteMux(definitions)
}

func assertFuzzRouteMatchConsistent(t *testing.T, matcher *routeMux, path requestPath) {
	t.Helper()

	first := matcher.match(http.MethodGet, path)
	second := matcher.match(http.MethodGet, path)
	if first.kind != second.kind || first.suppressBody != second.suppressBody || !reflect.DeepEqual(first.allow, second.allow) {
		t.Fatalf("match(%#v) is inconsistent: first = %#v, second = %#v", path, first, second)
	}
	if first.kind != routeMatchFound {
		return
	}
	if first.route == nil || second.route == nil {
		t.Fatalf("match(%#v) returned a nil route", path)
	}
	if first.route.definition.pattern.path != second.route.definition.pattern.path {
		t.Fatalf("match(%#v) route = %q then %q", path, first.route.definition.pattern.path, second.route.definition.pattern.path)
	}

	params, err := first.route.extract(path)
	if err != nil {
		t.Fatalf("matched route %q cannot extract %#v: %v", first.route.definition.pattern.path, path, err)
	}
	repeatedParams, err := first.route.extract(path)
	if err != nil {
		t.Fatalf("matched route %q cannot repeat extraction for %#v: %v", first.route.definition.pattern.path, path, err)
	}
	if !reflect.DeepEqual(params, repeatedParams) {
		t.Fatalf("extract(%#v) is inconsistent: first = %#v, second = %#v", path, params, repeatedParams)
	}
}
