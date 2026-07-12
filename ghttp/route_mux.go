package ghttp

import (
	"fmt"
	"net/http"
	"sort"
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
	definition routeDefinition
	handler    http.Handler
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
			if segmentIndex >= path.segments.Len() || path.segments.At(segmentIndex) != segment.value {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			segmentIndex++
		case routeSegmentParameter:
			if segmentIndex >= path.segments.Len() {
				return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
			}
			params.Add(segment.value, path.segments.At(segmentIndex))
			segmentIndex++
		case routeSegmentCatchAll:
			params.Add(segment.value, path.segments.JoinFrom(segmentIndex))
			segmentIndex = path.segments.Len()
		}
	}
	if segmentIndex != path.segments.Len() {
		return pathParamList{}, fmt.Errorf("%w: path does not match %s", ErrInvalidRequestPath, pattern.path)
	}
	return params, nil
}

type routeMux struct {
	methods map[string]*routeTree
	routes  []*compiledRoute
}

type routeTree struct {
	root routeMuxNode
}

type routeMuxNode struct {
	static    map[string]*routeMuxNode
	parameter *routeMuxNode
	catchAll  *routeMuxNode
	route     *compiledRoute
	trailing  *compiledRoute
}

func newRouteMux(definitions []routeDefinition) *routeMux {
	mux := &routeMux{methods: make(map[string]*routeTree)}
	for _, definition := range definitions {
		tree := mux.methods[definition.method]
		if tree == nil {
			tree = &routeTree{}
			mux.methods[definition.method] = tree
		}
		route := &compiledRoute{definition: definition.clone()}
		mux.routes = append(mux.routes, route)
		tree.insert(route)
	}
	return mux
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
	tree := m.methods[method]
	if tree == nil {
		return nil
	}
	return tree.lookup(path)
}

func (m *routeMux) allowedMethods(path requestPath) []string {
	allowed := make([]string, 0, len(m.methods)+1)
	seen := make(map[string]struct{}, len(m.methods)+1)
	for method := range m.methods {
		if m.lookup(method, path) == nil {
			continue
		}
		if _, exists := seen[method]; !exists {
			seen[method] = struct{}{}
			allowed = append(allowed, method)
		}
		if method == http.MethodGet {
			if _, exists := seen[http.MethodHead]; !exists {
				seen[http.MethodHead] = struct{}{}
				allowed = append(allowed, http.MethodHead)
			}
		}
	}
	sort.Strings(allowed)
	return allowed
}

func (t *routeTree) insert(route *compiledRoute) {
	node := &t.root
	for _, segment := range route.definition.pattern.segments {
		switch segment.kind {
		case routeSegmentStatic:
			if node.static == nil {
				node.static = make(map[string]*routeMuxNode)
			}
			child := node.static[segment.value]
			if child == nil {
				child = &routeMuxNode{}
				node.static[segment.value] = child
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
		node.trailing = route
		return
	}
	node.route = route
}

func (t *routeTree) lookup(path requestPath) *compiledRoute {
	return t.root.lookup(path, 0)
}

func (n *routeMuxNode) lookup(path requestPath, index int) *compiledRoute {
	if index == path.segments.Len() {
		if route := n.terminal(path.trailing); route != nil {
			return route
		}
		if n.catchAll != nil {
			return n.catchAll.terminal(path.trailing)
		}
		return nil
	}

	if child := n.static[path.segments.At(index)]; child != nil {
		if route := child.lookup(path, index+1); route != nil {
			return route
		}
	}
	if n.parameter != nil {
		if route := n.parameter.lookup(path, index+1); route != nil {
			return route
		}
	}
	if n.catchAll != nil {
		return n.catchAll.terminal(path.trailing)
	}
	return nil
}

func (n *routeMuxNode) terminal(trailing bool) *compiledRoute {
	if trailing {
		return n.trailing
	}
	return n.route
}
