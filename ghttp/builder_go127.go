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

// Route is the pre-1.27 compatibility entry point. Its type arguments are
// accepted for source compatibility but ignored; terminal methods infer
// Req/Resp from the handler. Prefer Server.Route or Group.Route.
func Route[Req, Resp any](target routeTarget) *RouteBuilder {
	return newRouteBuilder(target)
}

func newRouteBuilder(target routeTarget) *RouteBuilder {
	return &RouteBuilder{core: newRouteBuilderCore(target)}
}

// Route starts a new route chain on the server.
func (s *Server) Route() *RouteBuilder {
	return newRouteBuilder(s)
}

// Route starts a new route chain on the group.
func (g *Group) Route() *RouteBuilder {
	return newRouteBuilder(g)
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

// Get registers a typed GET route with default options.
func (s *Server) Get[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	s.Route().GET(path).To(handler)
}

// Post registers a typed POST route with default options.
func (s *Server) Post[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	s.Route().POST(path).To(handler)
}

// Put registers a typed PUT route with default options.
func (s *Server) Put[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	s.Route().PUT(path).To(handler)
}

// Patch registers a typed PATCH route with default options.
func (s *Server) Patch[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	s.Route().PATCH(path).To(handler)
}

// Delete registers a typed DELETE route with default options.
func (s *Server) Delete[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	s.Route().DELETE(path).To(handler)
}

// Head registers a typed HEAD route with default options.
func (s *Server) Head[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	s.Route().HEAD(path).To(handler)
}

// Options registers a typed OPTIONS route with default options.
func (s *Server) Options[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	s.Route().OPTIONS(path).To(handler)
}

// Get registers a typed GET route with default options.
func (g *Group) Get[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	g.Route().GET(path).To(handler)
}

// Post registers a typed POST route with default options.
func (g *Group) Post[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	g.Route().POST(path).To(handler)
}

// Put registers a typed PUT route with default options.
func (g *Group) Put[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	g.Route().PUT(path).To(handler)
}

// Patch registers a typed PATCH route with default options.
func (g *Group) Patch[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	g.Route().PATCH(path).To(handler)
}

// Delete registers a typed DELETE route with default options.
func (g *Group) Delete[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	g.Route().DELETE(path).To(handler)
}

// Head registers a typed HEAD route with default options.
func (g *Group) Head[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	g.Route().HEAD(path).To(handler)
}

// Options registers a typed OPTIONS route with default options.
func (g *Group) Options[Req, Resp any](path string, handler func(context.Context, Req) (Resp, error)) {
	g.Route().OPTIONS(path).To(handler)
}
