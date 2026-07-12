package legacyrouter

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Matchit is the frozen pre-rebuild byte-prefix radix router.
type Matchit struct {
	methods map[string]*matchitMethod
}

type matchitMethod struct {
	root   *matchitNode
	static map[string]*routeEntry
	paths  map[string]struct{}
}

type matchitNode struct {
	prefix        string
	staticChild   map[byte]*matchitNode
	paramChild    *matchitNode
	paramName     string
	wildcardChild *matchitNode
	wildcardName  string
	entry         *routeEntry
}

type matchitRoutePart struct {
	kind string
	text string
}

// NewMatchit creates a frozen-baseline byte-prefix radix router.
func NewMatchit() *Matchit {
	return &Matchit{methods: make(map[string]*matchitMethod)}
}

func newMatchitMethod() *matchitMethod {
	return &matchitMethod{
		root:   &matchitNode{},
		static: make(map[string]*routeEntry),
		paths:  make(map[string]struct{}),
	}
}

// Register adds one handler using the historical byte-prefix registration
// path.
func (r *Matchit) Register(method, path string, handler http.Handler) error {
	method = strings.ToUpper(method)
	m := r.methods[method]
	if m == nil {
		m = newMatchitMethod()
		r.methods[method] = m
	}
	path = normalizeRoutePath(path)
	if _, exists := m.paths[path]; exists {
		return fmt.Errorf("%w: route %s already registered", ErrConflict, path)
	}
	m.paths[path] = struct{}{}
	entry := newRouteEntry(path, handler)
	if !strings.Contains(path, ":") && !strings.Contains(path, "*") {
		m.static[path] = entry
		return nil
	}
	m.insert(path, entry)
	return nil
}

// RegisterWithPathParams adds a handler that receives extracted path values.
func (r *Matchit) RegisterWithPathParams(method, path string, handler PathParamHandler) error {
	return registerWithPathParams(r.Register, method, path, handler)
}

func (m *matchitMethod) insert(path string, entry *routeEntry) {
	node := m.root
	for _, part := range matchitRouteParts(path) {
		switch part.kind {
		case "static":
			node = node.insertStatic(part.text)
		case "param":
			if node.paramChild == nil {
				node.paramChild = &matchitNode{}
			}
			node.paramChild.paramName = part.text
			node = node.paramChild
		case "wildcard":
			if node.wildcardChild == nil {
				node.wildcardChild = &matchitNode{}
			}
			node.wildcardChild.wildcardName = part.text
			node.wildcardChild.entry = entry
			return
		}
	}
	node.entry = entry
}

func matchitRouteParts(path string) []matchitRoutePart {
	segments := splitPathSegments(path)
	if len(segments) == 0 {
		return nil
	}
	parts := make([]matchitRoutePart, 0, len(segments))
	var static strings.Builder
	flushStatic := func() {
		if static.Len() == 0 {
			return
		}
		parts = append(parts, matchitRoutePart{kind: "static", text: static.String()})
		static.Reset()
	}
	for _, segment := range segments {
		switch {
		case strings.HasPrefix(segment, ":"):
			if static.Len() == 0 {
				static.WriteByte('/')
			} else if !strings.HasSuffix(static.String(), "/") {
				static.WriteByte('/')
			}
			flushStatic()
			parts = append(parts, matchitRoutePart{kind: "param", text: strings.TrimPrefix(segment, ":")})
		case strings.HasPrefix(segment, "*"):
			if static.Len() == 0 {
				static.WriteByte('/')
			} else if !strings.HasSuffix(static.String(), "/") {
				static.WriteByte('/')
			}
			flushStatic()
			parts = append(parts, matchitRoutePart{kind: "wildcard", text: strings.TrimPrefix(segment, "*")})
			return parts
		default:
			static.WriteByte('/')
			static.WriteString(segment)
		}
	}
	flushStatic()
	return parts
}

func (n *matchitNode) insertStatic(prefix string) *matchitNode {
	if prefix == "" {
		return n
	}
	if n.staticChild == nil {
		n.staticChild = make(map[byte]*matchitNode)
	}
	child := n.staticChild[prefix[0]]
	if child == nil {
		child = &matchitNode{prefix: prefix}
		n.staticChild[prefix[0]] = child
		return child
	}

	common := commonPrefixLen(prefix, child.prefix)
	if common == len(child.prefix) {
		return child.insertStatic(prefix[common:])
	}
	old := *child
	old.prefix = child.prefix[common:]
	*child = matchitNode{
		prefix:      child.prefix[:common],
		staticChild: map[byte]*matchitNode{old.prefix[0]: &old},
	}
	if common == len(prefix) {
		return child
	}
	next := &matchitNode{prefix: prefix[common:]}
	child.staticChild[next.prefix[0]] = next
	return next
}

func commonPrefixLen(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	i := 0
	for i < max && a[i] == b[i] {
		i++
	}
	return i
}

// ServeHTTP retains the historical byte-prefix lookup and method handling.
func (r *Matchit) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

func (r *Matchit) lookup(method, path string, params *PathParams) *routeEntry {
	m := r.methods[method]
	if m == nil {
		params.Reset()
		return nil
	}
	return m.lookup(path, params)
}

func (m *matchitMethod) lookup(path string, params *PathParams) *routeEntry {
	if entry := m.static[path]; entry != nil {
		params.Reset()
		return entry
	}
	params.Reset()
	if entry := m.root.lookup(path, 0, params); entry != nil {
		return entry
	}
	params.Reset()
	return nil
}

func (n *matchitNode) lookup(path string, index int, params *PathParams) *routeEntry {
	if n.prefix != "" {
		if len(path[index:]) < len(n.prefix) || path[index:index+len(n.prefix)] != n.prefix {
			return nil
		}
		index += len(n.prefix)
	}
	if index == len(path) {
		return n.entry
	}
	if n.staticChild != nil {
		if child := n.staticChild[path[index]]; child != nil {
			if entry := child.lookup(path, index, params); entry != nil {
				return entry
			}
		}
	}
	if n.paramChild != nil {
		end := index
		for end < len(path) && path[end] != '/' {
			end++
		}
		if end > index {
			paramLen := params.Len()
			params.Add(n.paramChild.paramName, path[index:end])
			if entry := n.paramChild.lookup(path, end, params); entry != nil {
				return entry
			}
			params.Truncate(paramLen)
		}
	}
	if n.wildcardChild != nil {
		params.Add(n.wildcardChild.wildcardName, remainingPath(path, index))
		return n.wildcardChild.entry
	}
	return nil
}

func (r *Matchit) allowedMethods(path string) []string {
	seen := make(map[string]struct{})
	allowed := make([]string, 0, len(r.methods))
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
	for method := range r.methods {
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
