package ghttp

import (
	"context"
	"net/http"
	"reflect"
)

// ===========================================================================
// Typed 入口：按输入组合分函数，handler 收裸参数，类型全推断，无包裹容器。
// 参数（path/query/header）经 struct tag 绑定；请求体经 InputSpec[B] 解码（JSONBody[B]/
// FormBody[B]/XMLBody[B]/TextBody[B]/Body[B](codec)）,表单文本与上传文件均归请求体。
// 输出走类型安全的 OutputSpec[O]。
// Typed entries: split by input shape, handlers take naked parameters, all type
// parameters inferred, no wrapper container. Params (path/query/header) bind via
// struct tags; the body is decoded by an InputSpec[B] (JSONBody[B]/FormBody[B]/
// XMLBody[B]/TextBody[B]/Body[B](codec)), with form text and uploaded files both
// belonging to the body. Output flows through the type-safe OutputSpec[O].
// ===========================================================================

// ——— 仅 params 入口（无 body）：Get / Delete ——

// GetParams GET 入口：把 path/query/header 绑定到 params 结构体，输出经 out 编码。
// params 须为结构体，字段用 path:/query:/header: tag 标注来源；无 tag 字段静默跳过。
// GetParams is the GET entry binding path/query/header into a params struct; the
// output is encoded by out. params must be a struct whose fields are annotated
// with path:/query:/header: tags; untagged fields are silently skipped.
func GetParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodGet, path, out, h)
}

// DeleteParams DELETE 入口（同 GetParams）。
// DeleteParams is the DELETE entry (same as GetParams).
func DeleteParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodDelete, path, out, h)
}

// PostParams POST 入口（仅 params,无 body）。适用于纯 path/query/header 的 POST 操作;
// 需要表单或文件上传时用 PostBody + FormBody[B]()。
// PostParams is the POST entry (params only, no body). Suitable for pure
// path/query/header POST operations; for form or file uploads use PostBody +
// FormBody[B]().
func PostParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodPost, path, out, h)
}

// PutParams PUT 入口（仅 params,无 body）。
// PutParams is the PUT entry (params only, no body).
func PutParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodPut, path, out, h)
}

// PatchParams PATCH 入口（仅 params,无 body）。
// PatchParams is the PATCH entry (params only, no body).
func PatchParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodPatch, path, out, h)
}

// HeadParams HEAD 入口（仅 params,无 body）。HEAD 语义上不返回响应体,故通常配
// NoContent[O]() 或只依赖 out 写出的响应头;标准库会为 HEAD 自动丢弃响应体。
// HeadParams is the HEAD entry (params only, no body). HEAD carries no response
// body by definition, so pair it with NoContent[O]() or rely on the headers out
// writes; the standard library discards the body for HEAD automatically.
func HeadParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodHead, path, out, h)
}

// OptionsParams OPTIONS 入口（仅 params,无 body）。用于自定义某路由的 OPTIONS 响应
// (如声明 Allow 或 CORS 头);框架的 CORS 中间件已处理标准预检,此入口用于业务化的
// OPTIONS 语义。
// OptionsParams is the OPTIONS entry (params only, no body). Use it to customize a
// route's OPTIONS response (declaring Allow or CORS headers); the CORS middleware
// already handles standard preflight, so this entry is for business-level OPTIONS
// semantics.
func OptionsParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodOptions, path, out, h)
}

// registerParams 是 params-only 入口的共享注册逻辑。
// mws 为路由级中间件(自由函数入口传空),在终端外层折叠,位于分组中间件【之内】。
// registerParams is the shared registration logic for params-only entries.
// mws holds route-level middleware (empty from the free-function entries); it folds
// around the terminal, INSIDE the group's middleware.
func registerParams[P, O any](r router, method, path string, out OutputSpec[O], h func(context.Context, P) (O, error), mws ...Middleware) error {
	if out == nil {
		return ErrMissingOutput
	}
	pt := reflect.TypeOf((*P)(nil)).Elem()
	plan, err := buildBindPlan(pt)
	if err != nil {
		return err
	}
	c := &compiledParams[P, O]{plan: plan, out: out, h: h}
	if err := r.register(method, path, chain(c.serve, mws)); err != nil {
		return err
	}
	r.owner().noteRoute(r, method, path, routeDoc{params: pt, out: outputDocOf(out)})
	return nil
}

// compiledParams 仅 params(无 body)的类型擦除执行器：注册期建 bindPlan;请求期只运行不反射。
// compiledParams is the params-only (no body) type-erased executor: bindPlan
// built at registration, executed without reflection at request time.
type compiledParams[P, O any] struct {
	plan *BindPlan
	out  OutputSpec[O]
	h    func(ctx context.Context, p P) (O, error)
}

func (e *compiledParams[P, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	var p P
	// 纯 path 端点完全不碰 query;needQuery=true 时才构造取值源(惰性扫描或建映射)。
	// Pure-path endpoints never touch the query; only when needQuery=true is a value
	// source built (lazy scan or parsed map).
	var q querySource
	if e.plan.needQuery {
		q = newQuerySource(req, e.plan)
	}
	if err := e.plan.apply(req, q, &p); err != nil {
		return err
	}
	out, err := e.h(ctx, p)
	if err != nil {
		return err
	}
	return e.out.encode(resp, out)
}

// ——— 无参数入口（仅输出，无 params 无 body）——

// GetNone GET 入口：无 params 无 body，只返回输出。适用于健康检查等简单端点。
// GetNone is the GET entry for endpoints without params or body, returning just
// output. Suitable for health checks and simple endpoints.
func GetNone[O any](r router, path string, out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(r, http.MethodGet, path, out, h)
}

// DeleteNone DELETE 入口：无 params 无 body(如删除固定路径的单例资源)。
// DeleteNone is the DELETE entry without params or body (e.g. deleting a
// singleton resource at a fixed path).
func DeleteNone[O any](r router, path string, out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(r, http.MethodDelete, path, out, h)
}

// PostNone POST 入口：无 params 无 body(如触发一个无入参的动作)。
// PostNone is the POST entry without params or body (e.g. triggering an action
// that takes no input).
func PostNone[O any](r router, path string, out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(r, http.MethodPost, path, out, h)
}

// PutNone PUT 入口：无 params 无 body。
// PutNone is the PUT entry without params or body.
func PutNone[O any](r router, path string, out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(r, http.MethodPut, path, out, h)
}

// PatchNone PATCH 入口：无 params 无 body。
// PatchNone is the PATCH entry without params or body.
func PatchNone[O any](r router, path string, out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(r, http.MethodPatch, path, out, h)
}

// HeadNone HEAD 入口：无 params 无 body。常用于探测资源存在性与元数据。
// HeadNone is the HEAD entry without params or body, typically probing resource
// existence and metadata.
func HeadNone[O any](r router, path string, out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(r, http.MethodHead, path, out, h)
}

// OptionsNone OPTIONS 入口：无 params 无 body。用于业务化的 OPTIONS 语义(标准 CORS
// 预检由 CORS 中间件处理)。
// OptionsNone is the OPTIONS entry without params or body, for business-level
// OPTIONS semantics (standard CORS preflight is handled by the CORS middleware).
func OptionsNone[O any](r router, path string, out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(r, http.MethodOptions, path, out, h)
}

// registerNone 是无 params 无 body 入口的共享注册逻辑;mws 见 registerParams。
// registerNone is the shared registration logic for entries without params or body;
// for mws see registerParams.
func registerNone[O any](r router, method, path string, out OutputSpec[O], h func(context.Context) (O, error), mws ...Middleware) error {
	if out == nil {
		return ErrMissingOutput
	}
	c := &compiledNone[O]{out: out, h: h}
	if err := r.register(method, path, chain(c.serve, mws)); err != nil {
		return err
	}
	r.owner().noteRoute(r, method, path, routeDoc{out: outputDocOf(out)})
	return nil
}

// compiledNone 是无 params 无 body 的极简执行器：只调业务函数再编码输出。
// compiledNone is the minimal executor for endpoints without params or body:
// calls the business function then encodes output.
type compiledNone[O any] struct {
	out OutputSpec[O]
	h   func(context.Context) (O, error)
}

func (e *compiledNone[O]) serve(ctx context.Context, req *Request, resp *Response) error {
	out, err := e.h(ctx)
	if err != nil {
		return err
	}
	return e.out.encode(resp, out)
}

// ——— params+body 入口：Post / Put / Patch ——

// PostParamsBody POST 入口：同时接收 params（path/query/header）与请求体（经 in 解码）。
// in 可为任意 InputSpec[B]（JSONBody[B]()/FormBody[B]()/XMLBody[B]()/Body[B](codec)）。
// PostParamsBody is the POST entry receiving both params (path/query/header) and
// a body (decoded by in). in may be any InputSpec[B] (JSONBody[B]()/FormBody[B]()/
// XMLBody[B]()/Body[B](codec)).
func PostParamsBody[P, B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPost, path, in, out, h)
}

// PutParamsBody PUT 入口（同 PostParamsBody）。
// PutParamsBody is the PUT entry (same as PostParamsBody).
func PutParamsBody[P, B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPut, path, in, out, h)
}

// PatchParamsBody PATCH 入口（同 PostParamsBody）。
// PatchParamsBody is the PATCH entry (same as PostParamsBody).
func PatchParamsBody[P, B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPatch, path, in, out, h)
}

// DeleteParamsBody DELETE 入口：params + 请求体。RFC 9110 允许 DELETE 携带请求体
// (语义由服务端定义),批量删除等场景需要它。
// DeleteParamsBody is the DELETE entry with params plus a body. RFC 9110 permits a
// body on DELETE (with server-defined semantics), which batch deletion needs.
func DeleteParamsBody[P, B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodDelete, path, in, out, h)
}

// registerParamsBody 是 params+body 入口的共享注册逻辑；in 为 nil 或内部无解码器时报
// ErrMissingCodec。mws 见 registerParams。
// registerParamsBody is the shared registration logic for params+body entries; a
// nil in, or one holding no decoder, returns ErrMissingCodec. For mws see
// registerParams.
func registerParamsBody[P, B, O any](r router, method, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, P, B) (O, error), mws ...Middleware) error {
	if out == nil {
		return ErrMissingOutput
	}
	// 两道检查各挡一种形态:in == nil 挡真正的 nil 接口值,codecMissing 挡 Body[B](nil)
	// 造出的非 nil 接口值(内部 decoder 为 nil)。少了后者,取 Content-Type 时会 panic。
	// The two checks catch different shapes: in == nil catches a genuinely nil interface
	// value, codecMissing catches the non-nil interface value Body[B](nil) builds (with a
	// nil decoder inside). Without the latter, reading the Content-Type panics.
	if in == nil || in.codecMissing() {
		return ErrMissingCodec
	}
	pt := reflect.TypeOf((*P)(nil)).Elem()
	plan, err := buildBindPlan(pt)
	if err != nil {
		return err
	}
	c := &compiledParamsBody[P, B, O]{plan: plan, in: in, out: out, h: h}
	if r.owner().strictContentType {
		c.wantCT = in.contentTypes()
	}
	if err := r.register(method, path, chain(c.serve, mws)); err != nil {
		return err
	}
	r.owner().noteRoute(r, method, path, routeDoc{
		params: pt,
		body:   reflect.TypeOf((*B)(nil)).Elem(),
		bodyCT: in.contentType(),
		out:    outputDocOf(out),
	})
	return nil
}

// compiledParamsBody params+body 的类型擦除执行器：两个裸参数分离(传输 params vs 纯净 body)。
// compiledParamsBody is the params+body type-erased executor: two naked
// parameters kept separate (transport params vs pure body).
type compiledParamsBody[P, B, O any] struct {
	plan *BindPlan
	in   InputSpec[B]
	out  OutputSpec[O]
	h    func(ctx context.Context, p P, b B) (O, error)
	// wantCT 非空时,请求期校验请求 Content-Type 属于该集合,不符则 415。注册期由
	// strictContentType 决定是否填充(空=不校验)。集合形态是为了让表单这类接受多个
	// Content-Type 的契约也能被严格校验。
	// wantCT, when non-empty, makes the request verify its Content-Type belongs to
	// the set, yielding 415 otherwise. Filled at registration per strictContentType
	// (empty = no check). It is a set so contracts accepting several Content-Types,
	// such as forms, can also be strictly checked.
	wantCT []string
}

func (e *compiledParamsBody[P, B, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	var p P
	var b B
	// 纯 path+body 端点完全不碰 query;needQuery=true 时才构造取值源。
	// Pure-path+body endpoints never touch the query; only when needQuery=true is a
	// value source built.
	var q querySource
	if e.plan.needQuery {
		q = newQuerySource(req, e.plan)
	}
	if err := e.plan.apply(req, q, &p); err != nil {
		return err
	}
	if len(e.wantCT) != 0 {
		if got := req.Header.Get("Content-Type"); !contentTypeIn(got, e.wantCT) {
			return unsupportedMediaTypeError(got, e.wantCT)
		}
	}
	if err := e.in.decode(req, &b); err != nil {
		return err
	}
	out, err := e.h(ctx, p, b)
	if err != nil {
		return err
	}
	return e.out.encode(resp, out)
}

// ——— 仅 body 入口（无 params）：Post / Put / Patch ——

// PostBody POST 入口：无 params，只接收请求体并解码。适用于创建资源。
// PostBody is the POST entry with no params, only receiving a decoded body.
// Suitable for resource creation endpoints.
func PostBody[B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(r, http.MethodPost, path, in, out, h)
}

// PutBody PUT 入口（同 PostBody）。
// PutBody is the PUT entry (same as PostBody).
func PutBody[B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(r, http.MethodPut, path, in, out, h)
}

// PatchBody PATCH 入口（同 PostBody）。
// PatchBody is the PATCH entry (same as PostBody).
func PatchBody[B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(r, http.MethodPatch, path, in, out, h)
}

// DeleteBody DELETE 入口：无 params,只接收请求体(如按条件批量删除)。
// DeleteBody is the DELETE entry with no params, receiving only a body (e.g.
// conditional batch deletion).
func DeleteBody[B, O any](r router, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(r, http.MethodDelete, path, in, out, h)
}

// registerBody 是仅 body 入口的共享注册逻辑；in 为 nil 或内部无解码器时报 ErrMissingCodec。
// mws 见 registerParams。
// registerBody is the shared registration logic for body-only entries; returns
// ErrMissingCodec if in is nil or holds no decoder. For mws see registerParams.
func registerBody[B, O any](r router, method, path string, in InputSpec[B], out OutputSpec[O], h func(context.Context, B) (O, error), mws ...Middleware) error {
	if out == nil {
		return ErrMissingOutput
	}
	// 见 registerParamsBody:两道检查分别挡 nil 接口值与内含 nil decoder 的非 nil 接口值。
	// See registerParamsBody: the two checks catch a nil interface value and a non-nil
	// one holding a nil decoder, respectively.
	if in == nil || in.codecMissing() {
		return ErrMissingCodec
	}
	c := &compiledBody[B, O]{in: in, out: out, h: h}
	if r.owner().strictContentType {
		c.wantCT = in.contentTypes()
	}
	if err := r.register(method, path, chain(c.serve, mws)); err != nil {
		return err
	}
	r.owner().noteRoute(r, method, path, routeDoc{
		body:   reflect.TypeOf((*B)(nil)).Elem(),
		bodyCT: in.contentType(),
		out:    outputDocOf(out),
	})
	return nil
}

// compiledBody 是仅 body 场景的执行器：params 为空，只解码 body。
// compiledBody is the executor for body-only scenarios: params empty, only
// decoding body.
type compiledBody[B, O any] struct {
	in  InputSpec[B]
	out OutputSpec[O]
	h   func(context.Context, B) (O, error)
	// wantCT 见 compiledParamsBody.wantCT。
	// wantCT: see compiledParamsBody.wantCT.
	wantCT []string
}

func (e *compiledBody[B, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	var b B
	if len(e.wantCT) != 0 {
		if got := req.Header.Get("Content-Type"); !contentTypeIn(got, e.wantCT) {
			return unsupportedMediaTypeError(got, e.wantCT)
		}
	}
	if err := e.in.decode(req, &b); err != nil {
		return err
	}
	out, err := e.h(ctx, b)
	if err != nil {
		return err
	}
	return e.out.encode(resp, out)
}
