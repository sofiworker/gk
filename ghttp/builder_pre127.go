//go:build !go1.27

package ghttp

import (
	"errors"
	"net/http"
	"reflect"
)

// RouteBuilder builds a route in a go-restful-style chain.
//
// This is the pre-Go-1.27 API: Req/Resp are type parameters of the builder
// because methods could not declare type parameters. Once Go 1.27 generic
// methods are available, the same chain is served by the non-generic
// RouteBuilder in builder_go127.go and Route ignores its type arguments.
type RouteBuilder[Req, Resp any] struct {
	core  *routeBuilderCore
	input compiledInput[Req]
}

// Route creates a new RouteBuilder on the given target.
// Usage: Route[CreateUserReq, UserResp](s).POST("/users/{id}").To(handler)
func Route[Req, Resp any](target routeTarget) *RouteBuilder[Req, Resp] {
	return &RouteBuilder[Req, Resp]{
		core:  newRouteBuilderCore(target),
		input: compileInput[Req](),
	}
}

func (b *RouteBuilder[Req, Resp]) POST(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodPost, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) GET(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodGet, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) PUT(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodPut, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) DELETE(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodDelete, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) PATCH(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodPatch, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) HEAD(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodHead, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) OPTIONS(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodOptions, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) CONNECT(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodConnect, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) TRACE(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodTrace, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) ANY(path string) *RouteBuilder[Req, Resp] {
	b.core.methodsSet(allHTTPMethods, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) CUSTOM(method, path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(method, path)
	return b
}

// Doc configures route documentation metadata.
func (b *RouteBuilder[Req, Resp]) Doc(opts ...DocOption) *RouteBuilder[Req, Resp] {
	b.core.docOptions(opts...)
	return b
}

// Produces declares response Content-Types for automatic response encoding.
func (b *RouteBuilder[Req, Resp]) Produces(contentTypes ...string) *RouteBuilder[Req, Resp] {
	b.core.setProduces(contentTypes...)
	return b
}

// Consumes declares the request Content-Types accepted for automatic body decoding.
func (b *RouteBuilder[Req, Resp]) Consumes(contentTypes ...string) *RouteBuilder[Req, Resp] {
	b.core.setConsumes(contentTypes...)
	return b
}

// MaxBodyBytes overrides the server request body size limit for this route.
// Values less than or equal to zero disable the request body size limit.
func (b *RouteBuilder[Req, Resp]) MaxBodyBytes(n int64) *RouteBuilder[Req, Resp] {
	b.core.setMaxBodyBytes(n)
	return b
}

// Validate adds route-level validation after input binding.
func (b *RouteBuilder[Req, Resp]) Validate(fn interface{}, opts ...ValidateOption) *RouteBuilder[Req, Resp] {
	b.core.ensureMutable()
	validator, err := coerceRouteValidator[Req](fn)
	if err != nil {
		b.core.setupErr = errors.Join(b.core.setupErr, err)
	} else {
		b.core.validator = validator
	}
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
func (b *RouteBuilder[Req, Resp]) SkipValidation() *RouteBuilder[Req, Resp] {
	b.core.skipValidationSet()
	return b
}

// Status declares the fixed success status for To handlers.
func (b *RouteBuilder[Req, Resp]) Status(code int) *RouteBuilder[Req, Resp] {
	b.core.status(code)
	return b
}

// ResponseHeader declares a fixed response header for To handlers.
func (b *RouteBuilder[Req, Resp]) ResponseHeader(name, value string) *RouteBuilder[Req, Resp] {
	b.core.responseHeader(name, value)
	return b
}

// ErrorWriter installs a route-level error writer. The writer returns true
// when it handled the response; false falls through to the server writer or
// the built-in error writer.
func (b *RouteBuilder[Req, Resp]) ErrorWriter(writer ErrorWriter) *RouteBuilder[Req, Resp] {
	b.core.setErrorWriter(writer)
	return b
}

// ProblemDetails makes this route use RFC 9457 application/problem+json
// error responses, overriding the server-wide error model.
func (b *RouteBuilder[Req, Resp]) ProblemDetails() *RouteBuilder[Req, Resp] {
	b.core.problemDetails()
	return b
}

// Use adds route-level middleware.
func (b *RouteBuilder[Req, Resp]) Use(mws ...Middleware) *RouteBuilder[Req, Resp] {
	b.core.use(mws...)
	return b
}

// Group branches into a new route group rooted at the builder target, like
// gin's r.Group. It must be called before a method/path is set; route-level
// options already configured on this builder are not transferred.
func (b *RouteBuilder[Req, Resp]) Group(prefix string, mws ...Middleware) *Group {
	return b.core.group(prefix, mws...)
}

// To registers the handler and finalizes the route.
func (b *RouteBuilder[Req, Resp]) To(handler HandlerFunc[Req, Resp]) {
	registerTypedHandler(b.core, b.input, handler)
}

// ToNoInput registers a handler that takes no request input. The response is
// written through the same typed pipeline as To.
func (b *RouteBuilder[Req, Resp]) ToNoInput(handler NoInputHandler[Resp]) {
	registerNoInputHandler(b.core, handler)
}

// ToNoOutput registers a handler that returns only an error. Success is 204
// by default and can be overridden with Status.
func (b *RouteBuilder[Req, Resp]) ToNoOutput(handler NoOutputHandler[Req]) {
	registerNoOutputHandler(b.core, b.input, handler)
}

// ToHTTP registers a raw http.Handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToHTTP(handler http.Handler) {
	b.core.toHandler(handler, false, routeTerminalRaw, 0, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToRaw registers a raw handler function with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToRaw(handler RawHandler) {
	b.ToHTTP(http.HandlerFunc(handler))
}

// ToHTTPFunc registers a parsed-input handler that writes the HTTP response itself.
func (b *RouteBuilder[Req, Resp]) ToHTTPFunc(handler HTTPHandlerFunc[Req]) {
	registerHTTPFuncHandler(b.core, b.input, handler, routeTerminalHTTPFunc, 0)
}

// ToRedirect registers a fixed redirect response.
func (b *RouteBuilder[Req, Resp]) ToRedirect(code int, location string) {
	b.ToRedirectFunc(code, func(Req) (string, error) {
		return location, nil
	})
}

// ToRedirectFunc registers a redirect response whose target uses parsed input.
func (b *RouteBuilder[Req, Resp]) ToRedirectFunc(code int, redirect RedirectFunc[Req]) {
	registerRedirectFuncHandler(b.core, b.input, code, redirect)
}

// ToSSE registers a Server-Sent Events handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToSSE(handler SSEHandler) {
	b.core.toSSE(handler, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToWebSocket registers a WebSocket upgrade handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToWebSocket(handler WebSocketHandler) {
	b.core.toWebSocket(handler, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToStatic registers a safe file server with the selected route path as its URL prefix.
func (b *RouteBuilder[Req, Resp]) ToStatic(root ...string) {
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
	b.core.registerStaticFS(fsys, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToStaticFS registers a file server with the selected route path as its URL prefix.
func (b *RouteBuilder[Req, Resp]) ToStaticFS(fs http.FileSystem) {
	b.core.beginTerminal()
	b.core.registerStaticFS(fs, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToStaticFile registers a single static file with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToStaticFile(filepath string) {
	b.core.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath)
	}), false, routeTerminalStatic, 0, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToHTML renders a configured template with fixed data for the selected route.
func (b *RouteBuilder[Req, Resp]) ToHTML(status int, name string, data interface{}) {
	b.core.toHTML(status, name, data, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}
