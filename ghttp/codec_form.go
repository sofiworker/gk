package ghttp

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sofiworker/gk/ghttp/wire"
)

// DefaultFormMaxMemory 是 multipart 解析时驻留内存的默认上限（32 MiB），超出部分写入临时文件。
// DefaultFormMaxMemory is the default in-memory budget for multipart parsing (32 MiB);
// the excess spills to temporary files.
const DefaultFormMaxMemory int64 = 32 << 20

// FormTag 是表单字段映射使用的结构体标签名。
// FormTag is the struct tag name used for form field mapping.
const FormTag = "form"

// 表单输入的哨兵错误。ErrFormInvalidValue 与 ErrFormUnknownField 同时满足
// errors.Is(err, ErrInvalidInput)，映射为 400；ErrFormUnsupportedType 是编程错误（500）。
// Sentinel errors of form input. ErrFormInvalidValue and ErrFormUnknownField also satisfy
// errors.Is(err, ErrInvalidInput) and map to 400; ErrFormUnsupportedType is a programming
// error (500).
var (
	// ErrFormInvalidValue 表示字段值无法转换为目标类型。
	// ErrFormInvalidValue means a field value cannot be converted to the target type.
	ErrFormInvalidValue = fmt.Errorf("%w: invalid form value", ErrInvalidInput)

	// ErrFormUnknownField 表示严格模式下出现了结构体未声明的字段。
	// ErrFormUnknownField means an undeclared field appeared in strict mode.
	ErrFormUnknownField = fmt.Errorf("%w: unknown form field", ErrInvalidInput)

	// ErrFormUnsupportedType 表示目标类型或其字段类型不受 FormInput 支持。
	// ErrFormUnsupportedType means the target type or one of its field types is unsupported.
	ErrFormUnsupportedType = errors.New("ghttp: unsupported form target type")
)

// formConfig 保存 FormInput 的配置。
// formConfig holds FormInput configuration.
type formConfig struct {
	maxMemory int64
	strict    bool
}

// FormOption 配置 FormInput。
// FormOption configures FormInput.
type FormOption func(*formConfig)

// WithFormMaxMemory 设置 multipart 解析的内存上限（字节），默认 DefaultFormMaxMemory；n<=0 保持默认。
// 超出部分的文件内容落盘到临时文件。该值不限制请求体总大小，总大小请用 WithMaxBodyBytes / WithBodyLimit。
// WithFormMaxMemory sets the in-memory budget for multipart parsing in bytes (default
// DefaultFormMaxMemory; n<=0 keeps the default). The excess is written to temporary files.
// It does not cap the total body size; use WithMaxBodyBytes / WithBodyLimit for that.
func WithFormMaxMemory(n int64) FormOption {
	return func(c *formConfig) {
		if n > 0 {
			c.maxMemory = n
		}
	}
}

// WithFormStrict 启用严格模式：出现结构体未声明的表单字段或文件字段时返回 400。
// WithFormStrict enables strict mode: undeclared form or file fields yield 400.
func WithFormStrict() FormOption {
	return func(c *formConfig) { c.strict = true }
}

// formMediaTypes 是 FormInput 接受的 media-type 集合。
// formMediaTypes is the media-type set accepted by FormInput.
var formMediaTypes = []string{wire.ContentTypeForm, wire.ContentTypeMultipart}

// FormInput 返回表单输入，把 application/x-www-form-urlencoded 或 multipart/form-data 请求体解码为 T。
// Content-Type 缺省视为 urlencoded，其他媒体类型返回 415。
//
// 字段按 `form:"name"` 标签映射（无标签用字段名，`form:"-"` 忽略，匿名嵌入结构体展开）。支持 string、
// bool、int/uint/float 各宽度、time.Time（RFC 3339）、encoding.TextUnmarshaler 实现、以上类型的切片与指针，
// 以及文件字段 *multipart.FileHeader / []*multipart.FileHeader（仅 multipart）。T 可为结构体或结构体指针。
// 缺失字段保持零值；非字符串类型的空值（如 "age="）同样保持零值。只读取请求体，不读取 URL 查询参数。
//
// 错误：转换失败为包装 ErrFormInvalidValue（亦即 ErrInvalidInput）的 400，信息含字段名；body 超限为 413；
// 严格模式下未知字段为 400。
//
// multipart 临时文件的生命周期：超过内存上限的文件部分会落盘，由 FileHeader.Open 读取。这些临时文件由
// net/http 服务器在请求处理结束后自动清理（等价于 MultipartForm.RemoveAll），因此 handler 不得在返回后
// 继续持有 FileHeader；需要保留内容请在 handler 内复制或保存。
//
// FormInput returns a form input decoding application/x-www-form-urlencoded or
// multipart/form-data bodies into T. A missing Content-Type is treated as urlencoded; other
// media types yield 415.
//
// Fields map via `form:"name"` (field name when untagged, `form:"-"` ignored, anonymous
// embedded structs flattened). Supported: string, bool, all int/uint/float widths, time.Time
// (RFC 3339), encoding.TextUnmarshaler implementations, slices and pointers of those, plus
// file fields *multipart.FileHeader / []*multipart.FileHeader (multipart only). T may be a
// struct or a pointer to a struct. Missing fields keep their zero value; so do empty values of
// non-string types (e.g. "age="). Only the body is read, never URL query parameters.
//
// Errors: a conversion failure is a 400 wrapping ErrFormInvalidValue (hence ErrInvalidInput)
// naming the field; an oversized body is 413; unknown fields in strict mode are 400.
//
// Multipart temp-file lifecycle: file parts beyond the memory budget are spilled to disk and
// read through FileHeader.Open. The net/http server removes them after the request finishes
// (equivalent to MultipartForm.RemoveAll), so a handler must not retain a FileHeader after it
// returns; copy or persist the content inside the handler.
func FormInput[T BodyConstraint](opts ...FormOption) Input[T] {
	cfg := formConfig{maxMemory: DefaultFormMaxMemory}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	typ := reflect.TypeOf((*T)(nil)).Elem()
	return InputFunc[T](func(_ context.Context, req *Request) (T, error) {
		var v T
		ct := req.Raw.Header.Get("Content-Type")
		if !wire.ContentTypeIn(ct, formMediaTypes) {
			return v, unsupportedMediaType(wire.ContentTypeForm, ct)
		}
		plan := formPlanFor(typ)
		if plan.err != nil {
			return v, plan.err
		}
		values, files, err := formRead(req.Raw, ct, cfg.maxMemory)
		if err != nil {
			return v, err
		}
		dst := reflect.ValueOf(&v).Elem()
		if plan.ptr {
			dst.Set(reflect.New(plan.structType))
			dst = dst.Elem()
		}
		if err := plan.fill(dst, values, files, cfg.strict); err != nil {
			return v, err
		}
		return v, nil
	})
}

// formRead 读取并解析请求体，返回文本值与文件。不触碰 URL 查询参数。
// formRead reads and parses the body, returning text values and files. URL query is ignored.
func formRead(r *http.Request, ct string, maxMemory int64) (url.Values, map[string][]*multipart.FileHeader, error) {
	if wire.MediaType(ct) == wire.ContentTypeMultipart {
		if err := r.ParseMultipartForm(maxMemory); err != nil {
			return nil, nil, bodyDecodeError(err)
		}
		if r.MultipartForm == nil {
			return url.Values{}, nil, nil
		}
		return url.Values(r.MultipartForm.Value), r.MultipartForm.File, nil
	}
	if r.Body == nil || r.Body == http.NoBody {
		return url.Values{}, nil, nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, bodyDecodeError(err)
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return nil, nil, HTTPError{Status: http.StatusBadRequest, Message: "invalid request body", Cause: err}
	}
	return values, nil, nil
}

// formField 描述一个映射字段。
// formField describes one mapped field.
type formField struct {
	name  string
	index []int
	typ   reflect.Type
	file  bool // *multipart.FileHeader 或其切片 / a file header or slice of them
}

// formPlan 是某类型的缓存反射计划。
// formPlan is the cached reflection plan of a type.
type formPlan struct {
	structType reflect.Type
	ptr        bool // T 是结构体指针 / T is a pointer to struct
	fields     []formField
	byName     map[string]*formField
	err        error
}

var (
	formPlans         sync.Map // reflect.Type -> *formPlan
	formFileHeaderPtr = reflect.TypeOf((*multipart.FileHeader)(nil))
	formTextUnmarshal = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	formTimeType      = reflect.TypeOf(time.Time{})
)

// formPlanFor 返回 t 的计划，按类型缓存（包括失败结果）。
// formPlanFor returns the plan for t, cached per type (failures included).
func formPlanFor(t reflect.Type) *formPlan {
	if p, ok := formPlans.Load(t); ok {
		return p.(*formPlan)
	}
	p, _ := formPlans.LoadOrStore(t, formBuildPlan(t))
	return p.(*formPlan)
}

// formBuildPlan 解析标签并构建计划。
// formBuildPlan parses tags and builds the plan.
func formBuildPlan(t reflect.Type) *formPlan {
	p := &formPlan{byName: map[string]*formField{}}
	st := t
	if st.Kind() == reflect.Ptr {
		p.ptr = true
		st = st.Elem()
	}
	if st.Kind() != reflect.Struct {
		p.err = fmt.Errorf("%w: %s is not a struct or pointer to struct", ErrFormUnsupportedType, t)
		return p
	}
	p.structType = st
	if err := p.collect(st, nil); err != nil {
		p.err = err
		return p
	}
	for i := range p.fields {
		f := &p.fields[i]
		if _, dup := p.byName[f.name]; !dup { // 先出现者优先 / first declaration wins
			p.byName[f.name] = f
		}
	}
	return p
}

// collect 递归收集字段，嵌入结构体展开。
// collect gathers fields recursively, flattening embedded structs.
func (p *formPlan) collect(st reflect.Type, prefix []int) error {
	for i := 0; i < st.NumField(); i++ {
		sf := st.Field(i)
		tag, hasTag := sf.Tag.Lookup(FormTag)
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		if sf.Anonymous && (!hasTag || name == "") {
			et := sf.Type
			if et.Kind() == reflect.Ptr {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct && !formIsLeaf(sf.Type) {
				if !sf.IsExported() && sf.Type.Kind() == reflect.Ptr {
					continue // 无法设置未导出的嵌入指针 / cannot set an unexported embedded pointer
				}
				if err := p.collect(et, index); err != nil {
					return err
				}
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		f := formField{name: name, index: index, typ: sf.Type}
		switch {
		case sf.Type == formFileHeaderPtr ||
			(sf.Type.Kind() == reflect.Slice && sf.Type.Elem() == formFileHeaderPtr):
			f.file = true
		case !formSupported(sf.Type):
			return fmt.Errorf("%w: field %s has type %s", ErrFormUnsupportedType, sf.Name, sf.Type)
		}
		p.fields = append(p.fields, f)
	}
	return nil
}

// formIsLeaf 报告 t 是否按单个值解析（TextUnmarshaler 或 time.Time），不应展开。
// formIsLeaf reports whether t parses as a single value (TextUnmarshaler or time.Time).
func formIsLeaf(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t == formTimeType || reflect.PointerTo(t).Implements(formTextUnmarshal)
}

// formSupported 报告字段类型是否受支持（标量、指针、切片）。
// formSupported reports whether a field type is supported (scalar, pointer, slice).
func formSupported(t reflect.Type) bool {
	if t.Kind() == reflect.Slice {
		return formScalarOK(t.Elem())
	}
	return formScalarOK(t)
}

// formScalarOK 报告标量（或其单层指针）是否受支持。
// formScalarOK reports whether a scalar (or a single pointer to it) is supported.
func formScalarOK(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		if t.Kind() == reflect.Ptr {
			return false
		}
	}
	if formIsLeaf(t) {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// fill 把值与文件写入 dst（结构体值）。
// fill writes values and files into dst (a struct value).
func (p *formPlan) fill(dst reflect.Value, values url.Values, files map[string][]*multipart.FileHeader, strict bool) error {
	if strict {
		for k := range values {
			if f, ok := p.byName[k]; !ok || f.file {
				return fmt.Errorf("%w: %q", ErrFormUnknownField, k)
			}
		}
		for k := range files {
			if f, ok := p.byName[k]; !ok || !f.file {
				return fmt.Errorf("%w: %q", ErrFormUnknownField, k)
			}
		}
	}
	for i := range p.fields {
		f := &p.fields[i]
		if f.file {
			fhs := files[f.name]
			if len(fhs) == 0 {
				continue
			}
			fv := formFieldAlloc(dst, f.index)
			if f.typ.Kind() == reflect.Slice {
				fv.Set(reflect.ValueOf(append([]*multipart.FileHeader(nil), fhs...)))
			} else {
				fv.Set(reflect.ValueOf(fhs[0]))
			}
			continue
		}
		vals := values[f.name]
		if len(vals) == 0 {
			continue
		}
		fv := formFieldAlloc(dst, f.index)
		if err := formSetField(fv, vals); err != nil {
			return fmt.Errorf("%w: field %q: %v", ErrFormInvalidValue, f.name, err)
		}
	}
	return nil
}

// formFieldAlloc 沿索引取字段，途中按需分配嵌入指针。
// formFieldAlloc walks the index path, allocating embedded pointers as needed.
func formFieldAlloc(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

// formSetField 设置标量或切片字段。
// formSetField sets a scalar or slice field.
func formSetField(fv reflect.Value, vals []string) error {
	if fv.Kind() == reflect.Slice {
		out := reflect.MakeSlice(fv.Type(), 0, len(vals))
		for _, s := range vals {
			e := reflect.New(fv.Type().Elem()).Elem()
			ok, err := formSetScalar(e, s)
			if err != nil {
				return err
			}
			if ok {
				out = reflect.Append(out, e)
			}
		}
		fv.Set(out)
		return nil
	}
	_, err := formSetScalar(fv, vals[0])
	return err
}

// formSetScalar 解析 s 写入 dst；返回是否实际设置。非字符串类型的空值被跳过。
// formSetScalar parses s into dst and reports whether it was set. Empty values of
// non-string types are skipped.
func formSetScalar(dst reflect.Value, s string) (bool, error) {
	t := dst.Type()
	if t.Kind() == reflect.Ptr {
		if s == "" && t.Elem().Kind() != reflect.String {
			return false, nil
		}
		n := reflect.New(t.Elem())
		ok, err := formSetScalar(n.Elem(), s)
		if err != nil || !ok {
			return false, err
		}
		dst.Set(n)
		return true, nil
	}
	if s == "" && t.Kind() != reflect.String {
		return false, nil
	}
	if t == formTimeType {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return false, fmt.Errorf("expected RFC 3339 time: %w", err)
		}
		dst.Set(reflect.ValueOf(tm))
		return true, nil
	}
	if reflect.PointerTo(t).Implements(formTextUnmarshal) {
		if err := dst.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s)); err != nil {
			return false, err
		}
		return true, nil
	}
	switch t.Kind() {
	case reflect.String:
		dst.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return false, fmt.Errorf("expected bool: %w", err)
		}
		dst.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, t.Bits())
		if err != nil {
			return false, fmt.Errorf("expected %s: %w", t.Kind(), err)
		}
		dst.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, t.Bits())
		if err != nil {
			return false, fmt.Errorf("expected %s: %w", t.Kind(), err)
		}
		dst.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, t.Bits())
		if err != nil {
			return false, fmt.Errorf("expected %s: %w", t.Kind(), err)
		}
		dst.SetFloat(n)
	default:
		return false, fmt.Errorf("%w: %s", ErrFormUnsupportedType, t)
	}
	return true, nil
}
