package ghttp

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type routeMatchKind uint8

const (
	routeMatchFound routeMatchKind = iota
	routeMatchNotFound
	routeMatchMethodNotAllowed
)

type routeMatchResult struct {
	kind         routeMatchKind
	route        *compiledRoute
	allow        []string
	suppressBody bool
}

type compiledRoute struct {
	definition         routeDefinition
	handler            http.Handler
	directHandler      pathParamHandler
	directValueHandler directPathValueHandler
	directName         string
	stateFree          bool
	contextHandler     ContextHandler
	fastHandler        http.HandlerFunc
	// fast 表示该 raw 路由无需 requestState 注入即可安全执行。
	// fast marks a raw route that can run without requestState injection.
	fast bool
	// paramPos 记录参数名到模式段索引的映射,供惰性解码按 key 定位。
	// paramPos maps a param name to its pattern segment index for lazy decoding.
	paramPos map[string]int
}

// extractMatched 解码已成功匹配路由的动态段，不重复校验静态结构。
// extractMatched decodes dynamic segments of an already matched route without
// revalidating its static structure.
func (r *compiledRoute) extractMatched(path requestPath) (pathParamList, error) {
	var params pathParamList
	for index, segment := range r.definition.pattern.segments {
		switch segment.kind {
		case routeSegmentParameter:
			value, err := path.DecodeAt(index)
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, r.definition.pattern.path)
			}
			params.Add(segment.value, value)
		case routeSegmentCatchAll:
			value, err := url.PathUnescape(path.RawJoinFrom(index))
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, r.definition.pattern.path)
			}
			params.Add(segment.value, value)
		}
	}
	return params, nil
}

func (r *compiledRoute) extract(path requestPath) (pathParamList, error) {
	if r == nil {
		return pathParamList{}, fmt.Errorf("%w: route is nil", ErrInvalidRequestPath)
	}
	pattern := r.definition.pattern
	if pattern.trailing != path.trailing {
		return pathParamList{}, fmt.Errorf("%w: trailing slash does not match %s", ErrInvalidRequestPath, pattern.path)
	}

	var params pathParamList
	segmentIndex := 0
	for _, segment := range pattern.segments {
		switch segment.kind {
		case routeSegmentStatic:
			if segmentIndex >= path.segments.Len() || !rawSegmentMatches(path.RawAt(segmentIndex), segment.value) {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			segmentIndex++
		case routeSegmentParameter:
			if segmentIndex >= path.segments.Len() {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			value, err := path.DecodeAt(segmentIndex)
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			params.Add(segment.value, value)
			segmentIndex++
		case routeSegmentCatchAll:
			value, err := url.PathUnescape(path.RawJoinFrom(segmentIndex))
			if err != nil {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			params.Add(segment.value, value)
			segmentIndex = path.segments.Len()
		}
	}
	if segmentIndex != path.segments.Len() {
		return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
	}
	return params, nil
}

// validate 校验请求路径是否仍匹配路由结构,不解码参数值。
// validate checks the request path against the route structure without decoding values.
// 用于 extractorTerminal 的防御性回退:中间件替换 context 后重新校验已匹配路径。
// used by the extractorTerminal defensive fallback to re-check the matched path.
func (r *compiledRoute) validate(path requestPath) error {
	if r == nil {
		return fmt.Errorf("%w: route is nil", ErrInvalidRequestPath)
	}
	pattern := r.definition.pattern
	if pattern.trailing != path.trailing {
		return fmt.Errorf("%w: trailing slash does not match %s", ErrInvalidRequestPath, pattern.path)
	}
	segmentIndex := 0
	for _, segment := range pattern.segments {
		switch segment.kind {
		case routeSegmentStatic:
			if segmentIndex >= path.segments.Len() || !rawSegmentMatches(path.RawAt(segmentIndex), segment.value) {
				return fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			segmentIndex++
		case routeSegmentParameter:
			if segmentIndex >= path.segments.Len() {
				return fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			segmentIndex++
		case routeSegmentCatchAll:
			segmentIndex = path.segments.Len()
		}
	}
	if segmentIndex != path.segments.Len() {
		return fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
	}
	return nil
}

type routeMux struct {
	root                routeMuxNode
	routes              []*compiledRoute
	staticByMethod      map[string]map[string]*compiledRoute
	staticGet           map[string]*compiledRoute
	directParamByMethod map[string]map[string]*compiledRoute
}

type routeStaticChild struct {
	segment string
	node    *routeMuxNode
}

type routeMuxNode struct {
	staticChildren []routeStaticChild
	staticIndex    *[256]int16
	staticMap      map[string]*routeMuxNode
	parameter      *routeMuxNode
	catchAll       *routeMuxNode
	routes         routeMethodSet
	trailing       routeMethodSet
}

type routeMethodSet struct {
	routes []*compiledRoute
}

func (s *routeMethodSet) add(route *compiledRoute) {
	for _, current := range s.routes {
		if current.definition.method == route.definition.method {
			return
		}
	}
	s.routes = append(s.routes, route)
}

func (s *routeMethodSet) find(method string) *compiledRoute {
	for _, route := range s.routes {
		if route.definition.method == method {
			return route
		}
	}
	return nil
}

func (s *routeMethodSet) collect(methods map[string]struct{}) {
	for _, route := range s.routes {
		methods[route.definition.method] = struct{}{}
	}
}

func newRouteMux(definitions []routeDefinition) *routeMux {
	mux := &routeMux{}
	for _, definition := range definitions {
		route := &compiledRoute{definition: definition.clone()}
		route.paramPos = make(map[string]int, len(definition.pattern.segments))
		for index, segment := range definition.pattern.segments {
			if segment.kind == routeSegmentParameter || segment.kind == routeSegmentCatchAll {
				route.paramPos[segment.value] = index
			}
		}
		mux.routes = append(mux.routes, route)
		mux.root.insert(route)
		if !definition.pattern.hasParams() {
			if mux.staticByMethod == nil {
				mux.staticByMethod = make(map[string]map[string]*compiledRoute)
			}
			byPath := mux.staticByMethod[definition.method]
			if byPath == nil {
				byPath = make(map[string]*compiledRoute)
				mux.staticByMethod[definition.method] = byPath
			}
			byPath[definition.pattern.path] = route
			if definition.method == http.MethodGet {
				if mux.staticGet == nil {
					mux.staticGet = make(map[string]*compiledRoute)
				}
				mux.staticGet[definition.pattern.path] = route
			}
		}
	}
	mux.root.freeze()
	return mux
}

// matchStatic 尝试未转义静态路径的冻结直达索引。
// matchStatic attempts the frozen direct index for an unescaped static path.
// 若需要动态树决定结果（包括静态路径只为其他方法存在）则返回 false。
// It returns false when the dynamic tree must decide the result, including a
// static path that exists only for another method.
func (m *routeMux) matchStatic(method, path string) (routeMatchResult, bool) {
	if m == nil || m.staticByMethod == nil {
		return routeMatchResult{}, false
	}
	byPath := m.staticByMethod[method]
	if byPath == nil && method != http.MethodHead {
		return routeMatchResult{}, false
	}
	if method == http.MethodHead && byPath == nil &&
		m.staticByMethod[http.MethodGet] == nil {
		return routeMatchResult{}, false
	}
	if strings.IndexByte(path, '%') >= 0 {
		return routeMatchResult{}, false
	}
	if method == http.MethodGet {
		if route := m.staticGet[path]; route != nil {
			return routeMatchResult{kind: routeMatchFound, route: route}, true
		}
		return routeMatchResult{}, false
	}
	if route := byPath[path]; route != nil {
		return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: method == http.MethodHead}, true
	}
	if method == http.MethodHead {
		if byPath := m.staticByMethod[http.MethodHead]; byPath != nil {
			if route := byPath[path]; route != nil {
				return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: true}, true
			}
		}
		if byPath := m.staticByMethod[http.MethodGet]; byPath != nil {
			if route := byPath[path]; route != nil {
				return routeMatchResult{kind: routeMatchFound, route: route, suppressBody: true}, true
			}
		}
	}
	return routeMatchResult{}, false
}

// addDirectParam 为“静态前缀 + 单个末尾参数”的直达终结器建立冻结索引。
// addDirectParam indexes direct terminals shaped as a static prefix followed by
// one final parameter.
func (m *routeMux) addDirectParam(route *compiledRoute) {
	if m == nil || route == nil {
		return
	}
	pattern := route.definition.pattern
	if pattern.trailing || len(pattern.segments) == 0 {
		return
	}
	last := pattern.segments[len(pattern.segments)-1]
	if last.kind != routeSegmentParameter {
		return
	}
	var prefix strings.Builder
	prefix.Grow(len(pattern.path))
	prefix.WriteByte('/')
	for index, segment := range pattern.segments[:len(pattern.segments)-1] {
		if segment.kind != routeSegmentStatic || segment.rawValue != segment.value || strings.ContainsRune(segment.value, '/') {
			return
		}
		if index > 0 {
			prefix.WriteByte('/')
		}
		prefix.WriteString(segment.value)
	}
	if len(pattern.segments) > 1 {
		prefix.WriteByte('/')
	}
	if m.directParamByMethod == nil {
		m.directParamByMethod = make(map[string]map[string]*compiledRoute)
	}
	byPrefix := m.directParamByMethod[route.definition.method]
	if byPrefix == nil {
		byPrefix = make(map[string]*compiledRoute)
		m.directParamByMethod[route.definition.method] = byPrefix
	}
	route.directName = last.value
	byPrefix[prefix.String()] = route
}

// matchDirectParam 匹配未转义的简单单参数路径；其他形态返回 false 走完整树。
// matchDirectParam matches an unescaped simple single-parameter path; all other
// shapes return false and fall back to the full tree.
func (m *routeMux) matchDirectParam(method, path string) (*compiledRoute, string, bool) {
	if m == nil || m.directParamByMethod == nil || method == http.MethodHead || path == "" {
		return nil, "", false
	}
	byPrefix := m.directParamByMethod[method]
	if byPrefix == nil {
		return nil, "", false
	}
	slash := strings.LastIndexByte(path, '/')
	if slash < 0 || slash == len(path)-1 {
		return nil, "", false
	}
	value := path[slash+1:]
	if value == "." || value == ".." {
		return nil, "", false
	}
	route := byPrefix[path[:slash+1]]
	return route, value, route != nil
}

func (m *routeMux) match(method string, path requestPath) routeMatchResult {
	if method == http.MethodHead {
		if route := m.lookup(http.MethodHead, path); route != nil {
			return routeMatchResult{
				kind:         routeMatchFound,
				route:        route,
				suppressBody: true,
			}
		}
		if route := m.lookup(http.MethodGet, path); route != nil {
			return routeMatchResult{
				kind:         routeMatchFound,
				route:        route,
				suppressBody: true,
			}
		}
	} else if route := m.lookup(method, path); route != nil {
		return routeMatchResult{kind: routeMatchFound, route: route}
	}

	allow := m.allowedMethods(path)
	if len(allow) > 0 {
		return routeMatchResult{kind: routeMatchMethodNotAllowed, allow: allow}
	}
	return routeMatchResult{kind: routeMatchNotFound}
}

func (m *routeMux) lookup(method string, path requestPath) *compiledRoute {
	return m.root.lookup(method, path, 0)
}

func (m *routeMux) allowedMethods(path requestPath) []string {
	methods := make(map[string]struct{}, 4)
	m.root.collect(path, 0, methods)
	if _, exists := methods[http.MethodGet]; exists {
		methods[http.MethodHead] = struct{}{}
	}
	allowed := make([]string, 0, len(methods))
	for method := range methods {
		allowed = append(allowed, method)
	}
	sort.Strings(allowed)
	return allowed
}

func (n *routeMuxNode) insert(route *compiledRoute) {
	node := n
	for _, segment := range route.definition.pattern.segments {
		switch segment.kind {
		case routeSegmentStatic:
			child := node.staticChildExact(segment.value)
			if child == nil {
				child = &routeMuxNode{}
				node.staticChildren = append(node.staticChildren, routeStaticChild{segment: segment.value, node: child})
			}
			node = child
		case routeSegmentParameter:
			if node.parameter == nil {
				node.parameter = &routeMuxNode{}
			}
			node = node.parameter
		case routeSegmentCatchAll:
			if node.catchAll == nil {
				node.catchAll = &routeMuxNode{}
			}
			node = node.catchAll
		}
	}
	if route.definition.pattern.trailing {
		node.trailing.add(route)
		return
	}
	node.routes.add(route)
}

func (n *routeMuxNode) staticChildExact(segment string) *routeMuxNode {
	for _, child := range n.staticChildren {
		if child.segment == segment {
			return child.node
		}
	}
	return nil
}

func (n *routeMuxNode) freeze() {
	for i := range n.staticChildren {
		n.staticChildren[i].node.freeze()
	}
	if n.parameter != nil {
		n.parameter.freeze()
	}
	if n.catchAll != nil {
		n.catchAll.freeze()
	}
	if len(n.staticChildren) < 4 {
		return
	}
	// 宽节点冻结后用 map 处理未转义段；转义段仍走无分配的首字节/线性回退。
	// Frozen wide nodes use a map for unescaped segments; escaped segments keep
	// the allocation-free first-byte/linear fallback.
	if len(n.staticChildren) >= 8 {
		n.staticMap = make(map[string]*routeMuxNode, len(n.staticChildren))
		for _, child := range n.staticChildren {
			n.staticMap[child.segment] = child.node
		}
	}
	var index [256]int16
	for i := range index {
		index[i] = -1
	}
	for i, child := range n.staticChildren {
		if child.segment == "" || index[child.segment[0]] >= 0 {
			return
		}
		index[child.segment[0]] = int16(i)
	}
	n.staticIndex = &index
}

func (n *routeMuxNode) lookup(method string, path requestPath, index int) *compiledRoute {
	if index == path.segments.Len() {
		if route := n.terminal(path.trailing).find(method); route != nil {
			return route
		}
		if n.catchAll != nil {
			return n.catchAll.terminal(path.trailing).find(method)
		}
		return nil
	}

	if child := n.staticChild(path.RawAt(index)); child != nil {
		if route := child.lookup(method, path, index+1); route != nil {
			return route
		}
	}
	if n.parameter != nil {
		if route := n.parameter.lookup(method, path, index+1); route != nil {
			return route
		}
	}
	if n.catchAll != nil {
		return n.catchAll.terminal(path.trailing).find(method)
	}
	return nil
}

func (n *routeMuxNode) collect(path requestPath, index int, methods map[string]struct{}) {
	if index == path.segments.Len() {
		n.terminal(path.trailing).collect(methods)
		if n.catchAll != nil {
			n.catchAll.terminal(path.trailing).collect(methods)
		}
		return
	}
	if child := n.staticChild(path.RawAt(index)); child != nil {
		child.collect(path, index+1, methods)
	}
	if n.parameter != nil {
		n.parameter.collect(path, index+1, methods)
	}
	if n.catchAll != nil {
		n.catchAll.terminal(path.trailing).collect(methods)
	}
}

func (n *routeMuxNode) staticChild(raw string) *routeMuxNode {
	escaped := strings.IndexByte(raw, '%') >= 0
	if !escaped {
		if n.staticMap != nil {
			return n.staticMap[raw]
		}
		if n.staticIndex != nil && raw != "" {
			index := n.staticIndex[raw[0]]
			if index >= 0 {
				child := n.staticChildren[index]
				if raw == child.segment {
					return child.node
				}
			}
			return nil
		}
	}
	for _, child := range n.staticChildren {
		if staticSegmentMatches(raw, child.segment) {
			return child.node
		}
	}
	return nil
}

func staticSegmentMatches(raw, expected string) bool {
	if raw == expected {
		return true
	}
	if strings.IndexByte(raw, '%') < 0 {
		return false
	}
	return rawSegmentMatches(raw, expected)
}

func (n *routeMuxNode) terminal(trailing bool) *routeMethodSet {
	if trailing {
		return &n.trailing
	}
	return &n.routes
}
