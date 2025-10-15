package gserver

import (
	"net/http"
	"regexp"
)

var (
	// regEnLetter matches english letters for http method name
	regEnLetter = regexp.MustCompile("^[A-Z]+$")

	// anyMethods for RouterGroup Any method
	anyMethods = []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete, http.MethodConnect,
		http.MethodTrace,
	}
)

type RouterGroup struct {
	Handlers []Handler
	basePath string
	engine   *Server
	root     bool
}

func (g *RouterGroup) Group(prefix string, handlers ...Handler) *RouterGroup {
	return &RouterGroup{
		basePath: prefix,
		engine:   g.engine,
		root:     false,
	}
}

func (g *RouterGroup) Handle(method, path string, handlers ...HandlerFunc) {
	if matched := regEnLetter.MatchString(method); !matched {
		panic("http method " + method + " is not valid")
	}
	g.engine.addRoute(method, path, handlers...)
}

func (g *RouterGroup) GET(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodGet, path, handlers...)
}

func (g *RouterGroup) POST(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPost, path, handlers...)
}

func (g *RouterGroup) PUT(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPut, path, handlers...)
}

func (g *RouterGroup) DELETE(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodDelete, path, handlers...)
}

func (g *RouterGroup) PATCH(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPatch, path, handlers...)
}

func (g *RouterGroup) HEAD(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodHead, path, handlers...)
}

func (g *RouterGroup) OPTIONS(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodOptions, path, handlers...)
}

func (g *RouterGroup) ANY(path string, handlers ...HandlerFunc) {
	for _, method := range anyMethods {
		g.Handle(method, path, handlers...)
	}
}
