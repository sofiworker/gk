package ghttp

import "context"

// compiledHandler 是类型擦除后的处理器接口:所有 typed handler 与 RawHandler
// 在注册期都被编译成它,进入同一棵路由树、走同一条 ServeHTTP 分派。
// compiledHandler is the type-erased handler interface. Every typed handler and
// every RawHandler is compiled into it at registration time, so all requests
// enter the same routing tree and share one ServeHTTP dispatch path.
type compiledHandler interface {
	// serve 执行该端点:解码输入、调用业务函数、编码输出;错误交由统一错误链。
	// serve runs the endpoint: decode inputs, invoke the business function,
	// encode the output; errors are handed to the unified error chain.
	serve(ctx context.Context, req *Request, resp *Response) error
}

// RawHandlerFunc 是完全接管 HTTP 响应的原始处理器形态,作为一等公民逃生入口,
// 而非隐藏在分派内部的 bypass。
// RawHandlerFunc is the raw handler form that fully owns the HTTP response. It
// is a first-class escape hatch, not a bypass hidden inside dispatch.
type RawHandlerFunc func(ctx context.Context, req *Request, resp *Response) error

// rawHandler 把 RawHandlerFunc 适配成 compiledHandler,使其与 typed handler
// 共享同一执行路径。
// rawHandler adapts a RawHandlerFunc into a compiledHandler so it shares the
// same execution path as typed handlers.
type rawHandler struct {
	fn RawHandlerFunc
}

// serve 实现 compiledHandler。
// serve implements compiledHandler.
func (h rawHandler) serve(ctx context.Context, req *Request, resp *Response) error {
	return h.fn(ctx, req, resp)
}
