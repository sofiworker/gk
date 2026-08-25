package ghttp

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
)

// ---------------------------------------------------------------------------
// 绑定计划：注册期反射建计划，请求期跑计划。params 结构体用 tag:path/query/header 标注字段来源。
// Bind plan: registered during startup via reflection over user's params struct;
// executed at request time. Params structs use tags path:/query:/header: to annotate sources.
// ---------------------------------------------------------------------------
type bindSrc uint8

const (
	bindSrcPath bindSrc = iota
	bindSrcQuery
	bindSrcHeader
)

// bindStep 绑定计划的一步：把解析出的原始值写入目标字段。
// bindStep is one step of the bind plan: writes the parsed raw value into the
// target field.
type bindStep struct {
	fieldIndex int
	source     bindSrc
	name       string
	kind       reflect.Kind
}

// BindPlan 一个 params 结构体的绑定计划(只含 path/query/header 步)。表单与文件属于请求
// 体,不在此计划内。
// BindPlan is a bind plan for a params struct (path/query/header steps only).
// Form fields and files belong to the request body, not this plan.
type BindPlan struct {
	steps []bindStep
}

// buildBindPlan 注册期反射遍历 params 结构体建计划；无 tag 字段静默跳过。空结构体产出空计划。
// params 只承载传输层参数(path/query/header);表单文本与上传文件属于请求体,由 form
// 解码器(FormBody[T])处理,不在此绑定。
// buildBindPlan reflects over params struct at registration; untagged fields are
// skipped. An empty struct yields an empty plan. Params carry only transport
// parameters (path/query/header); form text and uploaded files belong to the
// request body and are handled by the form decoder (FormBody[T]), not here.
func buildBindPlan(t reflect.Type) (*BindPlan, error) {
	plan := &BindPlan{}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: params must be struct, got %v", ErrInvalidParam, t.Kind())
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		var src bindSrc
		var name string
		switch {
		case f.Tag.Get("path") != "":
			src, name = bindSrcPath, f.Tag.Get("path")
		case f.Tag.Get("query") != "":
			src, name = bindSrcQuery, f.Tag.Get("query")
		case f.Tag.Get("header") != "":
			src, name = bindSrcHeader, f.Tag.Get("header")
		default:
			continue // 无 path/query/header tag,静默跳过(form:/Upload 归请求体)
		}
		switch f.Type.Kind() {
		case reflect.String,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64,
			reflect.Bool:
			plan.steps = append(plan.steps, bindStep{fieldIndex: i, source: src, name: name, kind: f.Type.Kind()})
		default:
			return nil, fmt.Errorf("%w: field %q unsupported bind kind %s", ErrInvalidParam, f.Name, f.Type.Kind())
		}
	}
	return plan, nil
}

// apply 请求期跑计划：query 由调用方预解析一次传入（避免重复 URL.Query()开销）。
// apply executes the plan at request time: query pre-parsed once by caller (avoids repeated URL.Query() cost).
func (p *BindPlan) apply(req *Request, query url.Values, paramsPtr any) error {
	if len(p.steps) == 0 {
		return nil
	}
	v := reflect.ValueOf(paramsPtr).Elem()
	for _, s := range p.steps {
		var raw string
		var ok bool
		switch s.source {
		case bindSrcPath:
			raw = req.Params.Get(s.name)
			ok = raw != ""
		case bindSrcQuery:
			if vs := query[s.name]; len(vs) > 0 {
				raw, ok = vs[0], true
			}
		case bindSrcHeader:
			raw = req.Header.Get(s.name)
			ok = raw != ""
		}
		if !ok {
			// 缺失:保留零值,静默跳过。
			// Missing: keep the zero value and skip silently.
			continue
		}
		fv := v.Field(s.fieldIndex)
		if err := setScalar(fv, s.kind, raw, s.source, s.name); err != nil {
			return err
		}
	}
	return nil
}

// setScalar 把原始字符串按 kind 解析并写入字段 fv。解析失败归 ErrInvalidInput(→400)。
// setScalar parses raw per kind and writes it into field fv. A parse failure maps
// to ErrInvalidInput (→400).
func setScalar(fv reflect.Value, kind reflect.Kind, raw string, src bindSrc, name string) error {
	switch kind {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, bindSrcName(src), name, err)
		}
		if fv.OverflowInt(n) {
			return fmt.Errorf("%w: %s %q: value out of range", ErrInvalidInput, bindSrcName(src), name)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, bindSrcName(src), name, err)
		}
		if fv.OverflowUint(n) {
			return fmt.Errorf("%w: %s %q: value out of range", ErrInvalidInput, bindSrcName(src), name)
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, bindSrcName(src), name, err)
		}
		fv.SetFloat(f)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, bindSrcName(src), name, err)
		}
		fv.SetBool(b)
	}
	return nil
}

func bindSrcName(src bindSrc) string {
	switch src {
	case bindSrcPath:
		return "path"
	case bindSrcQuery:
		return "query"
	case bindSrcHeader:
		return "header"
	}
	return "?"
}
