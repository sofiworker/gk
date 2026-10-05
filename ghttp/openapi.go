package ghttp

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// openAPIVersion 是输出文档的 OpenAPI 版本。
// openAPIVersion is the OpenAPI version of the generated document.
const openAPIVersion = "3.1.0"

// openAPIConfig 是 OpenAPI 生成配置。
// openAPIConfig is the OpenAPI generation configuration.
type openAPIConfig struct {
	title       string
	version     string
	description string
	servers     []string
	includeRaw  bool
}

// OpenAPIOption 配置 OpenAPI 文档生成。
// OpenAPIOption configures OpenAPI document generation.
type OpenAPIOption func(*openAPIConfig)

// WithOpenAPIInfo 设置 info 段的标题、版本与描述。
// WithOpenAPIInfo sets the info section's title, version and description.
func WithOpenAPIInfo(title, version, description string) OpenAPIOption {
	return func(c *openAPIConfig) {
		c.title, c.version, c.description = title, version, description
	}
}

// WithOpenAPIServers 设置 servers 段的 URL 列表。
// WithOpenAPIServers sets the URLs of the servers section.
func WithOpenAPIServers(urls ...string) OpenAPIOption {
	return func(c *openAPIConfig) { c.servers = append(c.servers, urls...) }
}

// WithOpenAPIIncludeRaw 设置是否输出 Raw 路由（默认不输出，因其没有类型信息）。
// WithOpenAPIIncludeRaw sets whether Raw routes are emitted (off by default: they carry
// no type information).
func WithOpenAPIIncludeRaw(include bool) OpenAPIOption {
	return func(c *openAPIConfig) { c.includeRaw = include }
}

// oaNewConfig 应用选项得到配置，并填充默认值。
// oaNewConfig applies the options and fills in defaults.
func oaNewConfig(opts []OpenAPIOption) openAPIConfig {
	c := openAPIConfig{title: "API", version: "0.0.0"}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// OpenAPI 基于 s.Routes() 生成 OpenAPI 3.1.0 的 JSON 文档。输出是确定性的：同样的路由得到
// 完全相同的字节（map 键由 encoding/json 排序）。
// OpenAPI generates an OpenAPI 3.1.0 JSON document from s.Routes(). The output is
// deterministic: identical routes yield identical bytes (map keys are sorted by
// encoding/json).
func OpenAPI(s *Server, opts ...OpenAPIOption) ([]byte, error) {
	doc, err := OpenAPIDocument(s, opts...)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(doc, "", "  ")
}

// OpenAPIDocument 返回 OpenAPI 文档的 map 形式，便于调用方在序列化前继续修改（如补充 security）。
// 选择 map 而非结构化类型，是因为 JSON Schema 是开放形态，map 最贴近规范且无需额外类型。
// OpenAPIDocument returns the document as a map so callers can amend it (e.g. add
// security) before serializing. A map is chosen over structured types because JSON Schema
// is open-ended and a map mirrors the spec without extra types.
func OpenAPIDocument(s *Server, opts ...OpenAPIOption) (map[string]any, error) {
	if s == nil {
		return nil, fmt.Errorf("ghttp: OpenAPI requires a non-nil server")
	}
	cfg := oaNewConfig(opts)
	b := newOABuilder()
	// 先登记 ErrorResponse，保证该名称归框架所有。
	// Register ErrorResponse first so the name belongs to the framework.
	errRef := b.schema(reflect.TypeFor[ErrorResponse]())

	paths := map[string]any{}
	for _, r := range s.Routes() {
		if r.Kind == RouteRaw && !cfg.includeRaw {
			continue
		}
		method := strings.ToLower(r.Method)
		if !oaMethods[method] {
			continue
		}
		oaPath, params := oaConvertPath(r.Path)
		item, _ := paths[oaPath].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[oaPath] = item
		}
		item[method] = b.operation(r, params, errRef)
	}

	info := map[string]any{"title": cfg.title, "version": cfg.version}
	if cfg.description != "" {
		info["description"] = cfg.description
	}
	doc := map[string]any{
		"openapi":    openAPIVersion,
		"info":       info,
		"paths":      paths,
		"components": map[string]any{"schemas": b.schemas},
	}
	if len(cfg.servers) > 0 {
		servers := make([]any, 0, len(cfg.servers))
		for _, u := range cfg.servers {
			servers = append(servers, map[string]any{"url": u})
		}
		doc["servers"] = servers
	}
	return doc, nil
}

// OpenAPIRoute 返回提供 OpenAPI JSON 的 Raw GET 路由。文档在首次请求时惰性生成并缓存，
// 因此可在其他路由注册之前创建该路由；首次请求之后新增的路由不会出现在文档里。
// OpenAPIRoute returns a Raw GET route serving the OpenAPI JSON. The document is generated
// lazily on the first request and cached, so the route may be created before the other
// routes are registered; routes added after the first request are not reflected.
func OpenAPIRoute(path string, s *Server, opts ...OpenAPIOption) Route {
	var (
		once sync.Once
		body []byte
		err  error
	)
	return Raw(http.MethodGet, path, func(_ context.Context, _ *Request, resp *Response) error {
		once.Do(func() { body, err = OpenAPI(s, opts...) })
		if err != nil {
			return err
		}
		resp.Header().Set("Content-Type", "application/json")
		_, werr := resp.Write(body)
		return werr
	})
}

// oaMethods 是 OpenAPI path item 支持的方法（小写）。
// oaMethods are the methods a path item supports (lowercase).
var oaMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// oaConvertPath 把 Gin 风格路径转为 OpenAPI 路径，并按出现顺序返回 path 参数名。
// oaConvertPath converts a Gin-style path to an OpenAPI path and returns the path
// parameter names in order of appearance.
func oaConvertPath(p string) (string, []string) {
	segs := strings.Split(p, "/")
	var params []string
	for i, seg := range segs {
		switch {
		case strings.HasPrefix(seg, ":"):
			name := seg[1:]
			params = append(params, name)
			segs[i] = "{" + name + "}"
		case strings.HasPrefix(seg, "*"):
			name := seg[1:]
			if name == "" {
				name = "path"
			}
			params = append(params, name)
			segs[i] = "{" + name + "}"
		}
	}
	return strings.Join(segs, "/"), params
}

// oaBuilder 累积 components.schemas 并为命名类型分配稳定的名称。
// oaBuilder accumulates components.schemas and assigns stable names to named types.
type oaBuilder struct {
	schemas map[string]any
	names   map[reflect.Type]string
	owners  map[string]reflect.Type
}

func newOABuilder() *oaBuilder {
	return &oaBuilder{
		schemas: map[string]any{},
		names:   map[reflect.Type]string{},
		owners:  map[string]reflect.Type{},
	}
}

var (
	oaPkgPrefixRe = regexp.MustCompile(`[A-Za-z0-9_.\-]+/`)
	oaIllegalRe   = regexp.MustCompile(`[^A-Za-z0-9._\-]`)
)

// oaSanitize 把类型名转为合法的 components 键：泛型实参中的包路径只保留末段，
// 其余非法字符（方括号、逗号、空格、星号等）替换为 "_"。
// oaSanitize turns a type name into a valid components key: package paths in generic
// arguments keep only their last element and other illegal characters (brackets, commas,
// spaces, asterisks, ...) become "_".
func oaSanitize(name string) string {
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i] + oaPkgPrefixRe.ReplaceAllString(name[i:], "")
	}
	return oaIllegalRe.ReplaceAllString(name, "_")
}

// oaName 返回命名类型的 components 名称及其是否为首次分配；同名不同类型时追加包路径后缀。
// oaName returns the components name of a named type and whether it was newly assigned; a
// same-named different type gets a package path suffix.
func (b *oaBuilder) oaName(t reflect.Type) (string, bool) {
	if n, ok := b.names[t]; ok {
		return n, false
	}
	name := oaSanitize(t.Name())
	if owner, taken := b.owners[name]; taken && owner != t {
		base := name + "_" + oaSanitize(strings.ReplaceAll(t.PkgPath(), "/", "_"))
		name = base
		for n := 2; ; n++ {
			if owner, taken := b.owners[name]; !taken || owner == t {
				break
			}
			name = fmt.Sprintf("%s_%d", base, n)
		}
	}
	b.names[t] = name
	b.owners[name] = t
	return name, true
}

var (
	oaTimeType      = reflect.TypeFor[time.Time]()
	oaJSONMarshaler = reflect.TypeFor[json.Marshaler]()
	oaTextMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
	oaPkgPath       = reflect.TypeFor[Server]().PkgPath()
	oaNoContentType = reflect.TypeFor[NoContentReply]()
	oaRedirectType  = reflect.TypeFor[RedirectReply]()
	oaFileType      = reflect.TypeFor[FileReply]()
	oaStreamType    = reflect.TypeFor[StreamReply]()
)

// oaDeref 剥掉所有指针层。
// oaDeref strips every pointer layer.
func oaDeref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// oaImplements 报告 t 或 *t 是否实现了接口 iface。
// oaImplements reports whether t or *t implements iface.
func oaImplements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

// schema 返回 t 的 JSON Schema。命名结构体放入 components 并返回 $ref；指针直接展开为其指向
// 类型的 schema（不生成 type: [X, "null"]：Go 里指针字段通常用 omitempty 表达可选，
// 这样 schema 更简洁，且 $ref 无需再包 oneOf）。
// schema returns t's JSON Schema. Named structs go into components and yield a $ref;
// pointers are unwrapped to the pointee's schema (no type: [X, "null"]: pointer fields
// usually express optionality via omitempty, which keeps schemas compact and avoids
// wrapping every $ref in oneOf).
func (b *oaBuilder) schema(t reflect.Type) map[string]any {
	t = oaDeref(t)
	if t == oaTimeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Interface {
		return map[string]any{}
	}
	if oaImplements(t, oaJSONMarshaler) {
		// 自定义序列化：形态未知。
		// Custom marshaling: the shape is unknown.
		return map[string]any{}
	}
	if oaImplements(t, oaTextMarshaler) {
		return map[string]any{"type": "string"}
	}
	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer", "format": "int32"}
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer", "format": "int32", "minimum": 0}
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer", "format": "int64", "minimum": 0}
	case reflect.Float32:
		return map[string]any{"type": "number", "format": "float"}
	case reflect.Float64:
		return map[string]any{"type": "number", "format": "double"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte", "contentEncoding": "base64"}
		}
		return map[string]any{"type": "array", "items": b.schema(t.Elem())}
	case reflect.Array:
		return map[string]any{"type": "array", "items": b.schema(t.Elem())}
	case reflect.Map:
		switch t.Key().Kind() {
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return map[string]any{"type": "object", "additionalProperties": b.schema(t.Elem())}
		}
		return map[string]any{"type": "object"}
	case reflect.Struct:
		if t.Name() == "" {
			return b.structSchema(t)
		}
		name, fresh := b.oaName(t)
		if fresh {
			// 先占位再构造，递归类型因此不会死循环。
			// Reserve before building so recursive types terminate.
			b.schemas[name] = map[string]any{}
			b.schemas[name] = b.structSchema(t)
		}
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	// chan/func/complex/unsafe.Pointer 等无法序列化的类型。
	// chan/func/complex/unsafe.Pointer and friends cannot be serialized.
	return map[string]any{}
}

// oaField 是结构体展开后的一个 JSON 字段。
// oaField is one JSON field of a flattened struct.
type oaField struct {
	name     string
	typ      reflect.Type
	required bool
	asString bool
}

// structSchema 构造结构体的 object schema。
// structSchema builds a struct's object schema.
func (b *oaBuilder) structSchema(t reflect.Type) map[string]any {
	var fields []oaField
	oaCollectFields(t, map[reflect.Type]bool{}, map[string]bool{}, &fields)

	props := map[string]any{}
	var required []any
	for _, f := range fields {
		if f.asString && oaIsScalar(oaDeref(f.typ)) {
			props[f.name] = map[string]any{"type": "string"}
		} else {
			props[f.name] = b.schema(f.typ)
		}
		if f.required {
			required = append(required, f.name)
		}
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// oaIsScalar 报告 t 是否是 `json:",string"` 可作用的标量。
// oaIsScalar reports whether t is a scalar `json:",string"` applies to.
func oaIsScalar(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// oaCollectFields 收集 t 的 JSON 字段：先收集自身字段，再展开匿名嵌入结构体（外层优先，
// 与 encoding/json 的遮蔽规则一致）。
// oaCollectFields gathers t's JSON fields: own fields first, then embedded structs are
// flattened (outer wins, matching encoding/json shadowing).
func oaCollectFields(t reflect.Type, visiting map[reflect.Type]bool, seen map[string]bool, out *[]oaField) {
	if visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)

	var embedded []reflect.Type
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, optsStr, _ := strings.Cut(tag, ",")
		if sf.Anonymous && name == "" {
			ft := oaDeref(sf.Type)
			if ft.Kind() == reflect.Struct && ft != oaTimeType && !oaImplements(ft, oaJSONMarshaler) {
				embedded = append(embedded, ft)
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		f := oaField{name: name, typ: sf.Type, required: true}
		for _, o := range strings.Split(optsStr, ",") {
			switch o {
			case "omitempty", "omitzero":
				f.required = false
			case "string":
				f.asString = true
			}
		}
		*out = append(*out, f)
	}
	for _, et := range embedded {
		oaCollectFields(et, visiting, seen, out)
	}
}

// operation 构造一个路由对应的 operation 对象。
// operation builds the operation object for a route.
func (b *oaBuilder) operation(r RouteInfo, pathParams []string, errRef map[string]any) map[string]any {
	op := map[string]any{}
	if r.Doc.Summary != "" {
		op["summary"] = r.Doc.Summary
	}
	if r.Doc.Description != "" {
		op["description"] = r.Doc.Description
	}
	if r.Doc.OperationID != "" {
		op["operationId"] = r.Doc.OperationID
	}
	if len(r.Doc.Tags) > 0 {
		tags := make([]any, len(r.Doc.Tags))
		for i, t := range r.Doc.Tags {
			tags[i] = t
		}
		op["tags"] = tags
	}
	if r.Doc.Deprecated {
		op["deprecated"] = true
	}
	if len(pathParams) > 0 {
		ps := make([]any, len(pathParams))
		for i, n := range pathParams {
			ps[i] = map[string]any{
				"name": n, "in": "path", "required": true,
				"schema": map[string]any{"type": "string"},
			}
		}
		op["parameters"] = ps
	}
	if r.BodyType != nil {
		op["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{"schema": b.schema(r.BodyType)},
			},
		}
	}
	responses := b.responses(r)
	responses["default"] = map[string]any{
		"description": "Error",
		"content": map[string]any{
			"application/json": map[string]any{"schema": errRef},
		},
	}
	op["responses"] = responses
	return op
}

// responses 按路由形态构造成功响应。
// responses builds the success responses for a route's kind.
func (b *oaBuilder) responses(r RouteInfo) map[string]any {
	jsonResp := func(desc string, s map[string]any) map[string]any {
		return map[string]any{
			"description": desc,
			"content":     map[string]any{"application/json": map[string]any{"schema": s}},
		}
	}
	noContent := func() map[string]any {
		return map[string]any{"204": map[string]any{"description": "No Content"}}
	}
	switch r.Kind {
	case RouteRaw:
		return map[string]any{"200": map[string]any{"description": "Raw response"}}
	case RouteAction, RouteProcedure:
		return noContent()
	}
	if r.ResultType == nil {
		return noContent()
	}
	t := oaDeref(r.ResultType)
	if t.PkgPath() == oaPkgPath {
		switch {
		case t == oaNoContentType:
			return noContent()
		case t == oaRedirectType:
			return map[string]any{"302": map[string]any{
				"description": "Redirect (status code may be customized via RedirectReply.Status)",
				"headers": map[string]any{"Location": map[string]any{
					"schema": map[string]any{"type": "string"},
				}},
			}}
		case t == oaFileType || t == oaStreamType:
			return map[string]any{"200": map[string]any{
				"description": "Binary content",
				"content": map[string]any{"application/octet-stream": map[string]any{
					"schema": map[string]any{"type": "string", "format": "binary"},
				}},
			}}
		case strings.HasPrefix(t.Name(), "Reply["):
			if f, ok := t.FieldByName("Body"); ok {
				return map[string]any{"200": jsonResp(
					"OK (status code may be customized via Reply.Status)", b.schema(f.Type))}
			}
		}
	}
	return map[string]any{"200": jsonResp("OK", b.schema(t))}
}
