package ghttp

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
)

var (
	ErrRouteMethodRequired = errors.New("route method is required")
	ErrRouteMethodEmpty    = errors.New("route method is empty")
	ErrRouteMethodInvalid  = errors.New("route method is invalid")

	allHTTPMethods = []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	}
)

// RouteBuilder builds a route in a go-restful-style chain.
type RouteBuilder[Req, Resp any] struct {
	target      routeTarget
	path        string
	methods     []string
	doc         string
	tags        []string
	operationID string
	reqType     reflect.Type
	responses   []responseSpec
	handler     HandlerFunc[Req, Resp]
	middlewares []MiddlewareFunc
}

type routeTarget interface {
	handleRoute(method, path string, handler http.Handler, mws ...MiddlewareFunc) error
	addRouteSpec(method, path, doc string, tags []string, operationID string, reqType reflect.Type, responses []responseSpec)
	owner() *Server
}

type responseSpec struct {
	Code        int
	Description string
	ModelType   reflect.Type
}

// responseSpecBuilder is a sub-builder for a single response.
type responseSpecBuilder[Req, Resp any] struct {
	builder *RouteBuilder[Req, Resp]
	code    int
}

// Route creates a new RouteBuilder on the given target.
// Usage: Route[CreateUserReq, UserResp](s).POST("/users/{id}").To(handler)
func Route[Req, Resp any](target routeTarget) *RouteBuilder[Req, Resp] {
	return &RouteBuilder[Req, Resp]{
		target: target,
	}
}

func (b *RouteBuilder[Req, Resp]) methodSet(method string, path string) string {
	b.methods = []string{method}
	b.path = path
	return b.path
}

func (b *RouteBuilder[Req, Resp]) methodsSet(methods []string, path string) string {
	b.methods = append(b.methods[:0], methods...)
	b.path = path
	return b.path
}

func (b *RouteBuilder[Req, Resp]) POST(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodPost, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) GET(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodGet, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) PUT(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodPut, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) DELETE(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodDelete, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) PATCH(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodPatch, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) HEAD(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodHead, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) OPTIONS(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodOptions, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) CONNECT(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodConnect, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) TRACE(path string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodTrace, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) ANY(path string) *RouteBuilder[Req, Resp] {
	b.methodsSet(allHTTPMethods, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) CUSTOM(method, path string) *RouteBuilder[Req, Resp] {
	b.methodSet(strings.ToUpper(strings.TrimSpace(method)), path)
	return b
}

// Doc sets the API documentation string.
func (b *RouteBuilder[Req, Resp]) Doc(s string) *RouteBuilder[Req, Resp] {
	b.doc = s
	return b
}

// Tags sets OpenAPI tags.
func (b *RouteBuilder[Req, Resp]) Tags(tags ...string) *RouteBuilder[Req, Resp] {
	b.tags = tags
	return b
}

// OperationID sets the OpenAPI operation ID.
func (b *RouteBuilder[Req, Resp]) OperationID(id string) *RouteBuilder[Req, Resp] {
	b.operationID = id
	return b
}

// Reads declares the request input type.
func (b *RouteBuilder[Req, Resp]) Reads(input Req) *RouteBuilder[Req, Resp] {
	b.reqType = reflect.TypeOf(input)
	return b
}

// Responds starts a response specification block.
func (b *RouteBuilder[Req, Resp]) Responds(code int) *responseSpecBuilder[Req, Resp] {
	return &responseSpecBuilder[Req, Resp]{
		builder: b,
		code:    code,
	}
}

func (rb *responseSpecBuilder[Req, Resp]) With(model interface{}) *responseSpecBuilder[Req, Resp] {
	rb.builder.responses = append(rb.builder.responses, responseSpec{
		Code:      rb.code,
		ModelType: reflect.TypeOf(model),
	})
	return rb
}

func (rb *responseSpecBuilder[Req, Resp]) Desc(desc string) *responseSpecBuilder[Req, Resp] {
	if len(rb.builder.responses) > 0 {
		rb.builder.responses[len(rb.builder.responses)-1].Description = desc
	}
	return rb
}

func (rb *responseSpecBuilder[Req, Resp]) End() *RouteBuilder[Req, Resp] {
	return rb.builder
}

// Use adds route-level middleware.
func (b *RouteBuilder[Req, Resp]) Use(mws ...MiddlewareFunc) *RouteBuilder[Req, Resp] {
	b.middlewares = append(b.middlewares, mws...)
	return b
}

// UseFunc adds handler-function middleware to the route.
func (b *RouteBuilder[Req, Resp]) UseFunc(mws ...HandlerMiddlewareFunc) *RouteBuilder[Req, Resp] {
	for _, mw := range mws {
		b.middlewares = append(b.middlewares, HandlerMiddleware(mw))
	}
	return b
}

// To registers the handler and finalizes the route.
func (b *RouteBuilder[Req, Resp]) To(handler HandlerFunc[Req, Resp]) error {
	b.handler = handler
	if len(b.methods) == 0 {
		return ErrRouteMethodRequired
	}
	for _, method := range b.methods {
		if method == "" {
			return ErrRouteMethodEmpty
		}
		if !isHTTPMethodToken(method) {
			return ErrRouteMethodInvalid
		}
		if err := b.target.handleRoute(method, b.path, b.buildHandlerChain(), b.middlewares...); err != nil {
			return err
		}
		b.register(method)
	}
	return nil
}

func (b *RouteBuilder[Req, Resp]) buildHandlerChain() http.Handler {
	h := b.buildHandler()
	return h
}

func (b *RouteBuilder[Req, Resp]) register(method string) {
	b.target.addRouteSpec(method, b.path, b.doc, b.tags, b.operationID, b.reqType, b.responses)
}

func (b *RouteBuilder[Req, Resp]) buildHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Req
		if err := parseInput(r, &input); err != nil {
			writeError(w, r, b.target.owner(), http.StatusBadRequest, err)
			return
		}

		server := b.target.owner()
		if server.validator != nil {
			if err := server.validator.Validate(r.Context(), &input); err != nil {
				writeError(w, r, server, http.StatusUnprocessableEntity, err)
				return
			}
		}

		resp, err := b.handler(r.Context(), &input)
		if err != nil {
			if isErrHandled(err) {
				return
			}
			writeError(w, r, server, http.StatusInternalServerError, err)
			return
		}

		if server.envelope != nil {
			ectx := &responseContext{w: w, r: r, codecMgr: server.codecMgr}
			server.envelope(ectx, resolveStatusCode(resp), resp, nil, server.codecMgr)
		}
	})
}

func isHTTPMethodToken(method string) bool {
	for i := 0; i < len(method); i++ {
		c := method[i]
		if c <= 32 || c >= 127 {
			return false
		}
		switch c {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}', ' ', '\t':
			return false
		}
	}
	return true
}

func writeError(w http.ResponseWriter, r *http.Request, s *Server, defaultCode int, err error) {
	if s.envelope != nil {
		ectx := &responseContext{w: w, r: r, codecMgr: s.codecMgr}
		code := defaultCode
		if he := AsError(err); he != nil {
			code = he.Code
		}
		s.envelope(ectx, code, nil, err, s.codecMgr)
	}
}
