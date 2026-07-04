package ghttp

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"runtime"
	"strings"
)

var (
	ErrRouteMethodRequired      = errors.New("route method is required")
	ErrRouteMethodEmpty         = errors.New("route method is empty")
	ErrRouteMethodInvalid       = errors.New("route method is invalid")
	ErrRouteProducesRequired    = errors.New("route produces content type is required")
	ErrRouteProducesUnsupported = errors.New("route produces content type is unsupported")

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
	target          routeTarget
	path            string
	methods         []string
	doc             string
	tags            []string
	operationID     string
	reqType         reflect.Type
	pathType        reflect.Type
	queryType       reflect.Type
	consumes        []string
	consumesSet     bool
	maxBodyBytes    int64
	maxBodyBytesSet bool
	produces        string
	responses       []responseSpec
	handler         HandlerFunc[Req, Resp]
	middlewares     []Middleware
	input           compiledInput[Req]
	codec           Codec // resolved from produces at registration
}

// compiledInput is the registration-time compiled constructor for a route's
// input type: newTarget allocates the parse/validate target and finish turns
// the filled target into the handler argument. Compiling this once per route
// removes the per-request reflect.New plus the reflect.Value.Interface()
// whole-struct copy that the typed path used to pay.
type compiledInput[Req any] struct {
	newTarget func() any
	finish    func(any) Req
}

func compileInput[Req any]() compiledInput[Req] {
	t := reflect.TypeFor[Req]()
	if t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct {
		elem := t.Elem()
		return compiledInput[Req]{
			newTarget: func() any { return reflect.New(elem).Interface() },
			finish:    func(target any) Req { return target.(Req) },
		}
	}
	return compiledInput[Req]{
		newTarget: func() any { return new(Req) },
		finish:    func(target any) Req { return *target.(*Req) },
	}
}

type routeTarget interface {
	handleRoute(method, path string, handler http.Handler, mws ...Middleware) error
	addRouteSpec(method, path, doc string, tags []string, operationID string, reqType, pathType, queryType reflect.Type, consumes []string, produces string, responses []responseSpec)
	consumesContentTypes() []string
	producesContentType() string
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
		input:  compileInput[Req](),
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

// Produces declares the response Content-Type for automatic response encoding.
func (b *RouteBuilder[Req, Resp]) Produces(contentType string) *RouteBuilder[Req, Resp] {
	b.produces = contentType
	return b
}

// Consumes declares the request Content-Types accepted for automatic body decoding.
func (b *RouteBuilder[Req, Resp]) Consumes(contentTypes ...string) *RouteBuilder[Req, Resp] {
	b.consumes = normalizeContentTypes(contentTypes)
	b.consumesSet = true
	return b
}

// MaxBodyBytes overrides the server request body size limit for this route.
// Values less than or equal to zero disable the request body size limit.
func (b *RouteBuilder[Req, Resp]) MaxBodyBytes(n int64) *RouteBuilder[Req, Resp] {
	b.maxBodyBytes = n
	b.maxBodyBytesSet = true
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
func (b *RouteBuilder[Req, Resp]) Use(mws ...Middleware) *RouteBuilder[Req, Resp] {
	b.middlewares = append(b.middlewares, mws...)
	return b
}

// To registers the handler and finalizes the route.
func (b *RouteBuilder[Req, Resp]) To(handler HandlerFunc[Req, Resp]) {
	if err := b.validateMethods(); err != nil {
		b.recordSetupError(err)
		return
	}
	if err := validateRequestParamsUsage[Req](); err != nil {
		b.recordSetupError(err)
		return
	}
	if err := b.resolveProduces(); err != nil {
		b.recordSetupError(err)
		return
	}
	b.resolveConsumes()
	b.handler = handler
	b.toHandler(b.buildHandlerChain())
}

// ToHTTP registers a raw http.Handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToHTTP(handler http.Handler) {
	b.toHandler(handler)
}

// ToRaw registers a raw handler function with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToRaw(handler RawHandler) {
	b.ToHTTP(http.HandlerFunc(handler))
}

// ToHTTPFunc registers a parsed-input handler that writes the HTTP response itself.
func (b *RouteBuilder[Req, Resp]) ToHTTPFunc(handler HTTPHandlerFunc[Req]) {
	if err := validateRequestParamsUsage[Req](); err != nil {
		b.recordSetupError(err)
		return
	}
	b.resolveConsumes()
	b.toHandler(pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		input, ok := b.parseAndValidateInputWithPathParams(w, r, params)
		if !ok {
			return
		}
		if err := handler(w, r, input); err != nil {
			if isErrHandled(err) {
				return
			}
			writeError(w, r, b.target.owner(), http.StatusInternalServerError, err)
		}
	}))
}

// ToRedirect registers a fixed redirect response.
func (b *RouteBuilder[Req, Resp]) ToRedirect(code int, location string) {
	b.ToRedirectFunc(code, func(Req) (string, error) {
		return location, nil
	})
}

// ToRedirectFunc registers a redirect response whose target uses parsed input.
func (b *RouteBuilder[Req, Resp]) ToRedirectFunc(code int, redirect RedirectFunc[Req]) {
	b.ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, input Req) error {
		location, err := redirect(input)
		if err != nil {
			return err
		}
		http.Redirect(w, r, location, code)
		return nil
	})
}

// ToSSE registers a Server-Sent Events handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToSSE(handler SSEHandler) {
	b.toHandler(buildSSEHandler(b.target.owner(), handler))
}

// ToWebSocket registers a WebSocket upgrade handler with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToWebSocket(handler WebSocketHandler) {
	b.toHandler(buildWebSocketHandler(b.target.owner(), handler))
}

// ToStatic registers a safe file server with the selected route path as its URL prefix.
func (b *RouteBuilder[Req, Resp]) ToStatic(root ...string) {
	staticRoot := b.target.owner().config.vfsPath
	if len(root) > 0 {
		staticRoot = root[0]
	}
	if staticRoot == "" {
		b.recordSetupError(ErrStaticRootRequired)
		return
	}
	fsys, err := NewSafeFS(staticRoot)
	if err != nil {
		b.recordSetupError(err)
		return
	}
	b.ToStaticFS(fsys)
}

// ToStaticFS registers a file server with the selected route path as its URL prefix.
func (b *RouteBuilder[Req, Resp]) ToStaticFS(fs http.FileSystem) {
	prefix := strings.TrimRight(b.path, "/")
	handler := http.StripPrefix(prefix, http.FileServer(fs))
	b.path = JoinPaths(b.path, "/*path")
	b.toHandler(handler)
}

// ToStaticFile registers a single static file with the selected route method and path.
func (b *RouteBuilder[Req, Resp]) ToStaticFile(filepath string) {
	b.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath)
	}))
}

// ToHTML renders a configured template with fixed data for the selected route.
func (b *RouteBuilder[Req, Resp]) ToHTML(status int, name string, data interface{}) {
	b.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := renderHTML(w, b.target.owner(), status, name, data); err != nil {
			writeError(w, r, b.target.owner(), http.StatusInternalServerError, err)
		}
	}))
}

func (b *RouteBuilder[Req, Resp]) toHandler(handler http.Handler) {
	if err := b.validateMethods(); err != nil {
		b.recordSetupError(err)
		return
	}
	for _, method := range b.methods {
		if err := b.target.handleRoute(method, b.path, handler, b.middlewares...); err != nil {
			b.recordSetupError(err, method)
			return
		}
		b.register(method)
	}
}

func (b *RouteBuilder[Req, Resp]) recordSetupError(err error, methodOverride ...string) {
	if err == nil {
		return
	}
	b.target.owner().recordSetupError(fmt.Errorf("route setup %s at %s: %w", b.setupErrorRoute(methodOverride...), setupErrorCaller(), err))
}

func (b *RouteBuilder[Req, Resp]) setupErrorRoute(methodOverride ...string) string {
	methods := b.methods
	if len(methodOverride) > 0 {
		methods = methodOverride
	}
	method := "<method unset>"
	if len(methods) > 0 {
		method = strings.Join(methods, ",")
	}
	path := b.path
	if strings.TrimSpace(path) == "" {
		path = "<path unset>"
	}
	return method + " " + path
}

func setupErrorCaller() string {
	for skip := 2; skip < 16; skip++ {
		_, file, line, ok := runtime.Caller(skip)
		if !ok {
			continue
		}
		if strings.HasSuffix(file, "ghttp/builder.go") || strings.HasSuffix(file, `ghttp\builder.go`) {
			continue
		}
		return fmt.Sprintf("%s:%d", file, line)
	}
	return "unknown"
}

func (b *RouteBuilder[Req, Resp]) validateMethods() error {
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
	}
	return nil
}

func (b *RouteBuilder[Req, Resp]) buildHandlerChain() http.Handler {
	h := b.buildHandler()
	return h
}

func (b *RouteBuilder[Req, Resp]) register(method string) {
	b.target.addRouteSpec(method, b.path, b.doc, b.tags, b.operationID, b.reqType, b.pathType, b.queryType, b.consumes, b.produces, b.responses)
}

func (b *RouteBuilder[Req, Resp]) buildHandler() http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		input, ok := b.parseAndValidateInputWithPathParams(w, r, params)
		if !ok {
			return
		}

		server := b.target.owner()
		resp, err := b.handler(r.Context(), input)
		if err != nil {
			if isErrHandled(err) {
				return
			}
			writeError(w, r, server, http.StatusInternalServerError, err)
			return
		}

		writeResponseCookies(w, resp)
		if server.envelope != nil {
			server.envelope(w, r, resolveStatusCode(resp), resp, nil, server.codecMgr)
			return
		}
		writeResponse(w, r, server, resolveStatusCode(resp), b.produces, b.codec, resp)
	})
}

func (b *RouteBuilder[Req, Resp]) resolveProduces() error {
	if b.produces == "" {
		b.produces = b.target.producesContentType()
	}
	if b.produces == "" {
		return ErrRouteProducesRequired
	}
	codec, ok := b.target.owner().codecMgr.Resolve(b.produces)
	if !ok {
		return ErrRouteProducesUnsupported
	}
	b.codec = codec
	return nil
}

func (b *RouteBuilder[Req, Resp]) resolveConsumes() {
	if b.consumesSet {
		return
	}
	b.consumes = append([]string(nil), b.target.consumesContentTypes()...)
}

func (b *RouteBuilder[Req, Resp]) parseAndValidateInput(w http.ResponseWriter, r *http.Request) (Req, bool) {
	return b.parseAndValidateInputWithPathParams(w, r, pathParamList{})
}

func (b *RouteBuilder[Req, Resp]) parseAndValidateInputWithPathParams(w http.ResponseWriter, r *http.Request, params pathParamList) (Req, bool) {
	target := b.input.newTarget()
	server := b.target.owner()
	if maxBodyBytes := b.effectiveMaxBodyBytes(server); maxBodyBytes > 0 {
		r = requestWithMaxBodyBytes(w, r, maxBodyBytes)
	}
	if err := validateRequestContentType(r, target, b.consumes); err != nil {
		writeError(w, r, server, http.StatusUnsupportedMediaType, err)
		var zero Req
		return zero, false
	}
	if err := parseInputWithConfigAndPathParams(r, target, server.config, params); err != nil {
		code := http.StatusBadRequest
		if isRequestBodyTooLarge(err) {
			code = http.StatusRequestEntityTooLarge
			err = Err(code, ErrRequestBodyTooLarge.Error(), WithCause(err))
		}
		writeError(w, r, server, code, err)
		var zero Req
		return zero, false
	}

	if server.validator != nil {
		if err := server.validator.Validate(r.Context(), target); err != nil {
			writeError(w, r, server, http.StatusUnprocessableEntity, err)
			var zero Req
			return zero, false
		}
	}

	return b.input.finish(target), true
}

func (b *RouteBuilder[Req, Resp]) effectiveMaxBodyBytes(s *Server) int64 {
	if b.maxBodyBytesSet {
		return b.maxBodyBytes
	}
	if s == nil || s.config == nil {
		return 0
	}
	return s.config.maxBodyBytes
}

func requestWithMaxBodyBytes(w http.ResponseWriter, r *http.Request, maxBodyBytes int64) *http.Request {
	if maxBodyBytes <= 0 || r == nil || r.Body == nil || r.Body == http.NoBody {
		return r
	}
	limited := r.WithContext(r.Context())
	limited.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return limited
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func validateRequestContentType(r *http.Request, target interface{}, consumes []string) error {
	if len(consumes) == 0 || !inputTargetHasBody(target) {
		return nil
	}
	contentType := r.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		return nil
	}
	mediaType := normalizeContentType(contentType)
	for _, allowed := range consumes {
		if mediaTypeMatches(allowed, mediaType) {
			return nil
		}
	}
	return Err(http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported media type %q", contentType), WithCause(ErrUnsupportedMediaType))
}

func inputTargetHasBody(target interface{}) bool {
	t := reflect.TypeOf(target)
	if t == nil {
		return false
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == reflect.TypeOf(Params{}) {
		return false
	}
	return getStructInfo(t).hasBody
}

func normalizeContentTypes(contentTypes []string) []string {
	normalized := make([]string, 0, len(contentTypes))
	for _, contentType := range contentTypes {
		contentType = normalizeContentType(contentType)
		if contentType == "" {
			continue
		}
		normalized = append(normalized, contentType)
	}
	return normalized
}

func normalizeContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		return strings.ToLower(mediaType)
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}

func mediaTypeMatches(allowed, actual string) bool {
	if allowed == "*/*" {
		return true
	}
	if strings.HasSuffix(allowed, "/*") {
		return strings.HasPrefix(actual, strings.TrimSuffix(allowed, "*"))
	}
	return allowed == actual
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
		s.envelope(w, r, code, nil, err, s.codecMgr)
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

// writeResponse writes resp with the codec resolved from produces at route
// registration; codec being nil means registration-time validation was
// bypassed, so fall back to a per-request resolve.
func writeResponse(w http.ResponseWriter, r *http.Request, s *Server, statusCode int, produces string, codec Codec, resp interface{}) {
	if codec == nil {
		resolved, ok := s.codecMgr.Resolve(produces)
		if !ok {
			writeError(w, r, s, http.StatusInternalServerError, ErrRouteProducesUnsupported)
			return
		}
		codec = resolved
	}
	w.Header().Set("Content-Type", produces)
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

func buildSSEHandler(s *Server, handler SSEHandler) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
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
		_ = handler(r.Context(), paramsFromRequestWithPathParams(r, s.config, params), stream)
	})
}

func buildWebSocketHandler(s *Server, handler WebSocketHandler) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		_ = handler
		_ = s
		_ = r
		_ = params
		w.WriteHeader(http.StatusNotImplemented)
	})
}
