package ghttp

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// --- 空输入 / None ---

// none 是缺席输入来源的实现:bind 返回一个恒产出占位零值的解码闭包。
// none is the implementation of an absent input source: bind returns a decode
// closure that always yields the placeholder zero value.
type none[T any] struct{}

func (none[T]) bind(*endpointSpec) (func(*Request) (T, error), error) {
	return func(*Request) (T, error) { var zero T; return zero, nil }, nil
}

// NoInput 声明某一输入来源缺席,配合占位类型使用,如 NoInput[NoQuery]()。
// NoInput declares an absent input source, used with a placeholder type, e.g.
// NoInput[NoQuery]().
func NoInput[T any]() InputSource[T] { return none[T]{} }

// --- 路径参数 ---

// pathString 从路径参数 name 提取字符串。
// pathString extracts the string path parameter named name.
type pathString struct{ name string }

func (p pathString) bind(*endpointSpec) (func(*Request) (string, error), error) {
	if p.name == "" {
		return nil, ErrInvalidParam
	}
	name := p.name
	return func(req *Request) (string, error) {
		return req.Params.Get(name), nil
	}, nil
}

// PathString 声明一个字符串路径参数来源。
// PathString declares a string path-parameter source.
func PathString(name string) InputSource[string] { return pathString{name: name} }

// pathInt64 从路径参数 name 提取 int64,解析失败返回 400 级输入错误。
// pathInt64 extracts an int64 path parameter named name; a parse failure returns
// a 400-class input error.
type pathInt64 struct{ name string }

func (p pathInt64) bind(*endpointSpec) (func(*Request) (int64, error), error) {
	if p.name == "" {
		return nil, ErrInvalidParam
	}
	name := p.name
	return func(req *Request) (int64, error) {
		raw := req.Params.Get(name)
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: path %q = %q is not an int64", ErrInvalidInput, name, raw)
		}
		return v, nil
	}, nil
}

// PathInt64 声明一个 int64 路径参数来源。
// PathInt64 declares an int64 path-parameter source.
func PathInt64(name string) InputSource[int64] { return pathInt64{name: name} }

// --- 请求体 ---

// Codec 是请求体编解码器:注册期由 Body[T](codec) 选定,decode 在请求期把字节解成 T。
// 本阶段只提供 JSON;XML/Form 等随阶段 3 补齐。
// Codec is a request-body codec: chosen at registration by Body[T](codec),
// decode turns bytes into T at request time. Only JSON this stage; XML/Form land
// in stage 3.
type Codec interface {
	// decode 把请求体读入 v(v 为 *T)。
	// decode reads the request body into v (v is a *T).
	decode(req *Request, v any) error
	// contentType 返回该 codec 期望的请求 Content-Type 前缀,供阶段 3 的 415 校验与
	// OpenAPI 使用;本阶段仅记录。
	// contentType returns the expected request Content-Type prefix, used by
	// stage 3's 415 check and OpenAPI; recorded only this stage.
	contentType() string
}

// jsonCodec 用标准库 encoding/json 解码请求体。
// jsonCodec decodes the request body with the standard encoding/json.
type jsonCodec struct{}

func (jsonCodec) contentType() string { return "application/json" }

func (jsonCodec) decode(req *Request, v any) error {
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	dec := json.NewDecoder(req.Body)
	if err := dec.Decode(v); err != nil && err != io.EOF {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return nil
}

// JSONBody 返回一个 JSON 请求体 codec,供 Body[T](JSONBody()) 使用。命名为 JSONBody
// (而非 JSON)以避让输出契约的泛型构造器 JSON[O]()——Go 不允许同包内 JSON 与 JSON[T]
// 同名共存。
// JSONBody returns a JSON request-body codec for use with Body[T](JSONBody()).
// Named JSONBody (not JSON) to make room for the output contract's generic
// constructor JSON[O]() — Go forbids a same-package JSON and JSON[T] coexisting.
func JSONBody() Codec { return jsonCodec{} }

// body 从请求体解码 T,格式由 codec 决定(与 T 解耦)。
// body decodes T from the request body; the format is decided by codec
// (decoupled from T).
type body[T any] struct{ codec Codec }

func (b body[T]) bind(*endpointSpec) (func(*Request) (T, error), error) {
	if b.codec == nil {
		return nil, ErrMissingCodec
	}
	codec := b.codec
	return func(req *Request) (T, error) {
		var v T
		if err := codec.decode(req, &v); err != nil {
			return v, err
		}
		return v, nil
	}, nil
}

// Body 声明一个请求体来源,格式由 codec 决定:Body[CreateUser](JSON())。
// Body declares a request-body source; the format is decided by codec:
// Body[CreateUser](JSON()).
func Body[T any](codec Codec) InputSource[T] { return body[T]{codec: codec} }
