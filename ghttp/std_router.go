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
	normalized := normalizeRoutePath(path)
	paramNames := extractParamNames(normalized)
	converted := convertPathParams(normalized)
	pattern := method + " " + converted
	r.mux.Handle(pattern, bridgeStdPathParams(paramNames, handler))
	return nil
}

func (r *StdRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimRight(req.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	if path != req.URL.Path {
		next := new(http.Request)
		*next = *req
		urlCopy := *req.URL
		urlCopy.Path = path
		urlCopy.RawPath = ""
		next.URL = &urlCopy
		req = next
	}
	r.mux.ServeHTTP(w, req)
}

func bridgeStdPathParams(paramNames []string, handler http.Handler) http.Handler {
	if len(paramNames) == 0 {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if paramsHandler, ok := handler.(pathParamHandler); ok {
			var params pathParamList
			for _, name := range paramNames {
				if value := req.PathValue(name); value != "" {
					params.Add(name, value)
				}
			}
			paramsHandler.ServeHTTPWithPathParams(w, req, params)
			return
		}

		var params pathParamList
		for _, name := range paramNames {
			if value := req.PathValue(name); value != "" {
				params.Add(name, value)
			}
		}
		handler.ServeHTTP(w, requestWithPathParams(req, params))
	})
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
