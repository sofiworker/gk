package ghttp

import (
	"context"
	"net/http"
)

// Authorizer 判断主体是否可对资源执行动作。
// Authorizer checks whether a subject may act on a resource.
type Authorizer interface {
	Authorize(ctx context.Context, subject, action, resource string) error
}

// RoleResolver 解析主体拥有的角色。
// RoleResolver resolves the roles assigned to a subject.
type RoleResolver interface {
	RolesFor(ctx context.Context, subject string) ([]string, error)
}

// PolicyStore 判断角色是否可对资源执行动作。
// PolicyStore answers whether a role may act on a resource.
type PolicyStore interface {
	Allowed(ctx context.Context, role, action, resource string) (bool, error)
}

// RBAC 是默认的基于角色的访问控制实现。
// RBAC is the default role-based access control implementation.
type RBAC struct {
	roles    RoleResolver
	policies PolicyStore
}

// NewRBAC 创建 RBAC authorizer。
// NewRBAC creates an RBAC authorizer.
func NewRBAC(roles RoleResolver, policies PolicyStore) *RBAC {
	return &RBAC{roles: roles, policies: policies}
}

// Authorize 在主体的任一角色被允许时放行请求。
// Authorize allows the request when any subject role is permitted.
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

// RBACMiddleware 对鉴权失败的请求返回 403。
// RBACMiddleware rejects unauthorized requests with 403.
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
