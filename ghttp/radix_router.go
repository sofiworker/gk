package ghttp

import (
	"net/http"
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
}

func newMethodMatcher() *MethodMatcher {
	return &MethodMatcher{
		staticGroup:  make(map[string]*routeEntry),
		segmentIndex: make(map[int]*CompressedRadixTree),
	}
}

func (m *MethodMatcher) add(path string, handler http.Handler) {
	entry := newRouteEntry(path, handler)

	hasParam := strings.Contains(path, ":")
	hasWildcard := strings.Contains(path, "*")

	if !hasParam && !hasWildcard {
		m.staticGroup[path] = entry
		return
	}

	segCount := pathSegmentCount(path)

	if !hasWildcard {
		tree, ok := m.segmentIndex[segCount]
		if !ok {
			tree = newCompressedRadixTree()
			m.segmentIndex[segCount] = tree
		}
		tree.insert(entry)
		return
	}

	if m.radixTree == nil {
		m.radixTree = newCompressedRadixTree()
	}
	m.radixTree.insert(entry)
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
	m.add(path, handler)
	return nil
}

func (r *RadixRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	method := req.Method
	path := req.URL.Path
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}

	m, ok := r.methodMatchers[method]
	if !ok {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var params pathParamList
	entry := m.lookup(path, &params)
	if entry == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if handler, ok := entry.handler.(pathParamHandler); ok {
		handler.ServeHTTPWithPathParams(w, req, params)
		return
	}

	entry.handler.ServeHTTP(w, req)
}
