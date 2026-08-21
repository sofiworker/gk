package ghttp

import "context"

// Handler 是执行链中的一环,与 compiledHandler.serve 同签名。typed 终端(compiled.serve)
// 与 RawHandler 都归一为它,因此中间件对二者一视同仁,类型契约的解码/编码只发生在终端内部。
// Handler is one link in the execution chain, sharing compiledHandler.serve's
// signature. Both a typed terminal (compiled.serve) and a RawHandler collapse
// into it, so middleware treats both uniformly and the typed contract's
// decode/encode happens only inside the terminal.
type Handler func(ctx context.Context, req *Request, resp *Response) error

// serve 让 Handler 直接满足 compiledHandler,使折叠后的链能以单一接口进树。
// serve makes a Handler satisfy compiledHandler directly, so a folded chain
// enters the tree as one interface.
func (h Handler) serve(ctx context.Context, req *Request, resp *Response) error {
	return h(ctx, req, resp)
}

// Middleware 以洋葱方式包裹下一环:返回的 Handler 可在调用 next 前做前处理、调用后做
// 后处理,或不调用 next 直接短路(echo 式)。next 返回的 error 沿调用栈上冒至统一错误链。
// Middleware wraps the next link onion-style: the returned Handler may run
// pre-processing before calling next, post-processing after, or short-circuit by
// not calling next at all (echo style). The error next returns bubbles up the
// call stack to the unified error chain.
type Middleware func(next Handler) Handler

// chain 在注册期把中间件栈折叠到 terminal 外层:mws[0] 最外、最先执行,terminal 殿后。
// 请求期无迭代状态(调用栈即状态),零每请求分配。空栈时原样返回 terminal。
// chain folds the middleware stack around terminal at registration time: mws[0]
// is outermost and runs first, terminal runs last. There is no per-request
// iteration state (the call stack is the state) and zero per-request allocation.
// With an empty stack it returns terminal unchanged.
func chain(terminal Handler, mws []Middleware) Handler {
	// 由内向外包裹,保证 mws[0] 处于最外层。
	// Wrap from the inside out so that mws[0] ends up outermost.
	for i := len(mws) - 1; i >= 0; i-- {
		terminal = mws[i](terminal)
	}
	return terminal
}

// router 是包内密封接口,统一 Mux 与 Group 的注册入口,使泛型自由函数 Handle/Get/...
// 能对二者通用。register 收到的是 typed 或 raw 终端,由实现方按需再叠加自己的中间件栈。
// router is the package-sealed registration interface unifying Mux and Group, so
// the generic free functions Handle/Get/... work against both. register receives
// a typed or raw terminal; the implementation layers its own middleware stack.
type router interface {
	// register 将 terminal(可能已在调用方内部折叠部分链)注册到 method + path。
	// register registers terminal (possibly partially folded by the caller) at
	// method + path.
	register(method, path string, terminal Handler) error
}
