package legacyrouter

import (
	"net/http"
	"strings"
)

// Std is the frozen pre-rebuild wrapper around Go 1.22 ServeMux patterns.
type Std struct {
	mux *http.ServeMux
}

// NewStd creates a frozen-baseline standard-library router.
func NewStd() *Std {
	return &Std{mux: http.NewServeMux()}
}

// Register adds one method-qualified ServeMux pattern.
func (r *Std) Register(method, path string, handler http.Handler) error {
	normalized := normalizeRoutePath(path)
	paramNames := extractParamNames(normalized)
	converted := convertPathParams(normalized)
	r.mux.Handle(method+" "+converted, bridgeStdPathParams(paramNames, handler))
	return nil
}

// RegisterWithPathParams adds a handler that receives extracted path values.
func (r *Std) RegisterWithPathParams(method, path string, handler PathParamHandler) error {
	return registerWithPathParams(r.Register, method, path, handler)
}

// ServeHTTP keeps the historical trailing-slash normalization before passing
// a request to ServeMux.
func (r *Std) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := normalizedRequestPath(req.URL.Path)
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
		var params PathParams
		for _, name := range paramNames {
			if value := req.PathValue(name); value != "" {
				params.Add(name, value)
			}
		}
		if paramsHandler, ok := handler.(PathParamHandler); ok {
			paramsHandler.ServeHTTPWithPathParams(w, req, params)
			return
		}
		handler.ServeHTTP(w, requestWithPathParams(req, params))
	})
}

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
