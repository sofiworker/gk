package ghttp

import (
	"net/http"
	"strings"
)

// StdRouter wraps http.ServeMux (Go 1.22+ pattern routing with {param} support).
type StdRouter struct {
	mux *http.ServeMux
}

func NewStdRouter() *StdRouter {
	return &StdRouter{mux: http.NewServeMux()}
}

func (r *StdRouter) Register(method, path string, handler http.Handler) error {
	converted := convertPathParams(normalizeRoutePath(path))
	pattern := method + " " + converted
	r.mux.Handle(pattern, handler)
	return nil
}

func (r *StdRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// convertPathParams converts ghttp-style :param and *wildcard to Go 1.22+ {param} syntax.
func convertPathParams(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		} else if strings.HasPrefix(part, "*") {
			parts[i] = "{" + part[1:] + "...}"
		}
	}
	return strings.Join(parts, "/")
}
