package ghttp

import (
	"fmt"
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
// 编解码接口(RequestDecoder / ResponseEncoder / Codec)与内置 JSONCodec/XMLCodec
// 见 codec.go。请求体来源统一走导出的 RequestDecoder,用户可只实现该侧以替换解析库。
// The codec interfaces (RequestDecoder / ResponseEncoder / Codec) and built-in
// JSONCodec/XMLCodec live in codec.go. The body source uses the exported
// RequestDecoder; users may implement only that side to swap the parser.

// JSONBody 返回一个 JSON 请求体解码器,供 Body[T](JSONBody()) 使用。等价于 JSONCodec()
// 的解码侧,保留此名以兼容既有调用点。
// JSONBody returns a JSON request-body decoder for Body[T](JSONBody()).
// Equivalent to the decode side of JSONCodec(); kept for existing call sites.
func JSONBody() RequestDecoder { return jsonCodec{} }

// body 从请求体解码 T,格式由 RequestDecoder 决定(与 T 解耦)。
// body decodes T from the request body; the format is decided by the
// RequestDecoder (decoupled from T).
type body[T any] struct{ dec RequestDecoder }

func (b body[T]) bind(*endpointSpec) (func(*Request) (T, error), error) {
	if b.dec == nil {
		return nil, ErrMissingCodec
	}
	dec := b.dec
	return func(req *Request) (T, error) {
		var v T
		if err := dec.Decode(req, &v); err != nil {
			return v, err
		}
		return v, nil
	}, nil
}

// Body 声明一个请求体来源,格式由 dec 决定:Body[CreateUser](JSONBody())。
// Body declares a request-body source; the format is decided by dec:
// Body[CreateUser](JSONBody()).
func Body[T any](dec RequestDecoder) InputSource[T] { return body[T]{dec: dec} }
