package ghttp

// ============================================================================
// C1 POC v3 —— 仅供评审,独立文件,不改动现有 typed.go/input.go/request.go。
// C1 POC v3 — review only, self-contained.
//
// 相比 v2,v3 落实评审结论(风格甲:专用入口):
//   · 消除 In[P,B]/None 包裹:handler 直接收裸参数,靠输入组合选专用入口
//       无输入      CGet(m, path, out, func(ctx) (O,err))
//       仅 params   CGetP(m, path, out, func(ctx, P) (O,err))       ← P 靠 tag 绑 path/query/header
//       仅 body     CPostB(m, path, codec, out, func(ctx, B) (O,err))
//       params+body CPostPB(m, path, codec, out, func(ctx, P,B) (O,err))
//   · 类型参数全靠 handler 字面量推断(已实测),调用点不写 [P,B,O]
//   · group 分组 + middleware 复用:直接用现有 m.Group / Use(见 demo)
//   · 三层逃生保留:半逃生(+*Request)、全逃生(现有 RawHandle)
//   · 无 tag 字段:静默跳过(评审定 a)
//   · query 每请求解析一次(v2 已修 v1 的重复解析坑)
//
// C-prefixed to avoid clashing; the prefix goes away on adoption into typed.go.
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
)

// ---------------------------------------------------------------------------
// 1. 绑定计划:注册期反射建,请求期跑(query 每请求解析一次)
// ---------------------------------------------------------------------------

type cSource uint8

const (
	cSourcePath cSource = iota
	cSourceQuery
	cSourceHeader
)

type cBindStep struct {
	fieldIndex int
	source     cSource
	name       string
	kind       reflect.Kind
}

// cBindPlan 一个 params 结构体的绑定计划;uploadIdx>=0 表示含 multipart 上传字段。
// cBindPlan is a params struct's bind plan; uploadIdx>=0 marks a multipart field.
type cBindPlan struct {
	steps      []cBindStep
	uploadIdx  int
	uploadName string
	isNone     bool // params 类型是 CNoInput(无 params 槽)
}

// buildParamsPlan 注册期反射遍历 params 结构体字段建计划。CNoInput 返回空计划。
// 无 tag 字段静默跳过(评审定 a)。
// buildParamsPlan reflects over the params struct at registration; CNoInput
// yields an empty plan. Untagged fields are silently skipped (decision a).
func buildParamsPlan(t reflect.Type) (*cBindPlan, error) {
	plan := &cBindPlan{uploadIdx: -1}
	if t == reflect.TypeOf(CNoInput{}) {
		plan.isNone = true
		return plan, nil
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: params must be a struct, got %s", ErrInvalidParam, t.Kind())
	}
	uploadType := reflect.TypeOf(CUpload{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type == uploadType {
			name := f.Tag.Get("form")
			if name == "" {
				name = f.Name
			}
			plan.uploadIdx, plan.uploadName = i, name
			continue
		}
		var src cSource
		var name string
		switch {
		case f.Tag.Get("path") != "":
			src, name = cSourcePath, f.Tag.Get("path")
		case f.Tag.Get("query") != "":
			src, name = cSourceQuery, f.Tag.Get("query")
		case f.Tag.Get("header") != "":
			src, name = cSourceHeader, f.Tag.Get("header")
		default:
			continue // 无绑定 tag,静默跳过。
		}
		switch f.Type.Kind() {
		case reflect.String, reflect.Int, reflect.Int64, reflect.Bool:
			plan.steps = append(plan.steps, cBindStep{fieldIndex: i, source: src, name: name, kind: f.Type.Kind()})
		default:
			return nil, fmt.Errorf("%w: field %q unsupported bind kind %s", ErrInvalidParam, f.Name, f.Type.Kind())
		}
	}
	return plan, nil
}

// apply 请求期跑计划:query 由调用方预解析一次传入。
// apply runs the plan; query is pre-parsed once by the caller.
func (p *cBindPlan) apply(req *Request, query url.Values, paramsPtr any) error {
	if p.isNone || (len(p.steps) == 0 && p.uploadIdx < 0) {
		return nil
	}
	v := reflect.ValueOf(paramsPtr).Elem()
	for _, s := range p.steps {
		var raw string
		var ok bool
		switch s.source {
		case cSourcePath:
			raw = req.Params.Get(s.name)
			ok = raw != ""
		case cSourceQuery:
			if vs := query[s.name]; len(vs) > 0 {
				raw, ok = vs[0], true
			}
		case cSourceHeader:
			raw = req.Header.Get(s.name)
			ok = raw != ""
		}
		if !ok {
			continue
		}
		fv := v.Field(s.fieldIndex)
		switch s.kind {
		case reflect.String:
			fv.SetString(raw)
		case reflect.Int, reflect.Int64:
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, cSourceName(s.source), s.name, err)
			}
			fv.SetInt(n)
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, cSourceName(s.source), s.name, err)
			}
			fv.SetBool(b)
		}
	}
	if p.uploadIdx >= 0 {
		if err := bindUpload(req, v, p.uploadIdx, p.uploadName); err != nil {
			return err
		}
	}
	return nil
}

func cSourceName(s cSource) string {
	switch s {
	case cSourcePath:
		return "path"
	case cSourceQuery:
		return "query"
	case cSourceHeader:
		return "header"
	}
	return "?"
}

// CNoInput 是无 params 槽的占位类型(仅用于底层执行器的类型参数,糖层入口不暴露它)。
// CNoInput is the no-params placeholder (only for the low-level executor's type
// parameter; the sugar entries never expose it).
type CNoInput struct{}

// CUpload 承接一个 multipart 上传文件。params 结构体放此类型字段即自动绑定。
// CUpload receives one multipart uploaded file.
type CUpload struct {
	Filename string
	Size     int64
	Header   *multipart.FileHeader
	Open     func() (multipart.File, error)
}

func bindUpload(req *Request, paramsVal reflect.Value, idx int, formName string) error {
	f, header, err := req.FormFile(formName)
	if err != nil {
		return fmt.Errorf("%w: upload %q: %v", ErrInvalidInput, formName, err)
	}
	_ = f.Close()
	paramsVal.Field(idx).Set(reflect.ValueOf(CUpload{
		Filename: header.Filename, Size: header.Size, Header: header, Open: header.Open,
	}))
	return nil
}

// ---------------------------------------------------------------------------
// 2. body 解码:业务 body 走 codec,不碰绑定计划(纯净)
// ---------------------------------------------------------------------------

type CBodyCodec interface {
	decodeBody(req *Request, v any) error
}

type cJSONBodyCodec struct{}

func (cJSONBodyCodec) decodeBody(req *Request, v any) error {
	if req.Body == nil {
		return nil
	}
	dec := json.NewDecoder(req.Body)
	if err := dec.Decode(v); err != nil && err != io.EOF {
		return fmt.Errorf("%w: json body: %v", ErrInvalidInput, err)
	}
	return nil
}

// CJSONBody 声明 JSON body codec。
// CJSONBody declares the JSON body codec.
func CJSONBody() CBodyCodec { return cJSONBodyCodec{} }

// ---------------------------------------------------------------------------
// 3. 输出契约:固定码 / 自定义码 / 信封 / 动态码 / 下载
// ---------------------------------------------------------------------------

type COutput[O any] interface {
	write(resp *Response, o O) error
}

type cJSONOut[O any] struct{ status int }

func (o cJSONOut[O]) write(resp *Response, v O) error { return writeJSONC(resp, o.status, v) }

// CJSON 声明 JSON 输出,默认 200;.Status 覆盖固定码。
// CJSON declares JSON output, default 200; .Status overrides.
func CJSON[O any]() cJSONOut[O]                   { return cJSONOut[O]{status: http.StatusOK} }
func (o cJSONOut[O]) Status(code int) cJSONOut[O] { o.status = code; return o }

// CResult 动态状态码 opt-in。
// CResult opts into dynamic status codes.
type CResult[O any] struct {
	Status int
	Value  O
}

func COK[O any](v O) CResult[O]               { return CResult[O]{Status: http.StatusOK, Value: v} }
func CCreated[O any](v O) CResult[O]          { return CResult[O]{Status: http.StatusCreated, Value: v} }
func CStatus[O any](code int, v O) CResult[O] { return CResult[O]{Status: code, Value: v} }

// CFileDownload 描述一次流式下载。
// CFileDownload describes a streaming download.
type CFileDownload struct {
	Filename    string
	ContentType string
	Content     io.Reader
	Size        int64
}

// ---------------------------------------------------------------------------
// 4. 底层执行器:统一编译 params 计划 + body codec + 执行闭包
// ---------------------------------------------------------------------------

type cEndpoint[P, B any] struct {
	plan  *cBindPlan
	codec CBodyCodec
	run   func(ctx context.Context, req *Request, p P, b B, resp *Response) error
}

func (e *cEndpoint[P, B]) serve(ctx context.Context, req *Request, resp *Response) error {
	var p P
	var b B
	var q url.Values
	if req.URL != nil {
		q = req.URL.Query() // 每请求一次
	}
	if err := e.plan.apply(req, q, &p); err != nil {
		return err
	}
	if e.codec != nil {
		if err := e.codec.decodeBody(req, &b); err != nil {
			return err
		}
	}
	return e.run(ctx, req, p, b, resp)
}

func compileC[P, B any](codec CBodyCodec, run func(ctx context.Context, req *Request, p P, b B, resp *Response) error) (*cEndpoint[P, B], error) {
	plan, err := buildParamsPlan(reflect.TypeOf((*P)(nil)).Elem())
	if err != nil {
		return nil, err
	}
	return &cEndpoint[P, B]{plan: plan, codec: codec, run: run}, nil
}

// ---------------------------------------------------------------------------
// 5. 专用入口(风格甲):无输入 / 仅 params / 仅 body / params+body
//    handler 收裸参数,类型全推断,零 In/None 包裹。
// ---------------------------------------------------------------------------

// —— 无输入:func(ctx) (O, error) ——

func CHandleNone[O any](r router, method, path string, out COutput[O], h func(ctx context.Context) (O, error)) error {
	e, err := compileC[CNoInput, CNoInput](nil, func(ctx context.Context, req *Request, _ CNoInput, _ CNoInput, resp *Response) error {
		o, err := h(ctx)
		if err != nil {
			return err
		}
		return out.write(resp, o)
	})
	if err != nil {
		return err
	}
	return r.register(method, path, e.serve)
}

// —— 仅 params:func(ctx, P) (O, error);P 靠 tag 绑 path/query/header ——

func CHandleP[P, O any](r router, method, path string, out COutput[O], h func(ctx context.Context, p P) (O, error)) error {
	e, err := compileC[P, CNoInput](nil, func(ctx context.Context, req *Request, p P, _ CNoInput, resp *Response) error {
		o, err := h(ctx, p)
		if err != nil {
			return err
		}
		return out.write(resp, o)
	})
	if err != nil {
		return err
	}
	return r.register(method, path, e.serve)
}

// —— 仅 body:func(ctx, B) (O, error);B 纯净业务体走 codec ——

func CHandleB[B, O any](r router, method, path string, codec CBodyCodec, out COutput[O], h func(ctx context.Context, b B) (O, error)) error {
	e, err := compileC[CNoInput, B](codec, func(ctx context.Context, req *Request, _ CNoInput, b B, resp *Response) error {
		o, err := h(ctx, b)
		if err != nil {
			return err
		}
		return out.write(resp, o)
	})
	if err != nil {
		return err
	}
	return r.register(method, path, e.serve)
}

// —— params+body:func(ctx, P, B) (O, error);两个裸参数,body 纯净 ——

func CHandlePB[P, B, O any](r router, method, path string, codec CBodyCodec, out COutput[O], h func(ctx context.Context, p P, b B) (O, error)) error {
	e, err := compileC[P, B](codec, func(ctx context.Context, req *Request, p P, b B, resp *Response) error {
		o, err := h(ctx, p, b)
		if err != nil {
			return err
		}
		return out.write(resp, o)
	})
	if err != nil {
		return err
	}
	return r.register(method, path, e.serve)
}

// ---------------------------------------------------------------------------
// 6. 固定 method 糖:Get/Post/... × 输入组合。命名规律:后缀 P=params, B=body, PB=两者。
// ---------------------------------------------------------------------------

// 无输入
func CGet[O any](r router, path string, out COutput[O], h func(context.Context) (O, error)) error {
	return CHandleNone[O](r, http.MethodGet, path, out, h)
}

// 仅 params
func CGetP[P, O any](r router, path string, out COutput[O], h func(context.Context, P) (O, error)) error {
	return CHandleP[P, O](r, http.MethodGet, path, out, h)
}
func CDeleteP[P, O any](r router, path string, out COutput[O], h func(context.Context, P) (O, error)) error {
	return CHandleP[P, O](r, http.MethodDelete, path, out, h)
}

// 仅 body
func CPostB[B, O any](r router, path string, codec CBodyCodec, out COutput[O], h func(context.Context, B) (O, error)) error {
	return CHandleB[B, O](r, http.MethodPost, path, codec, out, h)
}

// params+body
func CPostPB[P, B, O any](r router, path string, codec CBodyCodec, out COutput[O], h func(context.Context, P, B) (O, error)) error {
	return CHandlePB[P, B, O](r, http.MethodPost, path, codec, out, h)
}
func CPutPB[P, B, O any](r router, path string, codec CBodyCodec, out COutput[O], h func(context.Context, P, B) (O, error)) error {
	return CHandlePB[P, B, O](r, http.MethodPut, path, codec, out, h)
}

// ---------------------------------------------------------------------------
// 7. 三层逃生的其余两层:半逃生(+*Request)、动态码、下载
// ---------------------------------------------------------------------------

// 半逃生(仅 params):handler 额外收 *Request。
// Half escape (params-only): the handler additionally receives *Request.
func CHandleRawP[P, O any](r router, method, path string, out COutput[O], h func(ctx context.Context, req *Request, p P) (O, error)) error {
	e, err := compileC[P, CNoInput](nil, func(ctx context.Context, req *Request, p P, _ CNoInput, resp *Response) error {
		o, err := h(ctx, req, p)
		if err != nil {
			return err
		}
		return out.write(resp, o)
	})
	if err != nil {
		return err
	}
	return r.register(method, path, e.serve)
}

// 动态码(仅 params):handler 返回 CResult[O]。
// Dynamic status (params-only): the handler returns CResult[O].
func CResultP[P, O any](r router, method, path string, h func(ctx context.Context, p P) (CResult[O], error)) error {
	e, err := compileC[P, CNoInput](nil, func(ctx context.Context, req *Request, p P, _ CNoInput, resp *Response) error {
		res, err := h(ctx, p)
		if err != nil {
			return err
		}
		code := res.Status
		if code == 0 {
			code = http.StatusOK
		}
		return writeJSONC(resp, code, res.Value)
	})
	if err != nil {
		return err
	}
	return r.register(method, path, e.serve)
}

// 下载(仅 params):handler 返回 CFileDownload。
// Download (params-only): the handler returns CFileDownload.
func CDownloadP[P any](r router, method, path string, h func(ctx context.Context, p P) (CFileDownload, error)) error {
	e, err := compileC[P, CNoInput](nil, func(ctx context.Context, req *Request, p P, _ CNoInput, resp *Response) error {
		dl, err := h(ctx, p)
		if err != nil {
			return err
		}
		return writeDownload(resp, dl)
	})
	if err != nil {
		return err
	}
	return r.register(method, path, e.serve)
}

// 全逃生:无需新入口 —— 直接用现有 m.RawHandle(method, path, func(ctx, *Request, *Response) error)。
// Full escape: no new entry — use the existing m.RawHandle directly.

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

func writeJSONC(resp *Response, status int, v any) error {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.WriteHeader(status)
	enc := json.NewEncoder(resp)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func writeDownload(resp *Response, dl CFileDownload) error {
	ct := dl.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	resp.Header().Set("Content-Type", ct)
	if dl.Filename != "" {
		resp.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", dl.Filename))
	}
	if dl.Size > 0 {
		resp.Header().Set("Content-Length", strconv.FormatInt(dl.Size, 10))
	}
	resp.WriteHeader(http.StatusOK)
	_, err := io.Copy(resp, dl.Content)
	return err
}
