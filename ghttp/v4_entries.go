package ghttp

import (
	"context"
	"net/http"
	"reflect"
)

// ===========================================================================
// 甲-2 typed 入口（正式版专用）：参数全拼命名，RequestDecoder 单侧可传。
// v4-style typed entries (formal): full-spelling names, single-sided
// RequestDecoder allowed. These replace container-style Handle/P[Q,B]; those
// are marked Deprecated and will be phased out.
//
// 输出侧仍用类型安全的 OutputSpec[O]（支持 .Status/.WithEncoder），执行器直接以
// typed 值调用 encode，无 any 断言、无包装。输入 body 侧用导出的 RequestDecoder，
// 用户可只实现该侧替换解析库。
// Output stays type-safe via OutputSpec[O] (.Status/.WithEncoder); the executor
// calls encode with the typed value directly — no any-assertion, no wrapper.
// Body input uses the exported RequestDecoder, so users can implement only that
// side to swap parsers.
// ============================================================================

// compiledV4[P,O] 仅 params(无 body)的类型擦除执行器：注册期建 bindPlan;请求期只运行不反射。
// compiledV4[P,O] is the params-only (no body) type-erased executor: bindPlan
// built at registration, executed without reflection at request time.
type compiledV4[P, O any] struct {
	plan *BindPlan
	out  OutputSpec[O]
	h    func(ctx context.Context, p P) (O, error)
}

func (e *compiledV4[P, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	var p P
	if err := e.plan.apply(req, req.URL.Query(), &p); err != nil {
		return err
	}
	out, err := e.h(ctx, p)
	if err != nil {
		return err
	}
	return e.out.encode(resp, out)
}

// compiledV4Body[P,B,O] params+body 的类型擦除执行器：两个裸参数分离(传输 params vs 纯净 body)。
// compiledV4Body[P,B,O] is the params+body type-erased executor: two naked
// parameters kept separate (transport params vs pure body).
type compiledV4Body[P, B, O any] struct {
	plan *BindPlan
	dec  RequestDecoder
	out  OutputSpec[O]
	h    func(ctx context.Context, p P, b B) (O, error)
}

func (e *compiledV4Body[P, B, O]) serve(ctx context.Context, req *Request, resp *Response) error {
	var p P
	var b B
	if err := e.plan.apply(req, req.URL.Query(), &p); err != nil {
		return err
	}
	if err := e.dec.Decode(req, &b); err != nil {
		return err
	}
	out, err := e.h(ctx, p, b)
	if err != nil {
		return err
	}
	return e.out.encode(resp, out)
}

// ——— Get/Delete 专用入口（仅 params，无 body）——

// GetParams GET 专用入口：把 path/query/header 绑定到 params 结构体，输出经 out 编码。
// params 须为结构体，字段用 path:/query:/header: tag 标注来源；无 tag 字段静默跳过。
// GetParams is the GET entry binding path/query/header into a params struct; the
// output is encoded by out. params must be a struct whose fields are annotated
// with path:/query:/header: tags; untagged fields are silently skipped.
func GetParams[P, O any](r router, path string, out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(r, http.MethodGet, path, out, h)
}

// DeleteParams DELETE 专用入口（同 GetParams）。
// DeleteParams DELETE entry (same as GetParams).
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
	c := &compiledV4[P, O]{plan: plan, out: out, h: h}
	return r.register(method, path, c.serve)
}

// ——— Post/Put/Patch 专用入口（params+body）——

// PostParamsBody POST 专用入口：同时接收 params（path/query/header）与请求体（经 dec 解码）。
// dec 可为任意 RequestDecoder（JSONBody()/XMLCodec()/自定义），只需实现解码一侧。
// PostParamsBody is the POST entry receiving both params (path/query/header) and
// a body (decoded by dec). dec may be any RequestDecoder (JSONBody()/XMLCodec()/
// custom), needing only the decode side implemented.
func PostParamsBody[P, B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPost, path, dec, out, h)
}

// PutParamsBody PUT 专用入口（同 PostParamsBody）。
// PutParamsBody PUT entry (same as PostParamsBody).
func PutParamsBody[P, B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPut, path, dec, out, h)
}

// PatchParamsBody PATCH 专用入口（同 PostParamsBody）。
// PatchParamsBody PATCH entry (same as PostParamsBody).
func PatchParamsBody[P, B, O any](r router, path string, dec RequestDecoder, out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(r, http.MethodPatch, path, dec, out, h)
}

// registerParamsBody 是 params+body 入口的共享注册逻辑；dec 为 nil 时报 ErrMissingCodec。
// registerParamsBody is the shared registration logic for params+body entries;
// a nil dec returns ErrMissingCodec.
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
	c := &compiledV4Body[P, B, O]{plan: plan, dec: dec, out: out, h: h}
	return r.register(method, path, c.serve)
}
