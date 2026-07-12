package ghttp

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestRouteMuxUsesCurrentMethodPrecedence(t *testing.T) {
	t.Parallel()

	mux := newTestRouteMux(t,
		testRouteDefinition(t, http.MethodGet, "/users/new"),
		testRouteDefinition(t, http.MethodPost, "/users/{id}"),
		testRouteDefinition(t, http.MethodGet, "/files/{name}"),
		testRouteDefinition(t, http.MethodGet, "/files/{path...}"),
	)

	tests := []struct {
		name       string
		method     string
		path       string
		wantRoute  string
		wantParams map[string]string
	}{
		{
			name:       "current method parameter wins over other method static",
			method:     http.MethodPost,
			path:       "/users/new",
			wantRoute:  "/users/{id}",
			wantParams: map[string]string{"id": "new"},
		},
		{
			name:       "parameter wins over catch all",
			method:     http.MethodGet,
			path:       "/files/readme",
			wantRoute:  "/files/{name}",
			wantParams: map[string]string{"name": "readme"},
		},
		{
			name:       "catch all handles remaining segments",
			method:     http.MethodGet,
			path:       "/files/css/app.css",
			wantRoute:  "/files/{path...}",
			wantParams: map[string]string{"path": "css/app.css"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestPath, err := parseRequestPath(tt.path, false)
			if err != nil {
				t.Fatalf("parseRequestPath(%q) error = %v", tt.path, err)
			}
			result := mux.match(tt.method, requestPath)
			if result.kind != routeMatchFound {
				t.Fatalf("match kind = %v, want routeMatchFound", result.kind)
			}
			if got := result.route.definition.pattern.path; got != tt.wantRoute {
				t.Fatalf("matched route = %q, want %q", got, tt.wantRoute)
			}
			params, err := result.route.extract(requestPath)
			if err != nil {
				t.Fatalf("extract error = %v", err)
			}
			if got := pathParamMap(params); !reflect.DeepEqual(got, tt.wantParams) {
				t.Fatalf("params = %#v, want %#v", got, tt.wantParams)
			}
		})
	}
}

func TestRouteMuxCatchAllMatchesZeroSegments(t *testing.T) {
	t.Parallel()

	mux := newTestRouteMux(t, testRouteDefinition(t, http.MethodGet, "/files/{path...}"))
	requestPath, err := parseRequestPath("/files", false)
	if err != nil {
		t.Fatalf("parseRequestPath error = %v", err)
	}
	result := mux.match(http.MethodGet, requestPath)
	if result.kind != routeMatchFound {
		t.Fatalf("match kind = %v, want routeMatchFound", result.kind)
	}
	params, err := result.route.extract(requestPath)
	if err != nil {
		t.Fatalf("extract error = %v", err)
	}
	if got := params.Get("path"); got != "" {
		t.Fatalf("catch-all parameter = %q, want empty string", got)
	}
	if got := params.Len(); got != 1 {
		t.Fatalf("parameter count = %d, want 1", got)
	}
}

func TestRouteMuxHEADAndMethodOutcomes(t *testing.T) {
	t.Parallel()

	mux := newTestRouteMux(t,
		testRouteDefinition(t, http.MethodGet, "/fallback"),
		testRouteDefinition(t, http.MethodGet, "/explicit"),
		testRouteDefinition(t, http.MethodHead, "/explicit"),
		testRouteDefinition(t, http.MethodPost, "/allow"),
		testRouteDefinition(t, "PURGE", "/allow"),
		testRouteDefinition(t, http.MethodGet, "/allow"),
	)

	tests := []struct {
		name         string
		method       string
		path         string
		wantKind     routeMatchKind
		wantMethod   string
		wantSuppress bool
		wantAllow    []string
	}{
		{
			name:         "head falls back to get",
			method:       http.MethodHead,
			path:         "/fallback",
			wantKind:     routeMatchFound,
			wantMethod:   http.MethodGet,
			wantSuppress: true,
		},
		{
			name:         "explicit head wins",
			method:       http.MethodHead,
			path:         "/explicit",
			wantKind:     routeMatchFound,
			wantMethod:   http.MethodHead,
			wantSuppress: true,
		},
		{
			name:      "options is not automatic",
			method:    http.MethodOptions,
			path:      "/allow",
			wantKind:  routeMatchMethodNotAllowed,
			wantAllow: []string{http.MethodGet, http.MethodHead, http.MethodPost, "PURGE"},
		},
		{
			name:     "unknown path is not found",
			method:   http.MethodPost,
			path:     "/missing",
			wantKind: routeMatchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestPath, err := parseRequestPath(tt.path, false)
			if err != nil {
				t.Fatalf("parseRequestPath(%q) error = %v", tt.path, err)
			}
			result := mux.match(tt.method, requestPath)
			if result.kind != tt.wantKind {
				t.Fatalf("match kind = %v, want %v", result.kind, tt.wantKind)
			}
			if tt.wantMethod != "" && result.route.definition.method != tt.wantMethod {
				t.Fatalf("route method = %q, want %q", result.route.definition.method, tt.wantMethod)
			}
			if result.suppressBody != tt.wantSuppress {
				t.Fatalf("suppressBody = %t, want %t", result.suppressBody, tt.wantSuppress)
			}
			if !reflect.DeepEqual(result.allow, tt.wantAllow) {
				t.Fatalf("Allow = %#v, want %#v", result.allow, tt.wantAllow)
			}
		})
	}
}

func TestCompiledRouteExtractorRejectsRewrittenPath(t *testing.T) {
	t.Parallel()

	mux := newTestRouteMux(t, testRouteDefinition(t, http.MethodGet, "/users/{id}"))
	requestPath, err := parseRequestPath("/users/42", false)
	if err != nil {
		t.Fatalf("parseRequestPath error = %v", err)
	}
	result := mux.match(http.MethodGet, requestPath)
	if result.kind != routeMatchFound {
		t.Fatalf("match kind = %v, want routeMatchFound", result.kind)
	}

	rewritten, err := parseRequestPath("/users/42/profile", false)
	if err != nil {
		t.Fatalf("parseRequestPath rewritten error = %v", err)
	}
	if _, err := result.route.extract(rewritten); !errors.Is(err, ErrInvalidRequestPath) {
		t.Fatalf("extract rewritten path error = %v, want ErrInvalidRequestPath", err)
	}
}

func newTestRouteMux(t *testing.T, definitions ...routeDefinition) *routeMux {
	t.Helper()
	return newRouteMux(definitions)
}

func testRouteDefinition(t *testing.T, method, path string) routeDefinition {
	t.Helper()
	return mustRouteDefinition(t, method, path)
}

func pathParamMap(params pathParamList) map[string]string {
	if params.Len() == 0 {
		return nil
	}
	values := make(map[string]string, params.Len())
	for i := 0; i < params.len; i++ {
		values[params.values[i].Key] = params.values[i].Value
	}
	for _, param := range params.overflow {
		values[param.Key] = param.Value
	}
	return values
}
