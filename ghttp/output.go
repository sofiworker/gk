package ghttp

import (
	"encoding/json"
	"net/http"
)

// jsonOutput 以 JSON 编码输出 T,状态码可经 Status 覆盖(默认 200)。
// 本阶段用标准库 encoding/json;零反射编码器随阶段 3 引入(设计 §9.3)。
// jsonOutput encodes T as JSON; the status code is overridable via Status
// (default 200). Uses standard encoding/json this stage; a zero-reflection
// encoder arrives in stage 3 (design §9.3).
type jsonOutput[T any] struct{ status int }

// Status 覆盖该输出的状态码,返回新值(值语义,便于链式:JSON[T]().Status(201))。
// Status overrides this output's status code, returning the new value (value
// semantics for chaining: JSON[T]().Status(201)).
func (o jsonOutput[T]) Status(code int) jsonOutput[T] {
	o.status = code
	return o
}

func (o jsonOutput[T]) encode(resp *Response, v T) error {
	status := o.status
	if status == 0 {
		status = http.StatusOK
	}
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.WriteHeader(status)
	return json.NewEncoder(resp).Encode(v)
}

// JSON 返回一个 JSON 输出契约,默认状态码 200,可用 .Status(code) 覆盖。
// JSON returns a JSON output contract, default status 200, overridable with
// .Status(code).
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
// 占位类型如 NoBody),但不会被写出。
// NoContent returns a body-less output contract (default 204). The business
// function still returns an O value (typically a placeholder like NoBody), which
// is not written.
func NoContent[T any]() noContent[T] { return noContent[T]{} }
