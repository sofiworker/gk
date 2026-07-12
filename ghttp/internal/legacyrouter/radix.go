package legacyrouter

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const radixStaticIndexThreshold = 8

// Radix is the frozen pre-rebuild three-layer compressed radix router.
type Radix struct {
	methodMatchers map[string]*methodMatcher
}

type methodMatcher struct {
	staticGroup map[string]*routeEntry
	paramTree   *compressedRadixTree
	radixTree   *compressedRadixTree
	paths       map[string]struct{}
}

type radixStaticChild struct {
	segment string
	node    *compressedRadixNode
}

type compressedRadixNode struct {
	prefix         string
	staticChildren []radixStaticChild
	staticIndex    map[string]*compressedRadixNode
	paramChild     *compressedRadixNode
	paramName      string
	wildcardChild  *compressedRadixNode
	entry          *routeEntry
}

type compressedRadixTree struct {
	root *compressedRadixNode
}

// NewRadix creates a frozen-baseline radix router.
func NewRadix() *Radix {
	return &Radix{methodMatchers: make(map[string]*methodMatcher)}
}

func newMethodMatcher() *methodMatcher {
	return &methodMatcher{
		staticGroup: make(map[string]*routeEntry),
		paths:       make(map[string]struct{}),
	}
}

// Register adds one handler using the historical radix registration path.
func (r *Radix) Register(method, path string, handler http.Handler) error {
	method = strings.ToUpper(method)
	matcher := r.methodMatchers[method]
	if matcher == nil {
		matcher = newMethodMatcher()
		r.methodMatchers[method] = matcher
	}
	return matcher.add(normalizeRoutePath(path), handler)
}

// RegisterWithPathParams adds a handler that receives extracted path values.
func (r *Radix) RegisterWithPathParams(method, path string, handler PathParamHandler) error {
	return registerWithPathParams(r.Register, method, path, handler)
}

func (m *methodMatcher) add(path string, handler http.Handler) error {
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
	if !hasWildcard {
		if m.paramTree == nil {
			m.paramTree = newCompressedRadixTree()
		}
		m.paramTree.insert(entry)
		return nil
	}
	if m.radixTree == nil {
		m.radixTree = newCompressedRadixTree()
	}
	m.radixTree.insert(entry)
	return nil
}

// ServeHTTP retains the historical static, parameter, then wildcard lookup
// ordering and method outcome handling.
func (r *Radix) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := normalizedRequestPath(req.URL.Path)
	var params PathParams
	if req.Method == http.MethodHead {
		if entry := r.lookup(http.MethodHead, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
		if entry := r.lookup(http.MethodGet, path, &params); entry != nil {
			serveRouteEntry(headResponseWriter{ResponseWriter: w}, req, entry, params)
			return
		}
	} else if entry := r.lookup(req.Method, path, &params); entry != nil {
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

func (r *Radix) lookup(method, path string, params *PathParams) *routeEntry {
	matcher := r.methodMatchers[method]
	if matcher == nil {
		params.Reset()
		return nil
	}
	return matcher.lookup(path, params)
}

func (r *Radix) allowedMethods(path string) []string {
	seen := make(map[string]struct{})
	allowed := make([]string, 0, len(r.methodMatchers))
	add := func(method string) {
		if _, ok := seen[method]; ok {
			return
		}
		seen[method] = struct{}{}
		allowed = append(allowed, method)
	}
	for _, method := range allHTTPMethods {
		var params PathParams
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
		var params PathParams
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

func (m *methodMatcher) lookup(path string, params *PathParams) *routeEntry {
	if entry, ok := m.staticGroup[path]; ok {
		params.Reset()
		return entry
	}
	if m.paramTree != nil {
		params.Reset()
		if entry := m.paramTree.lookup(path, params); entry != nil {
			return entry
		}
	}
	if m.radixTree != nil {
		params.Reset()
		if entry := m.radixTree.lookup(path, params); entry != nil {
			return entry
		}
	}
	params.Reset()
	return nil
}

func newCompressedRadixTree() *compressedRadixTree {
	return &compressedRadixTree{root: &compressedRadixNode{}}
}

func (t *compressedRadixTree) insert(entry *routeEntry) {
	segments := splitPathSegments(entry.path)
	if len(segments) == 0 {
		t.root.entry = entry
		return
	}
	node := t.root
	for i, segment := range segments {
		if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
			isWildcard := strings.HasPrefix(segment, "*")
			paramName := segment[1:]
			if isWildcard {
				if node.wildcardChild == nil {
					node.wildcardChild = &compressedRadixNode{}
				}
				node.wildcardChild.entry = entry
				node.wildcardChild.prefix = segment
				return
			}
			if node.paramChild == nil {
				node.paramChild = &compressedRadixNode{}
			}
			node.paramChild.paramName = paramName
			if i == len(segments)-1 {
				node.paramChild.entry = entry
			}
			node = node.paramChild
			continue
		}

		child := node.staticChild(segment)
		if child == nil {
			child = &compressedRadixNode{prefix: segment}
			node.addStaticChild(segment, child)
		}
		if i == len(segments)-1 {
			child.entry = entry
		}
		node = child
	}
}

func (t *compressedRadixTree) lookup(path string, params *PathParams) *routeEntry {
	return t.lookupRecursive(t.root, path, 0, params)
}

func (t *compressedRadixTree) lookupRecursive(node *compressedRadixNode, path string, index int, params *PathParams) *routeEntry {
	segment, next, ok := nextPathSegment(path, index)
	if !ok {
		return node.entry
	}
	if child := node.staticChild(segment); child != nil {
		if entry := t.lookupRecursive(child, path, next, params); entry != nil {
			return entry
		}
	}
	if node.paramChild != nil {
		paramCount := params.Len()
		params.Add(node.paramChild.paramName, segment)
		if entry := t.lookupRecursive(node.paramChild, path, next, params); entry != nil {
			return entry
		}
		params.Truncate(paramCount)
	}
	if node.wildcardChild != nil && node.wildcardChild.entry != nil {
		params.Add(strings.TrimPrefix(node.wildcardChild.prefix, "*"), remainingPath(path, index))
		return node.wildcardChild.entry
	}
	return nil
}

func (n *compressedRadixNode) staticChild(segment string) *compressedRadixNode {
	if n.staticIndex != nil {
		return n.staticIndex[segment]
	}
	for i := range n.staticChildren {
		if n.staticChildren[i].segment == segment {
			return n.staticChildren[i].node
		}
	}
	return nil
}

func (n *compressedRadixNode) addStaticChild(segment string, child *compressedRadixNode) {
	n.staticChildren = append(n.staticChildren, radixStaticChild{segment: segment, node: child})
	if n.staticIndex != nil {
		n.staticIndex[segment] = child
		return
	}
	if len(n.staticChildren) < radixStaticIndexThreshold {
		return
	}
	n.staticIndex = make(map[string]*compressedRadixNode, len(n.staticChildren))
	for _, staticChild := range n.staticChildren {
		n.staticIndex[staticChild.segment] = staticChild.node
	}
}
