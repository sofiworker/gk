package ghttp

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
)

// defaultMaxMultipartMemory 是解析 multipart 表单时驻留内存的上限,超出部分写入临时文件
// (与 net/http 默认 32MiB 一致)。
// defaultMaxMultipartMemory caps the in-memory portion when parsing a multipart
// form; the rest spills to temp files (matches net/http's 32 MiB default).
const defaultMaxMultipartMemory = 32 << 20

// defaultMaxFormBytes 是 URL 编码表单体的读取上限，与 net/http 为
// PostFormValue/ParseForm 设的 10 MiB 一致：POST/PUT/PATCH 走 ParseForm 时已被此上限
// 保护，唯独其余方法（DELETE 等）由本包自行读体，若不设同一上限就等于“换个 method 参数
// 即可绕过限额”，让 opt-in 的 LimitBody 形同虚设。
// defaultMaxFormBytes caps a URL-encoded body at the same 10 MiB net/http applies
// to ParseForm. POST/PUT/PATCH get that protection free via ParseForm; only the
// remaining methods (DELETE, …) read the body here, so without the same cap the
// limit would be bypassable by switching the method, making the opt-in LimitBody
// moot.
const defaultMaxFormBytes = 10 << 20

// defaultMaxTextBodyBytes 是 text/plain 请求体的读取上限。textCodec 必须把整块体读进
// 内存才能交给 *string/*[]byte，而 JSON/XML 解码器是流式的（边读边解析，内存有界），
// 因此只有这一处需要额外设防。
// defaultMaxTextBodyBytes caps a text/plain body. textCodec must hold the whole
// body in memory to hand it to a *string/*[]byte, whereas the JSON/XML decoders
// stream (bounded memory), so this is the only site needing the extra guard.
const defaultMaxTextBodyBytes = 10 << 20

// readAllCapped 读取至多 limit 字节；超出即返回 ErrRequestEntityTooLarge（错误链映射
// 413），从而在解码层自身设限，不依赖调用方是否挂了 opt-in 的 LimitBody。
// readAllCapped reads at most limit bytes and returns ErrRequestEntityTooLarge
// beyond it (the error chain maps it to 413), so the cap lives in the decoder
// itself rather than depending on whether the caller mounted the opt-in LimitBody.
func readAllCapped(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrRequestEntityTooLarge
	}
	return data, nil
}

// uploadType / sliceUploadType 是 Upload 与 []Upload 的反射类型,供 form 解码识别文件字段。
// 包级缓存,避免每次解码重复 reflect.TypeOf。
// uploadType / sliceUploadType are the reflect types of Upload and []Upload, used
// by form decoding to recognize file fields. Package-level cache to avoid
// repeating reflect.TypeOf on every decode.
var (
	uploadType      = reflect.TypeOf(Upload{})
	sliceUploadType = reflect.TypeOf([]Upload(nil))
)

// ——— FormBody ——— //

// contentTypeFormURLEncoded / contentTypeMultipartForm 是表单的两个合法请求
// Content-Type。提为常量,使解码分派、严格校验声明与 OpenAPI 生成读同一份字面量。
// contentTypeFormURLEncoded / contentTypeMultipartForm are the two legal form
// request Content-Types. Hoisted to constants so decode dispatch, the strict-check
// declaration and OpenAPI generation all read one literal.
const (
	contentTypeFormURLEncoded = "application/x-www-form-urlencoded"
	contentTypeMultipartForm  = "multipart/form-data"
)

// formContentTypes 是 formCodec 声明的可接受 Content-Type 集合。包级不可变切片,使
// ContentTypes() 不必每次调用都分配。
// formContentTypes is the accepted Content-Type set formCodec declares. A
// package-level immutable slice, so ContentTypes() allocates nothing per call.
var formContentTypes = []string{contentTypeFormURLEncoded, contentTypeMultipartForm}

// formCodec 解码 application/x-www-form-urlencoded 与 multipart/form-data 到 struct:
// 文本字段(form tag)绑标量/切片/映射,Upload / []Upload 字段(form tag)绑上传文件。
// urlencoded 只有文本字段;multipart 同时含文本(MultipartForm.Value)与文件
// (MultipartForm.File)。
// formCodec decodes application/x-www-form-urlencoded and multipart/form-data
// into a struct: text fields (form tag) bind scalars/slices/maps, and Upload /
// []Upload fields (form tag) bind uploaded files. urlencoded carries text only;
// multipart carries both text (MultipartForm.Value) and files
// (MultipartForm.File).
type formCodec struct{}

// ContentType 返回 urlencoded——两个可接受类型中更常见的那个,作为只认单值契约的调用方
// (以及 OpenAPI 文档)的代表值。完整集合经 ContentTypes 声明。
// ContentType returns urlencoded — the more common of the two acceptable types — as
// the representative value for callers (and OpenAPI documents) that only understand
// the single-valued contract. The full set is declared through ContentTypes.
func (formCodec) ContentType() string { return contentTypeFormURLEncoded }

// ContentTypes 声明表单接受的两个 Content-Type,使严格校验能拒掉既非 urlencoded 也非
// multipart 的请求体(此前声明空串等于放行,非表单体会被静默解成零值结构体)。
// ContentTypes declares the two Content-Types a form accepts, letting the strict
// check reject a body that is neither urlencoded nor multipart (declaring the empty
// string previously meant "pass", so a non-form body was silently decoded into a
// zero-valued struct).
func (formCodec) ContentTypes() []string { return formContentTypes }

func (formCodec) Decode(req *Request, v any) error {
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	if mediaType(req.Header.Get("Content-Type")) == contentTypeMultipartForm {
		if err := req.ParseMultipartForm(defaultMaxMultipartMemory); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		var files map[string][]*multipart.FileHeader
		if req.MultipartForm != nil {
			files = req.MultipartForm.File
		}
		return decodeFormStruct(req.MultipartForm.Value, files, v)
	}
	values, err := parseURLEncodedBody(req)
	if err != nil {
		return err
	}
	return decodeFormStruct(values, nil, v)
}

// parseURLEncodedBody 解析 urlencoded 请求体并返回其字段值。
//
// 它不直接用 req.ParseForm() + req.PostForm:标准库只对 POST/PUT/PATCH 读取请求体填充
// PostForm,对 DELETE 等方法【只解析查询串】而把 PostForm 留成空值。而 RFC 9110 允许
// DELETE 携带请求体(本包也提供 DeleteBody/DeleteParamsBody 入口),于是
// DeleteBody + FormBody 会静默解出零值结构体——与 M9 的空 Content-Type 放行一样属于
// fail-open。故对这些方法自行读取并解析请求体。
//
// 同一坑的另一面见 middleware_csrf.go 的 csrfFormToken:那里是 multipart 体被 ParseForm
// 置为"已解析的空 PostForm"后再也补不回来。
// parseURLEncodedBody parses an urlencoded request body and returns its values.
//
// It deliberately avoids req.ParseForm() + req.PostForm: the standard library reads
// the body into PostForm only for POST/PUT/PATCH, and for methods such as DELETE it
// parses ONLY the query string, leaving PostForm empty. Yet RFC 9110 permits a body
// on DELETE (and this package exposes DeleteBody/DeleteParamsBody entries), so
// DeleteBody + FormBody silently decoded a zero-valued struct — the same fail-open
// shape as M9's empty-Content-Type pass. Such methods therefore read and parse the
// body here.
//
// For the other face of this same pitfall see csrfFormToken in middleware_csrf.go,
// where ParseForm leaves a multipart body as an "already parsed, empty" PostForm
// that can never be recovered.
func parseURLEncodedBody(req *Request) (url.Values, error) {
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		// 标准库为这些方法读体填 PostForm;走它以复用其缓存(重复调用不重读体),
		// 也让同一请求里的 CSRF 中间件与 form 解码共享一次解析结果。
		// The stdlib fills PostForm for these methods; using it reuses that cache (a
		// repeat call does not re-read the body) and lets the CSRF middleware and form
		// decoding in one request share a single parse.
		if err := req.ParseForm(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return req.PostForm, nil
	}
	// 其余方法(DELETE 等)标准库不读体,在此自行读取,故必须自己设帽:LimitBody 是
	// opt-in 中间件,没挂上的服务此前可以被一条超大 DELETE 体直接打满内存。
	// For other methods (DELETE, …) the stdlib does not read the body, so we do — and
	// must cap it here: LimitBody is opt-in, so a server without it could be driven
	// out of memory by one oversized DELETE body.
	raw, err := readAllCapped(req.Body, defaultMaxFormBytes)
	if err != nil {
		// 413 原样返回：decodeError 会把一切错误包成 ErrInvalidInput，而分类表先匹配
		// ErrInvalidInput → 400，超限就被误报成解析失败。
		// Return the 413 verbatim: decodeError wraps everything as ErrInvalidInput, and
		// the classifier matches ErrInvalidInput first, so an oversized body would be
		// misreported as a parse failure.
		return nil, err
	}
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return values, nil
}

// ---------------------------------------------------------------------------
// form 绑定计划:与 params 侧共用 fieldBinder 引擎,因此两侧支持完全相同的类型形态
// (标量、指针、TextUnmarshaler、切片/数组、map[string]T),并同样递归展开无 tag 的
// 嵌套/内嵌结构体。计划按目标类型缓存,同一 body 类型只反射分析一次。
// The form bind plan shares the fieldBinder engine with the params side, so both
// support identical shapes (scalars, pointers, TextUnmarshaler, slices/arrays,
// map[string]T) and both expand untagged nested/embedded structs recursively. The
// plan is cached per target type, so each body type is analyzed by reflection once.
// ---------------------------------------------------------------------------

// formFieldKind 区分一个 form 字段绑的是值还是上传文件。
// formFieldKind distinguishes whether a form field binds a value or an upload.
type formFieldKind uint8

const (
	ffValue       formFieldKind = iota // 文本值(经 fieldBinder)/ text value (via fieldBinder)
	ffUpload                           // 单文件 Upload / single-file Upload
	ffUploadSlice                      // 多文件 []Upload / multi-file []Upload
)

// formStep 是 form 计划的一步。
// formStep is one step of a form plan.
type formStep struct {
	index  []int
	name   string
	kind   formFieldKind
	binder *fieldBinder // 仅 ffValue 非 nil / non-nil only for ffValue
}

// formPlan 是一个 form 请求体类型的已编译绑定计划。
// formPlan is the compiled bind plan for one form body type.
type formPlan struct {
	steps []formStep
}

// formPlanCache 按目标类型缓存已编译的 form 计划。form 解码的入口是非泛型的
// RequestDecoder.Decode(req, v any),目标类型只有请求期才知道,无法像 params 那样在
// 注册期编译一次;故用 sync.Map 让每个类型只付一次反射代价,之后请求期零反射分析。
// formPlanCache caches compiled form plans per target type. Form decoding enters
// through the non-generic RequestDecoder.Decode(req, v any), so the target type is
// known only at request time and cannot be compiled once at registration as
// params are; a sync.Map therefore makes each type pay the reflection cost once,
// after which request time does no type analysis.
var formPlanCache sync.Map // reflect.Type → *formPlan

// formPlanFor 返回 t 的 form 绑定计划,首次遇到该类型时编译并缓存。
// formPlanFor returns t's form bind plan, compiling and caching it on first sight.
func formPlanFor(t reflect.Type) (*formPlan, error) {
	if p, ok := formPlanCache.Load(t); ok {
		return p.(*formPlan), nil
	}
	p := &formPlan{}
	if err := p.collect(t, nil, 0); err != nil {
		return nil, err
	}
	actual, _ := formPlanCache.LoadOrStore(t, p)
	return actual.(*formPlan), nil
}

// collect 递归收集 t 的 form 步:带 form tag 的字段编译绑定器,无 tag 的结构体字段展开。
// collect gathers t's form steps recursively: form-tagged fields compile a
// binder, untagged struct fields are expanded.
func (p *formPlan) collect(t reflect.Type, prefix []int, depth int) error {
	if depth > maxBindDepth {
		return fmt.Errorf("%w: form nesting exceeds %d levels at %s", ErrInvalidInput, maxBindDepth, t)
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		tag := f.Tag.Get("form")
		index := appendIndex(prefix, i)

		if tag == "" {
			// 无 form tag:结构体(或结构体指针)递归展开,使公共表单字段可抽成嵌入
			// 结构体复用;其余字段静默跳过。
			// No form tag: expand a struct (or struct pointer) recursively so
			// shared form fields can be reused via embedding; others are skipped.
			if st, ok := structTypeOf(f.Type); ok && !implementsTextUnmarshaler(st) &&
				st != uploadType {
				if err := p.collect(st, index, depth+1); err != nil {
					return err
				}
			}
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "-" {
			continue
		}

		switch f.Type {
		case uploadType:
			p.steps = append(p.steps, formStep{index: index, name: name, kind: ffUpload})
			continue
		case sliceUploadType:
			p.steps = append(p.steps, formStep{index: index, name: name, kind: ffUploadSlice})
			continue
		}

		binder, ok := newFieldBinder(f.Type)
		if !ok {
			return fmt.Errorf("%w: form field %q unsupported bind type %s", ErrInvalidInput, f.Name, f.Type)
		}
		p.steps = append(p.steps, formStep{index: index, name: name, kind: ffValue, binder: binder})
	}
	return nil
}

// decodeFormStruct 把表单文本值与上传文件映射到 struct 的 form tag 字段上。文本字段经
// 共享的 fieldBinder 引擎赋值(标量/指针/TextUnmarshaler/切片/映射),Upload / []Upload
// 字段从 files 绑定。文件缺失时保留零值。
// decodeFormStruct maps form text values and uploaded files onto a struct's
// form-tagged fields. Text fields are assigned through the shared fieldBinder
// engine (scalars/pointers/TextUnmarshaler/slices/maps), and Upload / []Upload
// fields bind from files. A missing file leaves the zero value.
func decodeFormStruct(values map[string][]string, files map[string][]*multipart.FileHeader, dst any) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("%w: decodeFormStruct target must be a pointer to struct, got %T", ErrInvalidInput, dst)
	}
	sv := rv.Elem()
	plan, err := formPlanFor(sv.Type())
	if err != nil {
		return err
	}
	for _, s := range plan.steps {
		switch s.kind {
		case ffUpload:
			if hs := files[s.name]; len(hs) > 0 {
				fieldByIndexAlloc(sv, s.index).Set(reflect.ValueOf(uploadFromHeader(hs[0])))
			}
		case ffUploadSlice:
			if hs := files[s.name]; len(hs) > 0 {
				ups := make([]Upload, len(hs))
				for j, h := range hs {
					ups[j] = uploadFromHeader(h)
				}
				fieldByIndexAlloc(sv, s.index).Set(reflect.ValueOf(ups))
			}
		default:
			if err := s.applyValue(values, sv); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyValue 把表单文本值写入字段:映射收 name[key] 形态(或 `*` 收全部),切片收重复
// 出现与逗号分隔两种风格,其余取首值。缺失一律保留零值。
//
// 它接收结构体根值而非目标字段:字段定位会沿途分配 nil 结构体指针,只有确认取到值后
// 才定位,`*Nested` 形态的嵌套表单块才能在整块缺省时保持 nil。
// applyValue writes form text values into the field: a map collects the
// name[key] shape (or every key under `*`), a slice accepts both repetition and
// comma separation, and others take the first value. Anything missing keeps the
// zero value.
//
// It takes the struct root rather than the target field: locating a field allocates
// nil struct pointers on the way, so locating only after a value is confirmed is what
// lets a `*Nested` form block stay nil when the whole block is absent.
func (s *formStep) applyValue(values map[string][]string, sv reflect.Value) error {
	switch s.binder.vk {
	case vkMap:
		kv := collectBracketed(values, s.name)
		if len(kv) == 0 {
			return nil
		}
		return s.binder.setMapping(fieldByIndexAlloc(sv, s.index), kv, bindSrcForm, s.name)
	case vkSlice:
		raws := expandList(values[s.name])
		if len(raws) == 0 {
			return nil
		}
		return s.binder.setSequence(fieldByIndexAlloc(sv, s.index), raws, bindSrcForm, s.name)
	default:
		vv, ok := values[s.name]
		if !ok || len(vv) == 0 {
			return nil
		}
		return s.binder.setOne(fieldByIndexAlloc(sv, s.index), vv[0], bindSrcForm, s.name)
	}
}

// ——— TextBody ——— //

// textCodec 解码任意文本请求体为 string 或 []byte。
// 目标类型(*string 或 *[]byte)在请求期根据用户类型参数自动判定。
// textCodec decodes an arbitrary text request body into a string or []byte.
// The target type (*string or *[]byte) is determined at request time by the
// user's type parameter.
type textCodec struct{}

func (textCodec) ContentType() string { return "text/plain" }

func (textCodec) Decode(req *Request, v any) error {
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	data, err := readAllCapped(req.Body, defaultMaxTextBodyBytes)
	if err != nil {
		// 同上：保留 413 语义，不被包成 400。
		// As above: preserve the 413 rather than wrapping it into a 400.
		return err
	}
	switch dst := v.(type) {
	case *string:
		*dst = string(data)
		return nil
	case *[]byte:
		*dst = data
		return nil
	default:
		return fmt.Errorf("%w: TextBody target must be *string or *[]byte, got %T", ErrInvalidInput, v)
	}
}
