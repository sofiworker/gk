package ghttp

import (
	"fmt"
	"mime/multipart"
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
// bindStep is one step of the bind plan: writes parsed raw value into target field.
type bindStep struct {
	fieldIndex int
	source     bindSrc
	name       string
	kind       reflect.Kind
}

// BindPlan 一个 params 结构体的绑定计划；uploadIdx>=0 表示含 multipart 上传字段（无 tag 字段静默跳过）。
// BindPlan is a bind plan for a params struct; uploadIdx>=0 marks a multipart field. Untagged fields are silently skipped.
type BindPlan struct {
	steps      []bindStep
	uploadIdx  int
	uploadName string
	isNone     bool // params类型是 NoInput(无 params 槽)
}

// buildBindPlan 注册期反射遍历 params 结构体建计划。NoInput 返回空计划；无 tag 字段静默跳过。
// buildBindPlan reflects over params struct at registration; NoInput yields empty plan; untagged fields skipped.
func buildBindPlan(t reflect.Type) (*BindPlan, error) {
	plan := &BindPlan{uploadIdx: -1}
	if t == reflect.TypeOf(noInput{}) {
		plan.isNone = true
		return plan, nil
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: params must be struct, got %v", ErrInvalidParam, t.Kind())
	}
	uploadType := reflect.TypeOf(Upload{})
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
			continue // 无绑定 tag,静默跳过
		}
		switch f.Type.Kind() {
		case reflect.String, reflect.Int, reflect.Int64, reflect.Bool:
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
	if p.isNone || (len(p.steps) == 0 && p.uploadIdx < 0) {
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
			continue
		}
		fv := v.Field(s.fieldIndex)
		switch s.kind {
		case reflect.String:
			fv.SetString(raw)
		case reflect.Int, reflect.Int64:
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, bindSrcName(s.source), s.name, err)
			}
			fv.SetInt(n)
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, bindSrcName(s.source), s.name, err)
			}
			fv.SetBool(b)
		}
	}
	if p.uploadIdx >= 0 {
		if err := p.applyUpload(req, v); err != nil {
			return err
		}
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

// noInput 是无 params 槽的占位类型（仅用于内部执行器的类型参数，入口层不暴露它）。
// noInput is a no-params placeholder used internally in executors; not exposed at entry layer.
type noInput struct{}

// Upload 承接一个 multipart 上传文件。params 结构体放此类型字段即自动绑定。
// Upload receives one multipart uploaded file; include this field type in params struct to auto-bind.
type Upload struct {
	Filename string
	Size     int64
	Header   *multipart.FileHeader
	Open     func() (multipart.File, error)
}

func (p *BindPlan) applyUpload(req *Request, paramsVal reflect.Value) error {
	f, header, err := req.FormFile(p.uploadName)
	if err != nil {
		return fmt.Errorf("%w: upload %q: %v", ErrInvalidInput, p.uploadName, err)
	}
	_ = f.Close()
	paramsVal.Field(p.uploadIdx).Set(reflect.ValueOf(Upload{
		Filename: header.Filename, Size: header.Size, Header: header, Open: header.Open,
	}))
	return nil
}
