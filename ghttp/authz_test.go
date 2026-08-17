package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticRoles map[string][]string

func (r staticRoles) RolesFor(_ context.Context, subject string) ([]string, error) {
	return r[subject], nil
}

type staticPolicies map[string]bool

func (p staticPolicies) Allowed(_ context.Context, role, action, resource string) (bool, error) {
	return p[role+"|"+action+"|"+resource], nil
}

func TestRBACAuthorize(t *testing.T) {
	authz := NewRBAC(
		staticRoles{"alice": {"admin"}},
		staticPolicies{"admin|users:read|/users": true},
	)
	if err := authz.Authorize(context.Background(), "alice", "users:read", "/users"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := authz.Authorize(context.Background(), "bob", "users:read", "/users"); err == nil {
		t.Fatal("expected denial for unknown subject")
	}
}

func TestRBACMiddleware(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	authz := NewRBAC(
		staticRoles{"alice": {"admin"}},
		staticPolicies{"admin|users:read|/users": true},
	)
	app.Use(RBACMiddleware(
		authz,
		func(r *http.Request) string { return r.Header.Get("X-User") },
		func(r *http.Request) string { return "users:read" },
		func(r *http.Request) string { return r.URL.Path },
	))
	app.MustMount(Handle(Get("/users"), StructInput[Params](), JSONOutput[struct{}](), func(context.Context, Params) (struct{}, error) {
		return struct{}{}, nil
	}))

	okReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	okReq.Header.Set("X-User", "alice")
	okRec := httptest.NewRecorder()
	app.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, want 200", okRec.Code)
	}

	deniedReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	deniedReq.Header.Set("X-User", "bob")
	deniedRec := httptest.NewRecorder()
	app.ServeHTTP(deniedRec, deniedReq)
	if deniedRec.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d, want 403", deniedRec.Code)
	}
}
