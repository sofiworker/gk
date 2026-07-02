package ghttp

import (
	"net/http"
	"reflect"
)

// Group holds a set of routes with a common prefix.
type Group struct {
	server      *Server
	prefix      string
	produces    string
	consumes    []string
	consumesSet bool
	middlewares []Middleware
}

// Use appends group-level middleware.
func (g *Group) Use(mws ...Middleware) *Group {
	g.middlewares = append(g.middlewares, mws...)
	return g
}

// Produces declares the default response Content-Type for routes in this group.
func (g *Group) Produces(contentType string) *Group {
	g.produces = contentType
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
	groupMiddlewares := make([]Middleware, 0, len(g.middlewares)+len(mws))
	groupMiddlewares = append(groupMiddlewares, g.middlewares...)
	groupMiddlewares = append(groupMiddlewares, mws...)
	return &Group{
		server:      g.server,
		prefix:      JoinPaths(g.prefix, prefix),
		produces:    g.produces,
		consumes:    append([]string(nil), g.consumes...),
		consumesSet: g.consumesSet,
		middlewares: groupMiddlewares,
	}
}

func (g *Group) handleRoute(method, path string, handler http.Handler, mws ...Middleware) error {
	all := make([]Middleware, 0, len(g.middlewares)+len(mws))
	all = append(all, g.middlewares...)
	all = append(all, mws...)
	return g.server.handleRoute(method, JoinPaths(g.prefix, path), handler, all...)
}

func (g *Group) addRouteSpec(method, path, doc string, tags []string, operationID string, reqType, pathType, queryType reflect.Type, consumes []string, produces string, responses []responseSpec) {
	g.server.addRouteSpec(method, JoinPaths(g.prefix, path), doc, tags, operationID, reqType, pathType, queryType, consumes, produces, responses)
}

func (g *Group) producesContentType() string {
	if g.produces != "" {
		return g.produces
	}
	return g.server.producesContentType()
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
