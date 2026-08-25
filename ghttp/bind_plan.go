package ghttp

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// defaultMaxMultipartMemory 是解析 multipart 表单时驻留内存的上限,超出部分写入临时文件
// (与 net/http 默认 32MiB 一致)。formCodec 亦复用此常量。
// defaultMaxMultipartMemory caps the in-memory portion when parsing a multipart
// form; the rest spills to temp files (matches net/http's 32 MiB default).
// formCodec reuses this constant too.
const defaultMaxMultipartMemory = 32 << 20

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

// bindStep 绑定计划的一步：把解析出的原始值写入目标字段,并跑注册期编译好的校验规则。
// bindStep is one step of the bind plan: writes the parsed raw value into the
// target field, then runs validation rules compiled at registration.
type bindStep struct {
	fieldIndex int
	source     bindSrc
	name       string
	kind       reflect.Kind
	// rules 是注册期从 validate tag 编译的校验闭包(可空);请求期在字段赋值后依次跑。
	// rules are validation closures compiled from the validate tag at
	// registration (may be empty); run in order after the field is set.
	rules []fieldRule
	// required 表示该字段必填(validate 含 required);缺失即 ErrValidation。
	// required marks the field as mandatory (validate has required); a miss
	// yields ErrValidation.
	required bool
}

// uploadStep 绑定计划中一个 multipart 上传字段：可为单文件(Upload)或多文件([]Upload)。
// uploadStep is one multipart upload field in the bind plan: either a single
// file (Upload) or multiple files ([]Upload).
type uploadStep struct {
	fieldIndex int
	name       string
	// multi 为 true 表示字段类型是 []Upload,绑定同名的全部文件;false 表示单个 Upload。
	// multi is true when the field is []Upload, binding all files under the same
	// name; false for a single Upload.
	multi bool
	// required 为 true 时(validate 含 required)缺文件即 ErrMissingRequired;否则可选,
	// 缺失时单文件保留零值、多文件保留 nil。
	// required (validate has required) makes a missing file an ErrMissingRequired;
	// otherwise optional, leaving a zero Upload or nil slice when absent.
	required bool
}

// BindPlan 一个 params 结构体的绑定计划；uploads 承载 multipart 上传字段（无 tag 字段静默跳过）。
// BindPlan is a bind plan for a params struct; uploads carries multipart fields. Untagged fields are silently skipped.
type BindPlan struct {
	steps   []bindStep
	uploads []uploadStep
}

// buildBindPlan 注册期反射遍历 params 结构体建计划；无 tag 字段静默跳过。空结构体产出空计划。
// buildBindPlan reflects over params struct at registration; untagged fields are
// skipped. An empty struct yields an empty plan.
func buildBindPlan(t reflect.Type) (*BindPlan, error) {
	plan := &BindPlan{}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: params must be struct, got %v", ErrInvalidParam, t.Kind())
	}
	uploadType := reflect.TypeOf(Upload{})
	sliceUploadType := reflect.TypeOf([]Upload(nil))
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type == uploadType || f.Type == sliceUploadType {
			name := f.Tag.Get("form")
			if name == "" {
				name = f.Name
			}
			// upload 字段仅识别 validate 中的 required(其余标量规则对文件无意义)。
			// Upload fields only honor required in validate (other scalar rules
			// are meaningless for files).
			required := uploadRequired(f.Tag.Get("validate"))
			plan.uploads = append(plan.uploads, uploadStep{
				fieldIndex: i, name: name, multi: f.Type == sliceUploadType, required: required,
			})
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
		case reflect.String,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64,
			reflect.Bool:
			step := bindStep{fieldIndex: i, source: src, name: name, kind: f.Type.Kind()}
			required, rules, err := compileFieldRules(f.Tag.Get("validate"), f.Type.Kind())
			if err != nil {
				return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidParam, f.Name, err)
			}
			step.required, step.rules = required, rules
			plan.steps = append(plan.steps, step)
		default:
			return nil, fmt.Errorf("%w: field %q unsupported bind kind %s", ErrInvalidParam, f.Name, f.Type.Kind())
		}
	}
	return plan, nil
}

// apply 请求期跑计划：query 由调用方预解析一次传入（避免重复 URL.Query()开销）。
// apply executes the plan at request time: query pre-parsed once by caller (avoids repeated URL.Query() cost).
func (p *BindPlan) apply(req *Request, query url.Values, paramsPtr any) error {
	if len(p.steps) == 0 && len(p.uploads) == 0 {
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
			// 缺失:required 则报校验错误,否则保留零值静默跳过。
			// Missing: required yields a validation error, else keep the zero
			// value and skip silently.
			if s.required {
				return fmt.Errorf("%w: %s %q is required", ErrValidation, bindSrcName(s.source), s.name)
			}
			continue
		}
		fv := v.Field(s.fieldIndex)
		if err := setScalar(fv, s.kind, raw, s.source, s.name); err != nil {
			return err
		}
		for _, rule := range s.rules {
			if err := rule(fv); err != nil {
				return fmt.Errorf("%w: %s %q: %v", ErrValidation, bindSrcName(s.source), s.name, err)
			}
		}
	}
	for _, u := range p.uploads {
		if err := p.applyUpload(req, v, u); err != nil {
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

// uploadRequired 从 validate tag 判断 upload 字段是否必填(只认 required 项,忽略其余)。
// uploadRequired reports whether an upload field is required from its validate
// tag (honors only the required item, ignoring the rest).
func uploadRequired(tag string) bool {
	if tag == "" {
		return false
	}
	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == "required" {
			return true
		}
	}
	return false
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

// Upload 承接一个 multipart 上传文件。params 结构体放此类型字段即自动绑定;
// 放 []Upload 字段则绑定同名的全部文件(如 <input multiple>)。
// Upload receives one multipart uploaded file; include this field type in a
// params struct to auto-bind, or []Upload to bind all files under the same name
// (e.g. <input multiple>).
type Upload struct {
	// Filename 是客户端声明的原始文件名(不可信,落盘前须净化)。
	// Filename is the client-declared original file name (untrusted; sanitize
	// before persisting).
	Filename string
	// Size 是文件字节数。
	// Size is the file size in bytes.
	Size int64
	// ContentType 是该 part 的 Content-Type 头(可能为空)。
	// ContentType is the part's Content-Type header (may be empty).
	ContentType string
	// Header 是底层 multipart 文件头,含全部 part 头。
	// Header is the underlying multipart file header, carrying all part headers.
	Header *multipart.FileHeader
	// Open 打开文件内容读取;调用方负责 Close。
	// Open opens the file content for reading; the caller must Close it.
	Open func() (multipart.File, error)
}

// Bytes 读取整个上传文件内容到内存。大文件请改用 Open 流式处理以免占用过多内存。
// Bytes reads the whole uploaded file into memory. For large files prefer Open
// to stream it and avoid excessive memory use.
func (u Upload) Bytes() ([]byte, error) {
	if u.Open == nil {
		return nil, fmt.Errorf("%w: upload not bound", ErrInvalidInput)
	}
	f, err := u.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// Save 把上传文件内容写到 path(截断已存在文件)。它流式拷贝,不整体载入内存。
// path 由调用方确定并须自行净化 Filename,本方法不据 Filename 拼接路径以防目录穿越。
// Save writes the uploaded content to path (truncating an existing file). It
// streams the copy without loading the whole file into memory. The caller
// chooses path and must sanitize Filename; Save never derives the path from
// Filename, to prevent path traversal.
func (u Upload) Save(path string) (err error) {
	if u.Open == nil {
		return fmt.Errorf("%w: upload not bound", ErrInvalidInput)
	}
	src, err := u.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := dst.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(dst, src)
	return err
}

// applyUpload 请求期把一个 upload 字段(单/多文件)从 multipart 表单绑定到 params。
// applyUpload binds one upload field (single/multi) from the multipart form into
// params at request time.
func (p *BindPlan) applyUpload(req *Request, paramsVal reflect.Value, u uploadStep) error {
	// 确保 multipart 表单已解析(FormFile 内部会解析,但多文件走 MultipartForm.File)。
	// Ensure the multipart form is parsed (FormFile parses lazily, but the
	// multi-file path reads MultipartForm.File directly).
	if req.MultipartForm == nil {
		if err := req.ParseMultipartForm(defaultMaxMultipartMemory); err != nil {
			if u.required {
				return fmt.Errorf("%w: upload %q: %v", ErrMissingRequired, u.name, err)
			}
			return nil
		}
	}
	var headers []*multipart.FileHeader
	if req.MultipartForm != nil && req.MultipartForm.File != nil {
		headers = req.MultipartForm.File[u.name]
	}
	if len(headers) == 0 {
		if u.required {
			return fmt.Errorf("%w: upload %q", ErrMissingRequired, u.name)
		}
		return nil // 可选:保留零值 Upload / nil slice。Optional: keep zero Upload / nil slice.
	}
	if u.multi {
		uploads := make([]Upload, len(headers))
		for i, h := range headers {
			uploads[i] = uploadFromHeader(h)
		}
		paramsVal.Field(u.fieldIndex).Set(reflect.ValueOf(uploads))
		return nil
	}
	paramsVal.Field(u.fieldIndex).Set(reflect.ValueOf(uploadFromHeader(headers[0])))
	return nil
}

// uploadFromHeader 从 multipart 文件头构造 Upload(不打开文件,Open 惰性提供)。
// uploadFromHeader builds an Upload from a multipart file header (does not open
// the file; Open provides lazy access).
func uploadFromHeader(h *multipart.FileHeader) Upload {
	return Upload{
		Filename:    h.Filename,
		Size:        h.Size,
		ContentType: h.Header.Get("Content-Type"),
		Header:      h,
		Open:        h.Open,
	}
}
