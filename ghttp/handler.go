package ghttp

import (
	"context"
	"net/http"
)

// HandlerFunc 是泛型 handler 签名。
// HandlerFunc is the generic handler signature.
// Req 为解析后的请求输入结构体，Resp 为响应输出结构体。
// Req is the parsed request input; Resp is the response output.
type HandlerFunc[Req, Resp any] func(ctx context.Context, input Req) (Resp, error)

// NoInputHandler 用于无请求体/参数的路由。
// NoInputHandler is for endpoints without request input.
type NoInputHandler[Resp any] func(ctx context.Context) (Resp, error)

// NoOutputHandler 用于只返回 error 的路由。
// NoOutputHandler is for endpoints that only return an error.
type NoOutputHandler[Req any] func(ctx context.Context, input Req) error

// RawHandler 允许直接访问 http.ResponseWriter 与 *http.Request。
// RawHandler allows direct access to the raw request/response.
type RawHandler func(w http.ResponseWriter, r *http.Request)

// HTTPHandlerFunc 是自行写响应的解析输入 handler。
// HTTPHandlerFunc writes the response itself after parsing input.
type HTTPHandlerFunc[Req any] func(w http.ResponseWriter, r *http.Request, input Req) error

// StatusCoder 允许类型化响应显式声明 HTTP 状态码。
// StatusCoder lets a typed response declare its HTTP status.
type StatusCoder interface {
	StatusCode() int
}

// ResponseHeaderWriter 允许类型化响应显式添加响应头。
// ResponseHeaderWriter lets a typed response add response headers.
type ResponseHeaderWriter interface {
	WriteResponseHeaders(http.Header)
}

// RedirectFunc 为解析后的请求解析重定向目标。
// RedirectFunc resolves a redirect target for a parsed request.
type RedirectFunc[Req any] func(input Req) (string, error)
