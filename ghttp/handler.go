package ghttp

import (
	"context"
	"net/http"
)

// 泛型业务 handler 的签名为普通函数类型:
// func(context.Context, I) (O, error),由 Handle/GetJSON 等入口直接接受。
// 历史别名 HandlerFunc[Req,Resp] 已随执行模型重写删除,链处理器名让位给
// 新执行链的 HandlerFunc func(*Ctx)。
// The generic business handler signature is a plain function type:
// func(context.Context, I) (O, error), accepted directly by Handle/GetJSON.
// The historical HandlerFunc[Req,Resp] alias was removed in the execution
// model rewrite; the chain handler name now belongs to the new chain's
// HandlerFunc func(*Ctx).

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
