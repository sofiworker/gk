package ghttp

import "net/http"

// Router is the pluggable routing interface.
// It implements http.Handler so the Server delegates to it directly.
// Routes are registered before the server starts; the route table is read-only at runtime.
type Router interface {
	http.Handler
	Register(method, path string, handler http.Handler) error
}
