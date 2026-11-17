package ghttp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

// Validator 校验解码后的请求体。返回的错误若链上已有 HTTPError 则原样使用其状态码，
// 否则包装 ErrInvalidInput（映射为 400）。
// Validator checks a decoded request body. An error already carrying an HTTPError keeps
// its status; anything else is wrapped in ErrInvalidInput (maps to 400).
type Validator interface {
	Validate(ctx context.Context, v any) error
}

// ValidatorFunc 是函数形式的 Validator。
// ValidatorFunc is the function form of Validator.
type ValidatorFunc func(ctx context.Context, v any) error

// Validate 实现 Validator。
// Validate implements Validator.
func (f ValidatorFunc) Validate(ctx context.Context, v any) error { return f(ctx, v) }

// selfValidator 是 body 类型自带的无 context 校验方法。
// selfValidator is a context-free validation method on the body type.
type selfValidator interface{ Validate() error }

// selfValidatorCtx 是 body 类型自带的带 context 校验方法。
// selfValidatorCtx is a context-aware validation method on the body type.
type selfValidatorCtx interface {
	Validate(ctx context.Context) error
}

// WithValidator 为路由添加 body 校验器（可经 Server.With、Group 设为默认值），多个校验器按添加
// 顺序执行，遇到第一个错误即停止。校验在 Data() 首次解码成功后执行，因此仍然是 lazy 的：
// handler 不调用 Data() 就不会校验。
// WithValidator adds a body validator to the route (usable as a default via Server.With
// or Group). Validators run in order and stop at the first error. Validation runs right
// after the first successful decode in Data(), so it stays lazy: no Data() call, no
// validation.
func WithValidator(validators ...Validator) Option {
	return func(o *routeOptions) {
		for _, v := range validators {
			if v == nil {
				o.setErr(errors.New("ghttp: nil validator"))
				return
			}
		}
		o.validators = append(o.validators, validators...)
	}
}

// selfValidate 调用 body 自带的 Validate 方法：先看值本身，再看其指针（指针接收者）。
// selfValidate calls the body's own Validate method, checking the value first and then
// its pointer (pointer receivers).
func selfValidate[T any](ctx context.Context, v *T) error {
	// body 为 JSON null 时指针类型 T 解码为 nil，不能在 nil 上调用方法
	// A JSON null body decodes a pointer T to nil; never call methods on nil
	if rv := reflect.ValueOf(*v); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return fmt.Errorf("%w: request body is null", ErrInvalidInput)
	}
	for _, target := range []any{*v, v} {
		switch sv := target.(type) {
		case selfValidatorCtx:
			return sv.Validate(ctx)
		case selfValidator:
			return sv.Validate()
		}
	}
	return nil
}

// hasSelfValidate 报告 T 或 *T 是否带有 Validate 方法。
// hasSelfValidate reports whether T or *T has a Validate method.
func hasSelfValidate[T any]() bool {
	var zero T
	for _, target := range []any{zero, &zero} {
		switch target.(type) {
		case selfValidatorCtx, selfValidator:
			return true
		}
	}
	return false
}

// validationError 统一校验错误：已带 HTTPError 的保持其状态，其余包装 ErrInvalidInput。
// validationError normalizes validation errors: HTTPErrors keep their status, others wrap
// ErrInvalidInput.
func validationError(err error) error {
	if err == nil {
		return nil
	}
	var he HTTPError
	if errors.As(err, &he) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrInvalidInput, err)
}

// validatingInput 在 Input 解码成功后先执行 body 自带的 Validate，再依次执行路由校验器
// （校验器收到的是解码出的值本身）。没有任何校验时原样返回 in。
// validatingInput runs the body's own Validate and then the route validators (which
// receive the decoded value itself) after a successful decode. Without any validation it
// returns in unchanged.
func validatingInput[T BodyConstraint](in Input[T], validators []Validator) Input[T] {
	self := hasSelfValidate[T]()
	if len(validators) == 0 && !self {
		return in
	}
	return InputFunc[T](func(ctx context.Context, req *Request) (T, error) {
		v, err := in.Decode(ctx, req)
		if err != nil {
			return v, err
		}
		if self {
			if err := selfValidate(ctx, &v); err != nil {
				return v, validationError(err)
			}
		}
		for _, val := range validators {
			if err := val.Validate(ctx, v); err != nil {
				return v, validationError(err)
			}
		}
		return v, nil
	})
}
