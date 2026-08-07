package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

var (
	ErrRouteMethodRequired       = errors.New("route method is required")
	ErrRouteMethodEmpty          = errors.New("route method is empty")
	ErrRouteMethodInvalid        = errors.New("route method is invalid")
	ErrRouteProducesRequired     = errors.New("route produces content type is required")
	ErrRouteProducesUnsupported  = errors.New("route produces content type is unsupported")
	ErrRouteValidatorUnsupported = errors.New("route validator is unsupported")

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

// compiledInput 是路由输入类型在注册期编译的构造器。
// compiledInput is the registration-time compiled input constructor.
// newTarget 分配解析/校验目标，finish 将填充后的目标转为 handler 参数。
// newTarget allocates the target; finish turns it into the handler argument.
// 每个路由只编译一次，消除了类型化路径逐请求的 reflect.New 与整结构拷贝。
// compiling once removes per-request reflect.New and whole-struct copies.
type compiledInput[Req any] struct {
	directParams bool
	newTarget    func() any
	finish       func(any) Req
}

func compileInput[Req any]() compiledInput[Req] {
	t := reflect.TypeFor[Req]()
	if t == reflect.TypeFor[Params]() {
		return compiledInput[Req]{directParams: true}
	}
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
	routePath(path string) string
	routeGroup() *Group
	consumesContentTypes() []string
	producesContentTypes() []string
	owner() *Server
}

type responseCodec struct {
	contentType string
	codec       Codec
}

type responseHeader struct {
	name  string
	value string
}

// routeBuilderCore 保存 pre-1.27 与 Go 1.27 两套 builder 共享的注册状态。
// routeBuilderCore holds state shared by both builder versions.
// 两个公开 builder 只在 Req/Resp 类型参数位置不同。
// the two public builders differ only in where type parameters live.
type routeBuilderCore struct {
	target          routeTarget
	path            string
	methods         []string
	doc             RouteDoc
	consumes        []string
	consumesSet     bool
	maxBodyBytes    int64
	maxBodyBytesSet bool
	produces        []string
	middlewares     []Middleware
	codecs          []responseCodec // 注册时从 produces 解析；resolved from produces at registration.
	validator       any             // 收敛后的 routeValidateFunc[Req]；after coercion.
	validationError error
	setupErr        error
	skipValidation  bool
	finalized       bool
	responseStatus  int
	responseHeaders []responseHeader
	errorWriter     ErrorWriter
	wsCheckOrigin   func(*http.Request) bool
}

func newRouteBuilderCore(target routeTarget) *routeBuilderCore {
	return &routeBuilderCore{target: target}
}

func (c *routeBuilderCore) methodSet(method string, path string) string {
	c.ensureMethodUnset()
	c.methods = []string{method}
	c.path = path
	return c.path
}

func (c *routeBuilderCore) methodsSet(methods []string, path string) string {
	c.ensureMethodUnset()
	c.methods = append(c.methods[:0], methods...)
	c.path = path
	return c.path
}

func (c *routeBuilderCore) docOptions(opts ...DocOption) {
	c.ensureMutable()
	for _, opt := range opts {
		if opt != nil {
			opt(&c.doc)
		}
	}
}

func (c *routeBuilderCore) setProduces(contentTypes ...string) {
	c.ensureMutable()
	c.produces = normalizeContentTypes(contentTypes)
}

func (c *routeBuilderCore) setConsumes(contentTypes ...string) {
	c.ensureMutable()
	c.consumes = normalizeContentTypes(contentTypes)
	c.consumesSet = true
}

func (c *routeBuilderCore) setMaxBodyBytes(n int64) {
	c.ensureMutable()
	c.maxBodyBytes = n
	c.maxBodyBytesSet = true
}

func (c *routeBuilderCore) skipValidationSet() {
	c.ensureMutable()
	c.skipValidation = true
}

func (c *routeBuilderCore) status(code int) {
	c.ensureMutable()
	c.responseStatus = code
}

func (c *routeBuilderCore) responseHeader(name, value string) {
	c.ensureMutable()
	c.responseHeaders = append(c.responseHeaders, responseHeader{name: name, value: value})
}

func (c *routeBuilderCore) setErrorWriter(writer ErrorWriter) {
	c.ensureMutable()
	c.errorWriter = writer
}

func (c *routeBuilderCore) setWebSocketCheckOrigin(check func(*http.Request) bool) {
	c.ensureMutable()
	c.wsCheckOrigin = check
}

func (c *routeBuilderCore) problemDetails() {
	c.ensureMutable()
	c.errorWriter = problemErrorWriter(c.target.owner())
}

func (c *routeBuilderCore) use(mws ...Middleware) {
	c.ensureMutable()
	c.middlewares = append(c.middlewares, mws...)
}

// group 以 builder target 为根创建新路由组（gin 的 r.Group 语义）。
// group branches into a new route group rooted at the builder target.
// 必须在设置 method/path 之前调用；已设置的路由级选项不转移。
// it must be called before a method/path is set; route options are not transferred.
func (c *routeBuilderCore) group(prefix string, mws ...Middleware) *Group {
	c.ensureMethodUnset()
	switch target := c.target.(type) {
	case *Server:
		return target.Group(prefix, mws...)
	case *Group:
		return target.Group(prefix, mws...)
	default:
		panic(fmt.Sprintf("ghttp: unsupported route target %T", c.target))
	}
}

func (c *routeBuilderCore) ensureMethodUnset() {
	c.ensureMutable()
	if len(c.methods) > 0 {
		panic(ErrRouteMethodAlreadySet)
	}
}

func (c *routeBuilderCore) ensureMutable() {
	if c.finalized {
		panic(ErrRouteBuilderFinalized)
	}
	c.target.owner().assertMutable()
}

func (c *routeBuilderCore) beginTerminal() {
	c.ensureMutable()
	if c.setupErr != nil {
		c.panicSetupError(c.setupErr)
	}
	c.finalized = true
}

func (c *routeBuilderCore) panicSetupError(err error, methodOverride ...string) {
	if err == nil {
		return
	}
	panic(fmt.Errorf("route setup %s: %w", c.setupErrorRoute(methodOverride...), err))
}

func (c *routeBuilderCore) setupErrorRoute(methodOverride ...string) string {
	methods := c.methods
	if len(methodOverride) > 0 {
		methods = methodOverride
	}
	method := "<method unset>"
	if len(methods) > 0 {
		method = strings.Join(methods, ",")
	}
	path := c.path
	if strings.TrimSpace(path) == "" {
		path = "<path unset>"
	}
	return method + " " + path
}

func (c *routeBuilderCore) validateMethods() error {
	if len(c.methods) == 0 {
		return ErrRouteMethodRequired
	}
	for _, method := range c.methods {
		if method == "" {
			return ErrRouteMethodEmpty
		}
		if !isHTTPMethodToken(method) {
			return ErrRouteMethodInvalid
		}
	}
	return nil
}

func (c *routeBuilderCore) resolveProduces() error {
	if len(c.produces) == 0 {
		c.produces = append([]string(nil), c.target.producesContentTypes()...)
	}
	if len(c.produces) == 0 {
		return ErrRouteProducesRequired
	}
	c.codecs = c.codecs[:0]
	for _, contentType := range c.produces {
		codec, ok := c.target.owner().codecMgr.Resolve(contentType)
		if !ok {
			return ErrRouteProducesUnsupported
		}
		c.codecs = append(c.codecs, responseCodec{contentType: contentType, codec: codec})
	}
	return nil
}

func (c *routeBuilderCore) resolveConsumes() {
	if c.consumesSet {
		return
	}
	c.consumes = append([]string(nil), c.target.consumesContentTypes()...)
}

func (c *routeBuilderCore) effectiveMaxBodyBytes(s *Server) int64 {
	if c.maxBodyBytesSet {
		return c.maxBodyBytes
	}
	if s == nil || s.config == nil {
		return 0
	}
	return s.config.maxBodyBytes
}

func (c *routeBuilderCore) directParamsGlobalValidator() Validator {
	server := c.target.owner()
	if c.skipValidation || server.validator == nil {
		return nil
	}
	return server.validator
}

// registerHandler 为每个选中方法构建 routeDefinition 并交给注册中心。
// registerHandler builds routeDefinitions and hands them to the registry.
// reqType/respType 供 OpenAPI 推断；nil 表示该终结器不暴露类型化 schema。
// reqType/respType feed OpenAPI; nil means no typed schema.
func (c *routeBuilderCore) registerHandler(handler http.Handler, needsExtractor bool, terminal routeTerminalKind, responseStatus int, reqType, respType reflect.Type) {
	if err := c.validateMethods(); err != nil {
		c.panicSetupError(err)
	}
	pattern, err := parseRoutePattern(c.target.routePath(c.path), c.target.owner().config.strictRouting)
	if err != nil {
		c.panicSetupError(err)
	}
	definitions := make([]routeDefinition, 0, len(c.methods))
	for _, method := range c.methods {
		definitions = append(definitions, routeDefinition{
			method:          method,
			pattern:         pattern,
			handler:         handler,
			middlewares:     append([]Middleware(nil), c.middlewares...),
			group:           c.target.routeGroup(),
			needsExtractor:  needsExtractor,
			terminal:        terminal,
			responseStatus:  responseStatus,
			responseHeaders: append([]responseHeader(nil), c.responseHeaders...),
			errorWriter:     c.errorWriter,
			doc:             c.doc.clone(),
			reqType:         reqType,
			respType:        respType,
			consumes:        append([]string(nil), c.consumes...),
			produces:        append([]string(nil), c.produces...),
		})
	}
	if err := c.target.owner().registerDefinitions(definitions...); err != nil {
		c.panicSetupError(err)
	}
}

func (c *routeBuilderCore) toHandler(handler http.Handler, needsExtractor bool, terminal routeTerminalKind, responseStatus int, reqType, respType reflect.Type) {
	c.beginTerminal()
	if isNilHTTPHandler(handler) {
		c.panicSetupError(ErrRouteHandlerNil)
	}
	c.registerHandler(handler, needsExtractor, terminal, responseStatus, reqType, respType)
}

func (c *routeBuilderCore) registerStaticFS(fs http.FileSystem, reqType, respType reflect.Type) {
	if fs == nil {
		c.panicSetupError(ErrStaticRootRequired)
	}
	prefix := strings.TrimRight(c.path, "/")
	handler := http.StripPrefix(prefix, http.FileServer(fs))
	c.path = joinRoutePaths(c.path, "{path...}")
	c.registerHandler(handler, false, routeTerminalStatic, 0, reqType, respType)
}

func (c *routeBuilderCore) toHTML(status int, name string, data interface{}, reqType, respType reflect.Type) {
	c.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := renderHTML(w, c.target.owner(), status, name, data); err != nil {
			writeError(w, r, c.target.owner(), http.StatusInternalServerError, err)
		}
	}), false, routeTerminalHTML, status, reqType, respType)
}

func (c *routeBuilderCore) toSSE(handler SSEHandler, reqType, respType reflect.Type) {
	if handler == nil {
		c.beginTerminal()
		c.panicSetupError(ErrRouteHandlerNil)
	}
	c.toHandler(buildSSEHandler(c.target.owner(), handler), true, routeTerminalSSE, 0, reqType, respType)
}

func (c *routeBuilderCore) toWebSocket(handler WebSocketHandler, reqType, respType reflect.Type) {
	if handler == nil {
		c.beginTerminal()
		c.panicSetupError(ErrRouteHandlerNil)
	}
	c.toHandler(buildWebSocketHandler(c.target.owner(), handler, c.wsCheckOrigin), true, routeTerminalWebSocket, http.StatusSwitchingProtocols, reqType, respType)
}

func (c *routeBuilderCore) writeError(w http.ResponseWriter, r *http.Request, defaultCode int, err error) {
	writeRouteError(w, r, c.target.owner(), c.errorWriter, c.produces, defaultCode, err)
}

// writeRouteError 依次分发到路由级错误 writer、服务器 writer 与内置 writer。
// writeRouteError dispatches through route, server and built-in writers.
// 类型化管线与路径提取终结器共用，确保路由 writer 覆盖请求路径上的所有错误。
// shared by the typed pipeline and extraction terminal.
func writeRouteError(w http.ResponseWriter, r *http.Request, owner *Server, writer ErrorWriter, produces []string, defaultCode int, err error) {
	if owner == nil {
		writeErrorWithCodec(w, r, nil, defaultCode, err, produces, nil)
		return
	}
	if owner.dispatchError(w, r, defaultCode, err) {
		return
	}
	if writer != nil && writer(w, r, statusCodeFromError(defaultCode, err), err) {
		return
	}
	if owner.config.errorWriter != nil && owner.config.errorWriter(w, r, statusCodeFromError(defaultCode, err), err) {
		return
	}
	writeErrorWithCodec(w, r, owner, defaultCode, err, produces, nil)
}

func (c *routeBuilderCore) writeTypedResponse(w http.ResponseWriter, r *http.Request, server *Server, resp interface{}) {
	status := c.responseStatus
	if status == 0 {
		status = http.StatusOK
	}
	if sc, ok := resp.(StatusCoder); ok {
		if code := sc.StatusCode(); code != 0 {
			status = code
		}
	}
	if status < http.StatusContinue || status > 599 {
		c.writeError(w, r, http.StatusInternalServerError, Err(http.StatusInternalServerError, "invalid response status"))
		return
	}
	if hw, ok := resp.(ResponseHeaderWriter); ok {
		hw.WriteResponseHeaders(w.Header())
	}
	for _, h := range c.responseHeaders {
		w.Header().Set(h.name, h.value)
	}
	contentType, codec, ok := negotiateRouteCodec(w, r, server, c.produces, c.codecs)
	if !ok {
		return
	}
	if !responseHasBody(status) {
		w.WriteHeader(status)
		return
	}
	if server.envelope != nil {
		server.envelope(w, r, status, resp, nil, contentType, codec)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = codec.Marshal(w, resp)
}

// coerceRouteValidator 将 core 上的未类型化 validator 收敛为具体 Req 类型。
// coerceRouteValidator normalizes the untyped validator to the route's Req.
// pre-1.27 在 Validate() 时已知 Req；1.27 在终结方法处收敛。
// pre-1.27 knows Req at Validate(); 1.27 coerces at the terminal.
func coerceRouteValidator[Req any](stored any) (routeValidateFunc[Req], error) {
	switch validator := stored.(type) {
	case nil:
		return nil, nil
	case routeValidateFunc[Req]:
		return validator, nil
	case ValidateFunc[Req]:
		return routeValidateFunc[Req](validator), nil
	case func(context.Context, Req) error:
		return validator, nil
	case SimpleValidateFunc[Req]:
		return func(_ context.Context, req Req) error {
			return validator(req)
		}, nil
	case func(Req) error:
		return func(_ context.Context, req Req) error {
			return validator(req)
		}, nil
	default:
		return nil, ErrRouteValidatorUnsupported
	}
}

// registerTypedHandler 注册两套 builder 共用的“有输入有输出”终结器。
// registerTypedHandler registers the typed in+out terminal shared by both builders.
func registerTypedHandler[Req, Resp any](core *routeBuilderCore, input compiledInput[Req], handler HandlerFunc[Req, Resp]) {
	core.beginTerminal()
	if handler == nil {
		core.panicSetupError(ErrRouteHandlerNil)
	}
	if err := core.validateMethods(); err != nil {
		core.panicSetupError(err)
	}
	if err := validateRequestParamsUsage[Req](); err != nil {
		core.panicSetupError(err)
	}
	if err := core.resolveProduces(); err != nil {
		core.panicSetupError(err)
	}
	core.resolveConsumes()
	validator, err := coerceRouteValidator[Req](core.validator)
	if err != nil {
		core.panicSetupError(err)
	}
	var h http.Handler
	if input.directParams {
		directHandler := any(handler).(HandlerFunc[Params, Resp])
		var validatorParams routeValidateFunc[Params]
		if validator != nil {
			validatorParams = any(validator).(routeValidateFunc[Params])
		}
		if gv := core.directParamsGlobalValidator(); gv != nil {
			h = buildParamsHandlerWithGlobalValidator(core, directHandler, gv, validatorParams)
		} else {
			h = buildParamsHandler(core, directHandler, validatorParams)
		}
	} else {
		h = buildTypedHandler(core, input, handler, validator)
	}
	core.registerHandler(h, true, routeTerminalTyped, core.responseStatus, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// registerNoInputHandler 注册无请求输入的 handler。
// registerNoInputHandler registers a handler with no request input.
// 不解析 body、不校验 Content-Type；参数仍可通过 MatchedParams 获取。
// it never parses a body or validates Content-Type; params stay via MatchedParams.
func registerNoInputHandler[Resp any](core *routeBuilderCore, handler NoInputHandler[Resp]) {
	core.beginTerminal()
	if handler == nil {
		core.panicSetupError(ErrRouteHandlerNil)
	}
	if err := core.validateMethods(); err != nil {
		core.panicSetupError(err)
	}
	if err := core.resolveProduces(); err != nil {
		core.panicSetupError(err)
	}
	core.resolveConsumes()
	h := buildNoInputHandler(core, handler)
	core.registerHandler(h, true, routeTerminalTyped, core.responseStatus, reflect.TypeOf(struct{}{}), reflect.TypeFor[Resp]())
}

// registerNoOutputHandler 注册只返回 error 的 handler。
// registerNoOutputHandler registers a handler that returns only an error.
// 成功默认 204，可用 Status 覆盖。
// success defaults to 204 and can be overridden with Status.
func registerNoOutputHandler[Req any](core *routeBuilderCore, input compiledInput[Req], handler NoOutputHandler[Req]) {
	core.beginTerminal()
	if handler == nil {
		core.panicSetupError(ErrRouteHandlerNil)
	}
	if err := core.validateMethods(); err != nil {
		core.panicSetupError(err)
	}
	if err := validateRequestParamsUsage[Req](); err != nil {
		core.panicSetupError(err)
	}
	core.resolveConsumes()
	validator, err := coerceRouteValidator[Req](core.validator)
	if err != nil {
		core.panicSetupError(err)
	}
	status := core.responseStatus
	if status == 0 {
		status = http.StatusNoContent
	}
	h := buildNoOutputHandler(core, input, handler, validator, status)
	core.registerHandler(h, true, routeTerminalTyped, status, reflect.TypeFor[Req](), nil)
}

func registerHTTPFuncHandler[Req any](core *routeBuilderCore, input compiledInput[Req], handler HTTPHandlerFunc[Req], terminal routeTerminalKind, responseStatus int) {
	core.beginTerminal()
	if handler == nil {
		core.panicSetupError(ErrRouteHandlerNil)
	}
	if err := validateRequestParamsUsage[Req](); err != nil {
		core.panicSetupError(err)
	}
	core.resolveConsumes()
	validator, err := coerceRouteValidator[Req](core.validator)
	if err != nil {
		core.panicSetupError(err)
	}
	var h http.Handler
	if input.directParams {
		directHandler := any(handler).(HTTPHandlerFunc[Params])
		var validatorParams routeValidateFunc[Params]
		if validator != nil {
			validatorParams = any(validator).(routeValidateFunc[Params])
		}
		if gv := core.directParamsGlobalValidator(); gv != nil {
			h = buildParamsHTTPHandlerWithGlobalValidator(core, directHandler, gv, validatorParams)
		} else {
			h = buildParamsHTTPHandler(core, directHandler, validatorParams)
		}
	} else {
		h = buildHTTPFuncHandler(core, input, handler, validator)
	}
	core.registerHandler(h, true, terminal, responseStatus, reflect.TypeFor[Req](), nil)
}

func registerRedirectFuncHandler[Req any](core *routeBuilderCore, input compiledInput[Req], code int, redirect func(Req) (string, error)) {
	if redirect == nil {
		core.beginTerminal()
		core.panicSetupError(ErrRouteHandlerNil)
	}
	registerHTTPFuncHandler(core, input, func(w http.ResponseWriter, r *http.Request, input Req) error {
		location, err := redirect(input)
		if err != nil {
			return err
		}
		http.Redirect(w, r, location, code)
		return nil
	}, routeTerminalRedirect, code)
}

func buildTypedHandler[Req, Resp any](core *routeBuilderCore, input compiledInput[Req], handler HandlerFunc[Req, Resp], validator routeValidateFunc[Req]) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		parsed, ok := parseAndValidateRouteInput(core, input, w, r, params, validator)
		if !ok {
			return
		}

		server := core.target.owner()
		resp, err := handler(r.Context(), parsed)
		if err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
			return
		}

		writeResponseCookies(w, resp)
		core.writeTypedResponse(w, r, server, resp)
	})
}

func buildNoInputHandler[Resp any](core *routeBuilderCore, handler NoInputHandler[Resp]) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, _ pathParamList) {
		server := core.target.owner()
		resp, err := handler(r.Context())
		if err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
			return
		}

		writeResponseCookies(w, resp)
		core.writeTypedResponse(w, r, server, resp)
	})
}

func buildNoOutputHandler[Req any](core *routeBuilderCore, input compiledInput[Req], handler NoOutputHandler[Req], validator routeValidateFunc[Req], status int) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		parsed, ok := parseAndValidateRouteInput(core, input, w, r, params, validator)
		if !ok {
			return
		}
		if err := handler(r.Context(), parsed); err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		if status < http.StatusContinue || status > 599 {
			core.writeError(w, r, http.StatusInternalServerError, Err(http.StatusInternalServerError, "invalid response status"))
			return
		}
		for _, h := range core.responseHeaders {
			w.Header().Set(h.name, h.value)
		}
		w.WriteHeader(status)
	})
}

func buildHTTPFuncHandler[Req any](core *routeBuilderCore, input compiledInput[Req], handler HTTPHandlerFunc[Req], validator routeValidateFunc[Req]) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		parsed, ok := parseAndValidateRouteInput(core, input, w, r, params, validator)
		if !ok {
			return
		}
		if err := handler(w, r, parsed); err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
		}
	})
}

func buildParamsHandler[Resp any](core *routeBuilderCore, handler HandlerFunc[Params, Resp], validator routeValidateFunc[Params]) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		input, ok := parseAndValidateParamsWithPathParams(core, w, r, params, validator)
		if !ok {
			return
		}

		server := core.target.owner()
		resp, err := handler(r.Context(), input)
		if err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
			return
		}

		writeResponseCookies(w, resp)
		core.writeTypedResponse(w, r, server, resp)
	})
}

func buildParamsHandlerWithGlobalValidator[Resp any](core *routeBuilderCore, handler HandlerFunc[Params, Resp], globalValidator Validator, validator routeValidateFunc[Params]) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		input, ok := parseAndValidateParamsWithPathParams(core, w, r, params, validator)
		if !ok || !validateDirectParamsWithGlobalValidator(core, w, r, &input, globalValidator) {
			return
		}

		server := core.target.owner()
		resp, err := handler(r.Context(), input)
		if err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
			return
		}

		writeResponseCookies(w, resp)
		core.writeTypedResponse(w, r, server, resp)
	})
}

func buildParamsHTTPHandler(core *routeBuilderCore, handler HTTPHandlerFunc[Params], validator routeValidateFunc[Params]) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		input, ok := parseAndValidateParamsWithPathParams(core, w, r, params, validator)
		if !ok {
			return
		}
		if err := handler(w, r, input); err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
		}
	})
}

func buildParamsHTTPHandlerWithGlobalValidator(core *routeBuilderCore, handler HTTPHandlerFunc[Params], globalValidator Validator, validator routeValidateFunc[Params]) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		input, ok := parseAndValidateParamsWithPathParams(core, w, r, params, validator)
		if !ok || !validateDirectParamsWithGlobalValidator(core, w, r, &input, globalValidator) {
			return
		}
		if err := handler(w, r, input); err != nil {
			if isErrHandled(err) {
				return
			}
			core.writeError(w, r, http.StatusInternalServerError, err)
		}
	})
}

func parseAndValidateParamsWithPathParams(core *routeBuilderCore, w http.ResponseWriter, r *http.Request, params pathParamList, validator routeValidateFunc[Params]) (Params, bool) {
	server := core.target.owner()
	if maxBodyBytes := core.effectiveMaxBodyBytes(server); maxBodyBytes > 0 {
		r = requestWithMaxBodyBytes(w, r, maxBodyBytes)
	}
	input := paramsFromRequestWithPathParams(r, server.config, params)
	if validator != nil {
		if err := validator(r.Context(), input); err != nil {
			core.writeError(w, r, http.StatusUnprocessableEntity, mappedValidationError(core.validationError, err))
			return Params{}, false
		}
	}
	return input, true
}

func validateDirectParamsWithGlobalValidator(core *routeBuilderCore, w http.ResponseWriter, r *http.Request, input *Params, validator Validator) bool {
	if err := validator.Validate(r.Context(), input); err != nil {
		core.writeError(w, r, http.StatusUnprocessableEntity, err)
		return false
	}
	return true
}

func parseAndValidateRouteInput[Req any](core *routeBuilderCore, input compiledInput[Req], w http.ResponseWriter, r *http.Request, params pathParamList, validator routeValidateFunc[Req]) (Req, bool) {
	server := core.target.owner()
	if maxBodyBytes := core.effectiveMaxBodyBytes(server); maxBodyBytes > 0 {
		r = requestWithMaxBodyBytes(w, r, maxBodyBytes)
	}
	var (
		target any
		parsed Req
	)
	if input.directParams {
		parsed = any(paramsFromRequestWithPathParams(r, server.config, params)).(Req)
		target = parsed
	} else {
		target = input.newTarget()
	}
	if err := validateRequestContentType(r, target, core.consumes); err != nil {
		core.writeError(w, r, http.StatusUnsupportedMediaType, err)
		var zero Req
		return zero, false
	}
	if !input.directParams {
		if err := parseInputWithConfigAndPathParams(r, target, server.config, server.codecMgr, params); err != nil {
			code := http.StatusBadRequest
			if he := AsError(err); he != nil {
				code = he.Code
			}
			if isRequestBodyTooLarge(err) {
				code = http.StatusRequestEntityTooLarge
				err = Err(code, ErrRequestBodyTooLarge.Error(), WithCause(err))
			}
			core.writeError(w, r, code, err)
			var zero Req
			return zero, false
		}
		parsed = input.finish(target)
	}
	if validator != nil {
		if err := validator(r.Context(), parsed); err != nil {
			core.writeError(w, r, http.StatusUnprocessableEntity, mappedValidationError(core.validationError, err))
			var zero Req
			return zero, false
		}
	}

	if server.validator != nil && !core.skipValidation {
		if err := server.validator.Validate(r.Context(), target); err != nil {
			core.writeError(w, r, http.StatusUnprocessableEntity, err)
			var zero Req
			return zero, false
		}
	}

	return parsed, true
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
		return Err(http.StatusUnsupportedMediaType, "missing Content-Type", WithCause(ErrUnsupportedMediaType))
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
	if s.dispatchError(w, r, defaultCode, err) {
		return
	}
	if s != nil && s.config.errorWriter != nil && s.config.errorWriter(w, r, statusCodeFromError(defaultCode, err), err) {
		return
	}
	writeErrorWithCodec(w, r, s, defaultCode, err, nil, nil)
}

func writeErrorWithCodec(w http.ResponseWriter, r *http.Request, s *Server, defaultCode int, err error, produces []string, codecs []responseCodec) {
	if responseErrorWriteBlocked(r) {
		return
	}
	if s == nil {
		http.Error(w, err.Error(), statusCodeFromError(defaultCode, err))
		return
	}
	code := statusCodeFromError(defaultCode, err)
	if s.config.problemDetails {
		writeProblemDetails(w, r, s, code, err)
		return
	}
	body := HTTPError{Code: code, Message: http.StatusText(code), Err: err}
	if he := AsError(err); he != nil {
		body = *he
	} else if s.config.exposeErrorDetails {
		body.Message = err.Error()
	}
	if strings.TrimSpace(body.Message) == "" {
		body.Message = http.StatusText(code)
	}
	contentType, codec, _ := selectResponseCodec(s, r.Header.Get("Accept"), produces, codecs)
	if codec == nil {
		contentType = MIMEJSON
		codec, _ = s.codecMgr.Resolve(MIMEJSON)
	}
	if s.envelope != nil {
		s.envelope(w, r, code, nil, err, contentType, codec)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(code)
	_ = codec.Marshal(w, &body)
}

func writeProblemDetails(w http.ResponseWriter, r *http.Request, s *Server, code int, err error) {
	detail := http.StatusText(code)
	if he := AsError(err); he != nil {
		if he.Message != "" {
			detail = he.Message
		}
	} else if s.config.exposeErrorDetails {
		detail = err.Error()
	}
	body := map[string]any{
		"type":     "about:blank",
		"title":    http.StatusText(code),
		"status":   code,
		"detail":   detail,
		"instance": r.URL.Path,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func statusCodeFromError(defaultCode int, err error) int {
	if he := AsError(err); he != nil {
		return he.Code
	}
	return defaultCode
}

func responseHasBody(status int) bool {
	return status >= http.StatusOK && status != http.StatusNoContent && status != http.StatusNotModified
}

func selectResponseCodec(s *Server, accept string, produces []string, codecs []responseCodec) (string, Codec, bool) {
	if len(codecs) == 0 {
		if len(produces) == 0 && s != nil {
			produces = s.produces
		}
		codecs = resolveResponseCodecs(s, produces)
	}
	if len(codecs) == 0 {
		return "", nil, false
	}
	candidates := make([]string, len(codecs))
	for i, c := range codecs {
		candidates[i] = c.contentType
	}
	contentType, codec, ok := s.codecMgr.Select(accept, candidates)
	if !ok {
		return "", nil, false
	}
	return contentType, codec, true
}

func negotiateRouteCodec(w http.ResponseWriter, r *http.Request, s *Server, produces []string, codecs []responseCodec) (string, Codec, bool) {
	contentType, codec, matched := selectResponseCodec(s, r.Header.Get("Accept"), produces, codecs)
	if matched {
		return contentType, codec, true
	}
	if len(codecs) == 0 {
		writeError(w, r, s, http.StatusInternalServerError, ErrRouteProducesUnsupported)
		return "", nil, false
	}
	if s.config.lenientContentNegotiation {
		return codecs[0].contentType, codecs[0].codec, true
	}
	writeError(w, r, s, http.StatusNotAcceptable, Err(http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable)))
	return "", nil, false
}

func resolveResponseCodecs(s *Server, produces []string) []responseCodec {
	if s == nil {
		return nil
	}
	out := make([]responseCodec, 0, len(produces))
	for _, contentType := range produces {
		codec, ok := s.codecMgr.Resolve(contentType)
		if !ok {
			continue
		}
		out = append(out, responseCodec{contentType: contentType, codec: codec})
	}
	return out
}

func isNilHTTPHandler(handler http.Handler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	return (value.Kind() == reflect.Func || value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface) && value.IsNil()
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
		if err := handler(r.Context(), paramsFromRequestWithPathParams(r, s.config, params), stream); err != nil {
			if s.logger != nil {
				s.logger.ErrorContext(r.Context(), "sse handler error", "error", err, "path", r.URL.Path)
			}
		}
	})
}
