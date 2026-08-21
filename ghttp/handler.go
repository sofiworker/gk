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
// 而非隐藏在分派内部的 bypass。它与 Handler 同签名,注册时直接转为 Handler 进链,
// 与 typed 终端共享同一执行路径(无需额外适配器)。
// RawHandlerFunc is the raw handler form that fully owns the HTTP response. It
// is a first-class escape hatch, not a bypass hidden inside dispatch. It shares
// Handler's signature and is converted straight to a Handler at registration,
// sharing the same execution path as typed terminals (no adapter needed).
type RawHandlerFunc func(ctx context.Context, req *Request, resp *Response) error
