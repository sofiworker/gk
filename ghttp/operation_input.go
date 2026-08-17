package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"sync"
)

type operationRequest struct {
	request      *http.Request
	writer       http.ResponseWriter
	params       pathParamList
	server       *Server
	query        url.Values
	queryParsed  bool
	maxBodyBytes int64
}

func newOperationRequest(writer http.ResponseWriter, request *http.Request, params pathParamList, server *Server, operation *Operation) operationRequest {
	maxBodyBytes := int64(0)
	if server != nil && server.config != nil {
		maxBodyBytes = server.config.maxBodyBytes
	}
	if operation != nil && operation.maxBodyBytesSet {
		maxBodyBytes = operation.maxBodyBytes
	}
	operationRequest := operationRequest{
		request: request, writer: writer, params: params, server: server,
		maxBodyBytes: maxBodyBytes,
	}
	if state := requestStateFromRequest(request); state != nil && state.body != nil {
		state.body.setLimit(maxBodyBytes)
	}
	return operationRequest
}

func (r *operationRequest) path(name string) string {
	if value := r.params.Get(name); value != "" {
		return value
	}
	state := requestStateFromRequest(r.request)
	if state == nil || state.matched == nil {
		return ""
	}
	return state.matched.get(name, &r.params)
}

func (r *operationRequest) queryValues() url.Values {
	if !r.queryParsed {
		r.query = r.request.URL.Query()
		r.queryParsed = true
	}
	return r.query
}

// RequestView 是自定义输入构造器使用的只读请求视图。
// RequestView is the read-only request view used by custom input constructors.
type RequestView struct {
	request *operationRequest
}

// Context 返回请求取消信号。
// Context returns the request cancellation signal.
func (v RequestView) Context() context.Context { return v.request.request.Context() }

// HTTPRequest 返回底层请求，作为输入侧逃生口。
// HTTPRequest returns the underlying request as the input-side escape hatch.
func (v RequestView) HTTPRequest() *http.Request { return v.request.request }

// Path 返回已匹配路径参数的原始字符串值。
// Path returns the raw string value of a matched path parameter.
func (v RequestView) Path(name string) string { return v.request.path(name) }

// Query 返回全部查询参数；调用方不得修改返回值。
// Query returns all query parameters; callers must not mutate the returned value.
func (v RequestView) Query() url.Values { return v.request.queryValues() }

// Header 返回请求头；调用方不得修改返回值。
// Header returns request headers; callers must not mutate the returned value.
func (v RequestView) Header() http.Header { return v.request.request.Header }

// Cookie 返回指定 Cookie。
// Cookie returns the named cookie.
func (v RequestView) Cookie(name string) (*http.Cookie, error) {
	return v.request.request.Cookie(name)
}

// ClientIP 返回按 Server 信任代理策略解析的客户端 IP。
// ClientIP returns the client IP resolved through the Server trusted-proxy policy.
func (v RequestView) ClientIP() string {
	if v.request.server == nil || v.request.server.config == nil || v.request.server.config.clientIPResolver == nil {
		return defaultClientIPResolver(v.request.request)
	}
	return v.request.server.config.clientIPResolver(v.request.request)
}

// Input 描述如何从 HTTP 请求构造类型 T。
// Input describes how a value of type T is constructed from an HTTP request.
type Input[T any] interface {
	build(*operationRequest) (T, error)
}

type inputMetadataProvider interface {
	inputMetadata() InputMetadata
	inputSetupError() error
}

type inputFunc[T any] struct {
	fn       func(*operationRequest) (T, error)
	metadata InputMetadata
	setupErr error
}

type directPathInput[T any] struct {
	inputFunc[T]
	name   string
	direct func(string) (T, error)
}

func (i directPathInput[T]) directPathName() string { return i.name }
func (i directPathInput[T]) buildDirectPathValue(value string) (T, error) {
	return i.direct(value)
}

type directPathInputBuilder[T any] interface {
	directPathName() string
	buildDirectPathValue(string) (T, error)
}

type requestIndependentInputBuilder[T any] interface {
	buildRequestIndependentValue() (T, error)
}

type stateIndependentInputBuilder interface {
	stateIndependentInput() bool
}

func (f inputFunc[T]) build(request *operationRequest) (T, error) {
	var zero T
	if f.setupErr != nil {
		return zero, f.setupErr
	}
	if f.fn == nil {
		return zero, ErrOperationInputNil
	}
	return f.fn(request)
}

func (f inputFunc[T]) inputMetadata() InputMetadata { return cloneInputMetadata(f.metadata) }
func (f inputFunc[T]) inputSetupError() error       { return f.setupErr }

func metadataOfInput(input any) (InputMetadata, error) {
	provider, ok := input.(inputMetadataProvider)
	if !ok {
		return InputMetadata{}, nil
	}
	return provider.inputMetadata(), provider.inputSetupError()
}

// InputFunc 将显式构造函数适配为输入描述器。
// InputFunc adapts an explicit constructor into an input descriptor.
func InputFunc[T any](constructor func(RequestView) (T, error)) Input[T] {
	return InputFuncWithMetadata(constructor, InputMetadata{})
}

// InputFuncWithMetadata 将显式构造函数及其 OpenAPI 元数据适配为输入描述器。
// InputFuncWithMetadata adapts an explicit constructor and its OpenAPI metadata into an input descriptor.
func InputFuncWithMetadata[T any](constructor func(RequestView) (T, error), metadata InputMetadata) Input[T] {
	if constructor == nil {
		return inputFunc[T]{metadata: cloneInputMetadata(metadata), setupErr: ErrInputConstructorNil}
	}
	return inputFunc[T]{
		fn: func(request *operationRequest) (T, error) {
			return constructor(RequestView{request: request})
		},
		metadata: cloneInputMetadata(metadata),
	}
}

// EmptyInput 是无输入 Operation 的显式输入值。
// EmptyInput is the explicit input value for an Operation with no input.
type EmptyInput struct{}

type emptyInputDescriptor struct{}

func (emptyInputDescriptor) build(*operationRequest) (EmptyInput, error) {
	return EmptyInput{}, nil
}

func (emptyInputDescriptor) buildRequestIndependentValue() (EmptyInput, error) {
	return EmptyInput{}, nil
}

// NoInput 创建不读取请求参数或请求体的输入契约。
// NoInput creates an input contract that reads no request parameters or body.
func NoInput() Input[EmptyInput] {
	return emptyInputDescriptor{}
}

// HTTPRequest 创建直接返回底层 *http.Request 的输入契约。
// HTTPRequest creates an input contract that returns the underlying *http.Request.
func HTTPRequest() Input[*http.Request] {
	return inputFunc[*http.Request]{fn: func(request *operationRequest) (*http.Request, error) {
		return request.request, nil
	}}
}

type compiledStructInput[T any] struct {
	directParams bool
	directPath   bool
	bindPath     func(any, pathParamList) error
	newTarget    func() any
	finish       func(any) T
	info         *structInfo
	hasBody      bool
	release      func(any)
}

func compileStructInput[T any]() compiledStructInput[T] {
	inputType := reflect.TypeFor[T]()
	if inputType == reflect.TypeFor[Params]() {
		return compiledStructInput[T]{directParams: true}
	}
	if inputType.Kind() == reflect.Ptr && inputType.Elem().Kind() == reflect.Struct {
		elementType := inputType.Elem()
		info := getStructInfo(elementType)
		return compiledStructInput[T]{
			newTarget:  func() any { return reflect.New(elementType).Interface() },
			finish:     func(target any) T { return target.(T) },
			info:       info,
			hasBody:    info.isLazyBody || info.hasBody,
			directPath: info.pathOnly && !info.isLazyBody && !info.hasBody,
			bindPath: func(target any, params pathParamList) error {
				return bindDirectPathInput(target, info, params)
			},
		}
	}
	var info *structInfo
	if inputType.Kind() == reflect.Struct {
		info = getStructInfo(inputType)
	}
	pool := &sync.Pool{New: func() any { return new(T) }}
	return compiledStructInput[T]{
		newTarget: func() any {
			target := pool.Get()
			reflect.ValueOf(target).Elem().SetZero()
			return target
		},
		finish:  func(target any) T { return *target.(*T) },
		release: func(target any) { pool.Put(target) },
		info:    info,
		hasBody: info != nil && (info.isLazyBody || info.hasBody),
		directPath: info != nil && info.pathOnly &&
			!info.isLazyBody && !info.hasBody,
		bindPath: func(target any, params pathParamList) error {
			return bindDirectPathInput(target, info, params)
		},
	}
}

type structInputDescriptor[T any] struct {
	compiled     compiledStructInput[T]
	metadata     InputMetadata
	contentTypes []string
	setupErr     error
}

func (i *structInputDescriptor[T]) build(request *operationRequest) (T, error) {
	var zero T
	if i == nil {
		return zero, ErrOperationInputNil
	}
	if i.setupErr != nil {
		return zero, i.setupErr
	}
	compiled := i.compiled
	if compiled.directParams {
		return any(paramsFromRequestWithPathParams(request.request, request.server.config, request.params)).(T), nil
	}
	target := compiled.newTarget()
	if compiled.release != nil {
		defer compiled.release(target)
	}
	if err := validateRequestContentType(request.request, compiled.hasBody, i.contentTypes); err != nil {
		return zero, err
	}
	httpRequest := request.request
	if request.maxBodyBytes > 0 {
		httpRequest = requestWithMaxBodyBytes(request.writer, httpRequest, request.maxBodyBytes)
		if state := requestStateFromRequest(httpRequest); state != nil && state.body != nil {
			state.body.setLimit(request.maxBodyBytes)
		}
	}
	var err error
	if compiled.directPath && compiled.bindPath != nil && request.params.Len() > 0 {
		err = compiled.bindPath(target, request.params)
	} else {
		err = parseCompiledInput(httpRequest, target, request.server.config, request.server.codecMgr, request.params, compiled.info)
	}
	if err != nil {
		if isRequestBodyTooLarge(err) || errors.Is(err, ErrRequestBodyTooLarge) {
			return zero, Err(http.StatusRequestEntityTooLarge, http.StatusText(http.StatusRequestEntityTooLarge), WithCause(err))
		}
		return zero, err
	}
	return compiled.finish(target), nil
}

func (i *structInputDescriptor[T]) inputMetadata() InputMetadata {
	if i == nil {
		return InputMetadata{}
	}
	return cloneInputMetadata(i.metadata)
}

func (i *structInputDescriptor[T]) inputSetupError() error {
	if i == nil {
		return ErrOperationInputNil
	}
	return i.setupErr
}

func (i *structInputDescriptor[T]) stateIndependentInput() bool {
	return i != nil && i.compiled.directPath
}

// StructInput 从结构体 tag、嵌入 Params 与 Body 字段编译输入契约。
// StructInput compiles an input contract from struct tags, embedded Params and the Body field.
// contentTypes 为空时保持宽松解码规则，并在 OpenAPI 中按 JSON 描述请求体。
// An empty contentTypes list preserves lenient decoding and documents the request body as JSON in OpenAPI.
func StructInput[T any](contentTypes ...string) Input[T] {
	compiled := compileStructInput[T]()
	inputType := reflect.TypeFor[T]()
	setupErr := validateRequestParamsUsage[T]()
	if inputType != reflect.TypeFor[Params]() {
		baseType := inputType
		if baseType.Kind() == reflect.Ptr {
			baseType = baseType.Elem()
		}
		if baseType.Kind() != reflect.Struct {
			setupErr = errors.Join(setupErr, fmt.Errorf("struct input requires a struct type, got %s", inputType))
		}
	}
	metadata := structInputMetadata(inputType, contentTypes)
	return &structInputDescriptor[T]{
		compiled:     compiled,
		metadata:     metadata,
		contentTypes: normalizeContentTypes(contentTypes),
		setupErr:     setupErr,
	}
}

func structInputMetadata(inputType reflect.Type, contentTypes []string) InputMetadata {
	metadata := InputMetadata{}
	for _, location := range []ParameterLocation{
		ParameterLocationPath,
		ParameterLocationQuery,
		ParameterLocationHeader,
		ParameterLocationCookie,
	} {
		required := location == ParameterLocationPath
		for _, source := range extractParametersFromType(inputType, string(location), required) {
			metadata.Parameters = append(metadata.Parameters, InputParameter{
				Name:        source.Name,
				Location:    location,
				Required:    source.Required,
				Description: source.Description,
				Schema:      cloneOpenAPIValue(source.Schema),
			})
		}
	}
	bodySchema := extractBodySchema(inputType)
	if bodySchema == nil {
		return metadata
	}
	documentedTypes := normalizeContentTypes(contentTypes)
	if len(documentedTypes) == 0 {
		documentedTypes = []string{MIMEJSON}
	}
	content := make(map[string]any, len(documentedTypes))
	for _, contentType := range documentedTypes {
		content[contentType] = cloneOpenAPIValue(bodySchema)
	}
	metadata.RequestBody = &RequestBodyMetadata{Required: true, Content: content}
	return metadata
}

type validatedInput[T any] struct {
	input       Input[T]
	validator   func(context.Context, T) error
	replacement error
	setupErr    error
}

// ErrOperationValidatorNil 表示输入校验器为空。
// ErrOperationValidatorNil indicates that an input validator is nil.
var ErrOperationValidatorNil = errors.New("operation input validator is nil")

func (i validatedInput[T]) build(request *operationRequest) (T, error) {
	var zero T
	if i.setupErr != nil {
		return zero, i.setupErr
	}
	value, err := i.input.build(request)
	if err != nil {
		return zero, err
	}
	if err := i.validator(request.request.Context(), value); err != nil {
		mapped := mappedValidationError(i.replacement, err)
		if AsError(mapped) != nil {
			return zero, mapped
		}
		return zero, Err(http.StatusUnprocessableEntity, http.StatusText(http.StatusUnprocessableEntity), WithCause(mapped))
	}
	return value, nil
}

func (i validatedInput[T]) inputMetadata() InputMetadata {
	metadata, _ := metadataOfInput(i.input)
	return metadata
}

func (i validatedInput[T]) inputSetupError() error { return i.setupErr }

func (i validatedInput[T]) stateIndependentInput() bool {
	stateIndependent, ok := i.input.(stateIndependentInputBuilder)
	return ok && stateIndependent.stateIndependentInput()
}

// ValidatedInput 在输入构造成功后执行路由级校验。
// ValidatedInput runs endpoint-level validation after input construction succeeds.
func ValidatedInput[T any](input Input[T], validator func(context.Context, T) error, options ...ValidateOption) Input[T] {
	if input == nil {
		return validatedInput[T]{setupErr: ErrOperationInputNil}
	}
	if validator == nil {
		return validatedInput[T]{setupErr: ErrOperationValidatorNil}
	}
	var config validateOptions
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	_, setupErr := metadataOfInput(input)
	return validatedInput[T]{input: input, validator: validator, replacement: config.err, setupErr: setupErr}
}

// NumberConstraint 同时约束数值请求参数并补充 OpenAPI schema。
// NumberConstraint constrains a numeric request parameter and augments its OpenAPI schema.
type NumberConstraint struct {
	minimum *float64
	maximum *float64
}

// Minimum 声明数值下限。
// Minimum declares a numeric lower bound.
func Minimum(value float64) NumberConstraint { return NumberConstraint{minimum: &value} }

// Maximum 声明数值上限。
// Maximum declares a numeric upper bound.
func Maximum(value float64) NumberConstraint { return NumberConstraint{maximum: &value} }

// StringConstraint 同时约束字符串请求参数并补充 OpenAPI schema。
// StringConstraint constrains a string request parameter and augments its OpenAPI schema.
type StringConstraint struct {
	allowed []string
}

// AllowedValues 声明字符串允许值集合。
// AllowedValues declares the allowed set of string values.
func AllowedValues(values ...string) StringConstraint {
	return StringConstraint{allowed: append([]string(nil), values...)}
}

func constrainedNumberSchema(schema map[string]any, constraints []NumberConstraint) map[string]any {
	cloned := cloneOpenAPIValue(schema).(map[string]any)
	for _, constraint := range constraints {
		if constraint.minimum != nil {
			cloned["minimum"] = *constraint.minimum
		}
		if constraint.maximum != nil {
			cloned["maximum"] = *constraint.maximum
		}
	}
	return cloned
}

func validateNumberConstraints(name string, value float64, constraints []NumberConstraint) error {
	for _, constraint := range constraints {
		if constraint.minimum != nil && value < *constraint.minimum {
			return BadRequest(fmt.Sprintf("parameter %q is below minimum %v", name, *constraint.minimum))
		}
		if constraint.maximum != nil && value > *constraint.maximum {
			return BadRequest(fmt.Sprintf("parameter %q is above maximum %v", name, *constraint.maximum))
		}
	}
	return nil
}

func constrainedStringSchema(schema map[string]any, constraints []StringConstraint) map[string]any {
	cloned := cloneOpenAPIValue(schema).(map[string]any)
	for _, constraint := range constraints {
		if len(constraint.allowed) > 0 {
			cloned["enum"] = append([]string(nil), constraint.allowed...)
		}
	}
	return cloned
}

func validateStringConstraints(name, value string, constraints []StringConstraint) error {
	for _, constraint := range constraints {
		if len(constraint.allowed) == 0 {
			continue
		}
		matched := false
		for _, candidate := range constraint.allowed {
			if value == candidate {
				matched = true
				break
			}
		}
		if !matched {
			return BadRequest(fmt.Sprintf("parameter %q has an unsupported value", name))
		}
	}
	return nil
}

func parameterInput[T any](name string, location ParameterLocation, required bool, schema map[string]any, build func(*operationRequest) (T, error)) Input[T] {
	return inputFunc[T]{
		fn: build,
		metadata: InputMetadata{Parameters: []InputParameter{{
			Name: name, Location: location, Required: required, Schema: schema,
		}}},
	}
}

// PathString 声明一个必需的字符串路径参数。
// PathString declares one required string path parameter.
func PathString(name string, constraints ...StringConstraint) Input[string] {
	parse := func(value string) (string, error) {
		if value == "" {
			return "", BadRequest(fmt.Sprintf("path parameter %q is missing", name))
		}
		return value, validateStringConstraints(name, value, constraints)
	}
	input := parameterInput(name, ParameterLocationPath, true, constrainedStringSchema(map[string]any{"type": "string"}, constraints), func(request *operationRequest) (string, error) {
		return parse(request.path(name))
	}).(inputFunc[string])
	return directPathInput[string]{inputFunc: input, name: name, direct: parse}
}

// PathInt64 声明一个必需的 int64 路径参数。
// PathInt64 declares one required int64 path parameter.
func PathInt64(name string, constraints ...NumberConstraint) Input[int64] {
	parse := func(value string) (int64, error) {
		if value == "" {
			return 0, BadRequest(fmt.Sprintf("path parameter %q is missing", name))
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, Err(http.StatusBadRequest, fmt.Sprintf("path parameter %q is invalid", name), WithCause(err))
		}
		return parsed, validateNumberConstraints(name, float64(parsed), constraints)
	}
	input := parameterInput(name, ParameterLocationPath, true, constrainedNumberSchema(map[string]any{"type": "integer", "format": "int64"}, constraints), func(request *operationRequest) (int64, error) {
		return parse(request.path(name))
	}).(inputFunc[int64])
	return directPathInput[int64]{inputFunc: input, name: name, direct: parse}
}

// PathBool 声明一个必需的 bool 路径参数。
// PathBool declares one required bool path parameter.
func PathBool(name string) Input[bool] {
	parse := func(value string) (bool, error) {
		parsed, err := strconv.ParseBool(value)
		if value == "" || err != nil {
			return false, Err(http.StatusBadRequest, fmt.Sprintf("path parameter %q is invalid", name), WithCause(err))
		}
		return parsed, nil
	}
	input := parameterInput(name, ParameterLocationPath, true, map[string]any{"type": "boolean"}, func(request *operationRequest) (bool, error) {
		return parse(request.path(name))
	}).(inputFunc[bool])
	return directPathInput[bool]{inputFunc: input, name: name, direct: parse}
}

// PathFloat64 声明一个必需的 float64 路径参数。
// PathFloat64 declares one required float64 path parameter.
func PathFloat64(name string, constraints ...NumberConstraint) Input[float64] {
	parse := func(value string) (float64, error) {
		parsed, err := strconv.ParseFloat(value, 64)
		if value == "" || err != nil {
			return 0, Err(http.StatusBadRequest, fmt.Sprintf("path parameter %q is invalid", name), WithCause(err))
		}
		return parsed, validateNumberConstraints(name, parsed, constraints)
	}
	input := parameterInput(name, ParameterLocationPath, true, constrainedNumberSchema(map[string]any{"type": "number", "format": "double"}, constraints), func(request *operationRequest) (float64, error) {
		return parse(request.path(name))
	}).(inputFunc[float64])
	return directPathInput[float64]{inputFunc: input, name: name, direct: parse}
}

// PathRemainder 声明一个 catch-all 路径参数。
// PathRemainder declares one catch-all path parameter.
func PathRemainder(name string) Input[string] { return PathString(name) }

// QueryString 声明一个必需的字符串查询参数。
// QueryString declares one required string query parameter.
func QueryString(name string, constraints ...StringConstraint) Input[string] {
	return parameterInput(name, ParameterLocationQuery, true, constrainedStringSchema(map[string]any{"type": "string"}, constraints), func(request *operationRequest) (string, error) {
		value := request.queryValues().Get(name)
		if value == "" {
			return "", BadRequest(fmt.Sprintf("query parameter %q is missing", name))
		}
		return value, validateStringConstraints(name, value, constraints)
	})
}

// QueryInt 声明一个必需的 int 查询参数。
// QueryInt declares one required int query parameter.
func QueryInt(name string, constraints ...NumberConstraint) Input[int] {
	return parameterInput(name, ParameterLocationQuery, true, constrainedNumberSchema(map[string]any{"type": "integer"}, constraints), func(request *operationRequest) (int, error) {
		value := request.queryValues().Get(name)
		parsed, err := strconv.Atoi(value)
		if value == "" || err != nil {
			return 0, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, validateNumberConstraints(name, float64(parsed), constraints)
	})
}

// QueryIntDefault 声明一个带默认值的 int 查询参数。
// QueryIntDefault declares an int query parameter with a default value.
func QueryIntDefault(name string, defaultValue int, constraints ...NumberConstraint) Input[int] {
	return parameterInput(name, ParameterLocationQuery, false, constrainedNumberSchema(map[string]any{"type": "integer", "default": defaultValue}, constraints), func(request *operationRequest) (int, error) {
		value := request.queryValues().Get(name)
		if value == "" {
			return defaultValue, validateNumberConstraints(name, float64(defaultValue), constraints)
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, validateNumberConstraints(name, float64(parsed), constraints)
	})
}

// QueryBool 声明一个必需的 bool 查询参数。
// QueryBool declares one required bool query parameter.
func QueryBool(name string) Input[bool] {
	return parameterInput(name, ParameterLocationQuery, true, map[string]any{"type": "boolean"}, func(request *operationRequest) (bool, error) {
		value := request.queryValues().Get(name)
		parsed, err := strconv.ParseBool(value)
		if value == "" || err != nil {
			return false, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, nil
	})
}

// QueryFloat64 声明一个必需的 float64 查询参数。
// QueryFloat64 declares one required float64 query parameter.
func QueryFloat64(name string, constraints ...NumberConstraint) Input[float64] {
	return parameterInput(name, ParameterLocationQuery, true, constrainedNumberSchema(map[string]any{"type": "number", "format": "double"}, constraints), func(request *operationRequest) (float64, error) {
		value := request.queryValues().Get(name)
		parsed, err := strconv.ParseFloat(value, 64)
		if value == "" || err != nil {
			return 0, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, validateNumberConstraints(name, parsed, constraints)
	})
}

// QueryStrings 声明一个可重复的字符串查询参数。
// QueryStrings declares one repeatable string query parameter.
func QueryStrings(name string) Input[[]string] {
	return parameterInput(name, ParameterLocationQuery, false, map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, func(request *operationRequest) ([]string, error) {
		return request.queryValues()[name], nil
	})
}

// HeaderString 声明一个必需的字符串请求头。
// HeaderString declares one required string request header.
func HeaderString(name string) Input[string] {
	return parameterInput(name, ParameterLocationHeader, true, map[string]any{"type": "string"}, func(request *operationRequest) (string, error) {
		value := request.request.Header.Get(name)
		if value == "" {
			return "", BadRequest(fmt.Sprintf("header %q is missing", name))
		}
		return value, nil
	})
}

// CookieString 声明一个必需的字符串 Cookie。
// CookieString declares one required string cookie.
func CookieString(name string) Input[string] {
	return parameterInput(name, ParameterLocationCookie, true, map[string]any{"type": "string"}, func(request *operationRequest) (string, error) {
		cookie, err := request.request.Cookie(name)
		if err != nil {
			return "", BadRequest(fmt.Sprintf("cookie %q is missing", name))
		}
		return cookie.Value, nil
	})
}

// JSONBody 声明一个必需的 JSON 请求体，并按顺序执行显式校验器。
// JSONBody declares one required JSON request body and runs explicit validators in order.
func JSONBody[T any](validators ...func(T) error) Input[T] {
	return inputFunc[T]{
		fn: func(request *operationRequest) (T, error) {
			var value T
			if err := validateRequestContentType(request.request, true, []string{MIMEJSON}); err != nil {
				return value, err
			}
			body, err := readOperationBody(request)
			if err != nil {
				var maxBytesError *http.MaxBytesError
				if errors.As(err, &maxBytesError) {
					return value, Err(http.StatusRequestEntityTooLarge, http.StatusText(http.StatusRequestEntityTooLarge), WithCause(ErrRequestBodyTooLarge))
				}
				return value, Err(http.StatusBadRequest, "invalid JSON body", WithCause(err))
			}
			if err := json.Unmarshal(body, &value); err != nil {
				return value, Err(http.StatusBadRequest, "invalid JSON body", WithCause(err))
			}
			for _, validator := range validators {
				if validator == nil {
					continue
				}
				if err := validator(value); err != nil {
					return value, Err(http.StatusBadRequest, err.Error(), WithCause(err))
				}
			}
			return value, nil
		},
		metadata: InputMetadata{RequestBody: &RequestBodyMetadata{
			Required: true,
			Content:  map[string]any{MIMEJSON: generateSchema(reflect.TypeFor[T]())},
		}},
	}
}

// FormBody 声明一个 application/x-www-form-urlencoded 请求体。
// FormBody declares an application/x-www-form-urlencoded request body.
func FormBody() Input[url.Values] {
	return inputFunc[url.Values]{
		fn: func(request *operationRequest) (url.Values, error) {
			if err := validateRequestContentType(request.request, true, []string{MIMEPOSTForm}); err != nil {
				return nil, err
			}
			_, values, err := formValuesFromRequest(request.request)
			if err != nil {
				return nil, operationBodyError("invalid form body", err)
			}
			return values, nil
		},
		metadata: InputMetadata{RequestBody: &RequestBodyMetadata{
			Required: true,
			Content:  map[string]any{MIMEPOSTForm: map[string]any{"type": "object"}},
		}},
	}
}

// MultipartFile 声明一个必需的 multipart 文件字段。
// MultipartFile declares one required multipart file field.
func MultipartFile(name string, maxMemory int64) Input[*FileHeader] {
	return inputFunc[*FileHeader]{
		fn: func(request *operationRequest) (*FileHeader, error) {
			if err := validateRequestContentType(request.request, true, []string{MIMEMultipartPOSTForm}); err != nil {
				return nil, err
			}
			form, err := MultipartForm(request.request, maxMemory)
			if err != nil {
				return nil, operationBodyError("invalid multipart body", err)
			}
			files := form.File[name]
			if len(files) == 0 {
				return nil, BadRequest(fmt.Sprintf("multipart file %q is missing", name))
			}
			return &FileHeader{FileHeader: files[0]}, nil
		},
		metadata: InputMetadata{RequestBody: &RequestBodyMetadata{
			Required: true,
			Content: map[string]any{MIMEMultipartPOSTForm: map[string]any{
				"type":       "object",
				"properties": map[string]any{name: map[string]any{"type": "string", "format": "binary"}},
				"required":   []string{name},
			}},
		}},
	}
}

func operationBodyError(message string, err error) error {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) || errors.Is(err, ErrRequestBodyTooLarge) {
		return Err(http.StatusRequestEntityTooLarge, http.StatusText(http.StatusRequestEntityTooLarge), WithCause(ErrRequestBodyTooLarge))
	}
	return Err(http.StatusBadRequest, message, WithCause(err))
}

func readOperationBody(request *operationRequest) ([]byte, error) {
	if request.request == nil || request.request.Body == nil || request.request.Body == http.NoBody {
		return nil, io.EOF
	}
	limit := request.maxBodyBytes
	if state := requestStateFromRequest(request.request); state != nil && state.body != nil {
		state.body.setLimit(limit)
		return RawBody(request.request)
	}
	reader := io.Reader(request.request.Body)
	if limit > 0 {
		reader = http.MaxBytesReader(request.writer, request.request.Body, limit)
	}
	return io.ReadAll(reader)
}

// InputPair 是两个输入描述器的组合值。
// InputPair is the composed value of two input descriptors.
type InputPair[A, B any] struct {
	First  A
	Second B
}

// InputTriple 是三个输入描述器的组合值。
// InputTriple is the composed value of three input descriptors.
type InputTriple[A, B, C any] struct {
	First  A
	Second B
	Third  C
}

// CombineInputs 按顺序组合两个输入描述器。
// CombineInputs combines two input descriptors in order.
func CombineInputs[A, B any](first Input[A], second Input[B]) Input[InputPair[A, B]] {
	return MapInputs(first, second, func(firstValue A, secondValue B) InputPair[A, B] {
		return InputPair[A, B]{First: firstValue, Second: secondValue}
	})
}

// CombineInputs3 按顺序组合三个输入描述器。
// CombineInputs3 combines three input descriptors in order.
func CombineInputs3[A, B, C any](first Input[A], second Input[B], third Input[C]) Input[InputTriple[A, B, C]] {
	return MapInputs3(first, second, third, func(firstValue A, secondValue B, thirdValue C) InputTriple[A, B, C] {
		return InputTriple[A, B, C]{First: firstValue, Second: secondValue, Third: thirdValue}
	})
}

// MapInputs 按顺序构造两个输入，并映射为业务输入类型。
// MapInputs builds two inputs in order and maps them into a business input type.
func MapInputs[A, B, T any](first Input[A], second Input[B], mapFunc func(A, B) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	metadata, metadataErr := mergeInputMetadata(firstMetadata, secondMetadata)
	setupErr := errors.Join(firstErr, secondErr, metadataErr)
	if first == nil || second == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}
	return inputFunc[T]{
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil {
				return zero, ErrOperationInputNil
			}
			if mapFunc == nil {
				return zero, ErrInputMapperNil
			}
			firstValue, err := first.build(request)
			if err != nil {
				return zero, err
			}
			secondValue, err := second.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
}

// MapInputs3 按顺序构造三个输入，并映射为业务输入类型。
// MapInputs3 builds three inputs in order and maps them into a business input type.
func MapInputs3[A, B, C, T any](first Input[A], second Input[B], third Input[C], mapFunc func(A, B, C) T) Input[T] {
	firstPair := CombineInputs(first, second)
	return MapInputs(firstPair, third, func(pair InputPair[A, B], thirdValue C) T {
		return mapFunc(pair.First, pair.Second, thirdValue)
	})
}
