package ghttp

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseRoutePatternNormalizesEscapedStaticSegments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		strict     bool
		wantPath   string
		wantKinds  []routeSegmentKind
		wantValues []string
	}{
		{
			name:       "missing leading slash",
			path:       "users/{UserID}",
			wantPath:   "/users/{UserID}",
			wantKinds:  []routeSegmentKind{routeSegmentStatic, routeSegmentParameter},
			wantValues: []string{"users", "UserID"},
		},
		{
			name:       "unicode static segment",
			path:       "/caf%C3%A9",
			wantPath:   "/café",
			wantKinds:  []routeSegmentKind{routeSegmentStatic},
			wantValues: []string{"café"},
		},
		{
			name:       "escaped parameter syntax remains static",
			path:       "/%7Bid%7D",
			wantPath:   "/{id}",
			wantKinds:  []routeSegmentKind{routeSegmentStatic},
			wantValues: []string{"{id}"},
		},
		{
			name:       "escaped slash stays in one segment",
			path:       "/objects/%2F",
			wantPath:   "/objects/%2F",
			wantKinds:  []routeSegmentKind{routeSegmentStatic, routeSegmentStatic},
			wantValues: []string{"objects", "/"},
		},
		{
			name:       "default routing removes trailing slash",
			path:       "/users/",
			wantPath:   "/users",
			wantKinds:  []routeSegmentKind{routeSegmentStatic},
			wantValues: []string{"users"},
		},
		{
			name:       "strict routing retains trailing slash",
			path:       "/users/",
			strict:     true,
			wantPath:   "/users/",
			wantKinds:  []routeSegmentKind{routeSegmentStatic},
			wantValues: []string{"users"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, err := parseRoutePattern(tt.path, tt.strict)
			if err != nil {
				t.Fatalf("parseRoutePattern(%q) error = %v", tt.path, err)
			}
			if pattern.path != tt.wantPath {
				t.Fatalf("path = %q, want %q", pattern.path, tt.wantPath)
			}
			if got := routeSegmentKinds(pattern.segments); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("segment kinds = %#v, want %#v", got, tt.wantKinds)
			}
			if got := routeSegmentValues(pattern.segments); !reflect.DeepEqual(got, tt.wantValues) {
				t.Fatalf("segment values = %#v, want %#v", got, tt.wantValues)
			}
		})
	}
}

func TestParseRoutePatternRejectsInvalidRouteSyntax(t *testing.T) {
	t.Parallel()

	tests := []string{
		"/users//active",
		"/users/.",
		"/users/..",
		"/users/%2E",
		"/users/%2e%2e",
		"/users/%",
		"/users/%zz",
		"/users/:id",
		"/users/*path",
		"/users/prefix-{id}",
		"/users/{id}/{id}",
		"/files/{path...}/raw",
		"/users/{1id}",
		"/users/{}",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			_, err := parseRoutePattern(path, false)
			if !errors.Is(err, ErrRoutePathInvalid) {
				t.Fatalf("parseRoutePattern(%q) error = %v, want ErrRoutePathInvalid", path, err)
			}
		})
	}
}

func TestParseRequestPathRejectsUnsafePaths(t *testing.T) {
	t.Parallel()

	tests := []string{
		"/users//active",
		"/users/.",
		"/users/..",
		"/users/%2E",
		"/users/%2e%2e",
		"/users/%",
		"/users/%zz",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			_, err := parseRequestPath(path, false)
			if !errors.Is(err, ErrInvalidRequestPath) {
				t.Fatalf("parseRequestPath(%q) error = %v, want ErrInvalidRequestPath", path, err)
			}
		})
	}
}

func TestJoinRoutePathsPreservesRouteTrailingSlashMeaning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prefix string
		route  string
		want   string
	}{
		{prefix: "/api/", route: "v1", want: "/api/v1"},
		{prefix: "/api/", route: "/v1", want: "/api/v1"},
		{prefix: "/api", route: "", want: "/api"},
		{prefix: "/api", route: "/", want: "/api/"},
		{prefix: "", route: "health", want: "/health"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"+"+tt.route, func(t *testing.T) {
			if got := joinRoutePaths(tt.prefix, tt.route); got != tt.want {
				t.Fatalf("joinRoutePaths(%q, %q) = %q, want %q", tt.prefix, tt.route, got, tt.want)
			}
		})
	}
}

func routeSegmentKinds(segments []routeSegment) []routeSegmentKind {
	kinds := make([]routeSegmentKind, len(segments))
	for i, segment := range segments {
		kinds[i] = segment.kind
	}
	return kinds
}

func routeSegmentValues(segments []routeSegment) []string {
	values := make([]string, len(segments))
	for i, segment := range segments {
		values[i] = segment.value
	}
	return values
}
