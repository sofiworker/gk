package ghttp

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// CompiledRouter is an experimental Router that stores route nodes in compact
// slices and walks them by integer index.
type CompiledRouter struct {
	methods map[string]*compiledMethod
}

type compiledMethod struct {
	static       map[string]*routeEntry
	roots        map[int]int
	wildcardRoot int
	nodes        []compiledNode
	paths        map[string]struct{}
}

type compiledNode struct {
	staticChildren []compiledStaticChild
	staticIndex    map[string]int
	paramChild     int
	paramName      string
	wildcardChild  int
	entry          *routeEntry
}

type compiledStaticChild struct {
	segment string
	index   int
}

func NewCompiledRouter() *CompiledRouter {
	return &CompiledRouter{methods: make(map[string]*compiledMethod)}
}

func newCompiledMethod() *compiledMethod {
	return &compiledMethod{
		static:       make(map[string]*routeEntry),
		roots:        make(map[int]int),
		wildcardRoot: -1,
		paths:        make(map[string]struct{}),
	}
}

func (r *CompiledRouter) Register(method, path string, handler http.Handler) error {
	method = strings.ToUpper(method)
	m := r.methods[method]
	if m == nil {
		m = newCompiledMethod()
		r.methods[method] = m
	}

	path = normalizeRoutePath(path)
	if _, exists := m.paths[path]; exists {
		return fmt.Errorf("%w: route %s already registered", ErrConflict, path)
	}
	m.paths[path] = struct{}{}

	entry := newRouteEntry(path, handler)
	hasParam := strings.Contains(path, ":")
	hasWildcard := strings.Contains(path, "*")
	if !hasParam && !hasWildcard {
		m.static[path] = entry
		return nil
	}

	segments := splitPathSegments(path)
	if hasWildcard {
		if m.wildcardRoot < 0 {
			m.wildcardRoot = m.addNode()
		}
		m.insert(m.wildcardRoot, segments, entry)
		return nil
	}

	segCount := len(segments)
	root, ok := m.roots[segCount]
	if !ok {
		root = m.addNode()
		m.roots[segCount] = root
	}
	m.insert(root, segments, entry)
	return nil
}

func (m *compiledMethod) addNode() int {
	m.nodes = append(m.nodes, compiledNode{
		paramChild:    -1,
		wildcardChild: -1,
	})
	return len(m.nodes) - 1
}

func (m *compiledMethod) insert(root int, segments []string, entry *routeEntry) {
	if len(segments) == 0 {
		m.nodes[root].entry = entry
		return
	}

	nodeIndex := root
	for i, segment := range segments {
		last := i == len(segments)-1

		if strings.HasPrefix(segment, "*") {
			child := m.nodes[nodeIndex].wildcardChild
			if child < 0 {
				child = m.addNode()
				m.nodes[nodeIndex].wildcardChild = child
			}
			m.nodes[child].paramName = strings.TrimPrefix(segment, "*")
			m.nodes[child].entry = entry
			return
		}

		if strings.HasPrefix(segment, ":") {
			child := m.nodes[nodeIndex].paramChild
			if child < 0 {
				child = m.addNode()
				m.nodes[nodeIndex].paramChild = child
			}
			m.nodes[child].paramName = strings.TrimPrefix(segment, ":")
			if last {
				m.nodes[child].entry = entry
			}
			nodeIndex = child
			continue
		}

		child := m.findStaticChild(nodeIndex, segment)
		if child < 0 {
			child = m.addNode()
			m.addStaticChild(nodeIndex, segment, child)
		}
		if last {
			m.nodes[child].entry = entry
		}
		nodeIndex = child
	}
}

func (m *compiledMethod) findStaticChild(nodeIndex int, segment string) int {
	node := &m.nodes[nodeIndex]
	if node.staticIndex != nil {
		if child, ok := node.staticIndex[segment]; ok {
			return child
		}
		return -1
	}
	for _, child := range node.staticChildren {
		if child.segment == segment {
			return child.index
		}
	}
	return -1
}

func (m *compiledMethod) addStaticChild(nodeIndex int, segment string, childIndex int) {
	node := &m.nodes[nodeIndex]
	node.staticChildren = append(node.staticChildren, compiledStaticChild{
		segment: segment,
		index:   childIndex,
	})
	if node.staticIndex != nil {
		node.staticIndex[segment] = childIndex
		return
	}
	if len(node.staticChildren) < 8 {
		return
	}
	node.staticIndex = make(map[string]int, len(node.staticChildren))
	for _, child := range node.staticChildren {
		node.staticIndex[child.segment] = child.index
	}
}

func (r *CompiledRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	method := req.Method
	path := strings.TrimRight(req.URL.Path, "/")
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

func (r *CompiledRouter) lookup(method, path string, params *pathParamList) *routeEntry {
	m := r.methods[method]
	if m == nil {
		params.Reset()
		return nil
	}
	return m.lookup(path, params)
}

func (m *compiledMethod) lookup(path string, params *pathParamList) *routeEntry {
	if entry := m.static[path]; entry != nil {
		params.Reset()
		return entry
	}

	if len(m.roots) > 0 {
		segCount := pathSegmentCount(path)
		if root, ok := m.roots[segCount]; ok {
			params.Reset()
			if entry := m.lookupNode(root, path, 0, params); entry != nil {
				return entry
			}
		}
	}

	if m.wildcardRoot >= 0 {
		params.Reset()
		if entry := m.lookupNode(m.wildcardRoot, path, 0, params); entry != nil {
			return entry
		}
	}

	params.Reset()
	return nil
}

func (m *compiledMethod) lookupNode(nodeIndex int, path string, index int, params *pathParamList) *routeEntry {
	node := &m.nodes[nodeIndex]
	segment, next, ok := nextPathSegment(path, index)
	if !ok {
		return node.entry
	}

	if child := m.findStaticChild(nodeIndex, segment); child >= 0 {
		if entry := m.lookupNode(child, path, next, params); entry != nil {
			return entry
		}
	}

	if node.paramChild >= 0 {
		child := &m.nodes[node.paramChild]
		paramLen := params.Len()
		params.Add(child.paramName, segment)
		if entry := m.lookupNode(node.paramChild, path, next, params); entry != nil {
			return entry
		}
		params.Truncate(paramLen)
	}

	if node.wildcardChild >= 0 {
		child := &m.nodes[node.wildcardChild]
		params.Add(child.paramName, remainingPath(path, index))
		return child.entry
	}

	return nil
}

func (r *CompiledRouter) allowedMethods(path string) []string {
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
	for method := range r.methods {
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
