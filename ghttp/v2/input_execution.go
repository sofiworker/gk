package v2

import (
	"context"
	"errors"
	"reflect"
)

// DecodeWith 用普通函数构造输入；函数负责每次请求的输入生命周期。
// DecodeWith constructs input through a function that owns each request's input lifetime.
func DecodeWith[I any](decode func(context.Context, *Request) (I, error)) Input[I] {
	return Input[I]{read: decode}
}

// 注册时选择值或指针构造路径；闭包只共享契约，每次调用创建独立输入。
// Select construction at registration; closures share contracts, never request input.
func compileInput[I any](in Input[I]) (func(context.Context, *Request) (I, error), error) {
	if in.read != nil {
		return in.read, nil
	}
	if !in.HasDecoder() {
		return nil, ErrNilDecoder
	}
	if compiled, ok := in.decoder.(interface{ registrationError() error }); ok {
		if err := compiled.registrationError(); err != nil {
			return nil, err
		}
	}
	if d, ok := in.decoder.(formDecoder[I]); ok && d.err != nil {
		return nil, d.err
	}
	typ := reflect.TypeFor[I]()
	if typ.Kind() != reflect.Pointer {
		return func(_ context.Context, req *Request) (I, error) {
			var value I
			err := in.Decode(req, &value)
			return value, err
		}, nil
	}
	if typ.Elem().Kind() == reflect.Pointer {
		return nil, errors.New("ghttp/v2: nested input pointers are unsupported")
	}
	element := typ.Elem()
	return func(_ context.Context, req *Request) (I, error) {
		var value I
		if err := in.Decode(req, &value); err != nil {
			return value, err
		}
		if reflect.ValueOf(value).IsNil() {
			value = reflect.New(element).Interface().(I)
		}
		return value, nil
	}, nil
}
