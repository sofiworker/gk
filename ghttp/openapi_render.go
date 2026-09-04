package ghttp

import (
	"sort"
	"strconv"
)

// ===========================================================================
// spec 序列化:手工拼接 JSON,不用 encoding/json。
//
// 理由不是性能(spec 只构建一次),而是【确定性】:map 遍历顺序随机,用 map[string]any +
// json.Marshal 会让同一份路由表每次产出字节不同的 spec,无法纳入版本控制 diff、也无法
// 做契约快照测试。手工拼接则按声明顺序输出,同一路由表恒产出同一份字节。
//
// Spec serialization: JSON is assembled by hand rather than via encoding/json.
//
// The reason is not performance (the spec is built once) but DETERMINISM: map
// iteration order is randomized, so map[string]any + json.Marshal would emit
// byte-different specs for one route table, defeating version-control diffs and
// contract snapshot tests. Hand assembly emits declaration order, so one route
// table always yields identical bytes.
// ===========================================================================

// jsonWriter 是一个极简 JSON 构造器,负责逗号分隔与字符串转义,使拼接代码免于手工维护
// 分隔符状态。
// jsonWriter is a minimal JSON builder handling comma separation and string
// escaping so assembly code need not track separator state by hand.
type jsonWriter struct {
	buf []byte
	// first 记录当前容器是否尚未写入成员,决定是否需要前置逗号。
	// first records whether the current container is still empty, deciding whether
	// a leading comma is needed.
	first []bool
	// atValue 表示刚写完一个键、下一次写入处于【值位置】。值位置不加逗号——否则
	// `"type":` 后紧跟的数组/对象会被误插一个逗号。写入值后由 sep 复位。
	// atValue marks that a key was just written so the next write is at a VALUE
	// position. A value position takes no comma — otherwise an array/object right
	// after `"type":` would get a spurious comma inserted. sep resets it.
	atValue bool
}

func newJSONWriter(capacity int) *jsonWriter {
	return &jsonWriter{buf: make([]byte, 0, capacity), first: make([]bool, 0, 16)}
}

func (w *jsonWriter) openObject() {
	w.sep()
	w.buf = append(w.buf, '{')
	w.first = append(w.first, true)
}

func (w *jsonWriter) closeObject() {
	w.buf = append(w.buf, '}')
	w.first = w.first[:len(w.first)-1]
}

func (w *jsonWriter) openArray() {
	w.sep()
	w.buf = append(w.buf, '[')
	w.first = append(w.first, true)
}

func (w *jsonWriter) closeArray() {
	w.buf = append(w.buf, ']')
	w.first = w.first[:len(w.first)-1]
}

// sep 在写入一个成员前放好分隔符:值位置不加逗号,容器内的第二个及以后成员加逗号。
// sep places the separator before a member: no comma at a value position, a comma
// before the second and later members of a container.
func (w *jsonWriter) sep() {
	if w.atValue {
		w.atValue = false
		return
	}
	if n := len(w.first); n > 0 {
		if w.first[n-1] {
			w.first[n-1] = false
		} else {
			w.buf = append(w.buf, ',')
		}
	}
}

// key 写入一个对象键(含其后的冒号),并把写入位置切到值位置。
// key writes an object key including the following colon, switching the write
// position to a value position.
func (w *jsonWriter) key(k string) {
	w.sep()
	w.buf = appendJSONString(w.buf, k)
	w.buf = append(w.buf, ':')
	w.atValue = true
}

// str 写入一个字符串值(数组元素或紧随 key 的值)。
// str writes a string value (an array element or the value after a key).
func (w *jsonWriter) str(s string) {
	w.sep()
	w.buf = appendJSONString(w.buf, s)
}

// field 写入一个字符串字段(键值对)。
// field writes one string field (a key/value pair).
func (w *jsonWriter) field(k, v string) {
	w.key(k)
	w.str(v)
}

// boolField 写入一个布尔字段。
// boolField writes one boolean field.
func (w *jsonWriter) boolField(k string, v bool) {
	w.key(k)
	w.sep()
	w.buf = strconv.AppendBool(w.buf, v)
}

// renderSpec 输出完整的 OpenAPI 3.1 文档。
// renderSpec emits the complete OpenAPI 3.1 document.
func (s *Server) renderSpec(reg *schemaRegistry, items map[string]*pathItem, order []string) []byte {
	w := newJSONWriter(4096)
	w.openObject()

	// OpenAPI 3.1 与 JSON Schema 2020-12 对齐,故声明 3.1.0 而非 3.0.x。
	// OpenAPI 3.1 aligns with JSON Schema 2020-12, hence 3.1.0 rather than 3.0.x.
	w.field("openapi", "3.1.0")

	w.key("info")
	w.openObject()
	w.field("title", s.openAPI.info.Title)
	if s.openAPI.info.Description != "" {
		w.field("description", s.openAPI.info.Description)
	}
	w.field("version", s.openAPI.info.Version)
	w.closeObject()

	if len(s.openAPI.servers) > 0 {
		w.key("servers")
		w.openArray()
		for _, srv := range s.openAPI.servers {
			w.openObject()
			w.field("url", srv.URL)
			if srv.Description != "" {
				w.field("description", srv.Description)
			}
			w.closeObject()
		}
		w.closeArray()
	}

	w.key("paths")
	w.openObject()
	for _, p := range order {
		item := items[p]
		w.key(item.path)
		w.openObject()
		methods := append([]string(nil), item.methods...)
		sort.Strings(methods)
		for _, mth := range methods {
			w.key(mth)
			writeOperation(w, item.ops[mth])
		}
		w.closeObject()
	}
	w.closeObject()

	if names := reg.names(); len(names) > 0 {
		w.key("components")
		w.openObject()
		w.key("schemas")
		w.openObject()
		for _, n := range names {
			w.key(n)
			writeSchema(w, reg.defs[n])
		}
		w.closeObject()
		w.closeObject()
	}

	w.closeObject()
	return w.buf
}

// writeOperation 输出一个操作对象。
// writeOperation emits one operation object.
func writeOperation(w *jsonWriter, op *operation) {
	w.openObject()
	if len(op.tags) > 0 {
		w.key("tags")
		w.openArray()
		for _, t := range op.tags {
			w.str(t)
		}
		w.closeArray()
	}
	if op.summary != "" {
		w.field("summary", op.summary)
	}
	w.field("operationId", op.id)

	if len(op.params) > 0 {
		w.key("parameters")
		w.openArray()
		for i := range op.params {
			p := &op.params[i]
			w.openObject()
			w.field("name", p.name)
			w.field("in", p.in)
			w.boolField("required", p.required)
			if p.explode {
				w.boolField("explode", true)
			}
			w.key("schema")
			writeSchema(w, p.s)
			w.closeObject()
		}
		w.closeArray()
	}

	if op.requestBody != nil {
		w.key("requestBody")
		w.openObject()
		w.boolField("required", op.requestBody.required)
		w.key("content")
		w.openObject()
		w.key(op.requestBody.contentType)
		w.openObject()
		w.key("schema")
		writeSchema(w, op.requestBody.s)
		w.closeObject()
		w.closeObject()
		w.closeObject()
	}

	w.key("responses")
	w.openObject()
	for i := range op.responses {
		r := &op.responses[i]
		w.key(strconv.Itoa(r.status))
		w.openObject()
		w.field("description", r.description)
		if r.s != nil {
			w.key("content")
			w.openObject()
			w.key(r.contentType)
			w.openObject()
			w.key("schema")
			writeSchema(w, r.s)
			w.closeObject()
			w.closeObject()
		}
		w.closeObject()
	}
	w.closeObject()

	w.closeObject()
}

// writeSchema 输出一个 schema 节点。$ref 节点按规范只输出 $ref 自身。
// writeSchema emits one schema node. A $ref node emits only the $ref itself, per spec.
func writeSchema(w *jsonWriter, s *schema) {
	if s == nil {
		w.openObject()
		w.closeObject()
		return
	}
	w.openObject()
	if s.ref != "" {
		if s.nullable {
			// 可空引用不能直接在 $ref 同级加 type:按 JSON Schema,$ref 旁的兄弟关键字在
			// 3.1 之前会被忽略,而 3.1 虽允许兄弟关键字,组合 $ref 与 type 仍语义含混。
			// 规范做法是用 oneOf 把引用与 null 并列。此前的实现在 ref 分支直接 return,
			// nullable 信息被静默丢弃,指针字段在文档里表现为"必定非空",与实际契约相反。
			// A nullable reference cannot simply add type beside $ref: per JSON Schema,
			// keywords next to $ref were ignored before 3.1, and although 3.1 permits
			// siblings, combining $ref with type stays ambiguous. The canonical form
			// places the reference and null side by side under oneOf. The previous code
			// returned early in the ref branch, silently dropping nullability, so
			// pointer fields were documented as always present — the opposite of the
			// actual contract.
			w.key("oneOf")
			w.openArray()
			w.openObject()
			w.field("$ref", "#/components/schemas/"+s.ref)
			w.closeObject()
			w.openObject()
			w.field("type", "null")
			w.closeObject()
			w.closeArray()
			w.closeObject()
			return
		}
		w.field("$ref", "#/components/schemas/"+s.ref)
		w.closeObject()
		return
	}
	if s.typ != "" {
		if s.nullable {
			// OpenAPI 3.1:可空经 type 数组表达,而非 3.0 的 nullable 关键字。
			// OpenAPI 3.1 expresses nullability with a type array, not 3.0's nullable.
			w.key("type")
			w.openArray()
			w.str(s.typ)
			w.str("null")
			w.closeArray()
		} else {
			w.field("type", s.typ)
		}
	}
	if s.format != "" {
		w.field("format", s.format)
	}
	if s.description != "" {
		w.field("description", s.description)
	}
	if len(s.enumVals) > 0 {
		w.key("enum")
		w.openArray()
		for _, e := range s.enumVals {
			w.str(e)
		}
		w.closeArray()
	}
	if s.items != nil {
		w.key("items")
		writeSchema(w, s.items)
	}
	if len(s.properties) > 0 {
		w.key("properties")
		w.openObject()
		for _, p := range s.properties {
			w.key(p.name)
			writeSchema(w, p.s)
		}
		w.closeObject()
	}
	if len(s.required) > 0 {
		w.key("required")
		w.openArray()
		for _, r := range s.required {
			w.str(r)
		}
		w.closeArray()
	}
	if s.additional != nil {
		w.key("additionalProperties")
		writeSchema(w, s.additional)
	}
	w.closeObject()
}
