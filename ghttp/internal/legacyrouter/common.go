// Package legacyrouter contains frozen pre-rebuild router implementations.
// They are retained only for ghttp correctness checks and benchmark baselines.
package legacyrouter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

const maxStackPathParams = 16

var (
	// ErrConflict reports a duplicate route registration within one method.
	ErrConflict = errors.New("route conflict")

	allHTTPMethods = []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	}
)

// Router is the minimal common surface of the frozen router baselines.
type Router interface {
	http.Handler
	Register(method, path string, handler http.Handler) error
	RegisterWithPathParams(method, path string, handler PathParamHandler) error
}

// PathParam is one parameter extracted from a matched route.
type PathParam struct {
	Key   string
	Value string
}

// PathParams is the allocation-light parameter container used by the frozen
// routers. The first sixteen values are stored inline, as in the legacy code.
type PathParams struct {
	values   [maxStackPathParams]PathParam
	overflow []PathParam
	len      int
}

// Get returns the first value associated with key, or an empty string.
func (ps PathParams) Get(key string) string {
	for i := 0; i < ps.len; i++ {
		if param := ps.values[i]; param.Key == key {
			return param.Value
		}
	}
	for _, param := range ps.overflow {
		if param.Key == key {
			return param.Value
		}
	}
	return ""
}

// Len returns the number of extracted parameters.
func (ps PathParams) Len() int {
	return ps.len + len(ps.overflow)
}

// Range calls fn once for every parameter in extraction order.
func (ps PathParams) Range(fn func(PathParam)) {
	for i := 0; i < ps.len; i++ {
		fn(ps.values[i])
	}
	for _, param := range ps.overflow {
		fn(param)
	}
}

func (ps *PathParams) Add(key, value string) {
	if ps.len < len(ps.values) {
		ps.values[ps.len] = PathParam{Key: key, Value: value}
		ps.len++
		return
	}
	ps.overflow = append(ps.overflow, PathParam{Key: key, Value: value})
}

func (ps *PathParams) Reset() {
	for i := 0; i < ps.len; i++ {
		ps.values[i] = PathParam{}
	}
	ps.len = 0
	ps.overflow = ps.overflow[:0]
}

func (ps *PathParams) Truncate(n int) {
	if n <= len(ps.values) {
		for i := n; i < ps.len; i++ {
			ps.values[i] = PathParam{}
		}
		ps.len = n
		ps.overflow = ps.overflow[:0]
		return
	}
	ps.len = len(ps.values)
	ps.overflow = ps.overflow[:n-len(ps.values)]
}

func (ps PathParams) toMap() map[string]string {
	if ps.Len() == 0 {
		return nil
	}
	params := make(map[string]string, ps.Len())
	ps.Range(func(param PathParam) {
		params[param.Key] = param.Value
	})
	return params
}

// PathParamHandler is the benchmark adapter for extracted route parameters.
type PathParamHandler interface {
	ServeHTTPWithPathParams(http.ResponseWriter, *http.Request, PathParams)
}

type pathParamHandlerWrapper struct {
	handler PathParamHandler
}

func (h pathParamHandlerWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTPWithPathParams(w, r, PathParams{})
}

func (h pathParamHandlerWrapper) ServeHTTPWithPathParams(w http.ResponseWriter, r *http.Request, params PathParams) {
	h.handler.ServeHTTPWithPathParams(w, r, params)
}

func registerWithPathParams(register func(string, string, http.Handler) error, method, path string, handler PathParamHandler) error {
	if handler == nil {
		return fmt.Errorf("path parameter handler is nil")
	}
	return register(method, path, pathParamHandlerWrapper{handler: handler})
}

type routeEntry struct {
	path       string
	handler    http.Handler
	paramNames []string
}

func newRouteEntry(path string, handler http.Handler) *routeEntry {
	return &routeEntry{
		path:       path,
		handler:    handler,
		paramNames: extractParamNames(path),
	}
}

func serveRouteEntry(w http.ResponseWriter, req *http.Request, entry *routeEntry, params PathParams) {
	if handler, ok := entry.handler.(PathParamHandler); ok {
		handler.ServeHTTPWithPathParams(w, req, params)
		return
	}
	entry.handler.ServeHTTP(w, req)
}

func normalizeRoutePath(path string) string {
	if strings.HasPrefix(path, "/") {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	if containsColonParam(path) {
		log.Printf("[ghttp] WARN route path %q uses deprecated :param syntax; use {param} syntax instead", path)
	}
	return convertBraceParamsToColon(path)
}

func containsColonParam(path string) bool {
	for _, segment := range splitPathSegments(path) {
		if strings.HasPrefix(segment, ":") && len(segment) > 1 {
			return true
		}
	}
	return false
}

func convertBraceParamsToColon(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if !strings.HasPrefix(part, "{") || !strings.HasSuffix(part, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
		if strings.HasSuffix(name, "...") {
			parts[i] = "*" + strings.TrimSuffix(name, "...")
			continue
		}
		parts[i] = ":" + name
	}
	return strings.Join(parts, "/")
}

func splitPathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func pathSegmentCount(path string) int {
	if path == "/" || path == "" {
		return 0
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return 0
	}
	count := 1
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			count++
		}
	}
	return count
}

func extractParamNames(path string) []string {
	var names []string
	for _, segment := range splitPathSegments(path) {
		if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
			names = append(names, segment[1:])
		}
	}
	return names
}

func nextPathSegment(path string, index int) (string, int, bool) {
	for index < len(path) && path[index] == '/' {
		index++
	}
	if index >= len(path) {
		return "", index, false
	}
	end := index
	for end < len(path) && path[end] != '/' {
		end++
	}
	return path[index:end], end, true
}

func remainingPath(path string, index int) string {
	for index < len(path) && path[index] == '/' {
		index++
	}
	return path[index:]
}

func normalizedRequestPath(path string) string {
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	return path
}

func isStandardHTTPMethod(method string) bool {
	for _, standard := range allHTTPMethods {
		if method == standard {
			return true
		}
	}
	return false
}

type headResponseWriter struct {
	http.ResponseWriter
}

func (w headResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

type contextKey string

const pathParamsKey contextKey = "http-path-params"

func requestWithPathParams(r *http.Request, params PathParams) *http.Request {
	if params.Len() == 0 {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), pathParamsKey, params.toMap()))
}
