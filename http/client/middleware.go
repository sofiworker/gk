package client

import (
	"context"
	"time"
)

// 本文件定义 client 的扩展模型：洋葱式中间件 + 只读 Hook。
// This file defines the client extension model: onion middleware plus read-only hooks.

// Handler 是请求执行链的一环：收下请求，返回响应或错误。链的最内环是真正的发送动作，
// 由执行引擎提供；中间件在它外面逐层包裹。
// Handler is one link in the request execution chain: it takes a request and returns
// a response or an error. The innermost link is the actual send performed by the
// execution engine; middleware wraps around it.
type Handler func(ctx context.Context, req *Request) (*Response, error)

// RequestMiddleware 以洋葱方式包裹下一环：可在调用 next 前做前处理、调用后做后处理，
// 或不调用 next 直接短路（返回缓存响应、mock、本地校验失败）。
//
// 它与 ghttp server 的 Middleware 同形（`func(next Handler) Handler`），因此两侧的心智
// 模型一致；区别只在 Handler 的签名——server 有 ResponseWriter，client 返回 *Response。
// RequestMiddleware wraps the next link onion-style: it may pre-process before calling
// next, post-process after, or short-circuit by not calling next at all (cached
// response, mock, failing a local check).
//
// It shares its shape with the ghttp server's Middleware (`func(next Handler)
// Handler`), so the mental model is identical on both sides; only the Handler
// signature differs — the server writes through a ResponseWriter, the client returns
// a *Response.
type RequestMiddleware func(next Handler) Handler

// ResponseHandler 是响应处理链的一环。
// ResponseHandler is one link in the response processing chain.
type ResponseHandler func(ctx context.Context, resp *Response) (*Response, error)

// ResponseMiddleware 包裹响应处理链，可在响应到达（body 已被按策略读入）后做统一改写：
// 归一化 header、把业务失败码转成 error、采集指标等。
// ResponseMiddleware wraps the response chain and can uniformly rewrite a response
// once it arrives (body already read per policy): normalize headers, turn a business
// failure code into an error, collect metrics.
type ResponseMiddleware func(next ResponseHandler) ResponseHandler

// BeforeRequestHook 在请求即将发出（中间件链进入内环）前观察请求。它不能改变流程，
// 只会因返回错误而中止本次请求。
// BeforeRequestHook observes a request just before it is sent (as the middleware chain
// reaches the inner link). It cannot change the flow; returning an error aborts the
// request.
type BeforeRequestHook func(ctx context.Context, req *Request) error

// AfterResponseHook 在响应到达后观察响应，不能改变流程。
// AfterResponseHook observes an arrived response without changing the flow.
type AfterResponseHook func(ctx context.Context, resp *Response) error

// SuccessHook 在请求被判定为成功（状态码可接受且无错误）后调用。
// SuccessHook runs after a request is judged successful (acceptable status, no error).
type SuccessHook func(ctx context.Context, resp *Response)

// ErrorHook 在请求失败时调用；resp 可能为 nil（传输层失败）。
// ErrorHook runs when a request fails; resp may be nil (transport failure).
type ErrorHook func(ctx context.Context, err error, resp *Response)

// RetryHook 在每次重试【之前】调用，携带本次重试的等待时长与触发原因。
// RetryHook runs BEFORE each retry, carrying the wait duration and the trigger.
type RetryHook func(attempt int, delay time.Duration, resp *Response, err error)

// PanicHook 在用户中间件 panic 时调用。默认行为是记录后继续向上 panic（不吞掉），
// 与 server 的 Recovery 中间件"显式选择才恢复"的取向一致。
// PanicHook runs when user middleware panics. By default the panic is recorded and
// then re-panics (never swallowed), matching the server's Recovery middleware stance
// that recovery is opt-in.
type PanicHook func(ctx context.Context, recovered any)
