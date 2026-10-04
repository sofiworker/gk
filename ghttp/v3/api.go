package v3

import (
	"context"
	"errors"
	"reflect"
)

// NoData 显式关闭默认输入解码，保留按需请求来源；客户端 body 不会被自动读取。
// NoData explicitly disables default decoding while retaining request sources; client bodies are not automatically read.
type NoData struct{}

// Method 使用统一请求包装；默认 JSON 数据，NoData 不解码，显式 codec 只针对 Data 的 T。
// Method uses a unified request wrapper with JSON data by default, no decoding for NoData, and explicit codecs targeting Data's T.
func Method[T, O any](method, path string, h func(context.Context, RequestOf[T]) (O, error), opts ...Option) Route {
	snapshot := append([]Option(nil), opts...)
	build := func(fullPath string, inherited []Option) Route {
		all := append(append([]Option(nil), inherited...), snapshot...)
		all = append(all, func(c *routeOptions) {
			var codec Input[T]
			if c.inputSet {
				var ok bool
				codec, ok = c.input.(Input[T])
				if !ok {
					c.input = Input[RequestOf[T]]{err: errors.New("ghttp/v3: data codec type mismatch")}
					return
				}
			} else if reflect.TypeFor[T]() == reflect.TypeFor[NoData]() {
				codec = Input[T]{read: func(context.Context, *Request) (T, error) { var zero T; return zero, nil }}
			} else {
				codec = JSONInput[T]()
			}
			wrapped := DecodeRequest(codec)
			if !c.inputSet && reflect.TypeFor[T]() == reflect.TypeFor[NoData]() {
				wrapped.sourceBinding = true
			}
			c.input = wrapped
		})
		return compileMethod(method, fullPath, h, all...)
	}
	r := build(path, nil)
	r.compile = build
	return r
}

// BindInput 显式启用标签来源绑定；未声明来源的字段保持零值，不隐式回退 JSON。
// BindInput explicitly enables tagged source binding; undeclared fields remain zero-valued without implicit JSON fallback.
func BindInput[T any]() Input[T] {
	typ := reflect.TypeFor[T]()
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return Input[T]{err: errors.New("ghttp/v3: binding input requires a struct or pointer to struct")}
	}
	plan := bindingPlan{}
	if err := compileBindingFields(typ, nil, map[reflect.Type]bool{}, &plan); err != nil {
		return Input[T]{err: err}
	}
	in := CustomInput[T](bindingDecoder[T]{plan: plan})
	in.schema, in.sourceBinding = reflect.TypeFor[T](), true
	return in
}
