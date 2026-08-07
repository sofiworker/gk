//go:build go1.27

package ghttp

import (
	"context"
	"net/http"
)

// RouteBuilder builds a route in a go-restful-style chain.
//
// This is the Go 1.27 API: the chain is non-generic and every terminal method
// declares its own type parameters, inferred from the handler. The package
// level Route function remains as a source-compatible shim for the
// pre-1.27 spelling and ignores its type arguments.
type RouteBuilder struct {
	core *routeBuilderCore
}

// Route is the deprecated pre-1.27 compatibility entry point. Its type
// arguments are accepted for source compatibility but ignored; it returns
// the same root-group builder that s.GET(path) and friends start, so old
// code keeps compiling without an extra layer. New code should call the
// verb methods directly (s.GET(path).To(handler), group.POST(path).To(...)).
func Route[Req, Resp any](target routeTarget) *RouteBuilder {
	return newRouteBuilder(target)
}

func newRouteBuilder(target routeTarget) *RouteBuilder {
	return &RouteBuilder{core: newRouteBuilderCore(target)}
}

// GET starts a GET route chain on the server, like gin's root group.
func (s *Server) GET(path string) *RouteBuilder {
	return newRouteBuilder(s).GET(path)
}

// POST starts a POST route chain on the server.
func (s *Server) POST(path string) *RouteBuilder {
	return newRouteBuilder(s).POST(path)
}

// PUT starts a PUT route chain on the server.
func (s *Server) PUT(path string) *RouteBuilder {
	return newRouteBuilder(s).PUT(path)
}

// DELETE starts a DELETE route chain on the server.
func (s *Server) DELETE(path string) *RouteBuilder {
	return newRouteBuilder(s).DELETE(path)
}

// PATCH starts a PATCH route chain on the server.
func (s *Server) PATCH(path string) *RouteBuilder {
	return newRouteBuilder(s).PATCH(path)
}

// HEAD starts a HEAD route chain on the server.
func (s *Server) HEAD(path string) *RouteBuilder {
	return newRouteBuilder(s).HEAD(path)
}

// OPTIONS starts an OPTIONS route chain on the server.
func (s *Server) OPTIONS(path string) *RouteBuilder {
	return newRouteBuilder(s).OPTIONS(path)
}

// CONNECT starts a CONNECT route chain on the server.
func (s *Server) CONNECT(path string) *RouteBuilder {
	return newRouteBuilder(s).CONNECT(path)
}

// TRACE starts a TRACE route chain on the server.
func (s *Server) TRACE(path string) *RouteBuilder {
	return newRouteBuilder(s).TRACE(path)
}

// ANY starts a route chain for every standard HTTP method on the server.
func (s *Server) ANY(path string) *RouteBuilder {
	return newRouteBuilder(s).ANY(path)
}

// CUSTOM starts a route chain with a custom HTTP method on the server.
func (s *Server) CUSTOM(method, path string) *RouteBuilder {
	return newRouteBuilder(s).CUSTOM(method, path)
}

// GET starts a GET route chain on the group.
func (g *Group) GET(path string) *RouteBuilder {
	return newRouteBuilder(g).GET(path)
}

// POST starts a POST route chain on the group.
func (g *Group) POST(path string) *RouteBuilder {
	return newRouteBuilder(g).POST(path)
}

// PUT starts a PUT route chain on the group.
func (g *Group) PUT(path string) *RouteBuilder {
	return newRouteBuilder(g).PUT(path)
}

// DELETE starts a DELETE route chain on the group.
func (g *Group) DELETE(path string) *RouteBuilder {
	return newRouteBuilder(g).DELETE(path)
}

// PATCH starts a PATCH route chain on the group.
func (g *Group) PATCH(path string) *RouteBuilder {
	return newRouteBuilder(g).PATCH(path)
}

// HEAD starts a HEAD route chain on the group.
func (g *Group) HEAD(path string) *RouteBuilder {
	return newRouteBuilder(g).HEAD(path)
}

// OPTIONS starts an OPTIONS route chain on the group.
func (g *Group) OPTIONS(path string) *RouteBuilder {
	return newRouteBuilder(g).OPTIONS(path)
}

// CONNECT starts a CONNECT route chain on the group.
func (g *Group) CONNECT(path string) *RouteBuilder {
	return newRouteBuilder(g).CONNECT(path)
}

// TRACE starts a TRACE route chain on the group.
func (g *Group) TRACE(path string) *RouteBuilder {
	return newRouteBuilder(g).TRACE(path)
}

// ANY starts a route chain for every standard HTTP method on the group.
func (g *Group) ANY(path string) *RouteBuilder {
	return newRouteBuilder(g).ANY(path)
}

// CUSTOM starts a route chain with a custom HTTP method on the group.
func (g *Group) CUSTOM(method, path string) *RouteBuilder {
	return newRouteBuilder(g).CUSTOM(method, path)
}

func (b *RouteBuilder) POST(path string) *RouteBuilder {
	b.core.methodSet(http.MethodPost, path)
	return b
}

func (b *RouteBuilder) GET(path string) *RouteBuilder {
	b.core.methodSet(http.MethodGet, path)
	return b
}

func (b *RouteBuilder) PUT(path string) *RouteBuilder {
	b.core.methodSet(http.MethodPut, path)
	return b
}

func (b *RouteBuilder) DELETE(path string) *RouteBuilder {
	b.core.methodSet(http.MethodDelete, path)
	return b
}

func (b *RouteBuilder) PATCH(path string) *RouteBuilder {
	b.core.methodSet(http.MethodPatch, path)
	return b
}

func (b *RouteBuilder) HEAD(path string) *RouteBuilder {
	b.core.methodSet(http.MethodHead, path)
	return b
}

func (b *RouteBuilder) OPTIONS(path string) *RouteBuilder {
	b.core.methodSet(http.MethodOptions, path)
	return b
}

func (b *RouteBuilder) CONNECT(path string) *RouteBuilder {
	b.core.methodSet(http.MethodConnect, path)
	return b
}

func (b *RouteBuilder) TRACE(path string) *RouteBuilder {
	b.core.methodSet(http.MethodTrace, path)
	return b
}

func (b *RouteBuilder) ANY(path string) *RouteBuilder {
	b.core.methodsSet(allHTTPMethods, path)
	return b
}

func (b *RouteBuilder) CUSTOM(method, path string) *RouteBuilder {
	b.core.methodSet(method, path)
	return b
}

// Doc configures route documentation metadata.
func (b *RouteBuilder) Doc(opts ...DocOption) *RouteBuilder {
	b.core.docOptions(opts...)
	return b
}

// Produces declares response Content-Types for automatic response encoding.
func (b *RouteBuilder) Produces(contentTypes ...string) *RouteBuilder {
	b.core.setProduces(contentTypes...)
	return b
}

// Consumes declares the request Content-Types accepted for automatic body decoding.
func (b *RouteBuilder) Consumes(contentTypes ...string) *RouteBuilder {
	b.core.setConsumes(contentTypes...)
	return b
}

// MaxBodyBytes overrides the server request body size limit for this route.
// Values less than or equal to zero disable the request body size limit.
func (b *RouteBuilder) MaxBodyBytes(n int64) *RouteBuilder {
	b.core.setMaxBodyBytes(n)
	return b
}

// Validate adds route-level validation after input binding. The concrete
// validator shape is checked when a typed terminal is called, because the
// request type is not known until then.
func (b *RouteBuilder) Validate(fn interface{}, opts ...ValidateOption) *RouteBuilder {
	b.core.ensureMutable()
	b.core.validator = fn
	var cfg validateOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	b.core.validationError = cfg.err
	return b
}

// SkipValidation disables the server-level validator for this route.
func (b *RouteBuilder) SkipValidation() *RouteBuilder {
	b.core.skipValidationSet()
	return b
}

// Status declares the fixed success status for typed handlers.
func (b *RouteBuilder) Status(code int) *RouteBuilder {
	b.core.status(code)
	return b
}

// ResponseHeader declares a fixed response header for typed handlers.
func (b *RouteBuilder) ResponseHeader(name, value string) *RouteBuilder {
	b.core.responseHeader(name, value)
	return b
}

// ErrorWriter installs a route-level error writer. The writer returns true
// when it handled the response; false falls through to the server writer or
// the built-in error writer.
func (b *RouteBuilder) ErrorWriter(writer ErrorWriter) *RouteBuilder {
	b.core.setErrorWriter(writer)
	return b
}

// ProblemDetails makes this route use RFC 9457 application/problem+json
// error responses, overriding the server-wide error model.
func (b *RouteBuilder) ProblemDetails() *RouteBuilder {
	b.core.problemDetails()
	return b
}

// Use adds route-level middleware.
func (b *RouteBuilder) Use(mws ...Middleware) *RouteBuilder {
	b.core.use(mws...)
	return b
}

// Group branches into a new route group rooted at the builder target, like
// gin's r.Group. It must be called before a method/path is set; route-level
// options already configured on this builder are not transferred.
func (b *RouteBuilder) Group(prefix string, mws ...Middleware) *Group {
	return b.core.group(prefix, mws...)
}

// To registers a typed handler and finalizes the route. Req and Resp are
// inferred from the handler.
func (b *RouteBuilder) To[Req, Resp any](handler func(context.Context, Req) (Resp, error)) {
	registerTypedHandler(b.core, compileInput[Req](), HandlerFunc[Req, Resp](handler))
}

// ToNoInput registers a handler that takes no request input. Req is inferred
// from the response type of the handler.
func (b *RouteBuilder) ToNoInput[Resp any](handler func(context.Context) (Resp, error)) {
	registerNoInputHandler(b.core, NoInputHandler[Resp](handler))
}

// ToNoOutput registers a handler that returns only an error. Success is 204
// by default and can be overridden with Status.
func (b *RouteBuilder) ToNoOutput[Req any](handler func(context.Context, Req) error) {
	registerNoOutputHandler(b.core, compileInput[Req](), NoOutputHandler[Req](handler))
}

// ToHTTP registers a raw http.Handler with the selected route method and path.
func (b *RouteBuilder) ToHTTP(handler http.Handler) {
	b.core.toHandler(handler, false, routeTerminalRaw, 0, nil, nil)
}

// ToRaw registers a raw handler function with the selected route method and path.
func (b *RouteBuilder) ToRaw(handler RawHandler) {
	b.ToHTTP(http.HandlerFunc(handler))
}

// ToHTTPFunc registers a parsed-input handler that writes the HTTP response itself.
func (b *RouteBuilder) ToHTTPFunc[Req any](handler func(http.ResponseWriter, *http.Request, Req) error) {
	registerHTTPFuncHandler(b.core, compileInput[Req](), HTTPHandlerFunc[Req](handler), routeTerminalHTTPFunc, 0)
}

// ToRedirect registers a fixed redirect response.
func (b *RouteBuilder) ToRedirect(code int, location string) {
	registerRedirectFuncHandler(b.core, compileInput[struct{}](), code, func(struct{}) (string, error) {
		return location, nil
	})
}

// ToRedirectFunc registers a redirect response whose target uses parsed input.
func (b *RouteBuilder) ToRedirectFunc[Req any](code int, redirect func(Req) (string, error)) {
	registerRedirectFuncHandler(b.core, compileInput[Req](), code, redirect)
}

// ToSSE registers a Server-Sent Events handler with the selected route method and path.
func (b *RouteBuilder) ToSSE(handler SSEHandler) {
	b.core.toSSE(handler, nil, nil)
}

// ToWebSocket registers a WebSocket upgrade handler with the selected route method and path.
func (b *RouteBuilder) ToWebSocket(handler WebSocketHandler) {
	b.core.toWebSocket(handler, nil, nil)
}

// WebSocketCheckOrigin overrides the server-level WebSocket origin check for
// this route. It must be called before ToWebSocket.
func (b *RouteBuilder) WebSocketCheckOrigin(check func(*http.Request) bool) *RouteBuilder {
	b.core.setWebSocketCheckOrigin(check)
	return b
}

// ToStatic registers a safe file server with the selected route path as its URL prefix.
func (b *RouteBuilder) ToStatic(root ...string) {
	b.core.beginTerminal()
	staticRoot := b.core.target.owner().config.vfsPath
	if len(root) > 0 {
		staticRoot = root[0]
	}
	if staticRoot == "" {
		b.core.panicSetupError(ErrStaticRootRequired)
	}
	fsys, err := NewSafeFS(staticRoot)
	if err != nil {
		b.core.panicSetupError(err)
	}
	b.core.registerStaticFS(fsys, nil, nil)
}

// ToStaticFS registers a file server with the selected route path as its URL prefix.
func (b *RouteBuilder) ToStaticFS(fs http.FileSystem) {
	b.core.beginTerminal()
	b.core.registerStaticFS(fs, nil, nil)
}

// ToStaticFile registers a single static file with the selected route method and path.
func (b *RouteBuilder) ToStaticFile(filepath string) {
	b.core.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath)
	}), false, routeTerminalStatic, 0, nil, nil)
}

// ToHTML renders a configured template with fixed data for the selected route.
func (b *RouteBuilder) ToHTML(status int, name string, data interface{}) {
	b.core.toHTML(status, name, data, nil, nil)
}
