package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// 本文件提供泛型薄壳：只解决"响应解码目标类型"这一件事，不承载任何状态与策略。
// 核心能力（状态判定、错误、中间件、重试、流式）全部在非泛型层，泛型入口只是转发。
// This file provides the generic shell: it solves exactly one thing — the decode target
// type — and carries no state or policy. Every capability (status decision, errors,
// middleware, retries, streaming) lives in the non-generic layer; generic entries only
// forward.
//
// 为什么是 sink 模式：Go 的类型推断不看返回类型，`Get[User](ctx, url)` 里的 [User]
// 无法省略；而 T 出现在参数位置时（`Into(ctx, &user)`）编译器能从 `*T` 推出 T。因此主推
// sink 形态，返回式只作补充。这也让"泛型入口"与"非泛型入口"的书写成本几乎相等。
// Why sink mode: Go's inference does not look at return types, so the [User] in
// `Get[User](ctx, url)` cannot be omitted; but when T appears in a parameter position
// (`Into(ctx, &user)`) the compiler infers T from `*T`. Hence sink is primary and the
// result-returning form is secondary, making generic and non-generic entries cost about
// the same to write.

// Result 是一次泛型请求的结果：解码后的数据与完整响应。
// Result is a generic request's outcome: the decoded data plus the full response.
type Result[T any] struct {
	Data     T
	Response *Response
}

// RequestOption 是"每请求覆盖"，与构造期的 Option 分开：泛型入口不必先构造一个 Request
// 就能带上少量参数。
// RequestOption is a per-request override, kept separate from construction-time Options
// so a generic entry can carry a few parameters without building a Request first.
type RequestOption func(*Request)

// WithRequestHeader 为本次请求设置一个头。
// WithRequestHeader sets one header for this request.
func WithRequestHeader(key, value string) RequestOption {
	return func(r *Request) { r.SetHeader(key, value) }
}

// WithRequestQuery 为本次请求设置一个查询参数。
// WithRequestQuery sets one query parameter for this request.
func WithRequestQuery(key string, value any) RequestOption {
	return func(r *Request) { r.SetQueryParam(key, value) }
}

// WithRequestContext 为本次请求设置 context。
// WithRequestContext sets the context for this request.
func WithRequestContext(ctx context.Context) RequestOption {
	return func(r *Request) { r.SetContext(ctx) }
}

// WithRequestTimeout 为本次请求设置独立超时（不改动 Client，也不影响并发中的其他请求）。
// WithRequestTimeout sets an independent timeout for this request, leaving the Client
// and other in-flight requests untouched.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(r *Request) { r.SetTimeout(d) }
}

// WithRequestBody 为本次请求设置请求体（编码方式按 Content-Type 推断，默认 JSON）。
// WithRequestBody sets this request's body (encoding follows Content-Type, JSON by
// default).
func WithRequestBody(v any) RequestOption {
	return func(r *Request) { r.SetBody(v) }
}

// WithRequestPathParam 为本次请求设置一个路径参数。
// WithRequestPathParam sets one path parameter for this request.
func WithRequestPathParam(name, value string) RequestOption {
	return func(r *Request) { r.SetPathParam(name, value) }
}

// applyRequestOptions 应用每请求选项；nil 选项被忽略，便于条件式构造。
// applyRequestOptions applies per-request options; nil options are ignored so they can
// be built conditionally.
func applyRequestOptions(r *Request, opts []RequestOption) {
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
}

// DecodeInto 把响应体解码进 dst（须为指针）。它是不需要重新解析 URL 的泛型补充：
// 已经拿到 *Response 时用它，语义与 Response.Decode 完全一致。
// DecodeInto decodes the response body into dst (a pointer). It is the generic
// supplement for when a *Response already exists and no URL needs re-parsing; its
// semantics match Response.Decode exactly.
func DecodeInto[T any](resp *Response, dst *T) error {
	if resp == nil {
		return fmt.Errorf("ghttp/client: DecodeInto called with a nil response")
	}
	if dst == nil {
		return fmt.Errorf("ghttp/client: DecodeInto called with a nil target")
	}
	return resp.Decode(dst)
}

// As 把响应解码成一个 Result[T]。
//
// 注意 T 只出现在返回值位置，因此必须显式实例化：`As[User](resp)`。需要零方括号时用
// 某个 Into 家族入口。
// As decodes a response into a Result[T].
//
// Note that T appears only in the return position, so it must be instantiated
// explicitly: `As[User](resp)`. Use an Into-family entry for zero brackets.
func As[T any](resp *Response) (*Result[T], error) {
	var data T
	if err := DecodeInto(resp, &data); err != nil {
		return nil, err
	}
	return &Result[T]{Data: data, Response: resp}, nil
}

// DoInto 用指定方法发请求并把响应体解码进 dst。dst 为 nil 时只发请求不解码。
// DoInto sends a request with the given method and decodes the body into dst. A nil dst
// sends without decoding.
func DoInto[T any](ctx context.Context, c *Client, method, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	if c == nil {
		return nil, fmt.Errorf("ghttp/client: nil client")
	}
	r := c.R().SetMethod(method).SetURL(rawURL)
	if ctx != nil {
		r.SetContext(ctx)
	}
	applyRequestOptions(r, opts)
	resp, err := r.Send()
	if err != nil {
		// 失败时仍可能带回响应（非 2xx），交给调用方按需读取。
		// A response may still come back on failure (non-2xx); hand it to the caller.
		return resp, err
	}
	if dst != nil {
		if derr := resp.Decode(dst); derr != nil {
			return resp, derr
		}
	}
	return resp, nil
}

// GetInto 以 GET 发请求并把响应体解码进 dst。
// GetInto issues a GET and decodes the body into dst.
func GetInto[T any](ctx context.Context, c *Client, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	return DoInto(ctx, c, http.MethodGet, rawURL, dst, opts...)
}

// PostInto 以 POST 发请求并把响应体解码进 dst。body 为 nil 时不发送请求体。
// PostInto issues a POST and decodes the body into dst; a nil body sends no payload.
func PostInto[T any](ctx context.Context, c *Client, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	all := make([]RequestOption, 0, len(opts)+1)
	if body != nil {
		all = append(all, WithRequestBody(body))
	}
	all = append(all, opts...)
	return DoInto(ctx, c, http.MethodPost, rawURL, dst, all...)
}

// PutInto 以 PUT 发请求并把响应体解码进 dst。
// PutInto issues a PUT and decodes the body into dst.
func PutInto[T any](ctx context.Context, c *Client, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	all := make([]RequestOption, 0, len(opts)+1)
	if body != nil {
		all = append(all, WithRequestBody(body))
	}
	all = append(all, opts...)
	return DoInto(ctx, c, http.MethodPut, rawURL, dst, all...)
}

// PatchInto 以 PATCH 发请求并把响应体解码进 dst。
// PatchInto issues a PATCH and decodes the body into dst.
func PatchInto[T any](ctx context.Context, c *Client, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	all := make([]RequestOption, 0, len(opts)+1)
	if body != nil {
		all = append(all, WithRequestBody(body))
	}
	all = append(all, opts...)
	return DoInto(ctx, c, http.MethodPatch, rawURL, dst, all...)
}

// DeleteInto 以 DELETE 发请求并把响应体解码进 dst。
// DeleteInto issues a DELETE and decodes the body into dst.
func DeleteInto[T any](ctx context.Context, c *Client, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	return DoInto(ctx, c, http.MethodDelete, rawURL, dst, opts...)
}
