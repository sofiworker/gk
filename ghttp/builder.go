package ghttp

import (
	"net/http"
	"reflect"
)

// RouteBuilder builds a route in a go-restful-style chain.
type RouteBuilder[Req, Resp any] struct {
	server      *Server
	path        string
	method      string
	doc         string
	tags        []string
	operationID string
	reqType     reflect.Type
	responses   []responseSpec
	handler     HandlerFunc[Req, Resp]
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

// Route creates a new RouteBuilder on the given path.
// Usage: Route[CreateUserReq, UserResp](s, "/users/{id}").POST("").To(handler)
func Route[Req, Resp any](s *Server, path string) *RouteBuilder[Req, Resp] {
	return &RouteBuilder[Req, Resp]{
		server: s,
		path:   path,
	}
}

func (b *RouteBuilder[Req, Resp]) methodSet(method string, subpath string) string {
	b.method = method
	b.path = JoinPaths(b.path, subpath)
	return b.path
}

func (b *RouteBuilder[Req, Resp]) POST(subpath string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodPost, subpath)
	return b
}

func (b *RouteBuilder[Req, Resp]) GET(subpath string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodGet, subpath)
	return b
}

func (b *RouteBuilder[Req, Resp]) PUT(subpath string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodPut, subpath)
	return b
}

func (b *RouteBuilder[Req, Resp]) DELETE(subpath string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodDelete, subpath)
	return b
}

func (b *RouteBuilder[Req, Resp]) PATCH(subpath string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodPatch, subpath)
	return b
}

func (b *RouteBuilder[Req, Resp]) HEAD(subpath string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodHead, subpath)
	return b
}

func (b *RouteBuilder[Req, Resp]) OPTIONS(subpath string) *RouteBuilder[Req, Resp] {
	b.methodSet(http.MethodOptions, subpath)
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

// To registers the handler and finalizes the route.
func (b *RouteBuilder[Req, Resp]) To(handler HandlerFunc[Req, Resp]) {
	b.handler = handler
	b.register()
	_ = b.server.router.Register(b.method, b.path, b.buildHandler())
}

func (b *RouteBuilder[Req, Resp]) register() {
	if b.server.openAPI != nil {
		b.server.openAPI.AddRoute(b.method, b.path, b.doc, b.tags, b.operationID, b.reqType, b.responses)
	}
}

func (b *RouteBuilder[Req, Resp]) buildHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Req
		if err := parseInput(r, &input); err != nil {
			writeError(w, r, b.server, http.StatusBadRequest, err)
			return
		}

		if b.server.validator != nil {
			if err := b.server.validator.Validate(r.Context(), &input); err != nil {
				writeError(w, r, b.server, http.StatusUnprocessableEntity, err)
				return
			}
		}

		resp, err := b.handler(r.Context(), &input)
		if err != nil {
			if isErrHandled(err) {
				return
			}
			writeError(w, r, b.server, http.StatusInternalServerError, err)
			return
		}

		if b.server.envelope != nil {
			ectx := &responseContext{w: w, r: r, codecMgr: b.server.codecMgr}
			b.server.envelope(ectx, resolveStatusCode(resp), resp, nil, b.server.codecMgr)
		}
	})
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

// ---- Server shortcut methods ----
// These must be top-level functions because Go 1.24 does not allow
// type parameters on methods. Usage: ghttp.Get(app, "/path", handler)

// Get registers a GET route.
func Get[Req, Resp any](s *Server, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	b := &RouteBuilder[Req, Resp]{server: s, path: path, method: http.MethodGet}
	for _, opt := range opts {
		opt(b)
	}
	b.To(handler)
}

// Post registers a POST route.
func Post[Req, Resp any](s *Server, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	b := &RouteBuilder[Req, Resp]{server: s, path: path, method: http.MethodPost}
	for _, opt := range opts {
		opt(b)
	}
	b.To(handler)
}

// Put registers a PUT route.
func Put[Req, Resp any](s *Server, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	b := &RouteBuilder[Req, Resp]{server: s, path: path, method: http.MethodPut}
	for _, opt := range opts {
		opt(b)
	}
	b.To(handler)
}

// Delete registers a DELETE route.
func Delete[Req, Resp any](s *Server, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	b := &RouteBuilder[Req, Resp]{server: s, path: path, method: http.MethodDelete}
	for _, opt := range opts {
		opt(b)
	}
	b.To(handler)
}

// Patch registers a PATCH route.
func Patch[Req, Resp any](s *Server, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	b := &RouteBuilder[Req, Resp]{server: s, path: path, method: http.MethodPatch}
	for _, opt := range opts {
		opt(b)
	}
	b.To(handler)
}

// RouteOption is an option that applies to a RouteBuilder.
type RouteOption func(interface{})
