package ghttp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	// ErrOperationNil 表示挂载了 nil Operation。
	// ErrOperationNil indicates that a nil Operation was mounted.
	ErrOperationNil = errors.New("operation is nil")
	// ErrOperationMethodRequired 表示 Operation 没有声明 HTTP 方法。
	// ErrOperationMethodRequired indicates that an Operation has no HTTP method.
	ErrOperationMethodRequired = errors.New("operation method is required")
	// ErrOperationMethodInvalid 表示 Operation 声明了非法 HTTP 方法。
	// ErrOperationMethodInvalid indicates that an Operation declares an invalid HTTP method.
	ErrOperationMethodInvalid = errors.New("operation method is invalid")
	// ErrOperationHandlerNil 表示 Operation 没有可执行 handler。
	// ErrOperationHandlerNil indicates that an Operation has no executable handler.
	ErrOperationHandlerNil = errors.New("operation handler is nil")
	// ErrEndpointBuilderNil 表示 Handle 收到了 nil EndpointBuilder。
	// ErrEndpointBuilderNil indicates that Handle received a nil EndpointBuilder.
	ErrEndpointBuilderNil = errors.New("endpoint builder is nil")
	// ErrOperationInputNil 表示 Operation 没有输入契约。
	// ErrOperationInputNil indicates that an Operation has no input contract.
	ErrOperationInputNil = errors.New("operation input is nil")
	// ErrOperationOutputNil 表示 Operation 没有输出契约。
	// ErrOperationOutputNil indicates that an Operation has no output contract.
	ErrOperationOutputNil = errors.New("operation output is nil")
	// ErrOperationFileSystemNil 表示静态文件 Operation 没有文件系统。
	// ErrOperationFileSystemNil indicates that a static-file Operation has no file system.
	ErrOperationFileSystemNil = errors.New("operation file system is nil")
	// ErrOperationFilePathRequired 表示静态文件 Operation 没有文件路径。
	// ErrOperationFilePathRequired indicates that a static-file Operation has no file path.
	ErrOperationFilePathRequired = errors.New("operation file path is required")
	// ErrOperationStatusInvalid 表示输出契约声明了无效 HTTP 状态码。
	// ErrOperationStatusInvalid indicates that an output contract declares an invalid HTTP status.
	ErrOperationStatusInvalid = errors.New("operation response status is invalid")
	// ErrOperationOutputWriterNil 表示自定义输出 writer 为空。
	// ErrOperationOutputWriterNil indicates that a custom output writer is nil.
	ErrOperationOutputWriterNil = errors.New("operation output writer is nil")
	// ErrOperationTemplateNil 表示 HTML 输出契约没有模板。
	// ErrOperationTemplateNil indicates that an HTML output contract has no template.
	ErrOperationTemplateNil = errors.New("operation template is nil")
)

type operationHandlerFactory func(*Server, *Operation) http.Handler

// Operation 是不可变、可复用的一等 HTTP endpoint 描述。
// Operation is an immutable, reusable first-class HTTP endpoint description.
type Operation struct {
	method           string
	path             string
	build            operationHandlerFactory
	fastBuild        func(*Server, *Operation) http.HandlerFunc
	middlewares      []Middleware
	doc              RouteDoc
	consumes         []string
	produces         []string
	openAPI          routeOpenAPIMetadata
	terminal         routeTerminalKind
	stateIndependent bool
	// requestOnly 标记输入链只读请求元数据;满足其它条件时可跳过请求状态注入。
	// requestOnly marks an input chain reading only request metadata; with the
	// other preconditions met, request-state injection can be skipped.
	requestOnly     bool
	setupErr        error
	status          int
	headers         []responseHeader
	wsCheckOrigin   func(*http.Request) bool
	maxBodyBytes    int64
	maxBodyBytesSet bool
	errorWriter     ErrorWriter
	problemDetails  bool
	skipValidation  bool
}

// Method 返回 HTTP 方法。
// Method returns the HTTP method.
func (o *Operation) Method() string {
	if o == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(o.method))
}

// Path 返回 endpoint 路径模式。
// Path returns the endpoint path pattern.
func (o *Operation) Path() string {
	if o == nil {
		return ""
	}
	return o.path
}

// WithMiddleware 返回追加路由中间件后的副本。
// WithMiddleware returns a copy with additional route middleware.
func (o *Operation) WithMiddleware(middlewares ...Middleware) *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.middlewares = append(cloned.middlewares, middlewares...)
	return cloned
}

// WithRequestContentTypes 返回覆盖请求 Content-Type 元数据后的副本。
// WithRequestContentTypes returns a copy with overridden request Content-Type metadata.
func (o *Operation) WithRequestContentTypes(contentTypes ...string) *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.consumes = normalizeContentTypes(contentTypes)
	return cloned
}

// WithResponseContentTypes 返回覆盖响应 Content-Type 元数据后的副本。
// WithResponseContentTypes returns a copy with overridden response Content-Type metadata.
func (o *Operation) WithResponseContentTypes(contentTypes ...string) *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.produces = normalizeContentTypes(contentTypes)
	return cloned
}

// Doc 返回附加 OpenAPI 文档选项后的副本。
// Doc returns a copy with additional OpenAPI documentation options.
func (o *Operation) Doc(options ...DocOption) *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	for _, option := range options {
		if option != nil {
			option(&cloned.doc)
		}
	}
	return cloned
}

// WithWebSocketOriginCheck 返回覆盖 WebSocket Origin 校验器的副本。
// WithWebSocketOriginCheck returns a copy with an overridden WebSocket Origin checker.
func (o *Operation) WithWebSocketOriginCheck(check func(*http.Request) bool) *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.wsCheckOrigin = check
	return cloned
}

// WithMaxBodyBytes 返回覆盖请求体大小上限后的副本。
// WithMaxBodyBytes returns a copy with an overridden request body size limit.
// 小于等于 0 时仅对该 Operation 禁用请求体大小限制。
// Values <= 0 disable the body size limit only for this Operation.
func (o *Operation) WithMaxBodyBytes(maxBytes int64) *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.maxBodyBytes = maxBytes
	cloned.maxBodyBytesSet = true
	return cloned
}

// WithErrorWriter 返回覆盖路由级错误 writer 后的副本。
// WithErrorWriter returns a copy with an overridden route-level error writer.
func (o *Operation) WithErrorWriter(writer ErrorWriter) *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.errorWriter = writer
	cloned.problemDetails = false
	return cloned
}

// WithProblemDetails 返回使用 RFC 9457 application/problem+json 错误响应的副本。
// WithProblemDetails returns a copy that uses RFC 9457 application/problem+json errors.
func (o *Operation) WithProblemDetails() *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.errorWriter = nil
	cloned.problemDetails = true
	return cloned
}

// WithoutServerValidation 返回跳过服务器级 Validator 的副本。
// WithoutServerValidation returns a copy that skips the server-wide Validator.
// 输入描述器自身的解析与显式校验仍会执行。
// Parsing and explicit validation owned by the input descriptor still run.
func (o *Operation) WithoutServerValidation() *Operation {
	if o == nil {
		return nil
	}
	cloned := o.clone()
	cloned.skipValidation = true
	return cloned
}

func (o *Operation) clone() *Operation {
	cloned := *o
	cloned.middlewares = append([]Middleware(nil), o.middlewares...)
	cloned.doc = o.doc.clone()
	cloned.consumes = append([]string(nil), o.consumes...)
	cloned.produces = append([]string(nil), o.produces...)
	cloned.openAPI = o.openAPI.clone()
	cloned.headers = append([]responseHeader(nil), o.headers...)
	return &cloned
}

// AllMethods 为全部标准 HTTP 方法返回独立的 Operation 副本。
// AllMethods returns independent Operation copies for every standard HTTP method.
func AllMethods(operation *Operation) []*Operation {
	if operation == nil {
		return []*Operation{nil}
	}
	operations := make([]*Operation, 0, len(allHTTPMethods))
	for _, method := range allHTTPMethods {
		cloned := operation.clone()
		cloned.method = method
		operations = append(operations, cloned)
	}
	return operations
}

// Mount 挂载一个一等 Operation。
// Mount mounts one first-class Operation.
func (s *Server) Mount(operation *Operation) error {
	return mountOperation(s, operation)
}

// MustMount 挂载全部 Operation，任一失败时 panic。
// MustMount mounts all Operations and panics on the first failure.
func (s *Server) MustMount(operations ...*Operation) *Server {
	for _, operation := range operations {
		if err := s.Mount(operation); err != nil {
			panic(err)
		}
	}
	return s
}

// Mount 挂载一个一等 Operation，并应用组前缀与组能力。
// Mount mounts one first-class Operation with the group prefix and capabilities.
func (g *Group) Mount(operation *Operation) error {
	return mountOperation(g, operation)
}

// MustMount 挂载全部 Operation，任一失败时 panic。
// MustMount mounts all Operations and panics on the first failure.
func (g *Group) MustMount(operations ...*Operation) *Group {
	for _, operation := range operations {
		if err := g.Mount(operation); err != nil {
			panic(err)
		}
	}
	return g
}

func mountOperation(target routeTarget, operation *Operation) error {
	if operation == nil {
		return ErrOperationNil
	}
	if operation.setupErr != nil {
		return operation.setupErr
	}
	if operation.build == nil {
		return ErrOperationHandlerNil
	}
	method := strings.ToUpper(strings.TrimSpace(operation.method))
	if method == "" {
		return ErrOperationMethodRequired
	}
	if !isHTTPMethodToken(method) {
		return fmt.Errorf("%w: %q", ErrOperationMethodInvalid, operation.method)
	}
	server := target.owner()
	server.assertMutable()
	pattern, err := parseRoutePattern(target.routePath(operation.path), server.config.strictRouting)
	if err != nil {
		return err
	}
	if err := validateOperationInputMetadata(operation.openAPI, pattern); err != nil {
		return err
	}
	mounted := operation
	if operation.problemDetails {
		mounted = operation.clone()
		mounted.errorWriter = problemErrorWriter(server)
	}
	handler := mounted.build(server, mounted)
	if handler == nil {
		return ErrOperationHandlerNil
	}
	if mounted.stateIndependent {
		handler = markStateIndependentTerminal(handler)
	}
	doc := mounted.doc.clone()
	if doc.Summary == "" {
		doc.Summary = method + " " + pattern.path
	}
	if doc.OperationID == "" {
		doc.OperationID = inferOperationID(method, pattern)
	}
	definition := routeDefinition{
		method:          method,
		pattern:         pattern,
		handler:         handler,
		fastBuild:       mounted.fastBuild,
		middlewares:     append([]Middleware(nil), mounted.middlewares...),
		group:           target.routeGroup(),
		needsExtractor:  pattern.hasParams(),
		requestOnly:     mounted.requestOnly,
		terminal:        mounted.terminal,
		responseStatus:  mounted.status,
		responseHeaders: append([]responseHeader(nil), mounted.headers...),
		errorWriter:     mounted.errorWriter,
		problemDetails:  mounted.problemDetails,
		doc:             doc,
		consumes:        append([]string(nil), mounted.consumes...),
		produces:        append([]string(nil), mounted.produces...),
		openAPI:         mounted.openAPI.clone(),
	}
	return server.registerDefinitions(definition)
}

func inferOperationID(method string, pattern routePattern) string {
	var builder strings.Builder
	builder.Grow(len(method) + len(pattern.path) + 8)
	builder.WriteString(strings.ToLower(method))
	if len(pattern.segments) == 0 {
		builder.WriteString("_root")
		return builder.String()
	}
	for _, segment := range pattern.segments {
		builder.WriteByte('_')
		if segment.kind == routeSegmentParameter || segment.kind == routeSegmentCatchAll {
			builder.WriteString("by_")
		}
		writeOperationIDPart(&builder, segment.value)
	}
	return builder.String()
}

func writeOperationIDPart(builder *strings.Builder, value string) {
	separator := false
	for _, char := range strings.ToLower(value) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			if separator && builder.Len() > 0 {
				builder.WriteByte('_')
			}
			builder.WriteRune(char)
			separator = false
			continue
		}
		separator = true
	}
}
