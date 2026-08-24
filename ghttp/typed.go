package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
)

// ===========================================================================
// Typed 入口：按输入组合分函数，handler 收裸参数，类型全推断，无包裹容器。
// 参数（path/query/header）经 struct tag 绑定；请求体经 RequestDecoder 解码，
// 用户可只实现解码一侧以替换解析库。输出走类型安全的 OutputSpec[O]。
// Typed entries: split by input shape, handlers take naked parameters, all type
// parameters inferred, no wrapper container. Params (path/query/header) bind via
// struct tags; the body is decoded by a RequestDecoder, so users may implement
// only the decode side to swap parsers. Output flows through the type-safe
// OutputSpec[O].
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

// registerParams 是 params-only 入口的共享注册逻辑。
// registerParams is the shared registration logic for params-only entries.
func registerParams[P, O any](r router, method, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	if out == nil {
		return ErrMissingOutput
	}
	plan, err := buildBindPlan(reflect.TypeOf((*P)(nil)).Elem())
	if err != nil {
		return err
	}
	c := &compiledParams[P, O]{plan: plan, out: out, h: h}
	return r.register(method, path, c.serve)
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
	if err := e.plan.apply(req, req.Query(), &p); err != nil {
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
	if out == nil {
		return ErrMissingOutput
	}
	c := &compiledNone[O]{out: out, h: h}
	return r.register(http.MethodGet, path, c.serve)
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

// PostParamsBody POST 入口：同时接收 params（path/query/header）与请求体（经 dec 解码）。
// dec 可为任意 RequestDecoder（JSONBody()/XMLCodec()/自定义），只需实现解码一侧。
// PostParamsBody is the POST entry receiving both params (path/query/header) and
// a body (decoded by dec). dec may be any RequestDecoder (JSONBody()/XMLCodec()/
// custom), needing only the decode side implemented.
func PostParamsBody[P, B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPost, path, dec, out, h)
}

// PutParamsBody PUT 入口（同 PostParamsBody）。
// PutParamsBody is the PUT entry (same as PostParamsBody).
func PutParamsBody[P, B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPut, path, dec, out, h)
}

// PatchParamsBody PATCH 入口（同 PostParamsBody）。
// PatchParamsBody is the PATCH entry (same as PostParamsBody).
func PatchParamsBody[P, B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPatch, path, dec, out, h)
}

// registerParamsBody 是 params+body 入口的共享注册逻辑；dec 为 nil 时报 ErrMissingCodec。
// registerParamsBody is the shared registration logic for params+body entries; a
// nil dec returns ErrMissingCodec.
func registerParamsBody[P, B, O any](r router, method, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	if out == nil {
		return ErrMissingOutput
	}
	if dec == nil {
		return ErrMissingCodec
	}
	plan, err := buildBindPlan(reflect.TypeOf((*P)(nil)).Elem())
	if err != nil {
		return err
	}
	c := &compiledParamsBody[P, B, O]{plan: plan, dec: dec, out: out, h: h, validate: bodyValidatorFor[B]()}
	if r.owner().strictContentType {
		c.wantCT = dec.ContentType()
	}
	return r.register(method, path, c.serve)
}

// compiledParamsBody params+body 的类型擦除执行器：两个裸参数分离(传输 params vs 纯净 body)。
// compiledParamsBody is the params+body type-erased executor: two naked
// parameters kept separate (transport params vs pure body).
type compiledParamsBody[P, B, O any] struct {
	plan *BindPlan
	dec  RequestDecoder
	out  OutputSpec[O]
	h    func(ctx context.Context, p P, b B) (O, error)
	// validate 非 nil 时(B 实现了 Validator)在解码后调用;nil 则跳过,零成本。
	// validate, when non-nil (B implements Validator), is called after decoding;
	// nil skips it at zero cost.
	validate func(*B) error
	// wantCT 非空时,请求期校验请求 Content-Type 与之一致,不符则 415。注册期由
	// strictContentType 决定是否填充(空=不校验)。
	// wantCT, when non-empty, makes the request verify its Content-Type matches
	// it, yielding 415 on mismatch. Filled at registration per strictContentType
	// (empty = no check).
	wantCT string
}

func (e *compiledParamsBody[P, B, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	var p P
	var b B
	if err := e.plan.apply(req, req.Query(), &p); err != nil {
		return err
	}
	if e.wantCT != "" && !contentTypeMatches(req.Header.Get("Content-Type"), e.wantCT) {
		return fmt.Errorf("%w: got %q want %q", ErrUnsupportedMediaType, mediaType(req.Header.Get("Content-Type")), e.wantCT)
	}
	if err := e.dec.Decode(req, &b); err != nil {
		return err
	}
	if e.validate != nil {
		if err := e.validate(&b); err != nil {
			return err
		}
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
func PostBody[B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(r, http.MethodPost, path, dec, out, h)
}

// PutBody PUT 入口（同 PostBody）。
// PutBody is the PUT entry (same as PostBody).
func PutBody[B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(r, http.MethodPut, path, dec, out, h)
}

// PatchBody PATCH 入口（同 PostBody）。
// PatchBody is the PATCH entry (same as PostBody).
func PatchBody[B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(r, http.MethodPatch, path, dec, out, h)
}

// registerBody 是仅 body 入口的共享注册逻辑；dec 为 nil 时报 ErrMissingCodec。
// registerBody is the shared registration logic for body-only entries; returns
// ErrMissingCodec if dec is nil.
func registerBody[B, O any](r router, method, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	if out == nil {
		return ErrMissingOutput
	}
	if dec == nil {
		return ErrMissingCodec
	}
	c := &compiledBody[B, O]{dec: dec, out: out, h: h, validate: bodyValidatorFor[B]()}
	if r.owner().strictContentType {
		c.wantCT = dec.ContentType()
	}
	return r.register(method, path, c.serve)
}

// compiledBody 是仅 body 场景的执行器：params 为空，只解码 body。
// compiledBody is the executor for body-only scenarios: params empty, only
// decoding body.
type compiledBody[B, O any] struct {
	dec RequestDecoder
	out OutputSpec[O]
	h   func(context.Context, B) (O, error)
	// validate 见 compiledParamsBody.validate。
	// validate: see compiledParamsBody.validate.
	validate func(*B) error
	// wantCT 见 compiledParamsBody.wantCT。
	// wantCT: see compiledParamsBody.wantCT.
	wantCT string
}

func (e *compiledBody[B, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	var b B
	if e.wantCT != "" && !contentTypeMatches(req.Header.Get("Content-Type"), e.wantCT) {
		return fmt.Errorf("%w: got %q want %q", ErrUnsupportedMediaType, mediaType(req.Header.Get("Content-Type")), e.wantCT)
	}
	if err := e.dec.Decode(req, &b); err != nil {
		return err
	}
	if e.validate != nil {
		if err := e.validate(&b); err != nil {
			return err
		}
	}
	out, err := e.h(ctx, b)
	if err != nil {
		return err
	}
	return e.out.encode(resp, out)
}
