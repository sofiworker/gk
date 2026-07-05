package ghttp

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// contextKey is used to store path params in request context.
type contextKey string

const pathParamsKey = contextKey("http-path-params")

// pathParams extracts path parameters from the request context.
func pathParams(r *http.Request) map[string]string {
	params, _ := r.Context().Value(pathParamsKey).(map[string]string)
	return params
}

// MethodMatcher groups routes by HTTP method.
type MethodMatcher struct {
	staticGroup  map[string]*routeEntry
	segmentIndex map[int]*CompressedRadixTree
	radixTree    *CompressedRadixTree
	paths        map[string]struct{}
}

func newMethodMatcher() *MethodMatcher {
	return &MethodMatcher{
		staticGroup:  make(map[string]*routeEntry),
		segmentIndex: make(map[int]*CompressedRadixTree),
		paths:        make(map[string]struct{}),
	}
}

func (m *MethodMatcher) add(path string, handler http.Handler) error {
	if _, exists := m.paths[path]; exists {
		return fmt.Errorf("%w: route %s already registered", ErrConflict, path)
	}
	m.paths[path] = struct{}{}
	entry := newRouteEntry(path, handler)

	hasParam := strings.Contains(path, ":")
	hasWildcard := strings.Contains(path, "*")

	if !hasParam && !hasWildcard {
		m.staticGroup[path] = entry
		return nil
	}

	segCount := pathSegmentCount(path)

	if !hasWildcard {
		tree, ok := m.segmentIndex[segCount]
		if !ok {
			tree = newCompressedRadixTree()
			m.segmentIndex[segCount] = tree
		}
		tree.insert(entry)
		return nil
	}

	if m.radixTree == nil {
		m.radixTree = newCompressedRadixTree()
	}
	m.radixTree.insert(entry)
	return nil
}

func (m *MethodMatcher) lookup(path string, params *pathParamList) *routeEntry {
	// 1. Static match first
	if entry, ok := m.staticGroup[path]; ok {
		params.Reset()
		return entry
	}

	// 2. Parameterized match by segment count
	segCount := pathSegmentCount(path)

	if tree, ok := m.segmentIndex[segCount]; ok {
		params.Reset()
		if entry := tree.lookup(path, params); entry != nil {
			return entry
		}
	}

	// 3. Wildcard match
	if m.radixTree != nil {
		params.Reset()
		if entry := m.radixTree.lookup(path, params); entry != nil {
			return entry
		}
	}

	params.Reset()
	return nil
}

// RadixRouter implements Router using the three-layer matching engine.
type RadixRouter struct {
	methodMatchers map[string]*MethodMatcher
}

func NewRadixRouter() *RadixRouter {
	return &RadixRouter{
		methodMatchers: make(map[string]*MethodMatcher),
	}
}

func (r *RadixRouter) Register(method, path string, handler http.Handler) error {
	method = strings.ToUpper(method)
	m, ok := r.methodMatchers[method]
	if !ok {
		m = newMethodMatcher()
		r.methodMatchers[method] = m
	}
	path = normalizeRoutePath(path)
	return m.add(path, handler)
}

func (r *RadixRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	method := req.Method
	path := req.URL.Path
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}

	var params pathParamList
	if method == http.MethodHead {
		if entry := r.lookup(http.MethodHead, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
		if entry := r.lookup(http.MethodGet, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
	} else if entry := r.lookup(method, path, &params); entry != nil {
		serveRouteEntry(w, req, entry, params)
		return
	}

	allowed := r.allowedMethods(path)
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

func (r *RadixRouter) lookup(method, path string, params *pathParamList) *routeEntry {
	m, ok := r.methodMatchers[method]
	if !ok {
		params.Reset()
		return nil
	}
	return m.lookup(path, params)
}

func serveRouteEntry(w http.ResponseWriter, req *http.Request, entry *routeEntry, params pathParamList) {
	if handler, ok := entry.handler.(pathParamHandler); ok {
		handler.ServeHTTPWithPathParams(w, req, params)
		return
	}

	entry.handler.ServeHTTP(w, req)
}

func (r *RadixRouter) allowedMethods(path string) []string {
	seen := make(map[string]struct{})
	var allowed []string
	add := func(method string) {
		if _, ok := seen[method]; ok {
			return
		}
		seen[method] = struct{}{}
		allowed = append(allowed, method)
	}

	for _, method := range allHTTPMethods {
		var params pathParamList
		if r.lookup(method, path, &params) == nil {
			continue
		}
		add(method)
		if method == http.MethodGet {
			add(http.MethodHead)
		}
	}

	var custom []string
	for method := range r.methodMatchers {
		if _, ok := seen[method]; ok || isStandardHTTPMethod(method) {
			continue
		}
		var params pathParamList
		if r.lookup(method, path, &params) != nil {
			custom = append(custom, method)
		}
	}
	sort.Strings(custom)
	for _, method := range custom {
		add(method)
	}

	return allowed
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
