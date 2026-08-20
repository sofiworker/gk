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
)

type operationRequest struct {
	request      *http.Request
	writer       http.ResponseWriter
	params       pathParamList
	server       *Server
	ctx          *Ctx
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
		ctx:          ctxFromRequest(request),
		maxBodyBytes: maxBodyBytes,
	}
	// body 限制先于懒创建写入 Ctx:Body() 首次创建 memoBody 时应用。
	// the body limit is written to the Ctx before lazy creation: Body()
	// applies it when the memoBody is first created.
	if operationRequest.ctx != nil {
		operationRequest.ctx.bodyLimit = maxBodyBytes
		if operationRequest.ctx.body != nil {
			operationRequest.ctx.body.setLimit(maxBodyBytes)
		}
	}
	return operationRequest
}

func (r *operationRequest) path(name string) string {
	if value := r.params.Get(name); value != "" {
		return value
	}
	if r.ctx == nil || r.ctx.lazyParams == nil {
		return ""
	}
	return r.ctx.lazyParams.get(name, &r.params)
}

// pathParamDirect 是 stateIndependent 直编的路径参数读取:优先命中预提取的
// params,缺失时回退惰性解码(不写缓存,避免栈参数取址逃逸)。
// pathParamDirect reads a path param for the stateIndependent direct build:
// pre-extracted params hit first, with a lazy decode fallback that does not
// write a cache (avoiding stack address escape).
func pathParamDirect(request *http.Request, params pathParamList, name string) string {
	if value := params.Get(name); value != "" {
		return value
	}
	c := ctxFromRequest(request)
	if c == nil || c.lazyParams == nil {
		return ""
	}
	return c.lazyParams.getOnce(name)
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
	// stateFn 是 stateIndependent 输入的免堆直编构建:request 与 params 按值
	// 传入,不构造 operationRequest。仅 parameterInput/combinator 提供。
	// stateFn is the heap-free direct build for stateIndependent inputs: it
	// takes request and params by value without constructing operationRequest.
	// Only parameterInput and the combinators provide it.
	stateFn func(request *http.Request, params pathParamList) (T, error)
	// requestOnly 标记输入只读请求头/query/path 等非 body 数据且不依赖
	// requestState;满足条件时整条输入链可走无状态快路径。
	// requestOnly marks an input that reads only non-body request data and
	// never reads the request body/state; a fully marked chain can take the
	// stateless fast path.
	requestOnly bool
	// stateIndependent 标记输入为标量参数组合:终端直读 match 填充的
	// params,不经请求 context 读 Ctx,dispatch 免注入。
	// stateIndependent marks scalar-param compositions: the terminal reads
	// the match-filled params directly and never resolves the Ctx through the
	// request context, so dispatch skips the attach.
	stateIndependent bool
}

func (f inputFunc[T]) requestOnlyInput() bool { return f.requestOnly }

func (f inputFunc[T]) stateIndependentInput() bool { return f.stateIndependent }

// requestOnlyInput 是输入契约的可选标记接口。
// requestOnlyInput is the optional marker for request-only input contracts.
type requestOnlyInput interface {
	requestOnlyInput() bool
}

// isRequestOnlyInput 报告输入是否只读请求元数据(不读 body、不依赖状态)。
// isRequestOnlyInput reports whether the input reads only request metadata
// (no body, no requestState dependency).
func isRequestOnlyInput(input any) bool {
	marker, ok := input.(requestOnlyInput)
	return ok && marker.requestOnlyInput()
}

// isStateIndependentInput 报告输入是否完全不读 requestState。
// isStateIndependentInput reports whether the input never reads requestState.
func isStateIndependentInput(input any) bool {
	marker, ok := input.(stateIndependentInputBuilder)
	return ok && marker.stateIndependentInput()
}

// mergeSixMetadata 链式合并六个输入描述器的元数据。
// mergeSixMetadata merges six input descriptors' metadata in order.
func mergeSixMetadata(m1, m2, m3, m4, m5, m6 InputMetadata) (InputMetadata, error) {
	merged, err := mergeInputMetadata(m1, m2)
	if err == nil {
		merged, err = mergeInputMetadata(merged, m3)
	}
	if err == nil {
		merged, err = mergeInputMetadata(merged, m4)
	}
	if err == nil {
		merged, err = mergeInputMetadata(merged, m5)
	}
	if err == nil {
		merged, err = mergeInputMetadata(merged, m6)
	}
	return merged, err
}

// mergeSevenMetadata 链式合并七个输入描述器的元数据。
// mergeSevenMetadata merges seven input descriptors' metadata in order.
func mergeSevenMetadata(m1, m2, m3, m4, m5, m6, m7 InputMetadata) (InputMetadata, error) {
	merged, err := mergeSixMetadata(m1, m2, m3, m4, m5, m6)
	if err == nil {
		merged, err = mergeInputMetadata(merged, m7)
	}
	return merged, err
}

// mergeEightMetadata 链式合并八个输入描述器的元数据。
// mergeEightMetadata merges eight input descriptors' metadata in order.
func mergeEightMetadata(m1, m2, m3, m4, m5, m6, m7, m8 InputMetadata) (InputMetadata, error) {
	merged, err := mergeSevenMetadata(m1, m2, m3, m4, m5, m6, m7)
	if err == nil {
		merged, err = mergeInputMetadata(merged, m8)
	}
	return merged, err
}

// mergeNineMetadata 链式合并九个输入描述器的元数据。
// mergeNineMetadata merges nine input descriptors' metadata in order.
func mergeNineMetadata(m1, m2, m3, m4, m5, m6, m7, m8, m9 InputMetadata) (InputMetadata, error) {
	merged, err := mergeEightMetadata(m1, m2, m3, m4, m5, m6, m7, m8)
	if err == nil {
		merged, err = mergeInputMetadata(merged, m9)
	}
	return merged, err
}

// mergeTenMetadata 链式合并十个输入描述器的元数据。
// mergeTenMetadata merges ten input descriptors' metadata in order.
func mergeTenMetadata(m1, m2, m3, m4, m5, m6, m7, m8, m9, m10 InputMetadata) (InputMetadata, error) {
	merged, err := mergeNineMetadata(m1, m2, m3, m4, m5, m6, m7, m8, m9)
	if err == nil {
		merged, err = mergeInputMetadata(merged, m10)
	}
	return merged, err
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

// stateDirectInputBuilder 为 stateIndependent 输入提供免堆直编构建:直接接收
// request 与 params 值,不构造 operationRequest,避免其按值携带参数列表时的
// 堆逃逸。无 stateFn 的输入回退到 operationRequest 路径。
// stateDirectInputBuilder provides a heap-free direct build for
// stateIndependent inputs: it takes the request and params directly without
// constructing operationRequest, avoiding the heap escape of the by-value
// param list. Inputs without a stateFn fall back to the operationRequest path.
type stateDirectInputBuilder[T any] interface {
	buildStateDirect(request *http.Request, params pathParamList) (T, error)
	stateDirectInput() bool
}

func (f inputFunc[T]) stateDirectInput() bool { return f.stateFn != nil }

// directStateFn 返回注册期固化的直取函数值:组合子(stateFn 链)通过函数值
// 直调而非接口虚调用,消除每层 dispatch 并允许内联。
// directStateFn returns the frozen direct-build function value: combinators
// (stateFn chains) call it by function value instead of interface dispatch,
// removing per-layer virtual calls and enabling inlining.
func (f inputFunc[T]) directStateFn() func(*http.Request, pathParamList) (T, error) {
	return f.stateFn
}

func directStateFnOf[V any](input Input[V]) (func(*http.Request, pathParamList) (V, error), bool) {
	if f, ok := input.(stateDirectFnProvider[V]); ok {
		if fn := f.directStateFn(); fn != nil {
			return fn, true
		}
	}
	return nil, false
}

type stateDirectFnProvider[T any] interface {
	directStateFn() func(*http.Request, pathParamList) (T, error)
}

func (f inputFunc[T]) buildStateDirect(request *http.Request, params pathParamList) (T, error) {
	var zero T
	if f.setupErr != nil {
		return zero, f.setupErr
	}
	if f.stateFn == nil {
		return zero, ErrOperationInputNil
	}
	return f.stateFn(request, params)
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

type validatedInput[T any] struct {
	input       Input[T]
	validator   func(context.Context, T) error
	replacement error
	setupErr    error
}

// ErrOperationValidatorNil 表示输入校验器为空。
// ErrOperationValidatorNil indicates that an input validator is nil.
var ErrOperationValidatorNil = errors.New("operation input validator is nil")

func (i validatedInput[T]) requestOnlyInput() bool { return isRequestOnlyInput(i.input) }

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

func (i validatedInput[T]) stateDirectInput() bool {
	builder, ok := i.input.(stateDirectInputBuilder[T])
	return ok && builder.stateDirectInput()
}

func (i validatedInput[T]) buildStateDirect(request *http.Request, params pathParamList) (T, error) {
	var zero T
	if i.setupErr != nil {
		return zero, i.setupErr
	}
	builder, ok := i.input.(stateDirectInputBuilder[T])
	if !ok || !builder.stateDirectInput() {
		return zero, ErrOperationInputNil
	}
	value, err := builder.buildStateDirect(request, params)
	if err != nil {
		return zero, err
	}
	if err := i.validator(request.Context(), value); err != nil {
		mapped := mappedValidationError(i.replacement, err)
		if AsError(mapped) != nil {
			return zero, mapped
		}
		return zero, Err(http.StatusUnprocessableEntity, http.StatusText(http.StatusUnprocessableEntity), WithCause(mapped))
	}
	return value, nil
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

func parameterInput[T any](name string, location ParameterLocation, required bool, schema map[string]any, build func(*operationRequest) (T, error), stateBuild func(*http.Request, pathParamList) (T, error)) Input[T] {
	return inputFunc[T]{
		fn:               build,
		stateFn:          stateBuild,
		requestOnly:      true,
		stateIndependent: true,
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
	}, func(request *http.Request, params pathParamList) (string, error) {
		return parse(pathParamDirect(request, params, name))
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
	}, func(request *http.Request, params pathParamList) (int64, error) {
		return parse(pathParamDirect(request, params, name))
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
	}, func(request *http.Request, params pathParamList) (bool, error) {
		return parse(pathParamDirect(request, params, name))
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
	}, func(request *http.Request, params pathParamList) (float64, error) {
		return parse(pathParamDirect(request, params, name))
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
	}, func(request *http.Request, _ pathParamList) (string, error) {
		value := request.URL.Query().Get(name)
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
	}, func(request *http.Request, _ pathParamList) (int, error) {
		value := request.URL.Query().Get(name)
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
	}, func(request *http.Request, _ pathParamList) (int, error) {
		value := request.URL.Query().Get(name)
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
	}, func(request *http.Request, _ pathParamList) (bool, error) {
		value := request.URL.Query().Get(name)
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
	}, func(request *http.Request, _ pathParamList) (float64, error) {
		value := request.URL.Query().Get(name)
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
	}, func(request *http.Request, _ pathParamList) ([]string, error) {
		return request.URL.Query()[name], nil
	})
}

// QueryStringDefault 声明一个可缺省、缺失时回退默认值的字符串查询参数。
// QueryStringDefault declares an optional string query parameter falling back
// to defaultValue when absent.
func QueryStringDefault(name string, defaultValue string, constraints ...StringConstraint) Input[string] {
	return parameterInput(name, ParameterLocationQuery, false, constrainedStringSchema(map[string]any{"type": "string", "default": defaultValue}, constraints), func(request *operationRequest) (string, error) {
		value := request.queryValues().Get(name)
		if value == "" {
			return defaultValue, validateStringConstraints(name, defaultValue, constraints)
		}
		return value, validateStringConstraints(name, value, constraints)
	}, func(request *http.Request, _ pathParamList) (string, error) {
		value := request.URL.Query().Get(name)
		if value == "" {
			return defaultValue, validateStringConstraints(name, defaultValue, constraints)
		}
		return value, validateStringConstraints(name, value, constraints)
	})
}

// QueryBoolDefault 声明一个可缺省、缺失时回退默认值的 bool 查询参数。
// QueryBoolDefault declares an optional bool query parameter falling back to
// defaultValue when absent.
func QueryBoolDefault(name string, defaultValue bool) Input[bool] {
	return parameterInput(name, ParameterLocationQuery, false, map[string]any{"type": "boolean", "default": defaultValue}, func(request *operationRequest) (bool, error) {
		value := request.queryValues().Get(name)
		if value == "" {
			return defaultValue, nil
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return false, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, nil
	}, func(request *http.Request, _ pathParamList) (bool, error) {
		value := request.URL.Query().Get(name)
		if value == "" {
			return defaultValue, nil
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return false, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, nil
	})
}

// QueryFloat64Default 声明一个可缺省、缺失时回退默认值的 float64 查询参数。
// QueryFloat64Default declares an optional float64 query parameter falling
// back to defaultValue when absent.
func QueryFloat64Default(name string, defaultValue float64, constraints ...NumberConstraint) Input[float64] {
	return parameterInput(name, ParameterLocationQuery, false, constrainedNumberSchema(map[string]any{"type": "number", "format": "double", "default": defaultValue}, constraints), func(request *operationRequest) (float64, error) {
		value := request.queryValues().Get(name)
		if value == "" {
			return defaultValue, validateNumberConstraints(name, defaultValue, constraints)
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, validateNumberConstraints(name, parsed, constraints)
	}, func(request *http.Request, _ pathParamList) (float64, error) {
		value := request.URL.Query().Get(name)
		if value == "" {
			return defaultValue, validateNumberConstraints(name, defaultValue, constraints)
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, Err(http.StatusBadRequest, fmt.Sprintf("query parameter %q is invalid", name), WithCause(err))
		}
		return parsed, validateNumberConstraints(name, parsed, constraints)
	})
}

// HeaderStringDefault 声明一个可缺省、缺失时回退默认值的字符串请求头。
// HeaderStringDefault declares an optional string header falling back to
// defaultValue when absent.
func HeaderStringDefault(name string, defaultValue string, constraints ...StringConstraint) Input[string] {
	return parameterInput(name, ParameterLocationHeader, false, constrainedStringSchema(map[string]any{"type": "string", "default": defaultValue}, constraints), func(request *operationRequest) (string, error) {
		value := request.request.Header.Get(name)
		if value == "" {
			return defaultValue, validateStringConstraints(name, defaultValue, constraints)
		}
		return value, validateStringConstraints(name, value, constraints)
	}, func(request *http.Request, _ pathParamList) (string, error) {
		value := request.Header.Get(name)
		if value == "" {
			return defaultValue, validateStringConstraints(name, defaultValue, constraints)
		}
		return value, validateStringConstraints(name, value, constraints)
	})
}

// CookieStringDefault 声明一个可缺省、缺失时回退默认值的字符串 Cookie。
// CookieStringDefault declares an optional string cookie falling back to
// defaultValue when absent.
func CookieStringDefault(name string, defaultValue string, constraints ...StringConstraint) Input[string] {
	return parameterInput(name, ParameterLocationCookie, false, constrainedStringSchema(map[string]any{"type": "string", "default": defaultValue}, constraints), func(request *operationRequest) (string, error) {
		cookie, err := request.request.Cookie(name)
		if err != nil {
			return defaultValue, validateStringConstraints(name, defaultValue, constraints)
		}
		return cookie.Value, validateStringConstraints(name, cookie.Value, constraints)
	}, func(request *http.Request, _ pathParamList) (string, error) {
		cookie, err := request.Cookie(name)
		if err != nil {
			return defaultValue, validateStringConstraints(name, defaultValue, constraints)
		}
		return cookie.Value, validateStringConstraints(name, cookie.Value, constraints)
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
	}, func(request *http.Request, _ pathParamList) (string, error) {
		value := request.Header.Get(name)
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
	}, func(request *http.Request, _ pathParamList) (string, error) {
		cookie, err := request.Cookie(name)
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
	if request.ctx != nil {
		request.ctx.bodyLimit = limit
		// 优先走 Ctx.Body 的 memo 路径,可复用中间件读体结果且自带 limit。
		// prefer the Ctx.Body memo path: it shares middleware body reads
		// and carries the limit.
		if request.ctx.body != nil {
			request.ctx.body.setLimit(limit)
		}
		return request.ctx.BodyBytes()
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

// stateDirectOf 报告输入是否支持免堆直编构建。
// stateDirectOf reports whether the input supports the heap-free direct build.
func stateDirectOf[V any](input Input[V]) (stateDirectInputBuilder[V], bool) {
	builder, ok := input.(stateDirectInputBuilder[V])
	if !ok || !builder.stateDirectInput() {
		return nil, false
	}
	return builder, true
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
	result := inputFunc[T]{
		requestOnly:      isRequestOnlyInput(first) && isRequestOnlyInput(second),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second),
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
	firstFn, firstOK := directStateFnOf(first)
	secondFn, secondOK := directStateFnOf(second)
	if firstOK && secondOK {
		result.stateFn = func(request *http.Request, params pathParamList) (T, error) {
			var zero T
			if mapFunc == nil {
				return zero, ErrInputMapperNil
			}
			firstValue, err := firstFn(request, params)
			if err != nil {
				return zero, err
			}
			secondValue, err := secondFn(request, params)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue), nil
		}
	}
	return result
}

// MapInputs3 按顺序构造三个输入，并映射为业务输入类型。
// MapInputs3 builds three inputs in order and maps them into a business input type.
// 扁平实现：直接顺序构造三个输入，不经过 CombineInputs 的中间 Pair 层。
// flat implementation: builds the three inputs directly without the
// intermediate Pair layer of CombineInputs.
func MapInputs3[A, B, C, T any](first Input[A], second Input[B], third Input[C], mapFunc func(A, B, C) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	pairMetadata, pairErr := mergeInputMetadata(firstMetadata, secondMetadata)
	metadata, metadataErr := mergeInputMetadata(pairMetadata, thirdMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, pairErr, metadataErr)
	if first == nil || second == nil || third == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}
	result := inputFunc[T]{
		requestOnly:      isRequestOnlyInput(first) && isRequestOnlyInput(second) && isRequestOnlyInput(third),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) && isStateIndependentInput(third),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
	firstFn, firstOK := directStateFnOf(first)
	secondFn, secondOK := directStateFnOf(second)
	thirdFn, thirdOK := directStateFnOf(third)
	if firstOK && secondOK && thirdOK {
		result.stateFn = func(request *http.Request, params pathParamList) (T, error) {
			var zero T
			if mapFunc == nil {
				return zero, ErrInputMapperNil
			}
			firstValue, err := firstFn(request, params)
			if err != nil {
				return zero, err
			}
			secondValue, err := secondFn(request, params)
			if err != nil {
				return zero, err
			}
			thirdValue, err := thirdFn(request, params)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue), nil
		}
	}
	return result
}

// MapInputs4 按顺序构造四个输入，并映射为业务输入类型。
// MapInputs4 builds four inputs in order and maps them into a business type.
func MapInputs4[A, B, C, D, T any](first Input[A], second Input[B], third Input[C], fourth Input[D], mapFunc func(A, B, C, D) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	fourthMetadata, fourthErr := metadataOfInput(fourth)
	pairMetadata, pairErr := mergeInputMetadata(firstMetadata, secondMetadata)
	pairMetadata, pairErr2 := mergeInputMetadata(pairMetadata, thirdMetadata)
	metadata, metadataErr := mergeInputMetadata(pairMetadata, fourthMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, fourthErr, pairErr, pairErr2, metadataErr)
	if first == nil || second == nil || third == nil || fourth == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}
	result := inputFunc[T]{
		requestOnly: isRequestOnlyInput(first) && isRequestOnlyInput(second) &&
			isRequestOnlyInput(third) && isRequestOnlyInput(fourth),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) &&
			isStateIndependentInput(third) && isStateIndependentInput(fourth),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil || fourth == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourth.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
	firstFn, firstOK := directStateFnOf(first)
	secondFn, secondOK := directStateFnOf(second)
	thirdFn, thirdOK := directStateFnOf(third)
	fourthFn, fourthOK := directStateFnOf(fourth)
	if firstOK && secondOK && thirdOK && fourthOK {
		result.stateFn = func(request *http.Request, params pathParamList) (T, error) {
			var zero T
			if mapFunc == nil {
				return zero, ErrInputMapperNil
			}
			firstValue, err := firstFn(request, params)
			if err != nil {
				return zero, err
			}
			secondValue, err := secondFn(request, params)
			if err != nil {
				return zero, err
			}
			thirdValue, err := thirdFn(request, params)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourthFn(request, params)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue), nil
		}
	}
	return result
}

// MapInputs5 按顺序构造五个输入，并映射为业务输入类型。
// MapInputs5 builds five inputs in order and maps them into a business type.
func MapInputs5[A, B, C, D, E, T any](first Input[A], second Input[B], third Input[C], fourth Input[D], fifth Input[E], mapFunc func(A, B, C, D, E) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	fourthMetadata, fourthErr := metadataOfInput(fourth)
	fifthMetadata, fifthErr := metadataOfInput(fifth)
	pairMetadata, pairErr := mergeInputMetadata(firstMetadata, secondMetadata)
	pairMetadata, pairErr2 := mergeInputMetadata(pairMetadata, thirdMetadata)
	pairMetadata, pairErr3 := mergeInputMetadata(pairMetadata, fourthMetadata)
	metadata, metadataErr := mergeInputMetadata(pairMetadata, fifthMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, fourthErr, fifthErr, pairErr, pairErr2, pairErr3, metadataErr)
	if first == nil || second == nil || third == nil || fourth == nil || fifth == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}
	result := inputFunc[T]{
		requestOnly: isRequestOnlyInput(first) && isRequestOnlyInput(second) &&
			isRequestOnlyInput(third) && isRequestOnlyInput(fourth) && isRequestOnlyInput(fifth),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) &&
			isStateIndependentInput(third) && isStateIndependentInput(fourth) && isStateIndependentInput(fifth),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil || fourth == nil || fifth == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourth.build(request)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifth.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
	firstFn, firstOK := directStateFnOf(first)
	secondFn, secondOK := directStateFnOf(second)
	thirdFn, thirdOK := directStateFnOf(third)
	fourthFn, fourthOK := directStateFnOf(fourth)
	fifthFn, fifthOK := directStateFnOf(fifth)
	if firstOK && secondOK && thirdOK && fourthOK && fifthOK {
		result.stateFn = func(request *http.Request, params pathParamList) (T, error) {
			var zero T
			if mapFunc == nil {
				return zero, ErrInputMapperNil
			}
			firstValue, err := firstFn(request, params)
			if err != nil {
				return zero, err
			}
			secondValue, err := secondFn(request, params)
			if err != nil {
				return zero, err
			}
			thirdValue, err := thirdFn(request, params)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourthFn(request, params)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifthFn(request, params)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue), nil
		}
	}
	return result
}

// MapInputs6 按顺序构造六个输入，并映射为业务输入类型。
// MapInputs6 builds six inputs in order and maps them into a business type.
func MapInputs6[A, B, C, D, E, F, T any](first Input[A], second Input[B], third Input[C], fourth Input[D], fifth Input[E], sixth Input[F], mapFunc func(A, B, C, D, E, F) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	fourthMetadata, fourthErr := metadataOfInput(fourth)
	fifthMetadata, fifthErr := metadataOfInput(fifth)
	sixthMetadata, sixthErr := metadataOfInput(sixth)
	metadata, mergeErr := mergeSixMetadata(firstMetadata, secondMetadata, thirdMetadata, fourthMetadata, fifthMetadata, sixthMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, fourthErr, fifthErr, sixthErr, mergeErr)
	if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}
	result := inputFunc[T]{
		requestOnly: isRequestOnlyInput(first) && isRequestOnlyInput(second) &&
			isRequestOnlyInput(third) && isRequestOnlyInput(fourth) &&
			isRequestOnlyInput(fifth) && isRequestOnlyInput(sixth),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) &&
			isStateIndependentInput(third) && isStateIndependentInput(fourth) &&
			isStateIndependentInput(fifth) && isStateIndependentInput(sixth),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourth.build(request)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifth.build(request)
			if err != nil {
				return zero, err
			}
			sixthValue, err := sixth.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue, sixthValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
	firstFn, firstOK := directStateFnOf(first)
	secondFn, secondOK := directStateFnOf(second)
	thirdFn, thirdOK := directStateFnOf(third)
	fourthFn, fourthOK := directStateFnOf(fourth)
	fifthFn, fifthOK := directStateFnOf(fifth)
	sixthFn, sixthOK := directStateFnOf(sixth)
	if firstOK && secondOK && thirdOK && fourthOK && fifthOK && sixthOK {
		result.stateFn = func(request *http.Request, params pathParamList) (T, error) {
			var zero T
			if mapFunc == nil {
				return zero, ErrInputMapperNil
			}
			firstValue, err := firstFn(request, params)
			if err != nil {
				return zero, err
			}
			secondValue, err := secondFn(request, params)
			if err != nil {
				return zero, err
			}
			thirdValue, err := thirdFn(request, params)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourthFn(request, params)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifthFn(request, params)
			if err != nil {
				return zero, err
			}
			sixthValue, err := sixthFn(request, params)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue, sixthValue), nil
		}
	}
	return result
}

// MapInputs7 按顺序构造七个输入，并映射为业务输入类型。
// MapInputs7 builds seven inputs in order and maps them into a business type.
func MapInputs7[A, B, C, D, E, F, G, T any](first Input[A], second Input[B], third Input[C], fourth Input[D], fifth Input[E], sixth Input[F], seventh Input[G], mapFunc func(A, B, C, D, E, F, G) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	fourthMetadata, fourthErr := metadataOfInput(fourth)
	fifthMetadata, fifthErr := metadataOfInput(fifth)
	sixthMetadata, sixthErr := metadataOfInput(sixth)
	seventhMetadata, seventhErr := metadataOfInput(seventh)
	metadata, mergeErr := mergeSevenMetadata(firstMetadata, secondMetadata, thirdMetadata, fourthMetadata, fifthMetadata, sixthMetadata, seventhMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, fourthErr, fifthErr, sixthErr, seventhErr, mergeErr)
	if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}

	result := inputFunc[T]{
		requestOnly: isRequestOnlyInput(first) && isRequestOnlyInput(second) &&
			isRequestOnlyInput(third) && isRequestOnlyInput(fourth) &&
			isRequestOnlyInput(fifth) && isRequestOnlyInput(sixth) && isRequestOnlyInput(seventh),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) &&
			isStateIndependentInput(third) && isStateIndependentInput(fourth) &&
			isStateIndependentInput(fifth) && isStateIndependentInput(sixth) && isStateIndependentInput(seventh),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourth.build(request)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifth.build(request)
			if err != nil {
				return zero, err
			}
			sixthValue, err := sixth.build(request)
			if err != nil {
				return zero, err
			}
			seventhValue, err := seventh.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue, sixthValue, seventhValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
	firstFn, firstOK := directStateFnOf(first)
	secondFn, secondOK := directStateFnOf(second)
	thirdFn, thirdOK := directStateFnOf(third)
	fourthFn, fourthOK := directStateFnOf(fourth)
	fifthFn, fifthOK := directStateFnOf(fifth)
	sixthFn, sixthOK := directStateFnOf(sixth)
	seventhFn, seventhOK := directStateFnOf(seventh)
	if firstOK && secondOK && thirdOK && fourthOK && fifthOK && sixthOK && seventhOK {
		result.stateFn = func(request *http.Request, params pathParamList) (T, error) {
			var zero T
			if mapFunc == nil {
				return zero, ErrInputMapperNil
			}
			firstValue, err := firstFn(request, params)
			if err != nil {
				return zero, err
			}
			secondValue, err := secondFn(request, params)
			if err != nil {
				return zero, err
			}
			thirdValue, err := thirdFn(request, params)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourthFn(request, params)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifthFn(request, params)
			if err != nil {
				return zero, err
			}
			sixthValue, err := sixthFn(request, params)
			if err != nil {
				return zero, err
			}
			seventhValue, err := seventhFn(request, params)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue, sixthValue, seventhValue), nil
		}
	}
	return result
}

// MapInputs8 按顺序构造八个输入，并映射为业务输入类型。
// MapInputs8 builds eight inputs in order and maps them into a business type.
func MapInputs8[A, B, C, D, E, F, G, H, T any](first Input[A], second Input[B], third Input[C], fourth Input[D], fifth Input[E], sixth Input[F], seventh Input[G], eighth Input[H], mapFunc func(A, B, C, D, E, F, G, H) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	fourthMetadata, fourthErr := metadataOfInput(fourth)
	fifthMetadata, fifthErr := metadataOfInput(fifth)
	sixthMetadata, sixthErr := metadataOfInput(sixth)
	seventhMetadata, seventhErr := metadataOfInput(seventh)
	eighthMetadata, eighthErr := metadataOfInput(eighth)
	metadata, mergeErr := mergeEightMetadata(firstMetadata, secondMetadata, thirdMetadata, fourthMetadata, fifthMetadata, sixthMetadata, seventhMetadata, eighthMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, fourthErr, fifthErr, sixthErr, seventhErr, eighthErr, mergeErr)
	if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil || eighth == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}

	return inputFunc[T]{
		requestOnly: isRequestOnlyInput(first) && isRequestOnlyInput(second) &&
			isRequestOnlyInput(third) && isRequestOnlyInput(fourth) &&
			isRequestOnlyInput(fifth) && isRequestOnlyInput(sixth) &&
			isRequestOnlyInput(seventh) && isRequestOnlyInput(eighth),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) &&
			isStateIndependentInput(third) && isStateIndependentInput(fourth) &&
			isStateIndependentInput(fifth) && isStateIndependentInput(sixth) &&
			isStateIndependentInput(seventh) && isStateIndependentInput(eighth),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil || eighth == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourth.build(request)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifth.build(request)
			if err != nil {
				return zero, err
			}
			sixthValue, err := sixth.build(request)
			if err != nil {
				return zero, err
			}
			seventhValue, err := seventh.build(request)
			if err != nil {
				return zero, err
			}
			eighthValue, err := eighth.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue, sixthValue, seventhValue, eighthValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
}

// MapInputs9 按顺序构造九个输入，并映射为业务输入类型。
// MapInputs9 builds nine inputs in order and maps them into a business type.
func MapInputs9[A, B, C, D, E, F, G, H, I, T any](first Input[A], second Input[B], third Input[C], fourth Input[D], fifth Input[E], sixth Input[F], seventh Input[G], eighth Input[H], ninth Input[I], mapFunc func(A, B, C, D, E, F, G, H, I) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	fourthMetadata, fourthErr := metadataOfInput(fourth)
	fifthMetadata, fifthErr := metadataOfInput(fifth)
	sixthMetadata, sixthErr := metadataOfInput(sixth)
	seventhMetadata, seventhErr := metadataOfInput(seventh)
	eighthMetadata, eighthErr := metadataOfInput(eighth)
	ninthMetadata, ninthErr := metadataOfInput(ninth)
	metadata, mergeErr := mergeNineMetadata(firstMetadata, secondMetadata, thirdMetadata, fourthMetadata, fifthMetadata, sixthMetadata, seventhMetadata, eighthMetadata, ninthMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, fourthErr, fifthErr, sixthErr, seventhErr, eighthErr, ninthErr, mergeErr)
	if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil || eighth == nil || ninth == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}

	return inputFunc[T]{
		requestOnly: isRequestOnlyInput(first) && isRequestOnlyInput(second) &&
			isRequestOnlyInput(third) && isRequestOnlyInput(fourth) &&
			isRequestOnlyInput(fifth) && isRequestOnlyInput(sixth) &&
			isRequestOnlyInput(seventh) && isRequestOnlyInput(eighth) && isRequestOnlyInput(ninth),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) &&
			isStateIndependentInput(third) && isStateIndependentInput(fourth) &&
			isStateIndependentInput(fifth) && isStateIndependentInput(sixth) &&
			isStateIndependentInput(seventh) && isStateIndependentInput(eighth) && isStateIndependentInput(ninth),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil || eighth == nil || ninth == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourth.build(request)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifth.build(request)
			if err != nil {
				return zero, err
			}
			sixthValue, err := sixth.build(request)
			if err != nil {
				return zero, err
			}
			seventhValue, err := seventh.build(request)
			if err != nil {
				return zero, err
			}
			eighthValue, err := eighth.build(request)
			if err != nil {
				return zero, err
			}
			ninthValue, err := ninth.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue, sixthValue, seventhValue, eighthValue, ninthValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
}

// MapInputs10 按顺序构造十个输入，并映射为业务输入类型。
// MapInputs10 builds ten inputs in order and maps them into a business type.
func MapInputs10[A, B, C, D, E, F, G, H, I, J, T any](first Input[A], second Input[B], third Input[C], fourth Input[D], fifth Input[E], sixth Input[F], seventh Input[G], eighth Input[H], ninth Input[I], tenth Input[J], mapFunc func(A, B, C, D, E, F, G, H, I, J) T) Input[T] {
	firstMetadata, firstErr := metadataOfInput(first)
	secondMetadata, secondErr := metadataOfInput(second)
	thirdMetadata, thirdErr := metadataOfInput(third)
	fourthMetadata, fourthErr := metadataOfInput(fourth)
	fifthMetadata, fifthErr := metadataOfInput(fifth)
	sixthMetadata, sixthErr := metadataOfInput(sixth)
	seventhMetadata, seventhErr := metadataOfInput(seventh)
	eighthMetadata, eighthErr := metadataOfInput(eighth)
	ninthMetadata, ninthErr := metadataOfInput(ninth)
	tenthMetadata, tenthErr := metadataOfInput(tenth)
	metadata, mergeErr := mergeTenMetadata(firstMetadata, secondMetadata, thirdMetadata, fourthMetadata, fifthMetadata, sixthMetadata, seventhMetadata, eighthMetadata, ninthMetadata, tenthMetadata)
	setupErr := errors.Join(firstErr, secondErr, thirdErr, fourthErr, fifthErr, sixthErr, seventhErr, eighthErr, ninthErr, tenthErr, mergeErr)
	if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil || eighth == nil || ninth == nil || tenth == nil {
		setupErr = errors.Join(setupErr, ErrOperationInputNil)
	}
	if mapFunc == nil {
		setupErr = errors.Join(setupErr, ErrInputMapperNil)
	}

	return inputFunc[T]{
		requestOnly: isRequestOnlyInput(first) && isRequestOnlyInput(second) &&
			isRequestOnlyInput(third) && isRequestOnlyInput(fourth) &&
			isRequestOnlyInput(fifth) && isRequestOnlyInput(sixth) &&
			isRequestOnlyInput(seventh) && isRequestOnlyInput(eighth) &&
			isRequestOnlyInput(ninth) && isRequestOnlyInput(tenth),
		stateIndependent: isStateIndependentInput(first) && isStateIndependentInput(second) &&
			isStateIndependentInput(third) && isStateIndependentInput(fourth) &&
			isStateIndependentInput(fifth) && isStateIndependentInput(sixth) &&
			isStateIndependentInput(seventh) && isStateIndependentInput(eighth) &&
			isStateIndependentInput(ninth) && isStateIndependentInput(tenth),
		fn: func(request *operationRequest) (T, error) {
			var zero T
			if first == nil || second == nil || third == nil || fourth == nil || fifth == nil || sixth == nil || seventh == nil || eighth == nil || ninth == nil || tenth == nil {
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
			thirdValue, err := third.build(request)
			if err != nil {
				return zero, err
			}
			fourthValue, err := fourth.build(request)
			if err != nil {
				return zero, err
			}
			fifthValue, err := fifth.build(request)
			if err != nil {
				return zero, err
			}
			sixthValue, err := sixth.build(request)
			if err != nil {
				return zero, err
			}
			seventhValue, err := seventh.build(request)
			if err != nil {
				return zero, err
			}
			eighthValue, err := eighth.build(request)
			if err != nil {
				return zero, err
			}
			ninthValue, err := ninth.build(request)
			if err != nil {
				return zero, err
			}
			tenthValue, err := tenth.build(request)
			if err != nil {
				return zero, err
			}
			return mapFunc(firstValue, secondValue, thirdValue, fourthValue, fifthValue, sixthValue, seventhValue, eighthValue, ninthValue, tenthValue), nil
		},
		metadata: metadata,
		setupErr: setupErr,
	}
}
