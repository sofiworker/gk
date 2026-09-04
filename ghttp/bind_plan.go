package ghttp

import (
	"fmt"
	"net/http"
	"net/textproto"
	"reflect"
	"strings"
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
	// bindSrcForm 只用于 form 请求体解码的错误定位,不参与 BindPlan(表单属于请求体)。
	// bindSrcForm only labels errors from form-body decoding; it never appears in
	// a BindPlan (forms belong to the request body).
	bindSrcForm
)

// bindMapAll 是映射字段收集"全部键"的标记名。写作 `query:"*"` 时该 map 收下所有查询
// 参数(而非仅 name[key] 形态),用于透传未知参数。
// bindMapAll marks a map field that collects EVERY key. Written as `query:"*"`,
// the map receives all query parameters (not only the name[key] shape), for
// passing through unknown parameters.
const bindMapAll = "*"

// bindStep 绑定计划的一步：把解析出的原始值写入目标字段。
// index 是到达该字段的字段索引链(支持嵌套结构体与内嵌字段),binder 是注册期编译出的赋值器。
// bindStep is one step of the bind plan: writes the parsed raw value into the
// target field. index is the field-index chain reaching that field (supporting
// nested structs and embedded fields), and binder is the setter compiled at
// registration.
type bindStep struct {
	index  []int
	source bindSrc
	name   string
	binder *fieldBinder
}

// BindPlan 一个 params 结构体的绑定计划(只含 path/query/header 步)。表单与文件属于请求
// 体,不在此计划内。
// BindPlan is a bind plan for a params struct (path/query/header steps only).
// Form fields and files belong to the request body, not this plan.
type BindPlan struct {
	steps []bindStep
	// needQuery 报告本计划是否真的读取 query；请求期据此跳过 URL.Query() 解析，使纯
	// path 端点不为 query 付费。注意:header 读取是轻量级的 Header.Get，无需特殊优化。
	// needQuery reports whether this plan actually reads query parameters; at
	// request time we skip url.ParseQuery if false, so pure-path endpoints don't
	// pay for parsing the query string. Note: header reads are lightweight
	// Header.Get calls that do not require special optimization.
	needQuery bool
	// queryMapStep 报告是否存在 map 形态的 query 步(`query:"filter"` 收 filter[k],
	// 或 `query:"*"` 收全部键)。这类步必须【枚举】所有 query 键才能收集,无法按键查找,
	// 因此它的存在会禁用惰性 query,请求期回退到 url.ParseQuery 建映射。
	// queryMapStep reports whether a map-shaped query step exists (`query:"filter"`
	// collecting filter[k], or `query:"*"` collecting every key). Such a step must
	// ENUMERATE all query keys and cannot look up by key, so its presence disables
	// lazy query and the request falls back to url.ParseQuery.
	queryMapStep bool
}

// canLazyQuery 报告本计划能否用惰性按键查找替代建 url.Values 映射。
// 前提是不含 map 形态的 query 步(那必须枚举全部键)。键数阈值在请求期另行判定,
// 因为键数是请求携带的、注册期不可知。
// canLazyQuery reports whether this plan can use lazy per-key lookup instead of
// building a url.Values map. It requires no map-shaped query step (those must
// enumerate every key). The key-count threshold is checked at request time, since
// the count comes with the request and is unknowable at registration.
func (p *BindPlan) canLazyQuery() bool { return !p.queryMapStep }

// buildBindPlan 注册期反射遍历 params 结构体建计划；无 tag 字段静默跳过。空结构体产出空计划。
// params 只承载传输层参数(path/query/header);表单文本与上传文件属于请求体,由 form
// 解码器(FormBody[T])处理,不在此绑定。
// 无 tag 的内嵌字段与结构体字段会被递归展开,因此公共参数可以抽成可复用的嵌入结构体。
// buildBindPlan reflects over params struct at registration; untagged fields are
// skipped. An empty struct yields an empty plan. Params carry only transport
// parameters (path/query/header); form text and uploaded files belong to the
// request body and are handled by the form decoder (FormBody[T]), not here.
// Untagged embedded and struct fields are expanded recursively, so shared
// parameters can be factored into reusable embedded structs.
func buildBindPlan(t reflect.Type) (*BindPlan, error) {
	plan := &BindPlan{}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: params must be struct, got %v", ErrInvalidParam, t.Kind())
	}
	if err := plan.collect(t, nil, 0); err != nil {
		return nil, err
	}
	return plan, nil
}

// collect 递归收集 t 的绑定步。prefix 是到达 t 的字段索引链,depth 防御自引用类型。
// collect recursively gathers t's bind steps. prefix is the field-index chain
// reaching t, and depth guards against self-referential types.
func (p *BindPlan) collect(t reflect.Type, prefix []int, depth int) error {
	if depth > maxBindDepth {
		return fmt.Errorf("%w: params nesting exceeds %d levels at %s", ErrInvalidParam, maxBindDepth, t)
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		src, name, tagged := bindTagOf(f)
		index := appendIndex(prefix, i)

		if tagged && name == "-" {
			// tag 值 "-" 按 encoding/json 惯例表示显式跳过该字段,使"这个字段不参与
			// 绑定"可以被明确声明,而不是靠省略 tag 隐含表达。
			// A "-" tag value means an explicit skip, per the encoding/json
			// convention, so "this field takes no part in binding" can be declared
			// outright instead of implied by omitting the tag.
			continue
		}

		if !tagged {
			// 无 path/query/header tag：结构体（或结构体指针）递归展开，使公共参数可
			// 抽成嵌入/嵌套结构体复用；其余字段静默跳过（form:/Upload 归请求体）。
			// No path/query/header tag: expand a struct (or struct pointer)
			// recursively so shared params can be reused via embedded/nested
			// structs; other fields are skipped silently (form:/Upload belong to
			// the body).
			if st, ok := structTypeOf(f.Type); ok && !implementsTextUnmarshaler(st) {
				// 未导出内嵌【指针】在此拒绝：值形态的内嵌字段 reflect 仍可写
				// （flagEmbedRO 非粘性），但指针内嵌请求期需沿途 v.Set(reflect.New(...))
				// 分配，而 Set 对未导出字段 panic。与其让一个合法的 struct 定义把每条
				// 带该 key 的请求打成 500，不如注册期就报错（同 encoding/json 立场）。
				// Reject unexported embedded POINTERS here: reflect can still write a
				// promoted field of an embedded value (flagEmbedRO is not sticky), but
				// an embedded pointer must be allocated on the way with
				// v.Set(reflect.New(...)), and Set panics on an unexported field. Better
				// to fail at registration than to 500 every request carrying that key —
				// the same stance encoding/json takes.
				if !f.IsExported() && f.Type.Kind() == reflect.Pointer {
					return fmt.Errorf("%w: embedded unexported pointer field %q cannot be bound; export it or embed the value instead", ErrInvalidParam, f.Name)
				}
				if err := p.collect(st, index, depth+1); err != nil {
					return err
				}
			}
			continue
		}

		binder, ok := newFieldBinder(f.Type)
		if !ok {
			return fmt.Errorf("%w: field %q unsupported bind type %s", ErrInvalidParam, f.Name, f.Type)
		}
		if binder.vk == vkMap && src == bindSrcPath {
			return fmt.Errorf("%w: field %q: path parameters cannot bind a map", ErrInvalidParam, f.Name)
		}
		if src == bindSrcHeader && binder.vk != vkMap {
			// 头名大小写不敏感,注册期一次性规范化,请求期直接命中 textproto 规范形式。
			// Header names are case-insensitive; canonicalize once at registration
			// so request time hits the textproto canonical form directly.
			name = textproto.CanonicalMIMEHeaderKey(name)
		}
		// 只有 query 来源需要额外标记（触发请求期解析 query）；path/header 无此需求，
		// 曾有的空 `case bindSrcHeader` 是死分支，已删。
		// Only the query source needs the extra flag (to trigger query parsing at
		// request time); path/header do not. The former empty `case bindSrcHeader`
		// was a dead branch and is gone.
		if src == bindSrcQuery {
			p.needQuery = true
			// map 形态的 query 步必须枚举全部键,无法按键查找,故禁用惰性 query。
			// A map-shaped query step must enumerate every key and cannot look up by
			// key, so it disables lazy query.
			if binder.vk == vkMap {
				p.queryMapStep = true
			}
		}
		p.steps = append(p.steps, bindStep{index: index, source: src, name: name, binder: binder})
	}
	return nil
}

// bindTagOf 读取字段的 path/query/header tag,返回来源、名字与是否带 tag。tag 值中若含逗号，
// 以逗号为界取第一部分作为参数名（允许 "name,omitempty" 等 json-style 选项共存）。
//
// A field's query/path/header tag may include commas (e.g., "name,omitempty"); this function
// takes the first comma-separated segment as the parameter name, allowing json-style options
// to coexist with ghttp's own semantics.
func bindTagOf(f reflect.StructField) (src bindSrc, name string, tagged bool) {
	var tagName, fieldTag string
	if f.Tag.Get("query") != "" {
		tagName = f.Tag.Get("query")
		fieldTag = "query"
	} else if f.Tag.Get("path") != "" {
		tagName = f.Tag.Get("path")
		fieldTag = "path"
	} else if f.Tag.Get("header") != "" {
		tagName = f.Tag.Get("header")
		fieldTag = "header"
	}
	if tagName == "" {
		return 0, "", false
	}
	name = tagName
	if i := strings.IndexByte(tagName, ','); i >= 0 {
		name = tagName[:i]
	}
	switch fieldTag {
	case "query":
		return bindSrcQuery, name, true
	case "path":
		return bindSrcPath, name, true
	case "header":
		return bindSrcHeader, name, true
	default:
		return 0, "", false
	}
}

// structTypeOf 返回 t 本身或其指向的结构体类型,并报告是否为结构体。
// structTypeOf returns t itself or the struct type it points to, reporting
// whether it is a struct at all.
func structTypeOf(t reflect.Type) (reflect.Type, bool) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		return t, true
	}
	return nil, false
}

// appendIndex 复制并追加一级字段下标。必须复制:各步共享同一 prefix 底层数组时,
// append 的原地扩展会让先前记录的索引链被后续兄弟字段覆写。
// appendIndex copies then appends one field index. Copying is required: if steps
// shared one prefix backing array, append's in-place growth would let later
// sibling fields overwrite index chains already recorded.
func appendIndex(prefix []int, i int) []int {
	out := make([]int, len(prefix)+1)
	copy(out, prefix)
	out[len(prefix)] = i
	return out
}

// fieldByIndexAlloc 沿字段索引链定位目标字段,途中遇到 nil 结构体指针即分配,
// 使 `*Nested` 形态的嵌套参数无需调用方预先构造。
// fieldByIndexAlloc walks the field-index chain to the target field, allocating
// any nil struct pointer on the way, so `*Nested` params need no pre-construction
// by the caller.
func fieldByIndexAlloc(v reflect.Value, index []int) reflect.Value {
	for depth, i := range index {
		if depth > 0 {
			if v.Kind() == reflect.Pointer {
				if v.IsNil() {
					v.Set(reflect.New(v.Type().Elem()))
				}
				v = v.Elem()
			}
		}
		v = v.Field(i)
	}
	return v
}

// apply 请求期跑计划：query 取值源由调用方构造一次传入(避免重复判定与重复解析)。
//
// 先取值、后定位字段:定位会沿途分配 nil 结构体指针,若在确认有值之前就定位,`*Nested`
// 形态的字段会被无条件分配,handler 便无法用 nil 判断"整块参数未提供"。
// apply executes the plan at request time: the query source is built once by the
// caller (avoiding repeated decisions and repeated parsing).
//
// Values are fetched BEFORE the field is located: locating allocates nil struct
// pointers on the way, so locating before confirming a value would allocate a
// `*Nested` field unconditionally and rob the handler of using nil to detect "this
// whole block was not provided".
func (p *BindPlan) apply(req *Request, query querySource, paramsPtr any) error {
	if len(p.steps) == 0 {
		return nil
	}
	v := reflect.ValueOf(paramsPtr).Elem()
	for _, s := range p.steps {
		switch s.binder.vk {
		case vkMap:
			kv := s.collectMap(req, query)
			if len(kv) == 0 {
				continue // 缺失:保留 nil 映射 / missing: keep the nil map
			}
			if err := s.binder.setMapping(fieldByIndexAlloc(v, s.index), kv, s.source, s.name); err != nil {
				return err
			}
		case vkSlice:
			raws := s.collectSlice(req, query)
			if len(raws) == 0 {
				continue // 缺失:保留 nil 切片 / missing: keep the nil slice
			}
			if err := s.binder.setSequence(fieldByIndexAlloc(v, s.index), raws, s.source, s.name); err != nil {
				return err
			}
		default:
			raw, ok := s.rawSingle(req, query)
			if !ok {
				continue // 缺失:保留零值(指针保持 nil)/ missing: keep zero (pointer stays nil)
			}
			if err := s.binder.setOne(fieldByIndexAlloc(v, s.index), raw, s.source, s.name); err != nil {
				return err
			}
		}
	}
	return nil
}

// rawSingle 取该步的单个原始值,并报告是否存在。
// rawSingle fetches this step's single raw value and reports its presence.
func (s *bindStep) rawSingle(req *Request, query querySource) (string, bool) {
	switch s.source {
	case bindSrcPath:
		raw := req.Params.Get(s.name)
		return raw, raw != ""
	case bindSrcQuery:
		return query.first(s.name)
	case bindSrcHeader:
		if vs := req.Header[s.name]; len(vs) > 0 {
			return vs[0], true
		}
	}
	return "", false
}

// collectSlice 收集该步的多值。query 与 header 支持重复出现与逗号分隔两种风格并可混用;
// path 段只能逗号分隔(一个 path 段就是一个值)。
// collectSlice gathers this step's multiple values. Query and header accept both
// repetition and comma separation, mixable; a path segment can only be
// comma-separated (one segment is one value).
func (s *bindStep) collectSlice(req *Request, query querySource) []string {
	switch s.source {
	case bindSrcPath:
		return splitList(req.Params.Get(s.name))
	case bindSrcQuery:
		return expandList(query.all(s.name))
	case bindSrcHeader:
		return expandList(req.Header[s.name])
	}
	return nil
}

// collectMap 收集 name[key] 形态(或 `*` 下的全部键)。header 来源额外去掉规范化前缀,
// 使 `header:"X-Meta"` 能收下 X-Meta-Foo 这类前缀族头。
// collectMap gathers the name[key] shape (or every key under `*`). The header source
// additionally strips a canonical prefix, so `header:"X-Meta"` can receive a prefix
// family such as X-Meta-Foo.
func (s *bindStep) collectMap(req *Request, query querySource) map[string][]string {
	switch s.source {
	case bindSrcQuery:
		return collectBracketed(query.mapValues(), s.name)
	case bindSrcHeader:
		return collectHeaderMap(req.Header, s.name)
	}
	return nil
}

// collectHeaderMap 收集头映射:`*` 收全部头,否则收 prefix- 前缀族(键为去前缀后的余部,
// 转小写以稳定)。前缀族比 name[key] 更贴合 HTTP 头惯例(如 X-Meta-User)。
// collectHeaderMap collects a header map: `*` takes every header, otherwise the
// prefix- family (keyed by the remainder, lowercased for stability). A prefix
// family fits HTTP header conventions (e.g. X-Meta-User) better than name[key].
func collectHeaderMap(h http.Header, prefix string) map[string][]string {
	if prefix == bindMapAll {
		if len(h) == 0 {
			return nil
		}
		out := make(map[string][]string, len(h))
		for k, vs := range h {
			out[strings.ToLower(k)] = vs
		}
		return out
	}
	open := textproto.CanonicalMIMEHeaderKey(prefix) + "-"
	var out map[string][]string
	for k, vs := range h {
		if len(k) <= len(open) || !strings.HasPrefix(k, open) {
			continue
		}
		if out == nil {
			out = make(map[string][]string, 4)
		}
		out[strings.ToLower(k[len(open):])] = vs
	}
	return out
}

func bindSrcName(src bindSrc) string {
	switch src {
	case bindSrcPath:
		return "path"
	case bindSrcQuery:
		return "query"
	case bindSrcHeader:
		return "header"
	case bindSrcForm:
		return "form"
	}
	return "?"
}
