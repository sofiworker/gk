package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
)

// EndpointBuilder 保存尚未绑定输入、输出和业务逻辑的方法与路径。
// EndpointBuilder holds a method and path before input, output, and logic are bound.
type EndpointBuilder struct {
	method string
	path   string
}

func validateOperationInput[T any](writer http.ResponseWriter, request *http.Request, server *Server, operation *Operation, value T) bool {
	if server == nil || server.validator == nil || operation == nil || operation.skipValidation {
		return true
	}
	if err := server.validator.Validate(request.Context(), value); err != nil {
		writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusUnprocessableEntity, err)
		return false
	}
	return true
}

// Get 创建 GET endpoint 描述起点。
// Get creates a GET endpoint description start.
func Get(path string) *EndpointBuilder { return endpoint(http.MethodGet, path) }

// Post 创建 POST endpoint 描述起点。
// Post creates a POST endpoint description start.
func Post(path string) *EndpointBuilder { return endpoint(http.MethodPost, path) }

// Put 创建 PUT endpoint 描述起点。
// Put creates a PUT endpoint description start.
func Put(path string) *EndpointBuilder { return endpoint(http.MethodPut, path) }

// Patch 创建 PATCH endpoint 描述起点。
// Patch creates a PATCH endpoint description start.
func Patch(path string) *EndpointBuilder { return endpoint(http.MethodPatch, path) }

// Delete 创建 DELETE endpoint 描述起点。
// Delete creates a DELETE endpoint description start.
func Delete(path string) *EndpointBuilder { return endpoint(http.MethodDelete, path) }

// Head 创建 HEAD endpoint 描述起点。
// Head creates a HEAD endpoint description start.
func Head(path string) *EndpointBuilder { return endpoint(http.MethodHead, path) }

// Options 创建 OPTIONS endpoint 描述起点。
// Options creates an OPTIONS endpoint description start.
func Options(path string) *EndpointBuilder { return endpoint(http.MethodOptions, path) }

// Connect 创建 CONNECT endpoint 描述起点。
// Connect creates a CONNECT endpoint description start.
func Connect(path string) *EndpointBuilder { return endpoint(http.MethodConnect, path) }

// Trace 创建 TRACE endpoint 描述起点。
// Trace creates a TRACE endpoint description start.
func Trace(path string) *EndpointBuilder { return endpoint(http.MethodTrace, path) }

// Endpoint 创建指定 HTTP 方法和路径的 endpoint 描述起点。
// Endpoint creates an endpoint description start for the given HTTP method and path.
func Endpoint(method, path string) *EndpointBuilder { return endpoint(method, path) }

func endpoint(method, path string) *EndpointBuilder {
	return &EndpointBuilder{method: method, path: path}
}

// Output 描述类型 T 如何写为 HTTP 响应。
// Output describes how a value of type T is written as an HTTP response.
type Output[T any] interface {
	ContentType() string
	StatusCode() int
	WriteBody(io.Writer, T) error
}

type outputPreparer interface {
	prepare(any) ([]byte, bool, error)
}

type outputValuePreflight[T any] interface {
	preflightValue(T) error
}

type outputWriterPreflight interface {
	preflightWriter(io.Writer) error
}

type preparedOutputWriter interface {
	writePrepared(io.Writer, []byte) error
}

type outputHeaderWriter interface {
	writeHeaders(http.Header)
}

type outputValueHeaderWriter[T any] interface {
	writeValueHeaders(http.Header, T) error
}

type outputHeaderMetadataProvider interface {
	responseHeaders() []responseHeader
}

type outputSchemaProvider interface {
	responseSchema() any
}

type outputContentTypesProvider interface {
	responseContentTypes() []string
}

type contextualOutput[T any] interface {
	writeResponse(http.ResponseWriter, *http.Request, *Server, ErrorWriter, T) error
}

type outputValueCapabilities struct {
	status  bool
	header  bool
	cookies bool
}

func compileOutputValueCapabilities[T any]() outputValueCapabilities {
	outputType := reflect.TypeFor[T]()
	return outputValueCapabilities{
		status:  outputType.Implements(reflect.TypeFor[StatusCoder]()),
		header:  outputType.Implements(reflect.TypeFor[ResponseHeaderWriter]()),
		cookies: outputType.Implements(reflect.TypeFor[CookieWriter]()),
	}
}

type envelopeCompatibleOutput interface {
	envelopeCompatible()
}

type wrappedOutput interface {
	innerOutput() any
}

type outputSetupErrorProvider interface {
	outputSetupError() error
}

func operationOutputSetupError(output any) error {
	if output == nil {
		return ErrOperationOutputNil
	}
	value := reflect.ValueOf(output)
	if (value.Kind() == reflect.Func || value.Kind() == reflect.Map || value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface || value.Kind() == reflect.Slice) && value.IsNil() {
		return ErrOperationOutputNil
	}
	if provider, ok := output.(outputSetupErrorProvider); ok {
		return provider.outputSetupError()
	}
	return nil
}

type jsonOutput[T any] struct{}

func (jsonOutput[T]) ContentType() string { return MIMEJSON }
func (jsonOutput[T]) StatusCode() int     { return http.StatusOK }
func (jsonOutput[T]) WriteBody(writer io.Writer, value T) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(body)
	return err
}
func (jsonOutput[T]) prepare(value any) ([]byte, bool, error) {
	body, err := json.Marshal(value)
	return body, true, err
}
func (jsonOutput[T]) writePrepared(writer io.Writer, body []byte) error {
	_, err := writer.Write(body)
	return err
}
func (jsonOutput[T]) responseSchema() any {
	return generateSchema(reflect.TypeFor[T]())
}
func (jsonOutput[T]) envelopeCompatible() {}

// JSONOutput 创建 JSON 200 输出契约。
// JSONOutput creates a JSON output contract with status 200.
func JSONOutput[T any]() Output[T] { return jsonOutput[T]{} }

type renderOutput[T any] struct{}

func (renderOutput[T]) ContentType() string { return MIMEJSON }
func (renderOutput[T]) StatusCode() int     { return http.StatusOK }
func (renderOutput[T]) WriteBody(io.Writer, Render[T]) error {
	return ErrRouteProducesUnsupported
}
func (renderOutput[T]) responseSchema() any { return generateSchema(reflect.TypeFor[T]()) }
func (renderOutput[T]) responseContentTypes() []string {
	return []string{MIMEJSON, MIMEXML}
}

func (renderOutput[T]) writeResponse(writer http.ResponseWriter, request *http.Request, server *Server, errorWriter ErrorWriter, value Render[T]) error {
	data, format, rawContentType := value.unwrapRender()
	status := http.StatusOK
	if statusCoder, ok := data.(StatusCoder); ok && statusCoder.StatusCode() != 0 {
		status = statusCoder.StatusCode()
	}
	if status < http.StatusContinue || status > 599 {
		return fmt.Errorf("invalid response status %d", status)
	}
	if headerWriter, ok := data.(ResponseHeaderWriter); ok {
		headerWriter.WriteResponseHeaders(writer.Header())
	}
	writeResponseCookies(writer, data)

	contentType := rawContentType
	var codec Codec
	if format == renderRaw {
		if contentType == "" {
			contentType = MIMEPlain
		}
	} else {
		candidates := []string{MIMEJSON}
		if format == renderXML {
			candidates = []string{MIMEXML}
		} else if format == renderAuto && server != nil && len(server.produces) > 0 {
			candidates = append([]string(nil), server.produces...)
		}
		contentType, codec, _ = selectResponseCodec(server, request.Header.Get("Accept"), candidates, nil)
		if codec == nil {
			if server != nil && server.config.lenientContentNegotiation {
				contentType = candidates[0]
				codec, _ = server.codecMgr.Resolve(contentType)
			} else {
				writeRouteError(writer, request, server, errorWriter, candidates, http.StatusNotAcceptable, Err(http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable)))
				return nil
			}
		}
	}
	if !responseHasBody(status) {
		writer.WriteHeader(status)
		return nil
	}
	if server != nil && server.envelope != nil && format != renderRaw {
		server.envelope(writer, request, status, data, nil, contentType, codec)
		return nil
	}
	var body bytes.Buffer
	if format == renderRaw {
		body.Write(data.([]byte))
	} else if err := codec.Marshal(&body, data); err != nil {
		return err
	}
	writer.Header()["Content-Type"] = []string{contentType}
	writer.WriteHeader(status)
	if _, err := writer.Write(body.Bytes()); err != nil {
		return &committedOperationOutputError{err: err}
	}
	return nil
}

// RenderOutput 支持单个响应在 JSON、XML 与原始字节之间显式选择格式。
// RenderOutput supports explicit per-response selection among JSON, XML and raw bytes.
func RenderOutput[T any]() Output[Render[T]] { return renderOutput[T]{} }

type codecOutput[T any] struct {
	contentTypes []string
}

func (o codecOutput[T]) ContentType() string {
	if len(o.contentTypes) == 0 {
		return ""
	}
	return o.contentTypes[0]
}

func (codecOutput[T]) StatusCode() int { return http.StatusOK }

func (codecOutput[T]) WriteBody(io.Writer, T) error {
	return ErrRouteProducesUnsupported
}

func (codecOutput[T]) responseSchema() any { return generateSchema(reflect.TypeFor[T]()) }

func (o codecOutput[T]) responseContentTypes() []string {
	return append([]string(nil), o.contentTypes...)
}

func (o codecOutput[T]) writeResponse(writer http.ResponseWriter, request *http.Request, server *Server, errorWriter ErrorWriter, value T) error {
	codecs := resolveResponseCodecs(server, o.contentTypes)
	if len(codecs) == 0 {
		return ErrRouteProducesUnsupported
	}
	contentType, codec, ok := selectResponseCodec(server, request.Header.Get("Accept"), o.contentTypes, codecs)
	if !ok {
		if server != nil && server.config.lenientContentNegotiation {
			contentType, codec = codecs[0].contentType, codecs[0].codec
		} else {
			writeRouteError(writer, request, server, errorWriter, o.contentTypes, http.StatusNotAcceptable, Err(http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable)))
			return nil
		}
	}
	if server != nil && server.envelope != nil {
		server.envelope(writer, request, http.StatusOK, value, nil, contentType, codec)
		return nil
	}
	var body bytes.Buffer
	if err := codec.Marshal(&body, value); err != nil {
		return err
	}
	writer.Header()["Content-Type"] = []string{contentType}
	writer.WriteHeader(http.StatusOK)
	if _, err := writer.Write(body.Bytes()); err != nil {
		return &committedOperationOutputError{err: err}
	}
	return nil
}

// CodecOutput 使用 Server 已注册 Codec 按 Accept 协商响应格式。
// CodecOutput negotiates the response format through Server-registered Codecs and Accept.
func CodecOutput[T any](contentTypes ...string) Output[T] {
	return codecOutput[T]{contentTypes: normalizeContentTypes(contentTypes)}
}

type textOutput struct{}

func (textOutput) ContentType() string { return MIMEPlain }
func (textOutput) StatusCode() int     { return http.StatusOK }
func (textOutput) WriteBody(writer io.Writer, value string) error {
	_, err := io.WriteString(writer, value)
	return err
}
func (textOutput) responseSchema() any { return map[string]any{"type": "string"} }

// TextOutput 创建 text/plain 200 输出契约。
// TextOutput creates a text/plain output contract with status 200.
func TextOutput() Output[string] { return textOutput{} }

type statusOutput[T any] struct {
	status int
	inner  Output[T]
}

func (o statusOutput[T]) ContentType() string { return o.inner.ContentType() }
func (o statusOutput[T]) StatusCode() int     { return o.status }
func (o statusOutput[T]) WriteBody(writer io.Writer, value T) error {
	return o.inner.WriteBody(writer, value)
}
func (o statusOutput[T]) prepare(value any) ([]byte, bool, error) {
	if preparer, ok := any(o.inner).(outputPreparer); ok {
		return preparer.prepare(value)
	}
	return nil, false, nil
}
func (o statusOutput[T]) writePrepared(writer io.Writer, body []byte) error {
	if prepared, ok := any(o.inner).(preparedOutputWriter); ok {
		return prepared.writePrepared(writer, body)
	}
	return nil
}
func (o statusOutput[T]) preflightValue(value T) error {
	if preflight, ok := any(o.inner).(outputValuePreflight[T]); ok {
		return preflight.preflightValue(value)
	}
	return nil
}
func (o statusOutput[T]) preflightWriter(writer io.Writer) error {
	if preflight, ok := any(o.inner).(outputWriterPreflight); ok {
		return preflight.preflightWriter(writer)
	}
	return nil
}
func (o statusOutput[T]) writeHeaders(header http.Header) {
	if headers, ok := any(o.inner).(outputHeaderWriter); ok {
		headers.writeHeaders(header)
	}
}
func (o statusOutput[T]) writeValueHeaders(header http.Header, value T) error {
	if writer, ok := any(o.inner).(outputValueHeaderWriter[T]); ok {
		return writer.writeValueHeaders(header, value)
	}
	return nil
}
func (o statusOutput[T]) responseSchema() any {
	if schema, ok := any(o.inner).(outputSchemaProvider); ok {
		return schema.responseSchema()
	}
	return nil
}
func (o statusOutput[T]) responseHeaders() []responseHeader {
	if headers, ok := any(o.inner).(outputHeaderMetadataProvider); ok {
		return append([]responseHeader(nil), headers.responseHeaders()...)
	}
	return nil
}
func (o statusOutput[T]) innerOutput() any { return o.inner }
func (o statusOutput[T]) outputSetupError() error {
	return operationOutputSetupError(o.inner)
}

// WithStatus 返回覆盖成功状态码的输出契约。
// WithStatus returns an output contract with an overridden success status.
func WithStatus[T any](status int, output Output[T]) Output[T] {
	return statusOutput[T]{status: status, inner: output}
}

type headerOutput[T any] struct {
	name  string
	value string
	inner Output[T]
}

func (o headerOutput[T]) ContentType() string { return o.inner.ContentType() }
func (o headerOutput[T]) StatusCode() int     { return o.inner.StatusCode() }
func (o headerOutput[T]) WriteBody(writer io.Writer, value T) error {
	return o.inner.WriteBody(writer, value)
}
func (o headerOutput[T]) prepare(value any) ([]byte, bool, error) {
	if preparer, ok := any(o.inner).(outputPreparer); ok {
		return preparer.prepare(value)
	}
	return nil, false, nil
}
func (o headerOutput[T]) writePrepared(writer io.Writer, body []byte) error {
	if prepared, ok := any(o.inner).(preparedOutputWriter); ok {
		return prepared.writePrepared(writer, body)
	}
	return nil
}
func (o headerOutput[T]) preflightValue(value T) error {
	if preflight, ok := any(o.inner).(outputValuePreflight[T]); ok {
		return preflight.preflightValue(value)
	}
	return nil
}
func (o headerOutput[T]) preflightWriter(writer io.Writer) error {
	if preflight, ok := any(o.inner).(outputWriterPreflight); ok {
		return preflight.preflightWriter(writer)
	}
	return nil
}
func (o headerOutput[T]) writeHeaders(header http.Header) {
	if headers, ok := any(o.inner).(outputHeaderWriter); ok {
		headers.writeHeaders(header)
	}
	header.Set(o.name, o.value)
}
func (o headerOutput[T]) writeValueHeaders(header http.Header, value T) error {
	if writer, ok := any(o.inner).(outputValueHeaderWriter[T]); ok {
		return writer.writeValueHeaders(header, value)
	}
	return nil
}
func (o headerOutput[T]) responseSchema() any {
	if schema, ok := any(o.inner).(outputSchemaProvider); ok {
		return schema.responseSchema()
	}
	return nil
}
func (o headerOutput[T]) responseHeaders() []responseHeader {
	var headers []responseHeader
	if provider, ok := any(o.inner).(outputHeaderMetadataProvider); ok {
		headers = append(headers, provider.responseHeaders()...)
	}
	return append(headers, responseHeader{name: o.name, value: o.value, description: "Fixed response header"})
}
func (o headerOutput[T]) innerOutput() any { return o.inner }
func (o headerOutput[T]) outputSetupError() error {
	return operationOutputSetupError(o.inner)
}

// WithResponseHeader 返回声明固定响应头的输出契约。
// WithResponseHeader returns an output contract that declares one fixed response header.
func WithResponseHeader[T any](name, value string, output Output[T]) Output[T] {
	return headerOutput[T]{name: name, value: value, inner: output}
}

// HandleNoInput 编译无需读取请求输入的 Operation。
// HandleNoInput compiles an Operation that does not read request input.
func HandleNoInput[O any](builder *EndpointBuilder, output Output[O], handler func(context.Context) (O, error)) *Operation {
	if handler == nil {
		if builder == nil {
			return &Operation{setupErr: ErrEndpointBuilderNil}
		}
		return &Operation{method: builder.method, path: builder.path, setupErr: ErrOperationHandlerNil}
	}
	return Handle(builder, NoInput(), output, func(ctx context.Context, _ EmptyInput) (O, error) {
		return handler(ctx)
	})
}

// HandleNoOutput 编译成功时不写响应体的 Operation。
// HandleNoOutput compiles an Operation that writes no response body on success.
func HandleNoOutput[I any](builder *EndpointBuilder, input Input[I], handler func(context.Context, I) error) *Operation {
	if handler == nil {
		if builder == nil {
			return &Operation{setupErr: ErrEndpointBuilderNil}
		}
		return &Operation{method: builder.method, path: builder.path, setupErr: ErrOperationHandlerNil}
	}
	return Handle(builder, input, NoContentOutput[I](), func(ctx context.Context, value I) (I, error) {
		return value, handler(ctx, value)
	})
}

func compileOperation[I, O any](builder *EndpointBuilder, input Input[I], output Output[O], handler func(context.Context, I) (O, error)) *Operation {
	if builder == nil {
		return &Operation{setupErr: ErrEndpointBuilderNil}
	}
	if input == nil {
		return &Operation{method: builder.method, path: builder.path, setupErr: ErrOperationInputNil}
	}
	if handler == nil {
		return &Operation{method: builder.method, path: builder.path, setupErr: ErrOperationHandlerNil}
	}
	if err := operationOutputSetupError(output); err != nil {
		return &Operation{method: builder.method, path: builder.path, setupErr: err}
	}
	status := output.StatusCode()
	if status < http.StatusContinue || status > 599 {
		return &Operation{method: builder.method, path: builder.path, setupErr: fmt.Errorf("%w: %d", ErrOperationStatusInvalid, status)}
	}
	operation := &Operation{
		method:           builder.method,
		path:             builder.path,
		terminal:         routeTerminalTyped,
		stateIndependent: supportsDirectOperationInput(input),
		requestOnly:      isRequestOnlyInput(input),
		status:           status,
	}
	if contentType := normalizeContentType(output.ContentType()); contentType != "" {
		operation.produces = []string{contentType}
	}
	if contentTypes, ok := any(output).(outputContentTypesProvider); ok {
		operation.produces = normalizeContentTypes(contentTypes.responseContentTypes())
	}
	if headers, ok := any(output).(outputHeaderMetadataProvider); ok {
		operation.headers = append([]responseHeader(nil), headers.responseHeaders()...)
	}
	inputMetadata, inputErr := metadataOfInput(input)
	operation.openAPI = compileInputMetadata(inputMetadata)
	operation.setupErr = inputErr
	if inputMetadata.RequestBody != nil {
		for contentType := range inputMetadata.RequestBody.Content {
			operation.consumes = append(operation.consumes, normalizeContentType(contentType))
		}
	}
	if schema, ok := any(output).(outputSchemaProvider); ok {
		operation.openAPI.responseSchema = schema.responseSchema()
	}
	if responses, ok := any(output).(outputResponsesProvider); ok {
		operation.openAPI.responses = responses.documentedResponses()
	}
	plan := compileOutputPlan(output)
	// 无请求输入的通用 Operation 直接编译为 HTTP handler，避开旧适配链。
	// Compile request-independent generic operations directly to HTTP handlers,
	// avoiding the legacy adapter chain while preserving their semantics.
	if directInput, ok := input.(requestIndependentInputBuilder[I]); ok {
		operation.fastBuild = func(server *Server, mounted *Operation) http.HandlerFunc {
			if mounted == nil {
				mounted = operation
			}
			return func(writer http.ResponseWriter, request *http.Request) {
				value, err := directInput.buildRequestIndependentValue()
				if err != nil {
					writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
					return
				}
				if !validateOperationInput(writer, request, server, mounted, value) {
					return
				}
				response, err := handler(request.Context(), value)
				if err != nil {
					writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
					return
				}
				handleOperationOutputError(writer, request, server, mounted, writePlannedOutput(writer, request, server, mounted.errorWriter, response, plan))
			}
		}
		// JSONOutput/TextOutput are fully known at registration time. Avoid the
		// generic Output capability and negotiation checks on their hot path.
		if _, specialized := any(output).(jsonOutput[O]); specialized {
			operation.fastBuild = func(server *Server, mounted *Operation) http.HandlerFunc {
				if mounted == nil {
					mounted = operation
				}
				return func(writer http.ResponseWriter, request *http.Request) {
					value, err := directInput.buildRequestIndependentValue()
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
						return
					}
					if !validateOperationInput(writer, request, server, mounted, value) {
						return
					}
					response, err := handler(request.Context(), value)
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
						return
					}
					// 输出值实现了状态/响应头/Cookie 钩子时必须走通用计划路径,
					// 直编路径硬编码 200 会丢失这些语义。
					// dynamic output hooks (status/header/cookies) require the
					// generic plan path; the inline path hardcodes 200 and would
					// silently drop them.
					if plan.valueCaps.status || plan.valueCaps.header || plan.valueCaps.cookies {
						handleOperationOutputError(writer, request, server, mounted, writePlannedOutput(writer, request, server, mounted.errorWriter, response, plan))
						return
					}
					if server.envelope != nil {
						codec, _ := server.codecMgr.Resolve(MIMEJSON)
						server.envelope(writer, request, http.StatusOK, response, nil, MIMEJSON, codec)
						return
					}
					body, err := json.Marshal(response)
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
						return
					}
					writer.Header()["Content-Type"] = []string{MIMEJSON}
					writer.WriteHeader(http.StatusOK)
					_, _ = writer.Write(body)
				}
			}
		} else if _, specialized := any(output).(textOutput); specialized {
			operation.fastBuild = func(server *Server, mounted *Operation) http.HandlerFunc {
				if mounted == nil {
					mounted = operation
				}
				return func(writer http.ResponseWriter, request *http.Request) {
					value, err := directInput.buildRequestIndependentValue()
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
						return
					}
					if !validateOperationInput(writer, request, server, mounted, value) {
						return
					}
					response, err := handler(request.Context(), value)
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
						return
					}
					writer.Header()["Content-Type"] = []string{MIMEPlain}
					writer.WriteHeader(http.StatusOK)
					_, _ = io.WriteString(writer, any(response).(string))
				}
			}
		}
	}
	operation.build = func(server *Server, mounted *Operation) http.Handler {
		if mounted.stateIndependent {
			if directInput, ok := input.(requestIndependentInputBuilder[I]); ok {
				return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, _ pathParamList) {
					value, err := directInput.buildRequestIndependentValue()
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
						return
					}
					if !validateOperationInput(writer, request, server, mounted, value) {
						return
					}
					response, err := handler(request.Context(), value)
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
						return
					}
					handleOperationOutputError(writer, request, server, mounted, writePlannedOutput(writer, request, server, mounted.errorWriter, response, plan))
				})
			}
			if directInput, ok := input.(directPathInputBuilder[I]); ok {
				if _, specialized := any(output).(jsonOutput[O]); specialized {
					return directPathValueHandlerFunc(func(writer http.ResponseWriter, request *http.Request, rawValue string) {
						value, err := directInput.buildDirectPathValue(rawValue)
						if err != nil {
							writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
							return
						}
						if !validateOperationInput(writer, request, server, mounted, value) {
							return
						}
						response, err := handler(request.Context(), value)
						if err != nil {
							writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
							return
						}
						if server.envelope != nil {
							codec, _ := server.codecMgr.Resolve(MIMEJSON)
							server.envelope(writer, request, http.StatusOK, response, nil, MIMEJSON, codec)
							return
						}
						body, err := json.Marshal(response)
						if err != nil {
							writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
							return
						}
						writer.Header()["Content-Type"] = []string{MIMEJSON}
						writer.WriteHeader(http.StatusOK)
						_, _ = writer.Write(body)
					})
				}
				if _, specialized := any(output).(textOutput); specialized {
					return directPathValueHandlerFunc(func(writer http.ResponseWriter, request *http.Request, rawValue string) {
						value, err := directInput.buildDirectPathValue(rawValue)
						if err != nil {
							writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
							return
						}
						if !validateOperationInput(writer, request, server, mounted, value) {
							return
						}
						response, err := handler(request.Context(), value)
						if err != nil {
							writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
							return
						}
						writer.Header()["Content-Type"] = []string{MIMEPlain}
						writer.WriteHeader(http.StatusOK)
						_, _ = io.WriteString(writer, any(response).(string))
					})
				}
				return directPathValueHandlerFunc(func(writer http.ResponseWriter, request *http.Request, rawValue string) {
					value, err := directInput.buildDirectPathValue(rawValue)
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
						return
					}
					if !validateOperationInput(writer, request, server, mounted, value) {
						return
					}
					response, err := handler(request.Context(), value)
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
						return
					}
					handleOperationOutputError(writer, request, server, mounted, writePlannedOutput(writer, request, server, mounted.errorWriter, response, plan))
				})
			}
		}
		// JSON/Text 输出在注册期直编,与快捷 Operation(GetJSON/GetText)的编译
		// 路径一致:json.Marshal/io.WriteString 直接写,跳过 outputPlan 的通用
		// 执行与协商检查。两个入口从此共享同一编译产物,不再有性能分叉。
		// JSON and Text outputs compile directly at registration time, sharing
		// the fast Operation (GetJSON/GetText) code shape: json.Marshal and
		// io.WriteString write straight through, skipping outputPlan's generic
		// execution and negotiation checks. Both entry points now share one
		// compiled shape with no performance fork.
		if _, specialized := any(output).(jsonOutput[O]); specialized {
			return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, params pathParamList) {
				value, err := buildOperationInputValue(writer, request, params, server, mounted, input)
				if err != nil {
					writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
					return
				}
				if !validateOperationInput(writer, request, server, mounted, value) {
					return
				}
				response, err := handler(request.Context(), value)
				if err != nil {
					writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
					return
				}
				if err := writeDirectJSON(writer, request, server, mounted, response, output.StatusCode(), plan.valueCaps); err != nil {
					writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
				}
			})
		}
		if _, specialized := any(output).(textOutput); specialized {
			return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, params pathParamList) {
				value, err := buildOperationInputValue(writer, request, params, server, mounted, input)
				if err != nil {
					writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
					return
				}
				if !validateOperationInput(writer, request, server, mounted, value) {
					return
				}
				response, err := handler(request.Context(), value)
				if err != nil {
					writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
					return
				}
				writer.Header()["Content-Type"] = []string{MIMEPlain}
				writer.WriteHeader(output.StatusCode())
				_, _ = io.WriteString(writer, any(response).(string))
			})
		}
		return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, params pathParamList) {
			value, err := buildOperationInputValue(writer, request, params, server, mounted, input)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
				return
			}
			if !validateOperationInput(writer, request, server, mounted, value) {
				return
			}
			response, err := handler(request.Context(), value)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
				return
			}
			handleOperationOutputError(writer, request, server, mounted, writePlannedOutput(writer, request, server, mounted.errorWriter, response, plan))
		})
	}
	return operation
}

// buildOperationInputValue 构建输入值:stateIndependent 输入走免堆直编路径
// (不构造 operationRequest,避免按值携带参数列表导致堆逃逸);其余输入走
// operationRequest 通用路径。
// buildOperationInputValue builds the input value: stateIndependent inputs take
// the heap-free direct path (no operationRequest, avoiding the by-value param
// list escape); the rest use the general operationRequest path.
func buildOperationInputValue[I any](writer http.ResponseWriter, request *http.Request, params pathParamList, server *Server, mounted *Operation, input Input[I]) (I, error) {
	if stateDirect, ok := input.(stateDirectInputBuilder[I]); ok && stateDirect.stateDirectInput() {
		return stateDirect.buildStateDirect(request, params)
	}
	operationRequest := newOperationRequest(writer, request, params, server, mounted)
	return input.build(&operationRequest)
}

// writeDirectJSON 是 JSON 输出的直编写路径:值能力(状态码/响应头/Cookie)
// 在注册期探测后按需断言,协商检查与 envelope 语义与 plan 路径一致。
// writeDirectJSON is the direct JSON write path: value capabilities (status,
// headers, cookies) assert only when the registration-time probe hit, and the
// negotiation and envelope semantics match the plan path.
func writeDirectJSON[O any](writer http.ResponseWriter, request *http.Request, server *Server, mounted *Operation, response O, defaultStatus int, valueCaps outputValueCapabilities) error {
	status := defaultStatus
	if valueCaps.status || valueCaps.header || valueCaps.cookies {
		dynamicValue := any(response)
		if valueCaps.status {
			if statusCoder := dynamicValue.(StatusCoder); statusCoder.StatusCode() != 0 {
				status = statusCoder.StatusCode()
			}
		}
		if valueCaps.header {
			dynamicValue.(ResponseHeaderWriter).WriteResponseHeaders(writer.Header())
		}
		if valueCaps.cookies {
			writeResponseCookies(writer, dynamicValue)
		}
	}
	// 显式输出契约不协商:GetJSON/JSONOutput 已声明响应为 JSON,Accept 头
	// 不影响输出格式。CodecOutput 是唯一按 Accept 协商的契约。
	// explicit output contracts do not negotiate: GetJSON/JSONOutput already
	// declare JSON, so the Accept header does not change the format.
	// CodecOutput remains the only Accept-negotiating contract.
	if server.envelope != nil {
		codec, _ := server.codecMgr.Resolve(MIMEJSON)
		server.envelope(writer, request, status, response, nil, MIMEJSON, codec)
		return nil
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	writer.Header()["Content-Type"] = []string{MIMEJSON}
	writer.WriteHeader(status)
	if !responseHasBody(status) {
		return nil
	}
	_, _ = writer.Write(body)
	return nil
}

func supportsDirectOperationInput[I any](input Input[I]) bool {
	if _, ok := input.(requestIndependentInputBuilder[I]); ok {
		return true
	}
	if _, ok := input.(directPathInputBuilder[I]); ok {
		return true
	}
	stateIndependent, ok := input.(stateIndependentInputBuilder)
	return ok && stateIndependent.stateIndependentInput()
}

type committedOperationOutputError struct {
	err error
}

func (e *committedOperationOutputError) Error() string { return e.err.Error() }
func (e *committedOperationOutputError) Unwrap() error { return e.err }

func handleOperationOutputError(writer http.ResponseWriter, request *http.Request, server *Server, operation *Operation, err error) {
	if err == nil {
		return
	}
	var committed *committedOperationOutputError
	if errors.As(err, &committed) {
		if server != nil && server.logger != nil {
			message := "http error after response completion"
			if operation != nil && operation.terminal == routeTerminalSSE {
				message = "sse handler error"
			}
			server.logger.ErrorContext(request.Context(), message, "error", committed.err)
		}
		return
	}
	writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusInternalServerError, err)
}

func directJSONOperationHandler[I, O any](server *Server, operation *Operation, input directPathInputBuilder[I], handler func(context.Context, I) (O, error), status int) http.Handler {
	valueCaps := compileOutputValueCapabilities[O]()
	return directPathValueHandlerFunc(func(writer http.ResponseWriter, request *http.Request, rawValue string) {
		value, err := input.buildDirectPathValue(rawValue)
		if err != nil {
			writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusBadRequest, err)
			return
		}
		if !validateOperationInput(writer, request, server, operation, value) {
			return
		}
		output, err := handler(request.Context(), value)
		if err != nil {
			writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusInternalServerError, err)
			return
		}
		if err := writeDirectJSON(writer, request, server, operation, output, status, valueCaps); err != nil {
			writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusInternalServerError, err)
		}
	})
}

func requestIndependentJSONOperationHandler[I, O any](server *Server, operation *Operation, input requestIndependentInputBuilder[I], handler func(context.Context, I) (O, error), status int) http.Handler {
	valueCaps := compileOutputValueCapabilities[O]()
	return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, _ pathParamList) {
		value, err := input.buildRequestIndependentValue()
		if err != nil {
			writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusBadRequest, err)
			return
		}
		if !validateOperationInput(writer, request, server, operation, value) {
			return
		}
		output, err := handler(request.Context(), value)
		if err != nil {
			writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusInternalServerError, err)
			return
		}
		if err := writeDirectJSON(writer, request, server, operation, output, status, valueCaps); err != nil {
			writeRouteError(writer, request, server, operation.errorWriter, operation.produces, http.StatusInternalServerError, err)
		}
	})
}

// outputPlan 是输出契约的注册期编译计划。历史 writeOperationOutput 每请求
// 做 7 次 any(output).(xxx) 能力断言,泛型只负责类型包装;plan 把能力探测
// 一次性完成,运行时按字段直调,零类型断言(值能力仅当注册期探测命中时才断言)。
// outputPlan is the registration-time compiled plan of an output contract.
// The historical writeOperationOutput ran seven any(output).(xxx) capability
// assertions per request, reducing generics to type wrapping; the plan probes
// capabilities once, and the runtime calls fields directly with zero type
// assertions (value capabilities assert only when the probe hit).
type outputPlan[T any] struct {
	status             int
	contentType        string
	valueCaps          outputValueCapabilities
	contextual         func(http.ResponseWriter, *http.Request, *Server, ErrorWriter, T) error
	preflightWriter    func(io.Writer) error
	preflightValue     func(T) error
	writeHeaders       func(http.Header)
	writeValueHeaders  func(http.Header, T) error
	prepare            func(any) ([]byte, bool, error)
	writePrepared      func(io.Writer, []byte) error
	writeBody          func(io.Writer, T) error
	envelopeCompatible bool
}

// compileOutputPlan 注册期打包输出契约的全部运行时能力。
// compileOutputPlan packs every runtime capability of an output contract at
// registration time.
func compileOutputPlan[T any](output Output[T]) outputPlan[T] {
	plan := outputPlan[T]{
		status:             output.StatusCode(),
		contentType:        output.ContentType(),
		valueCaps:          compileOutputValueCapabilities[T](),
		envelopeCompatible: isEnvelopeCompatible(output),
	}
	if contextual, ok := any(output).(contextualOutput[T]); ok {
		plan.contextual = contextual.writeResponse
	}
	if preflight, ok := any(output).(outputWriterPreflight); ok {
		plan.preflightWriter = preflight.preflightWriter
	}
	if preflight, ok := any(output).(outputValuePreflight[T]); ok {
		plan.preflightValue = preflight.preflightValue
	}
	if headers, ok := any(output).(outputHeaderWriter); ok {
		plan.writeHeaders = headers.writeHeaders
	}
	if headers, ok := any(output).(outputValueHeaderWriter[T]); ok {
		plan.writeValueHeaders = headers.writeValueHeaders
	}
	if preparer, ok := any(output).(outputPreparer); ok {
		plan.prepare = preparer.prepare
	}
	if preparedWriter, ok := any(output).(preparedOutputWriter); ok {
		plan.writePrepared = preparedWriter.writePrepared
	}
	plan.writeBody = output.WriteBody
	return plan
}

// writePlannedOutput 按注册期计划写响应,语义与历史 writeOperationOutput 一致。
// writePlannedOutput writes the response per the registration-time plan, with
// the same semantics as the historical writeOperationOutput.
func writePlannedOutput[T any](writer http.ResponseWriter, request *http.Request, server *Server, errorWriter ErrorWriter, value T, plan outputPlan[T]) error {
	status := plan.status
	if status < http.StatusContinue || status > 599 {
		return fmt.Errorf("invalid response status %d", status)
	}
	if plan.valueCaps.status || plan.valueCaps.header || plan.valueCaps.cookies {
		dynamicValue := any(value)
		if plan.valueCaps.status {
			if statusCoder := dynamicValue.(StatusCoder); statusCoder.StatusCode() != 0 {
				status = statusCoder.StatusCode()
			}
		}
		if plan.valueCaps.header {
			dynamicValue.(ResponseHeaderWriter).WriteResponseHeaders(writer.Header())
		}
		if plan.valueCaps.cookies {
			writeResponseCookies(writer, dynamicValue)
		}
	}
	if plan.contextual != nil {
		return plan.contextual(writer, request, server, errorWriter, value)
	}
	contentType := plan.contentType
	if plan.preflightWriter != nil {
		if err := plan.preflightWriter(writer); err != nil {
			return err
		}
	}
	if plan.preflightValue != nil {
		if err := plan.preflightValue(value); err != nil {
			return err
		}
	}
	if plan.writeHeaders != nil {
		plan.writeHeaders(writer.Header())
	}
	if plan.writeValueHeaders != nil {
		if err := plan.writeValueHeaders(writer.Header(), value); err != nil {
			return err
		}
	}
	if contentType != "" {
		writer.Header()["Content-Type"] = []string{contentType}
	}
	if server != nil && server.envelope != nil && plan.envelopeCompatible {
		codec, ok := server.codecMgr.Resolve(normalizeContentType(contentType))
		if ok {
			server.envelope(writer, request, status, value, nil, normalizeContentType(contentType), codec)
			return nil
		}
	}
	var prepared []byte
	preparedOK := false
	if plan.prepare != nil {
		var err error
		prepared, preparedOK, err = plan.prepare(any(value))
		if err != nil {
			return err
		}
	}
	writer.WriteHeader(status)
	if !responseHasBody(status) {
		return nil
	}
	if preparedOK && plan.writePrepared != nil {
		if err := plan.writePrepared(writer, prepared); err != nil {
			return &committedOperationOutputError{err: err}
		}
		return nil
	}
	if err := plan.writeBody(writer, value); err != nil {
		return &committedOperationOutputError{err: err}
	}
	return nil
}

func isEnvelopeCompatible(output any) bool {
	for output != nil {
		if _, ok := output.(envelopeCompatibleOutput); ok {
			return true
		}
		wrapped, ok := output.(wrappedOutput)
		if !ok {
			return false
		}
		output = wrapped.innerOutput()
	}
	return false
}

func jsonOperation[I, O any](method, path string, input Input[I], handler func(context.Context, I) (O, error), status int) *Operation {
	operation := &Operation{
		method:           method,
		path:             path,
		produces:         []string{MIMEJSON},
		terminal:         routeTerminalTyped,
		stateIndependent: supportsDirectOperationInput(input),
		status:           status,
	}
	if input == nil {
		operation.setupErr = ErrOperationInputNil
		return operation
	}
	if handler == nil {
		operation.setupErr = ErrOperationHandlerNil
		return operation
	}
	operation.openAPI.responseSchema = generateSchema(reflect.TypeFor[O]())
	inputMetadata, inputErr := metadataOfInput(input)
	compiledMetadata := compileInputMetadata(inputMetadata)
	compiledMetadata.responseSchema = operation.openAPI.responseSchema
	operation.openAPI = compiledMetadata
	operation.setupErr = inputErr
	if inputMetadata.RequestBody != nil {
		for contentType := range inputMetadata.RequestBody.Content {
			operation.consumes = append(operation.consumes, normalizeContentType(contentType))
		}
	}
	if directInput, ok := input.(requestIndependentInputBuilder[I]); ok {
		operation.fastBuild = func(server *Server, _ *Operation) http.HandlerFunc {
			return func(writer http.ResponseWriter, request *http.Request) {
				value, err := directInput.buildRequestIndependentValue()
				if err != nil {
					writeRouteError(writer, request, server, nil, operation.produces, http.StatusBadRequest, err)
					return
				}
				if !validateOperationInput(writer, request, server, operation, value) {
					return
				}
				output, err := handler(request.Context(), value)
				if err != nil {
					writeRouteError(writer, request, server, nil, operation.produces, http.StatusInternalServerError, err)
					return
				}
				if server.envelope != nil {
					codec, _ := server.codecMgr.Resolve(MIMEJSON)
					server.envelope(writer, request, status, output, nil, MIMEJSON, codec)
					return
				}
				body, err := json.Marshal(output)
				if err != nil {
					writeRouteError(writer, request, server, nil, operation.produces, http.StatusInternalServerError, err)
					return
				}
				writer.Header()["Content-Type"] = []string{MIMEJSON}
				writer.WriteHeader(status)
				_, _ = writer.Write(body)
			}
		}
	}
	operation.build = func(server *Server, mounted *Operation) http.Handler {
		if mounted.stateIndependent {
			if directInput, ok := input.(requestIndependentInputBuilder[I]); ok {
				return requestIndependentJSONOperationHandler(server, mounted, directInput, handler, status)
			}
			if directInput, ok := input.(directPathInputBuilder[I]); ok {
				return directJSONOperationHandler(server, mounted, directInput, handler, status)
			}
		}
		return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, params pathParamList) {
			value, err := buildOperationInputValue(writer, request, params, server, mounted, input)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
				return
			}
			if !validateOperationInput(writer, request, server, mounted, value) {
				return
			}
			output, err := handler(request.Context(), value)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
				return
			}
			if server.envelope != nil {
				codec, _ := server.codecMgr.Resolve(MIMEJSON)
				server.envelope(writer, request, status, output, nil, MIMEJSON, codec)
				return
			}
			body, err := json.Marshal(output)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
				return
			}
			writer.Header()["Content-Type"] = []string{MIMEJSON}
			writer.WriteHeader(status)
			_, _ = writer.Write(body)
		})
	}
	return operation
}

// GetJSON 创建返回 JSON 200 的 GET Operation。
// GetJSON creates a GET Operation that returns JSON with status 200.
func GetJSON[I, O any](path string, input Input[I], handler func(context.Context, I) (O, error)) *Operation {
	return jsonOperation(http.MethodGet, path, input, handler, http.StatusOK)
}

// PostJSON 创建返回 JSON 200 的 POST Operation。
// PostJSON creates a POST Operation that returns JSON with status 200.
func PostJSON[I, O any](path string, input Input[I], handler func(context.Context, I) (O, error)) *Operation {
	return jsonOperation(http.MethodPost, path, input, handler, http.StatusOK)
}

// PutJSON 创建返回 JSON 200 的 PUT Operation。
// PutJSON creates a PUT Operation that returns JSON with status 200.
func PutJSON[I, O any](path string, input Input[I], handler func(context.Context, I) (O, error)) *Operation {
	return jsonOperation(http.MethodPut, path, input, handler, http.StatusOK)
}

// PatchJSON 创建返回 JSON 200 的 PATCH Operation。
// PatchJSON creates a PATCH Operation that returns JSON with status 200.
func PatchJSON[I, O any](path string, input Input[I], handler func(context.Context, I) (O, error)) *Operation {
	return jsonOperation(http.MethodPatch, path, input, handler, http.StatusOK)
}

// DeleteJSON 创建返回 JSON 200 的 DELETE Operation。
// DeleteJSON creates a DELETE Operation that returns JSON with status 200.
func DeleteJSON[I, O any](path string, input Input[I], handler func(context.Context, I) (O, error)) *Operation {
	return jsonOperation(http.MethodDelete, path, input, handler, http.StatusOK)
}

// CreatedJSON 创建返回 JSON 201 的 POST Operation。
// CreatedJSON creates a POST Operation that returns JSON with status 201.
func CreatedJSON[I, O any](path string, input Input[I], handler func(context.Context, I) (O, error)) *Operation {
	return jsonOperation(http.MethodPost, path, input, handler, http.StatusCreated)
}

func textOperation[I any](method, path string, input Input[I], handler func(context.Context, I) (string, error)) *Operation {
	operation := &Operation{
		method:           method,
		path:             path,
		produces:         []string{MIMEPlain},
		terminal:         routeTerminalTyped,
		stateIndependent: supportsDirectOperationInput(input),
		status:           http.StatusOK,
	}
	if input == nil {
		operation.setupErr = ErrOperationInputNil
		return operation
	}
	if handler == nil {
		operation.setupErr = ErrOperationHandlerNil
		return operation
	}
	inputMetadata, inputErr := metadataOfInput(input)
	operation.openAPI = compileInputMetadata(inputMetadata)
	operation.openAPI.responseSchema = map[string]any{"type": "string"}
	operation.setupErr = inputErr
	if inputMetadata.RequestBody != nil {
		for contentType := range inputMetadata.RequestBody.Content {
			operation.consumes = append(operation.consumes, normalizeContentType(contentType))
		}
	}
	if directInput, ok := input.(requestIndependentInputBuilder[I]); ok {
		operation.fastBuild = func(server *Server, _ *Operation) http.HandlerFunc {
			return func(writer http.ResponseWriter, request *http.Request) {
				value, err := directInput.buildRequestIndependentValue()
				if err != nil {
					writeRouteError(writer, request, server, nil, operation.produces, http.StatusBadRequest, err)
					return
				}
				if !validateOperationInput(writer, request, server, operation, value) {
					return
				}
				output, err := handler(request.Context(), value)
				if err != nil {
					writeRouteError(writer, request, server, nil, operation.produces, http.StatusInternalServerError, err)
					return
				}
				writer.Header()["Content-Type"] = []string{MIMEPlain}
				writer.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(writer, output)
			}
		}
	}
	operation.build = func(server *Server, mounted *Operation) http.Handler {
		if mounted.stateIndependent {
			if directInput, ok := input.(requestIndependentInputBuilder[I]); ok {
				return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, _ pathParamList) {
					value, err := directInput.buildRequestIndependentValue()
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
						return
					}
					if !validateOperationInput(writer, request, server, mounted, value) {
						return
					}
					output, err := handler(request.Context(), value)
					if err != nil {
						writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
						return
					}
					writer.Header()["Content-Type"] = []string{MIMEPlain}
					writer.WriteHeader(http.StatusOK)
					_, _ = io.WriteString(writer, output)
				})
			}
		}
		return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, params pathParamList) {
			value, err := buildOperationInputValue(writer, request, params, server, mounted, input)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
				return
			}
			if !validateOperationInput(writer, request, server, mounted, value) {
				return
			}
			output, err := handler(request.Context(), value)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
				return
			}
			writer.Header()["Content-Type"] = []string{MIMEPlain}
			writer.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(writer, output)
		})
	}
	return operation
}

// GetText 创建返回 text/plain 200 的 GET Operation。
// GetText creates a GET Operation that returns text/plain with status 200.
func GetText[I any](path string, input Input[I], handler func(context.Context, I) (string, error)) *Operation {
	return textOperation(http.MethodGet, path, input, handler)
}

// PostText 创建返回 text/plain 200 的 POST Operation。
// PostText creates a POST Operation that returns text/plain with status 200.
func PostText[I any](path string, input Input[I], handler func(context.Context, I) (string, error)) *Operation {
	return textOperation(http.MethodPost, path, input, handler)
}

// PutText 创建返回 text/plain 200 的 PUT Operation。
// PutText creates a PUT Operation that returns text/plain with status 200.
func PutText[I any](path string, input Input[I], handler func(context.Context, I) (string, error)) *Operation {
	return textOperation(http.MethodPut, path, input, handler)
}

// PatchText 创建返回 text/plain 200 的 PATCH Operation。
// PatchText creates a PATCH Operation that returns text/plain with status 200.
func PatchText[I any](path string, input Input[I], handler func(context.Context, I) (string, error)) *Operation {
	return textOperation(http.MethodPatch, path, input, handler)
}

// DeleteText 创建返回 text/plain 200 的 DELETE Operation。
// DeleteText creates a DELETE Operation that returns text/plain with status 200.
func DeleteText[I any](path string, input Input[I], handler func(context.Context, I) (string, error)) *Operation {
	return textOperation(http.MethodDelete, path, input, handler)
}
