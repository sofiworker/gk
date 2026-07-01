package ghttp

import (
	"bytes"
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
	pathType    reflect.Type
	queryType   reflect.Type
	responses   []responseSpec
	handler     HandlerFunc[Req, Resp]
	middlewares []MiddlewareFunc
}

type routeTarget interface {
	handleRoute(method, path string, handler http.Handler, mws ...MiddlewareFunc) error
	addRouteSpec(method, path, doc string, tags []string, operationID string, reqType, pathType, queryType reflect.Type, responses []responseSpec)
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

// PathSchema declares a dedicated path-parameter schema for OpenAPI generation.
func (b *RouteBuilder[Req, Resp]) PathSchema(input interface{}) *RouteBuilder[Req, Resp] {
	b.pathType = reflect.TypeOf(input)
	return b
}

// QuerySchema declares a dedicated query-parameter schema for OpenAPI generation.
func (b *RouteBuilder[Req, Resp]) QuerySchema(input interface{}) *RouteBuilder[Req, Resp] {
	b.queryType = reflect.TypeOf(input)
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
	return b.toHandler(b.buildHandlerChain())
}

// ToHTTP registers a raw http.Handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToHTTP(handler http.Handler) error {
	return b.toHandler(handler)
}

// ToRaw registers a raw handler function with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToRaw(handler RawHandler) error {
	return b.ToHTTP(http.HandlerFunc(handler))
}

// ToSSE registers a Server-Sent Events handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToSSE(handler SSEHandler) error {
	return b.toHandler(buildSSEHandler(handler))
}

// ToWebSocket registers a WebSocket upgrade handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToWebSocket(handler WebSocketHandler) error {
	return b.toHandler(buildWebSocketHandler(handler))
}

// ToStatic registers a file server with the selected route path as its URL prefix.
func (b *RouteBuilder[Req, Resp]) ToStatic(root string) error {
	if root == "" {
		return ErrStaticRootRequired
	}
	return b.ToStaticFS(http.Dir(root))
}

// ToStaticFS registers a file server with the selected route path as its URL prefix.
func (b *RouteBuilder[Req, Resp]) ToStaticFS(fs http.FileSystem) error {
	prefix := strings.TrimRight(b.path, "/")
	handler := http.StripPrefix(prefix, http.FileServer(fs))
	b.path = JoinPaths(b.path, "/*path")
	return b.toHandler(handler)
}

// ToStaticFile registers a single static file with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToStaticFile(filepath string) error {
	return b.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath)
	}))
}

// ToHTML renders a configured template with fixed data for the selected route.
func (b *RouteBuilder[Req, Resp]) ToHTML(status int, name string, data interface{}) error {
	return b.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := renderHTML(w, b.target.owner(), status, name, data); err != nil {
			writeError(w, r, b.target.owner(), http.StatusInternalServerError, err)
		}
	}))
}

func (b *RouteBuilder[Req, Resp]) toHandler(handler http.Handler) error {
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
		if err := b.target.handleRoute(method, b.path, handler, b.middlewares...); err != nil {
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
	b.target.addRouteSpec(method, b.path, b.doc, b.tags, b.operationID, b.reqType, b.pathType, b.queryType, b.responses)
}

func (b *RouteBuilder[Req, Resp]) buildHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Req
		server := b.target.owner()
		if err := parseInputWithConfig(r, &input, server.config); err != nil {
			writeError(w, r, b.target.owner(), http.StatusBadRequest, err)
			return
		}

		if server.validator != nil {
			if err := server.validator.Validate(r.Context(), &input); err != nil {
				writeError(w, r, server, http.StatusUnprocessableEntity, err)
				return
			}
		}

		resp, err := b.handler(contextWithRequest(r.Context(), r), &input)
		if err != nil {
			if isErrHandled(err) {
				return
			}
			writeError(w, r, server, http.StatusInternalServerError, err)
			return
		}

		if server.envelope != nil {
			server.envelope(requestContext{w: w, r: r}, resolveStatusCode(resp), resp, nil, server.codecMgr)
			return
		}
		writeResponse(w, r, server, resolveStatusCode(resp), resp)
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
		code := defaultCode
		if he := AsError(err); he != nil {
			code = he.Code
		}
		s.envelope(requestContext{w: w, r: r}, code, nil, err, s.codecMgr)
		return
	}
	http.Error(w, err.Error(), statusCodeFromError(defaultCode, err))
}

func statusCodeFromError(defaultCode int, err error) int {
	if he := AsError(err); he != nil {
		return he.Code
	}
	return defaultCode
}

func writeResponse(w http.ResponseWriter, r *http.Request, s *Server, statusCode int, resp interface{}) {
	accept := r.Header.Get("Accept")
	codec := s.codecMgr.Negotiate(accept)
	w.Header().Set("Content-Type", codec.ContentTypes()[0])
	w.WriteHeader(statusCode)
	_ = codec.Marshal(w, resp)
}

func renderHTML(w http.ResponseWriter, s *Server, status int, name string, data interface{}) error {
	if s.renderer == nil {
		return ErrRendererNotConfigured
	}
	var buf bytes.Buffer
	if err := s.renderer.Render(name, data, &buf); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := w.Write(buf.Bytes())
	return err
}

func buildSSEHandler(handler SSEHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		stream := &SSEWriter{w: w, flusher: flusher}
		_ = handler(requestContext{w: w, r: r}, stream)
	})
}

func buildWebSocketHandler(handler WebSocketHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = handler
		_ = r
		w.WriteHeader(http.StatusNotImplemented)
	})
}

type requestContext struct {
	w http.ResponseWriter
	r *http.Request
}

func (c requestContext) ResponseWriter() http.ResponseWriter {
	return c.w
}

func (c requestContext) Request() *http.Request {
	return c.r
}

func (c requestContext) Query(key string) string {
	return queryParam(c.r, key)
}

func (c requestContext) DefaultQuery(key, defaultValue string) string {
	value := c.Query(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func (c requestContext) Path(key string) string {
	return pathParam(c.r, key)
}

func (c requestContext) DefaultPath(key, defaultValue string) string {
	value := c.Path(key)
	if value == "" {
		return defaultValue
	}
	return value
}
