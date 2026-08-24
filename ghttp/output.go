package ghttp

import (
	"net/http"
)

// OutputSpec 是输出契约:注册期固定格式/状态码,encode 在请求期写响应。(Out, error)
// 不默认 JSON、不默认 200,格式与状态码必须显式声明。
// OutputSpec is the output contract: format/status fixed at registration, encode
// writes the response at request time. (Out, error) defaults to neither JSON nor
// 200; format and status must be declared explicitly.
type OutputSpec[T any] interface {
	encode(resp *Response, v T) error
}

// jsonOutput 以 JSON 编码输出 T,状态码可经 Status 覆盖(默认 200)。默认用内置 JSONCodec
// 编码,也可经 WithEncoder 替换为任意 ResponseEncoder(自定义 JSON 库、XML、模板等)。
// jsonOutput encodes T as JSON; status overridable via Status (default 200).
// Encodes with the built-in JSONCodec by default, or any ResponseEncoder via
// WithEncoder (custom JSON lib, XML, templates, etc.).
type jsonOutput[T any] struct {
	status int
	enc    ResponseEncoder // nil 表示用内置 JSONCodec;非 nil 则用该编码器。
}

// Status 覆盖该输出的状态码,返回新值(值语义,便于链式:JSON[T]().Status(201))。
// Status overrides this output's status code, returning the new value (value
// semantics for chaining: JSON[T]().Status(201)).
func (o jsonOutput[T]) Status(code int) jsonOutput[T] {
	o.status = code
	return o
}

// WithEncoder 用自定义编码器替换内置 JSON 编码,返回新值。只换编码器、状态码不变。
// WithEncoder replaces the built-in JSON encoding with a custom encoder,
// returning the new value; only the encoder changes, status stays.
func (o jsonOutput[T]) WithEncoder(enc ResponseEncoder) jsonOutput[T] {
	o.enc = enc
	return o
}

func (o jsonOutput[T]) encode(resp *Response, v T) error {
	status := o.status
	if status == 0 {
		status = http.StatusOK
	}
	if o.enc != nil {
		// 自定义 encoder:用其声明的 Content-Type(若尚未被设置),避免输出丢失
		// 正确的 Content-Type(此前该分支从不设置,XML 等自定义输出被误当默认类型)。
		// Custom encoder: set its declared Content-Type (unless already set) so
		// the output does not lose the correct Content-Type (this branch never
		// set it before, mislabeling XML and other custom outputs).
		if resp.Header().Get("Content-Type") == "" {
			if ct := o.enc.ContentType(); ct != "" {
				resp.Header().Set("Content-Type", ct)
			}
		}
		resp.WriteHeader(status)
		return o.enc.Encode(resp, v)
	}
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.WriteHeader(status)
	return jsonCodec{}.Encode(resp, v)
}

// JSON 返回一个 JSON 输出契约,默认状态码 200,可用 .Status(code) 覆盖、.WithEncoder 替换编码器。
// JSON returns a JSON output contract, default status 200, overridable with
// .Status(code) and .WithEncoder(enc).
func JSON[T any]() jsonOutput[T] { return jsonOutput[T]{} }

// noContent 是无响应体输出:写状态码(默认 204),不写 body。O 为占位类型。
// noContent is a body-less output: writes the status (default 204), no body. O
// is a placeholder type.
type noContent[T any] struct{ status int }

func (o noContent[T]) Status(code int) noContent[T] {
	o.status = code
	return o
}

func (o noContent[T]) encode(resp *Response, _ T) error {
	status := o.status
	if status == 0 {
		status = http.StatusNoContent
	}
	resp.WriteHeader(status)
	return nil
}

// NoContent 返回一个无响应体输出契约(默认 204)。业务函数仍需返回一个 O 值(通常用
// struct{} 之类的占位类型),但不会被写出。
// NoContent returns a body-less output contract (default 204). The business
// function still returns an O value (typically a placeholder like struct{}),
// which is not written.
func NoContent[T any]() noContent[T] { return noContent[T]{} }
