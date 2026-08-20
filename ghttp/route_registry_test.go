package ghttp

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestRouteRegistryRejectsConflictingDefinitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		first routeDefinition
		next  []routeDefinition
	}{
		{
			name:  "same method static path",
			first: mustRouteDefinition(t, http.MethodGet, "/users"),
			next:  []routeDefinition{mustRouteDefinition(t, http.MethodGet, "/users")},
		},
		{
			name:  "same method parameter structure",
			first: mustRouteDefinition(t, http.MethodGet, "/users/{id}"),
			next:  []routeDefinition{mustRouteDefinition(t, http.MethodGet, "/users/{name}")},
		},
		{
			name:  "different methods use different shared parameter name",
			first: mustRouteDefinition(t, http.MethodGet, "/users/{id}"),
			next:  []routeDefinition{mustRouteDefinition(t, http.MethodPost, "/users/{name}/logs")},
		},
		{
			name:  "same method duplicate catch all",
			first: mustRouteDefinition(t, http.MethodGet, "/files/{path...}"),
			next:  []routeDefinition{mustRouteDefinition(t, http.MethodGet, "/files/{path...}")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newRouteRegistry()
			if err := registry.register(tt.first); err != nil {
				t.Fatalf("first registration error = %v", err)
			}
			if err := registry.register(tt.next...); !errors.Is(err, ErrRouteConflict) {
				t.Fatalf("conflicting registration error = %v, want ErrRouteConflict", err)
			}
		})
	}
}

func TestRouteRegistryAllowsDistinctRoutePrecedenceAndSharedNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		defs []routeDefinition
	}{
		{
			name: "static and parameter use fixed precedence",
			defs: []routeDefinition{
				mustRouteDefinition(t, http.MethodGet, "/users/new"),
				mustRouteDefinition(t, http.MethodGet, "/users/{id}"),
			},
		},
		{
			name: "parameter and catch all use fixed precedence",
			defs: []routeDefinition{
				mustRouteDefinition(t, http.MethodGet, "/files/{name}"),
				mustRouteDefinition(t, http.MethodGet, "/files/{path...}"),
			},
		},
		{
			name: "different methods share matching parameter name",
			defs: []routeDefinition{
				mustRouteDefinition(t, http.MethodGet, "/users/{userID}"),
				mustRouteDefinition(t, http.MethodPost, "/users/{userID}"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newRouteRegistry()
			if err := registry.register(tt.defs...); err != nil {
				t.Fatalf("registration error = %v", err)
			}
			if got := len(registry.snapshot()); got != len(tt.defs) {
				t.Fatalf("definition count = %d, want %d", got, len(tt.defs))
			}
		})
	}
}

func TestRouteRegistryTreatsTrailingSlashByRoutingMode(t *testing.T) {
	t.Parallel()

	defaultRegistry := newRouteRegistry()
	if err := defaultRegistry.register(mustRouteDefinition(t, http.MethodGet, "/users")); err != nil {
		t.Fatalf("default first registration error = %v", err)
	}
	if err := defaultRegistry.register(mustRouteDefinition(t, http.MethodGet, "/users/")); !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("default trailing slash error = %v, want ErrRouteConflict", err)
	}

	strictRegistry := newRouteRegistry()
	if err := strictRegistry.register(mustRouteDefinitionStrict(t, http.MethodGet, "/users")); err != nil {
		t.Fatalf("strict first registration error = %v", err)
	}
	if err := strictRegistry.register(mustRouteDefinitionStrict(t, http.MethodGet, "/users/")); err != nil {
		t.Fatalf("strict trailing slash registration error = %v", err)
	}
}

func TestRouteRegistryRegistersMultipleMethodsAtomically(t *testing.T) {
	t.Parallel()

	registry := newRouteRegistry()
	if err := registry.register(mustRouteDefinition(t, http.MethodGet, "/users")); err != nil {
		t.Fatalf("first registration error = %v", err)
	}

	err := registry.register(
		mustRouteDefinition(t, http.MethodGet, "/users"),
		mustRouteDefinition(t, http.MethodPost, "/users"),
	)
	if !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("atomic registration error = %v, want ErrRouteConflict", err)
	}
	if got := len(registry.snapshot()); got != 1 {
		t.Fatalf("definition count after failed atomic registration = %d, want 1", got)
	}
}

func TestRouteDefinitionCloneDeepCopiesDocumentation(t *testing.T) {
	t.Parallel()

	successCode := 1
	errorCode := 2
	original := routeDefinition{doc: RouteDoc{
		Tags:         []string{"users"},
		ExternalDocs: &ExternalDocsDoc{Description: "docs", URL: "https://example.com"},
		Success:      &DocMessage{Code: &successCode, Message: "ok"},
		Errors:       []DocMessage{{Code: &errorCode, Message: "bad input"}},
	}}
	cloned := original.clone()

	original.doc.Tags[0] = "changed"
	original.doc.ExternalDocs.URL = "https://changed.example.com"
	*original.doc.Success.Code = 3
	*original.doc.Errors[0].Code = 4

	if got := cloned.doc.Tags[0]; got != "users" {
		t.Fatalf("cloned tag = %q, want users", got)
	}
	if got := cloned.doc.ExternalDocs.URL; got != "https://example.com" {
		t.Fatalf("cloned external docs URL = %q", got)
	}
	if got := *cloned.doc.Success.Code; got != 1 {
		t.Fatalf("cloned success code = %d, want 1", got)
	}
	if got := *cloned.doc.Errors[0].Code; got != 2 {
		t.Fatalf("cloned error code = %d, want 2", got)
	}
}

func TestOperationRejectsNilTypedHandler(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	var handler func(context.Context, EmptyInput) (EmptyInput, error)

	defer func() {
		if recover() == nil {
			t.Fatal("To(nil) did not panic")
		}
	}()
	server.MustMount(Handle(Get("/users"), NoInput(), JSONOutput[EmptyInput](), handler))
}

func TestOperationRejectsNilRawHandler(t *testing.T) {
	t.Parallel()

	server := New()
	var handler http.Handler

	defer func() {
		if recover() == nil {
			t.Fatal("ToHTTP(nil) did not panic")
		}
	}()
	server.MustMount(RawOperation(http.MethodGet, "/users", handler))
}

func TestOperationRejectsNilParsedHandler(t *testing.T) {
	t.Parallel()

	server := New()
	var handler HTTPHandlerFunc[EmptyInput]

	defer func() {
		if recover() == nil {
			t.Fatal("ToHTTPFunc(nil) did not panic")
		}
	}()
	server.MustMount(HandleHTTP(Get("/users"), NoInput(), handler))
}

func TestOperationRejectsEmptyStaticRoot(t *testing.T) {
	t.Parallel()

	server := New()
	defer func() {
		if recover() == nil {
			t.Fatal("ToStatic() did not panic")
		}
	}()
	server.MustMount(StaticDirectory("/files", ""))
}

func TestOperationRejectsNilSpecializedHandlers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		register func(*Server)
	}{
		{
			name: "redirect",
			register: func(server *Server) {
				var handler RedirectFunc[EmptyInput]
				server.MustMount(RedirectFuncOperation(Get("/users"), NoInput(), http.StatusFound, handler))
			},
		},
		{
			name: "sse",
			register: func(server *Server) {
				var handler func(context.Context, EmptyInput, *SSEWriter) error
				server.MustMount(SSEOperation("/users", NoInput(), handler))
			},
		},
		{
			name: "websocket",
			register: func(server *Server) {
				var handler func(context.Context, EmptyInput, *WebSocketConn) error
				server.MustMount(WebSocketOperation("/users", NoInput(), handler))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s nil handler did not panic", tt.name)
				}
			}()
			tt.register(New())
		})
	}
}

func TestOperationNormalizesCustomMethod(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Endpoint("purge", "/cache/{key}"), NoInput(), JSONOutput[EmptyInput](), func(context.Context, EmptyInput) (EmptyInput, error) {
		return EmptyInput{}, nil
	}))

	definitions := server.registry.snapshot()
	if len(definitions) != 1 {
		t.Fatalf("definition count = %d, want 1", len(definitions))
	}
	if got := definitions[0].method; got != "PURGE" {
		t.Fatalf("method = %q, want PURGE", got)
	}
}

func assertRoutePanic(t *testing.T, want error, fn func()) {
	t.Helper()
	defer func() {
		t.Helper()
		got := recover()
		if got == nil {
			t.Fatalf("panic = nil, want errors.Is(_, %v)", want)
		}
		err, ok := got.(error)
		if !ok || !errors.Is(err, want) {
			t.Fatalf("panic = %#v, want errors.Is(_, %v)", got, want)
		}
	}()
	fn()
}

func mustRouteDefinition(t *testing.T, method, path string) routeDefinition {
	t.Helper()
	pattern, err := parseRoutePattern(path, false)
	if err != nil {
		t.Fatalf("parseRoutePattern(%q) error = %v", path, err)
	}
	return routeDefinition{method: method, pattern: pattern}
}

func mustRouteDefinitionStrict(t *testing.T, method, path string) routeDefinition {
	t.Helper()
	pattern, err := parseRoutePattern(path, true)
	if err != nil {
		t.Fatalf("parseRoutePattern(%q) error = %v", path, err)
	}
	return routeDefinition{method: method, pattern: pattern}
}
