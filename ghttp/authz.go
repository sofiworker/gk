package ghttp

import (
	"context"
	"net/http"
)

// Authorizer checks whether a subject may perform an action on a resource.
type Authorizer interface {
	Authorize(ctx context.Context, subject, action, resource string) error
}

// RoleResolver resolves the roles assigned to a subject.
type RoleResolver interface {
	RolesFor(ctx context.Context, subject string) ([]string, error)
}

// PolicyStore answers whether a role may perform an action on a resource.
type PolicyStore interface {
	Allowed(ctx context.Context, role, action, resource string) (bool, error)
}

// RBAC is the default role-based access control implementation.
type RBAC struct {
	roles    RoleResolver
	policies PolicyStore
}

// NewRBAC creates an RBAC authorizer.
func NewRBAC(roles RoleResolver, policies PolicyStore) *RBAC {
	return &RBAC{roles: roles, policies: policies}
}

// Authorize allows the request when any of the subject's roles is permitted.
func (r *RBAC) Authorize(ctx context.Context, subject, action, resource string) error {
	if r == nil || r.roles == nil || r.policies == nil {
		return ErrRBACNotConfigured
	}
	roles, err := r.roles.RolesFor(ctx, subject)
	if err != nil {
		return err
	}
	for _, role := range roles {
		allowed, err := r.policies.Allowed(ctx, role, action, resource)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	return ErrForbidden
}

// RBACMiddleware rejects requests that fail authorization with 403 Forbidden.
func RBACMiddleware(a Authorizer, subject, action, resource func(*http.Request) string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := a.Authorize(r.Context(), subject(r), action(r), resource(r)); err != nil {
				writeError(w, r, serverFromRequest(r), http.StatusForbidden, Err(http.StatusForbidden, http.StatusText(http.StatusForbidden)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
