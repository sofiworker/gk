package ghttp

import (
	"net/http"
	"reflect"
)

// Group holds a set of routes with a common prefix.
type Group struct {
	server      *Server
	parent      *Group
	prefix      string
	produces    []string
	consumes    []string
	consumesSet bool
	middlewares []Middleware
}

// Use appends group-level middleware.
func (g *Group) Use(mws ...Middleware) *Group {
	g.middlewares = append(g.middlewares, mws...)
	return g
}

// Produces declares the default response Content-Types for routes in this group.
func (g *Group) Produces(contentTypes ...string) *Group {
	g.produces = normalizeContentTypes(contentTypes)
	return g
}

// Consumes declares the default request Content-Types for routes in this group.
func (g *Group) Consumes(contentTypes ...string) *Group {
	g.consumes = normalizeContentTypes(contentTypes)
	g.consumesSet = true
	return g
}

// Group creates a nested route group.
func (g *Group) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		server:      g.server,
		parent:      g,
		prefix:      JoinPaths(g.prefix, prefix),
		produces:    append([]string(nil), g.produces...),
		consumes:    append([]string(nil), g.consumes...),
		consumesSet: g.consumesSet,
		middlewares: append([]Middleware(nil), mws...),
	}
}

func (g *Group) handleRoute(method, path string, handler http.Handler, mws ...Middleware) error {
	return g.server.handleRoute(method, JoinPaths(g.prefix, path), &groupRouteHandler{
		group:            g,
		handler:          handler,
		routeMiddlewares: append([]Middleware(nil), mws...),
	})
}

func (g *Group) addRouteSpec(method, path string, reqType, respType reflect.Type, doc RouteDoc, consumes, produces []string) {
	g.server.addRouteSpec(method, JoinPaths(g.prefix, path), reqType, respType, doc, consumes, produces)
}

func (g *Group) producesContentTypes() []string {
	if len(g.produces) > 0 {
		return g.produces
	}
	return g.server.producesContentTypes()
}

func (g *Group) consumesContentTypes() []string {
	if g.consumesSet {
		return g.consumes
	}
	return g.server.consumesContentTypes()
}

func (g *Group) owner() *Server {
	return g.server
}

func (g *Group) currentMiddlewares() []Middleware {
	if g == nil {
		return nil
	}
	var all []Middleware
	if g.parent != nil {
		all = append(all, g.parent.currentMiddlewares()...)
	}
	all = append(all, g.middlewares...)
	return all
}

type groupRouteHandler struct {
	group            *Group
	handler          http.Handler
	routeMiddlewares []Middleware
}

func (h *groupRouteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.currentHandler().ServeHTTP(w, r)
}

func (h *groupRouteHandler) ServeHTTPWithPathParams(w http.ResponseWriter, r *http.Request, params pathParamList) {
	handler := h.currentHandler()
	if pathHandler, ok := handler.(pathParamHandler); ok {
		pathHandler.ServeHTTPWithPathParams(w, r, params)
		return
	}
	handler.ServeHTTP(w, requestWithPathParams(r, params))
}

func (h *groupRouteHandler) currentHandler() http.Handler {
	middlewares := h.group.currentMiddlewares()
	middlewares = append(middlewares, h.routeMiddlewares...)
	return wrapRouteHandler(h.handler, middlewares...)
}
