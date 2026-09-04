package ghttp

import (
	"encoding"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ===========================================================================
// JSON Schema 生成:把 Go 类型映射为 OpenAPI 3.1 使用的 JSON Schema。全部工作发生在
// 注册后的 spec 构建期(一次性),请求期只读已构建好的字节。
//
// 具名结构体提取到 components/schemas 并以 $ref 引用,因此递归类型(树、链表)天然
// 终止;匿名结构体就地内联。
//
// JSON Schema generation: maps Go types to the JSON Schema used by OpenAPI 3.1.
// All work happens once during spec construction after registration; request time
// only reads the already-built bytes.
//
// Named structs are hoisted into components/schemas and referenced by $ref, so
// recursive types (trees, linked lists) terminate naturally; anonymous structs are
// inlined in place.
// ===========================================================================

// timeType 是 time.Time 的反射类型,用于映射为 date-time 格式的字符串。
// timeType is the reflect type of time.Time, mapped to a date-time string.
var timeType = reflect.TypeOf(time.Time{})

// schema 是一个 JSON Schema 节点。字段顺序即输出顺序(手工序列化,不依赖 map 遍历),
// 使同一份路由表每次都生成字节完全一致的 spec——便于把 spec 纳入版本控制与契约测试比对。
// schema is one JSON Schema node. Field order is output order (hand-serialized, not
// dependent on map iteration), so the same route table always produces a
// byte-identical spec — making it practical to commit the spec and diff it in
// contract tests.
type schema struct {
	ref         string
	typ         string
	format      string
	description string
	// nullable 走 OpenAPI 3.1 的 type 数组形式(["string","null"])而非 3.0 的
	// nullable 关键字。
	// nullable uses OpenAPI 3.1's type-array form (["string","null"]) rather than
	// 3.0's nullable keyword.
	nullable bool
	items    *schema
	// properties 保序:按结构体字段声明顺序输出,而非字母序,以贴合源码阅读顺序。
	// properties preserve order: emitted in struct field declaration order rather
	// than alphabetically, matching how the source reads.
	properties []schemaProp
	required   []string
	// additional 描述 map 的值 schema(additionalProperties)。
	// additional describes a map's value schema (additionalProperties).
	additional *schema
	// enumVals 供未来的枚举支持;当前始终为空。
	// enumVals is for future enum support; currently always empty.
	enumVals []string
}

// schemaProp 是一个对象属性(名字 + schema)。
// schemaProp is one object property (name plus schema).
type schemaProp struct {
	name string
	s    *schema
}

// schemaRegistry 在一次 spec 构建中收集具名结构体 schema,并按类型去重。
// schemaRegistry collects named struct schemas during one spec build, deduplicated
// by type.
type schemaRegistry struct {
	// byType 记录已生成的类型 → components 名字,兼作递归防护(建 schema 前先登记)。
	// byType maps an already-generated type to its components name, doubling as
	// recursion protection (registered before its schema is built).
	byType map[reflect.Type]string
	// defs 是 components/schemas 的内容,name → schema。
	// defs holds components/schemas content, name → schema.
	defs map[string]*schema
	// usedNames 防止不同包的同名类型互相覆盖(第二个得到 Name2 之类的后缀)。
	// usedNames prevents same-named types from different packages overwriting each
	// other (the second gets a Name2-style suffix).
	usedNames map[string]bool
	// errorName 是统一错误体的组件名,在注册器创建时抢先占位,确保它永不与用户类型冲突。
	// errorName is the component name of the unified error body, reserved at registry
	// creation so it can never collide with a user type.
	errorName string
}

func newSchemaRegistry() *schemaRegistry {
	r := &schemaRegistry{
		byType:    make(map[reflect.Type]string),
		defs:      make(map[string]*schema),
		usedNames: make(map[string]bool),
	}
	// 抢先占用错误体的组件名。用户业务类型若也叫 Error,uniqueName 会让它退让为 Error2,
	// 从而保证 400/500 的 $ref 始终指向框架的错误契约而非被业务结构顶替。
	// Reserve the error body's component name up front. If a user type is also named
	// Error, uniqueName yields Error2 for it, guaranteeing the 400/500 $ref always
	// points at the framework's error contract instead of being displaced by a
	// business shape.
	r.errorName = "Error"
	r.usedNames[r.errorName] = true
	return r
}

// names 返回 components/schemas 的名字,按字母序(稳定输出)。
// names returns components/schemas names in alphabetical order (stable output).
func (r *schemaRegistry) names() []string {
	out := make([]string, 0, len(r.defs))
	for n := range r.defs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// schemaFor 返回类型 t 的 schema。具名结构体提取到 components 并返回 $ref 节点;
// 其余形态就地展开。
// schemaFor returns the schema for type t. Named structs are hoisted into
// components and returned as a $ref node; other shapes are expanded in place.
func (r *schemaRegistry) schemaFor(t reflect.Type, depth int) *schema {
	if t == nil {
		return &schema{}
	}
	// 指针:解引用并标记可空(缺省值可为 null)。
	// Pointer: dereference and mark nullable (the value may be null).
	if t.Kind() == reflect.Pointer {
		s := r.schemaFor(t.Elem(), depth)
		out := *s
		out.nullable = true
		return &out
	}
	if t == timeType {
		return &schema{typ: "string", format: "date-time"}
	}
	// 实现 TextUnmarshaler 的类型按字符串表达(net.IP、自定义 ID 等)。
	// A TextUnmarshaler type is expressed as a string (net.IP, custom IDs, etc.).
	if t.Kind() != reflect.Struct && implementsTextUnmarshaler(t) {
		return &schema{typ: "string"}
	}
	if depth > maxSchemaDepth {
		return &schema{} // 空 schema = 任意值,防御异常深的类型 / empty schema = any value
	}

	switch t.Kind() {
	case reflect.Bool:
		return &schema{typ: "boolean"}
	case reflect.String:
		return &schema{typ: "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return &schema{typ: "integer", format: "int32"}
	case reflect.Int64:
		return &schema{typ: "integer", format: "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &schema{typ: "integer", format: "int32"}
	case reflect.Uint64:
		return &schema{typ: "integer", format: "int64"}
	case reflect.Float32:
		return &schema{typ: "number", format: "float"}
	case reflect.Float64:
		return &schema{typ: "number", format: "double"}

	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			// []byte 在 JSON 中是 base64 字符串。
			// []byte is a base64 string in JSON.
			return &schema{typ: "string", format: "byte"}
		}
		return &schema{typ: "array", items: r.schemaFor(t.Elem(), depth+1)}

	case reflect.Map:
		return &schema{typ: "object", additional: r.schemaFor(t.Elem(), depth+1)}

	case reflect.Struct:
		return r.structSchema(t, depth)

	case reflect.Interface:
		return &schema{} // any:不约束 / any: unconstrained
	}
	return &schema{}
}

// maxSchemaDepth 限制 schema 展开深度(具名结构体已由 $ref 终止递归,此处只防匿名嵌套)。
// maxSchemaDepth caps schema expansion depth (named structs already terminate
// recursion via $ref; this only guards anonymous nesting).
const maxSchemaDepth = 20

// structSchema 生成结构体 schema。具名类型提取到 components 后返回 $ref;匿名类型内联。
// structSchema builds a struct schema. A named type is hoisted into components and
// returned as a $ref; an anonymous type is inlined.
func (r *schemaRegistry) structSchema(t reflect.Type, depth int) *schema {
	if t.Name() == "" {
		return r.structBody(t, depth) // 匿名结构体:内联 / anonymous: inline
	}
	if name, ok := r.byType[t]; ok {
		return &schema{ref: name}
	}
	name := r.uniqueName(t)
	// 先登记再构建:自引用类型(如 Node{Next *Node})在构建其字段时会再次查到本名字,
	// 从而以 $ref 终止,不至于无限递归。
	// Register before building: a self-referential type (e.g. Node{Next *Node})
	// finds this name again while building its fields and terminates via $ref
	// instead of recursing forever.
	r.byType[t] = name
	r.defs[name] = r.structBody(t, 0)
	return &schema{ref: name}
}

// uniqueName 为类型分配 components 名字:优先用净化后的类型名,冲突时追加序号。
// uniqueName allocates a components name for a type: the sanitized type name when
// free, else a numeric suffix.
func (r *schemaRegistry) uniqueName(t reflect.Type) string {
	base := sanitizeComponentName(t.Name())
	if base == "" {
		base = "Anonymous"
	}
	name := base
	for i := 2; r.usedNames[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	r.usedNames[name] = true
	return name
}

// sanitizeComponentName 把 Go 类型名净化成合法的 OpenAPI 组件名。
//
// OpenAPI 3.1 限定组件名匹配 ^[a-zA-Z0-9._-]+$,而 Go 的泛型实例名形如
// `Page[github.com/sofiworker/gk/ghttp.User]`,含 `[`、`/`、`]` 三类非法字符;它们出现在
// `$ref` 里既不合规也未做 URI 转义,整份 spec 会被校验器判为非法。这里把非法字符统一替换
// 为 `_`,并折叠连续的 `_` 以免产生 `User___` 这类噪声名字。
// sanitizeComponentName turns a Go type name into a valid OpenAPI component name.
//
// OpenAPI 3.1 restricts component names to ^[a-zA-Z0-9._-]+$, whereas a Go generic
// instantiation is named like `Page[github.com/sofiworker/gk/ghttp.User]`, containing
// the illegal characters `[`, `/` and `]`; inside a `$ref` they are neither compliant
// nor URI-escaped, so the whole spec fails validation. Illegal characters are replaced
// with `_`, and runs of `_` are collapsed to avoid noisy names like `User___`.
func sanitizeComponentName(name string) string {
	ok := true
	for i := 0; i < len(name); i++ {
		if !isComponentNameByte(name[i]) {
			ok = false
			break
		}
	}
	if ok {
		return name
	}
	var b strings.Builder
	b.Grow(len(name))
	lastUnderscore := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isComponentNameByte(c) {
			b.WriteByte(c)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// isComponentNameByte 报告 c 是否属于 OpenAPI 组件名允许的字符集。
// isComponentNameByte reports whether c belongs to the character set OpenAPI allows in
// a component name.
func isComponentNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '.' || c == '_' || c == '-':
		return true
	default:
		return false
	}
}

// structBody 生成结构体的属性表。遵循 encoding/json 的规则:读 json tag、`-` 跳过、
// omitempty 视为可选、无 tag 的内嵌结构体提升其字段。
// structBody builds a struct's property table following encoding/json rules: read
// the json tag, skip `-`, treat omitempty as optional, and promote the fields of an
// untagged embedded struct.
func (r *schemaRegistry) structBody(t reflect.Type, depth int) *schema {
	s := &schema{typ: "object"}
	r.appendFields(s, t, depth)
	return s
}

// appendFields 把 t 的字段追加到 s(内嵌匿名结构体递归提升,与 encoding/json 一致)。
//
// 遮蔽规则:外层显式声明的字段会覆盖内嵌结构体提升上来的同名字段(encoding/json 的浅
// 深度优先规则)。因此这里先递归收集,再按"浅层优先"去重——直接 append 会让
// properties 与 required 出现重复键,那是非法的 JSON Schema。
// appendFields appends t's fields onto s, promoting embedded anonymous structs
// recursively as encoding/json does.
//
// Shadowing rule: a field declared on the outer struct overrides a same-named field
// promoted from an embedded struct (encoding/json's shallow-depth-wins rule). Fields
// are therefore collected recursively and then de-duplicated shallowest-first: a plain
// append would emit duplicate keys in properties and required, which is invalid JSON
// Schema.
func (r *schemaRegistry) appendFields(s *schema, t reflect.Type, depth int) {
	props, required := r.collectFields(t, depth, 0)
	s.properties = append(s.properties, props...)
	s.required = append(s.required, required...)
}

// collectFields 递归收集字段,返回按 json 语义去重后的属性与必填名。
// fieldDepth 是内嵌提升的层数,用于实现"浅层遮蔽深层"。
// collectFields recursively collects fields, returning properties and required names
// de-duplicated per json semantics. fieldDepth is the embedding-promotion level, used
// to implement "shallower shadows deeper".
func (r *schemaRegistry) collectFields(t reflect.Type, depth, fieldDepth int) ([]schemaProp, []string) {
	type entry struct {
		prop     schemaProp
		required bool
		depth    int
	}
	// order 保留首次出现顺序,保证 spec 输出稳定可 diff。
	// order preserves first-appearance order so the spec stays stable and diffable.
	var order []string
	seen := make(map[string]*entry)

	add := func(name string, prop schemaProp, required bool, d int) {
		if prev, ok := seen[name]; ok {
			// 已有更浅或同深度的同名字段:保留它。同深度冲突时 encoding/json 会把两者
			// 都丢弃,但对文档而言保留先出现的更有用,也不会产生重复键。
			// A same-named field at equal or shallower depth already exists: keep it.
			// encoding/json drops both on an equal-depth conflict, but for docs keeping
			// the first is more useful and still emits no duplicate key.
			if prev.depth <= d {
				return
			}
			*prev = entry{prop: prop, required: required, depth: d}
			return
		}
		seen[name] = &entry{prop: prop, required: required, depth: d}
		order = append(order, name)
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" && opts == "" {
			continue
		}
		// 无 json tag 名的内嵌结构体:字段提升到外层(encoding/json 语义)。这一步必须
		// 早于导出性检查——内嵌的【不导出结构体类型】(如 embedded base)其自身不可导出,
		// 但 encoding/json 仍会提升它导出的字段,漏掉这一分支会让 schema 少掉这些字段。
		// An embedded struct with no json name: its fields are promoted
		// (encoding/json semantics). This must precede the exportedness check: an
		// embedded UNEXPORTED struct type (e.g. embedded base) is itself
		// unexported, yet encoding/json still promotes its exported fields, and
		// skipping this branch would drop them from the schema.
		if f.Anonymous && name == "" {
			if st, ok := structTypeOf(f.Type); ok && st != timeType && !implementsTextUnmarshaler(st) {
				subProps, subRequired := r.collectFields(st, depth, fieldDepth+1)
				req := make(map[string]bool, len(subRequired))
				for _, n := range subRequired {
					req[n] = true
				}
				for _, p := range subProps {
					add(p.name, p, req[p.name], fieldDepth+1)
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fs := r.schemaFor(f.Type, depth+1)
		if d := f.Tag.Get("description"); d != "" {
			cp := *fs
			cp.description = d
			fs = &cp
		}
		// 非指针、非 omitempty 的字段视为必填:这是能从 Go 类型静态推断的最贴近的语义
		// (本框架不内置校验,故 required 只表达"结构上总会出现")。
		// A non-pointer field without omitempty counts as required: the closest
		// semantics inferable from Go types alone (the framework has no built-in
		// validation, so required only expresses "structurally always present").
		req := !strings.Contains(opts, "omitempty") && f.Type.Kind() != reflect.Pointer
		add(name, schemaProp{name: name, s: fs}, req, fieldDepth)
	}

	props := make([]schemaProp, 0, len(order))
	var required []string
	for _, n := range order {
		e := seen[n]
		props = append(props, e.prop)
		if e.required {
			required = append(required, n)
		}
	}
	return props, required
}

// paramSchemaFor 为 path/query/header 参数生成 schema。它比 body schema 简单:参数是
// 传输层标量、标量列表或 map,不含嵌套对象。
// paramSchemaFor builds the schema for a path/query/header parameter. It is simpler
// than a body schema: parameters are transport-level scalars, scalar lists, or
// maps, never nested objects.
func (r *schemaRegistry) paramSchemaFor(b *fieldBinder) *schema {
	switch b.vk {
	case vkText:
		return &schema{typ: "string"}
	case vkBytes:
		return &schema{typ: "string", format: "byte"}
	case vkSlice:
		return &schema{typ: "array", items: r.paramSchemaFor(b.elem)}
	case vkMap:
		return &schema{typ: "object", additional: r.paramSchemaFor(b.elem)}
	default:
		return scalarSchema(b.kind)
	}
}

// scalarSchema 返回标量 Kind 对应的 schema 节点。
// scalarSchema returns the schema node for a scalar Kind.
func scalarSchema(k reflect.Kind) *schema {
	switch k {
	case reflect.Bool:
		return &schema{typ: "boolean"}
	case reflect.Int64, reflect.Uint64:
		return &schema{typ: "integer", format: "int64"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &schema{typ: "integer", format: "int32"}
	case reflect.Float32:
		return &schema{typ: "number", format: "float"}
	case reflect.Float64:
		return &schema{typ: "number", format: "double"}
	default:
		return &schema{typ: "string"}
	}
}

// 确保 encoding 被引用(implementsTextUnmarshaler 在 bind_value.go,此处仅类型断言用)。
// Keep encoding referenced (implementsTextUnmarshaler lives in bind_value.go; this
// is only a type assertion aid).
var _ encoding.TextUnmarshaler = (*time.Time)(nil)
