package ghttp

import (
	"context"
	"net/http"
)

// RequestInput 是组合输入容器:路径 / 查询 / 请求体分别落在具名字段,由框架构造,不写进
// 业务结构体。缺某来源时该字段用零尺寸占位类型(NoPath/NoQuery/NoBody),配 None 空绑定。
// RequestInput is the composed input container: path / query / body land in
// named fields built by the framework, not written into business structs. When
// a source is absent, that field uses a zero-size placeholder type
// (NoPath/NoQuery/NoBody) paired with a None empty binding.
type RequestInput[P, Q, B any] struct {
	Path  P
	Query Q
	Body  B
}

// NoPath / NoQuery / NoBody 是零尺寸占位类型,标记对应输入来源缺席。
// NoPath / NoQuery / NoBody are zero-size placeholder types marking that the
// corresponding input source is absent.
type (
	NoPath  struct{}
	NoQuery struct{}
	NoBody  struct{}
)

// endpointSpec 是注册期编译端点时累积的静态描述。本阶段最小(仅 method/path);OpenAPI
// 所需的参数、内容类型等字段在阶段 3 补齐。
// endpointSpec is the static description accumulated while compiling an endpoint at
// registration time. Minimal this stage (method/path only); OpenAPI fields
// (parameters, content types) land in stage 3.
type endpointSpec struct {
	method string
	path   string
}

// InputSource 是一个输入来源的类型化契约:注册期 bind 固定提取器,返回运行期解码闭包
// 与可能的注册错误。本阶段为包内密封接口(仅内置来源实现);开放的自定义来源随阶段 3
// 的 OpenAPI 契约一并定稿。
// InputSource is the typed contract of one input source: bind fixes the
// extractor at registration and returns a request-time decode closure plus any
// registration error. This stage it is a package-sealed interface (built-in
// sources only); an open custom-source contract is finalized with stage 3's
// OpenAPI work.
type InputSource[T any] interface {
	bind(spec *endpointSpec) (decode func(req *Request) (T, error), err error)
}

// OutputSpec 是输出契约:注册期固定格式/状态码,encode 在请求期写响应。(Out, error)
// 不默认 JSON、不默认 200,格式与状态码必须显式声明。
// OutputSpec is the output contract: format/status fixed at registration,
// encode writes the response at request time. (Out, error) defaults to neither
// JSON nor 200; format and status must be declared explicitly.
type OutputSpec[T any] interface {
	encode(resp *Response, v T) error
}

// compiled 是 typed 端点类型擦除后的产物:注册期把解码器/编码器/业务函数固化其中,
// 请求期只执行,不反射、不重解析模板。它实现 compiledHandler,与 RawHandler 同型进树。
// compiled is the type-erased product of a typed endpoint: decoders/encoder/
// business function are fixed into it at registration; request time only
// executes, without reflection or template re-parsing. It satisfies
// compiledHandler and enters the tree uniformly with RawHandler.
type compiled[P, Q, B, O any] struct {
	decodeP func(req *Request) (P, error)
	decodeQ func(req *Request) (Q, error)
	decodeB func(req *Request) (B, error)
	out     OutputSpec[O]
	h       func(ctx context.Context, in RequestInput[P, Q, B]) (O, error)
}

// serve 是编译后的执行器:依次解码 P/Q/B,调用业务函数,再编码输出;任一步错误直接返回
// 交由统一错误链处理。解码顺序与 RequestInput 字段一致。
// serve is the compiled executor: decode P/Q/B in order, call the business
// function, then encode the output; an error at any step returns immediately to
// the unified error chain. Decode order matches RequestInput's fields.
func (c *compiled[P, Q, B, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	in, err := decodeInput(req, c.decodeP, c.decodeQ, c.decodeB)
	if err != nil {
		return err
	}
	out, err := c.h(ctx, in)
	if err != nil {
		return err
	}
	return c.out.encode(resp, out)
}

// compiledReq 是扩展 handler 形态(业务函数额外收到 *Request)类型擦除后的产物,与
// compiled 共享输入解码与输出编码,仅业务函数签名多一个 *Request 参数。
// compiledReq is the type-erased product of the extended handler form (the
// business function additionally receives *Request). It shares input decoding
// and output encoding with compiled; only the business signature differs by one
// *Request parameter.
type compiledReq[P, Q, B, O any] struct {
	decodeP func(req *Request) (P, error)
	decodeQ func(req *Request) (Q, error)
	decodeB func(req *Request) (B, error)
	out     OutputSpec[O]
	h       func(ctx context.Context, req *Request, in RequestInput[P, Q, B]) (O, error)
}

func (c *compiledReq[P, Q, B, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	in, err := decodeInput(req, c.decodeP, c.decodeQ, c.decodeB)
	if err != nil {
		return err
	}
	out, err := c.h(ctx, req, in)
	if err != nil {
		return err
	}
	return c.out.encode(resp, out)
}

// decodeInput 按 P→Q→B 顺序解码,组装 RequestInput;任一步失败即返回。两种 compiled
// 形态共享它,避免解码逻辑重复。
// decodeInput decodes in P→Q→B order and assembles the RequestInput, returning
// on the first failure. Both compiled forms share it to avoid duplicating the
// decode logic.
func decodeInput[P, Q, B any](
	req *Request,
	decodeP func(*Request) (P, error),
	decodeQ func(*Request) (Q, error),
	decodeB func(*Request) (B, error),
) (RequestInput[P, Q, B], error) {
	var in RequestInput[P, Q, B]
	var err error
	if in.Path, err = decodeP(req); err != nil {
		return in, err
	}
	if in.Query, err = decodeQ(req); err != nil {
		return in, err
	}
	if in.Body, err = decodeB(req); err != nil {
		return in, err
	}
	return in, nil
}

// bindInputs 在注册期把三个 InputSource 绑定成解码闭包;任一绑定错误即返回。两种注册
// 入口(Handle / HandleReq)共享它。
// bindInputs binds the three InputSources into decode closures at registration,
// returning on the first bind error. Both registration entries (Handle /
// HandleReq) share it.
func bindInputs[P, Q, B any](
	spec *endpointSpec,
	p InputSource[P], q InputSource[Q], b InputSource[B],
) (decodeP func(*Request) (P, error), decodeQ func(*Request) (Q, error), decodeB func(*Request) (B, error), err error) {
	if decodeP, err = p.bind(spec); err != nil {
		return
	}
	if decodeQ, err = q.bind(spec); err != nil {
		return
	}
	decodeB, err = b.bind(spec)
	return
}

// Handle 是 typed 注册入口:四个类型参数全靠调用点推断,不手写。它把 InputSource /
// OutputSpec / 业务函数在注册期编译为 compiled,再经 router(Mux 或 Group)折叠中间件后
// 进树。未声明 Output(out 为 nil)返回 ErrMissingOutput,绝不隐式选 JSON。
// Handle is the typed registration entry: all four type parameters are inferred
// at the call site, never written. It compiles the InputSource / OutputSpec /
// business function into a compiled at registration, then registers through the
// router (Mux or Group) which folds middleware and enters the tree. A missing
// output (out is nil) returns ErrMissingOutput; JSON is never chosen implicitly.
func Handle[P, Q, B, O any](
	r router, method, path string,
	p InputSource[P], q InputSource[Q], b InputSource[B],
	out OutputSpec[O],
	h func(ctx context.Context, in RequestInput[P, Q, B]) (O, error),
) error {
	if out == nil {
		return ErrMissingOutput
	}
	spec := &endpointSpec{method: method, path: path}
	decodeP, decodeQ, decodeB, err := bindInputs(spec, p, q, b)
	if err != nil {
		return err
	}
	c := &compiled[P, Q, B, O]{
		decodeP: decodeP,
		decodeQ: decodeQ,
		decodeB: decodeB,
		out:     out,
		h:       h,
	}
	return r.register(method, path, c.serve)
}

// HandleReq 是扩展 handler 形态的 typed 注册入口:业务函数在 typed 输入之外额外收到
// *Request(如需读原始头、访问 http.Request 底层能力),输出仍走 OutputSpec。它与
// Handle 共享输入绑定与类型擦除机制,进同一棵树、走同一条分派——不是 RawHandler 那种
// 完全接管响应,而是"typed 契约 + 额外原始请求"的中间形态。
// HandleReq is the typed registration entry for the extended handler form: the
// business function receives *Request in addition to the typed input (to read
// raw headers or reach the underlying http.Request), while output still flows
// through OutputSpec. It shares input binding and type erasure with Handle,
// entering the same tree and dispatch — not RawHandler's full response
// ownership, but the middle form "typed contract + extra raw request".
func HandleReq[P, Q, B, O any](
	r router, method, path string,
	p InputSource[P], q InputSource[Q], b InputSource[B],
	out OutputSpec[O],
	h func(ctx context.Context, req *Request, in RequestInput[P, Q, B]) (O, error),
) error {
	if out == nil {
		return ErrMissingOutput
	}
	spec := &endpointSpec{method: method, path: path}
	decodeP, decodeQ, decodeB, err := bindInputs(spec, p, q, b)
	if err != nil {
		return err
	}
	c := &compiledReq[P, Q, B, O]{
		decodeP: decodeP,
		decodeQ: decodeQ,
		decodeB: decodeB,
		out:     out,
		h:       h,
	}
	return r.register(method, path, c.serve)
}

// Get/Post/Put/Patch/Delete 是固定 method 的薄封装,类型参数同样全靠推断。
// Get/Post/Put/Patch/Delete are thin method-fixing wrappers; type parameters are
// likewise fully inferred.
func Get[P, Q, B, O any](r router, path string, p InputSource[P], q InputSource[Q], b InputSource[B], out OutputSpec[O], h func(context.Context, RequestInput[P, Q, B]) (O, error)) error {
	return Handle[P, Q, B, O](r, http.MethodGet, path, p, q, b, out, h)
}

func Post[P, Q, B, O any](r router, path string, p InputSource[P], q InputSource[Q], b InputSource[B], out OutputSpec[O], h func(context.Context, RequestInput[P, Q, B]) (O, error)) error {
	return Handle[P, Q, B, O](r, http.MethodPost, path, p, q, b, out, h)
}

func Put[P, Q, B, O any](r router, path string, p InputSource[P], q InputSource[Q], b InputSource[B], out OutputSpec[O], h func(context.Context, RequestInput[P, Q, B]) (O, error)) error {
	return Handle[P, Q, B, O](r, http.MethodPut, path, p, q, b, out, h)
}

func Patch[P, Q, B, O any](r router, path string, p InputSource[P], q InputSource[Q], b InputSource[B], out OutputSpec[O], h func(context.Context, RequestInput[P, Q, B]) (O, error)) error {
	return Handle[P, Q, B, O](r, http.MethodPatch, path, p, q, b, out, h)
}

func Delete[P, Q, B, O any](r router, path string, p InputSource[P], q InputSource[Q], b InputSource[B], out OutputSpec[O], h func(context.Context, RequestInput[P, Q, B]) (O, error)) error {
	return Handle[P, Q, B, O](r, http.MethodDelete, path, p, q, b, out, h)
}
