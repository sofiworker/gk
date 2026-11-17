//go:build go1.27

package client

import (
	"context"
	"net/http"
)

// 本文件是 Go ≥ 1.27 的方法级泛型糖：把 sink 模式接到 fluent 链末端。
//
// 【为何整份文件带 go1.27 构建标签】
// 泛型方法（规范：MethodDecl 允许 TypeParameters）需要 Go 1.27；项目最低支持 1.25，
// 因此这些方法必须整体隔离在带标签的文件里。低版本下它们【完全不存在】，用户看到的是
// 明确的 "has no field or method Into"，而不是一个能编译却在链接期出错的半成品。
//
// 【为何优先 sink（Into）而非返回式（As）】
// Go 的类型推断不看返回类型：`r.As[User]()` 必须手写方括号；而 `r.Into(ctx, &user)` 的
// T 能从 `*T` 推断，调用处零方括号。两者都提供，sink 为主推。
//
// This file is the Go >= 1.27 method-level generic sugar: sink mode wired onto the end
// of a fluent chain.
//
// WHY THE WHOLE FILE IS BUILD-TAGGED go1.27:
// generic methods (spec: MethodDecl permits TypeParameters) require Go 1.27, while the
// project supports 1.25 as its minimum, so these methods must live entirely inside a
// tagged file. On older toolchains they simply DO NOT EXIST and the user gets a clear
// "has no field or method Into" instead of a half-present API that fails later.
//
// WHY SINK (Into) IS PREFERRED OVER THE RESULT FORM (As):
// Go's inference ignores return types, so `r.As[User]()` needs explicit brackets, whereas
// `r.Into(ctx, &user)` infers T from `*T` with no brackets at all. Both are offered;
// sink is primary.

// prepareMethod 是各 fluent 终结方法共用的前置：把 ctx/method/url/body 与每请求选项落到
// 接收者上，随后交由 finishInto 发送与解码。抽出来只为让每个终结方法保持一行转发，避免
// 同一段前置逻辑在十来个方法里各写一遍而慢慢漂移。
// prepareMethod is the shared prologue of the fluent terminators: it lands
// ctx/method/url/body and per-request options onto the receiver, after which finishInto
// sends and decodes. It exists so each terminator stays a one-line forward and one piece
// of prologue cannot drift across a dozen methods.
func (r *Request) prepareMethod(ctx context.Context, method, rawURL string, body any, withBody bool, opts []RequestOption) {
	if ctx != nil {
		r.SetContext(ctx)
	}
	if method != "" {
		r.SetMethod(method)
	}
	if rawURL != "" {
		r.SetURL(rawURL)
	}
	if withBody && body != nil {
		r.SetBody(body)
	}
	applyRequestOptions(r, opts)
}

// finishInto 发送已配置好的请求，并在 dst 非 nil 时解码响应体。
// finishInto sends the configured request and decodes the body when dst is non-nil.
func (r *Request) finishInto[T any](dst *T) (*Response, error) {
	resp, err := r.Send()
	if err != nil {
		return resp, err
	}
	if dst != nil {
		if derr := resp.Decode(dst); derr != nil {
			return resp, derr
		}
	}
	return resp, nil
}

// Into 在 fluent 链末端以 sink 模式发出请求并把响应体解进 dst（须为指针），适用于
// method/URL 已在链上设置好的场景（或用 SetBody 等方法另行设置过）：
//
//	client.New().R().SetMethod(http.MethodGet).SetURL("/users").Into(ctx, &user)
//
// 若 method 与 URL 也一并给出，用下面的 GetInto/PostInto/… 更直接。
//
// dst 为 nil 时只发请求不解码。ctx 为 nil 时沿用 Request 上已设置的 context。
// Into terminates a fluent chain in sink mode: it sends the request and decodes the body
// into dst (a pointer), for when method/URL were already set on the chain (or configured
// separately):
//
//	client.New().R().SetMethod(http.MethodGet).SetURL("/users").Into(ctx, &user)
//
// When method and URL are given here too, the GetInto/PostInto/… below read better.
//
// A nil dst sends without decoding. A nil ctx keeps whatever context the Request
// already holds.
func (r *Request) Into[T any](ctx context.Context, dst *T) (*Response, error) {
	r.prepareMethod(ctx, "", "", nil, false, nil)
	return r.finishInto(dst)
}

// The method-shaped sink terminators below are the fluent counterparts of the Client's
// GetInto/PostInto/…: they set the method and URL themselves, so a chain reads as
//
//	client.New().R().SetQueryParam("id", 1).GetInto(ctx, "/users", &user)
//
// instead of spelling out SetMethod + SetURL + Into. T is still inferred from *T, so no
// type argument is written at the call site.

// DoInto 以指定方法与 URL 终结 fluent 链并把响应体解进 dst。
// DoInto terminates a fluent chain with the given method and URL, decoding the body into
// dst.
func (r *Request) DoInto[T any](ctx context.Context, method, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, method, rawURL, nil, false, opts)
	return r.finishInto(dst)
}

// GetInto 以 GET 终结 fluent 链并把响应体解进 dst。
// GetInto terminates a fluent chain with GET, decoding the body into dst.
func (r *Request) GetInto[T any](ctx context.Context, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, http.MethodGet, rawURL, nil, false, opts)
	return r.finishInto(dst)
}

// PostInto 以 POST 终结 fluent 链，body 非 nil 时作为请求体发送。
// PostInto terminates a fluent chain with POST, sending body when non-nil.
func (r *Request) PostInto[T any](ctx context.Context, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, http.MethodPost, rawURL, body, true, opts)
	return r.finishInto(dst)
}

// PutInto 以 PUT 终结 fluent 链。
// PutInto terminates a fluent chain with PUT.
func (r *Request) PutInto[T any](ctx context.Context, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, http.MethodPut, rawURL, body, true, opts)
	return r.finishInto(dst)
}

// PatchInto 以 PATCH 终结 fluent 链。
// PatchInto terminates a fluent chain with PATCH.
func (r *Request) PatchInto[T any](ctx context.Context, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, http.MethodPatch, rawURL, body, true, opts)
	return r.finishInto(dst)
}

// DeleteInto 以 DELETE 终结 fluent 链。需要 DELETE 带请求体时用 DoInto，
// 或先 SetBody 再调它。
// DeleteInto terminates a fluent chain with DELETE. For a DELETE with a body, use DoInto
// or set the body first.
func (r *Request) DeleteInto[T any](ctx context.Context, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, http.MethodDelete, rawURL, nil, false, opts)
	return r.finishInto(dst)
}

// HeadInto 以 HEAD 终结 fluent 链。HEAD 通常没有响应体，dst 可为 nil。
// HeadInto terminates a fluent chain with HEAD. HEAD usually has no body, so dst may be
// nil.
func (r *Request) HeadInto[T any](ctx context.Context, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, http.MethodHead, rawURL, nil, false, opts)
	return r.finishInto(dst)
}

// OptionsInto 以 OPTIONS 终结 fluent 链。
// OptionsInto terminates a fluent chain with OPTIONS.
func (r *Request) OptionsInto[T any](ctx context.Context, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	r.prepareMethod(ctx, http.MethodOptions, rawURL, nil, false, opts)
	return r.finishInto(dst)
}

// As 在 fluent 链末端以返回式终结，返回 Result[T]。T 只出现在返回值位置，因此必须显式
// 实例化：`r.As[User]()`。与 Into 一样，链上必须已设置 method 与 URL；需要零方括号时
// 请用 Into。
// As terminates a fluent chain in result form, returning Result[T]. T appears only in the
// return position, so it must be instantiated explicitly: `r.As[User]()`. Like Into, it
// requires method and URL to be set on the chain; use Into when zero brackets matter.
func (r *Request) As[T any]() (*Result[T], error) {
	resp, err := r.Send()
	if err != nil {
		return nil, err
	}
	return As[T](resp)
}

// GetInto 是包级 GetInto 的方法版：T 从 dst 推断。
// GetInto is the method form of the package-level GetInto: T is inferred from dst.
func (c *Client) GetInto[T any](ctx context.Context, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	return GetInto(ctx, c, rawURL, dst, opts...)
}

// DoInto 是包级 DoInto 的方法版。
// DoInto is the method form of the package-level DoInto.
func (c *Client) DoInto[T any](ctx context.Context, method, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	return DoInto(ctx, c, method, rawURL, dst, opts...)
}

// PostInto 是包级 PostInto 的方法版。
// PostInto is the method form of the package-level PostInto.
func (c *Client) PostInto[T any](ctx context.Context, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	return PostInto(ctx, c, rawURL, body, dst, opts...)
}

// PutInto 是包级 PutInto 的方法版。
// PutInto is the method form of the package-level PutInto.
func (c *Client) PutInto[T any](ctx context.Context, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	return PutInto(ctx, c, rawURL, body, dst, opts...)
}

// PatchInto 是包级 PatchInto 的方法版。
// PatchInto is the method form of the package-level PatchInto.
func (c *Client) PatchInto[T any](ctx context.Context, rawURL string, body any, dst *T, opts ...RequestOption) (*Response, error) {
	return PatchInto(ctx, c, rawURL, body, dst, opts...)
}

// DeleteInto 是包级 DeleteInto 的方法版。
// DeleteInto is the method form of the package-level DeleteInto.
func (c *Client) DeleteInto[T any](ctx context.Context, rawURL string, dst *T, opts ...RequestOption) (*Response, error) {
	return DeleteInto(ctx, c, rawURL, dst, opts...)
}
