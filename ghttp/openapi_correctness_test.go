package ghttp

import (
	"context"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// M16:OpenAPI 文档正确性
// ---------------------------------------------------------------------------

// oaErrorClash 是普通业务类型,用于泛型与引用检查。
type oaErrorClash struct {
	Detail string `json:"detail"`
}

// oaGenericPage 用于检验泛型实例的组件名净化。
type oaGenericPage[T any] struct {
	Items []T `json:"items"`
}

// oaShadowBase 与 oaShadowOuter 检验 encoding/json 的同名字段遮蔽规则。
type oaShadowBase struct {
	Name string `json:"name"`
	Tag  string `json:"tag"`
}

type oaShadowOuter struct {
	oaShadowBase
	Name string `json:"name"` // 遮蔽 base.Name
	Age  int    `json:"age"`
}

// TestOpenAPI_CatchAllPathTemplateNormalized 锁定 catch-all 路径键为合法 OpenAPI 模板。
//
// 路由层写 {fp...},但 OpenAPI 的模板变量语法只有 {fp}。原样写进 paths 键会让路径键与它
// 声明的 fp 参数对不上,整份 spec 非法,代码生成器与校验器都会拒绝。
func TestOpenAPI_CatchAllPathTemplateNormalized(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetParams(s, "/files/{fp...}", JSON[map[string]string](),
			func(_ context.Context, p struct {
				FP string `path:"fp"`
			}) (map[string]string, error) {
				return nil, nil
			}); err != nil {
			t.Fatal(err)
		}
	})
	paths := dig(t, doc, "paths").(map[string]any)
	if _, bad := paths["/files/{fp...}"]; bad {
		t.Errorf("the router's catch-all syntax leaked into the spec: %v", keysOf(paths))
	}
	item, ok := paths["/files/{fp}"].(map[string]any)
	if !ok {
		t.Fatalf("want a /files/{fp} path key, got %v", keysOf(paths))
	}
	// 路径键里的变量必须与声明的参数一一对应。
	params, _ := dig(t, item, "get", "parameters").([]any)
	var names []string
	for _, p := range params {
		if pm, ok := p.(map[string]any); ok && pm["in"] == "path" {
			names = append(names, pm["name"].(string))
		}
	}
	if !containsString(names, "fp") {
		t.Errorf("declared path params=%v, want fp to match the template variable", names)
	}
}

// TestSpecPathTemplate 直测模板归一。
func TestSpecPathTemplate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/users", "/users"},
		{"/users/{id}", "/users/{id}"},
		{"/files/{fp...}", "/files/{fp}"},
		{"/a/{x...}", "/a/{x}"},
		{"/", "/"},
	}
	for _, tc := range cases {
		if got := specPathTemplate(tc.in); got != tc.want {
			t.Errorf("specPathTemplate(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestOpenAPI_ErrorComponentNameIsReserved 锁定业务类型不能顶替框架的错误契约。
//
// errorSchema 原先硬编码组件名 "Error"。若用户业务类型也叫 Error 且先注册,
// 400/500 响应的 $ref 就指向了用户 schema——框架的错误契约被静默替换成业务结构,
// 是最难被发现的一类文档谎言。
func TestOpenAPI_ErrorComponentNameIsReserved(t *testing.T) {
	// 关键在于类型的【真实名字】就是 Error:reflect 的 t.Name() 会返回 "Error",
	// 与框架错误体的组件名正面冲突。函数内声明的具名类型同样满足这一点。
	type Error struct {
		Detail string `json:"detail"`
	}
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/e", JSONBody[Error](), JSON[Error](),
			func(_ context.Context, b Error) (Error, error) { return b, nil }); err != nil {
			t.Fatal(err)
		}
	})

	// components.schemas.Error 必须仍是框架的 {"error":{"code","message"}} 形状。
	errSchema := dig(t, doc, "components", "schemas", "Error").(map[string]any)
	props := dig(t, errSchema, "properties").(map[string]any)
	if _, ok := props["error"]; !ok {
		t.Errorf("components.schemas.Error was hijacked by a business type: %v", keysOf(props))
	}
	if _, bad := props["detail"]; bad {
		t.Error("the business type displaced the framework's error contract")
	}
	inner := dig(t, errSchema, "properties", "error").(map[string]any)
	innerProps := dig(t, inner, "properties").(map[string]any)
	for _, k := range []string{"code", "message"} {
		if _, ok := innerProps[k]; !ok {
			t.Errorf("the error schema lost its %q field", k)
		}
	}

	// 业务类型必须退让到一个带序号的名字,且仍被引用。
	schemas := dig(t, doc, "components", "schemas").(map[string]any)
	clash, ok := schemas["Error2"].(map[string]any)
	if !ok {
		t.Fatalf("the colliding business type should be renamed, schemas=%v", keysOf(schemas))
	}
	if _, ok := dig(t, clash, "properties").(map[string]any)["detail"]; !ok {
		t.Error("Error2 should carry the business type's fields")
	}

	// 400 响应必须指向框架的错误 schema,而不是业务类型。
	ref := dig(t, doc, "paths", "/e", "post", "responses", "400",
		"content", "application/json", "schema", "$ref")
	if ref != "#/components/schemas/Error" {
		t.Errorf("400 $ref=%v, want the framework error schema", ref)
	}
	// 请求体必须指向业务类型,而不是被错误 schema 顶替。
	bodyRef := dig(t, doc, "paths", "/e", "post", "requestBody",
		"content", "application/json", "schema", "$ref")
	if bodyRef != "#/components/schemas/Error2" {
		t.Errorf("requestBody $ref=%v, want the business type", bodyRef)
	}
}

// TestOpenAPI_ComponentNamesAreValid 锁定组件名合法。
//
// OpenAPI 3.1 限定组件名匹配 ^[a-zA-Z0-9._-]+$,而 Go 的泛型实例名形如
// `Page[github.com/sofiworker/gk/ghttp.User]`,含 [ / ] 三类非法字符;它们出现在 $ref 里
// 既不合规也未做 URI 转义,整份 spec 会被校验器判为非法。
func TestOpenAPI_ComponentNamesAreValid(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/g", JSONBody[oaGenericPage[oaErrorClash]](), JSON[map[string]string](),
			func(_ context.Context, b oaGenericPage[oaErrorClash]) (map[string]string, error) {
				return nil, nil
			}); err != nil {
			t.Fatal(err)
		}
	})
	schemas := dig(t, doc, "components", "schemas").(map[string]any)
	for name := range schemas {
		for i := 0; i < len(name); i++ {
			if !isComponentNameByte(name[i]) {
				t.Errorf("component name %q contains the illegal character %q", name, name[i])
				break
			}
		}
	}
	// 泛型实例确实被登记了(不是被整体丢弃)。
	found := false
	for name := range schemas {
		if strings.HasPrefix(name, "oaGenericPage") {
			found = true
		}
	}
	if !found {
		t.Errorf("the generic instantiation was not registered: %v", keysOf(schemas))
	}
	// 所有 $ref 指向的名字都必须真实存在,否则 spec 悬空。
	assertRefsResolve(t, doc, schemas)
}

// TestSanitizeComponentName 直测组件名净化。
func TestSanitizeComponentName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"User", "User"},
		{"my.Type_v1-x", "my.Type_v1-x"},
		{"Page[github.com/x/y.User]", "Page_github.com_x_y.User"},
		{"A[B]", "A_B"},
		{"[]", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := sanitizeComponentName(tc.in); got != tc.want {
			t.Errorf("sanitizeComponentName(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestOpenAPI_OperationIDsAreUnique 锁定 operationId 全局唯一。
//
// OpenAPI 要求它在整份文档内唯一,而由 method+路径生成的 id 会天然撞车:
// `GET /users/{id}` 与 `GET /users/by-id` 都归约成 getUsersById。共用一个 id 会让代码
// 生成器用后者覆盖前者,凭空丢掉一个端点。
func TestOpenAPI_OperationIDsAreUnique(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetParams(s, "/users/{id}", JSON[map[string]string](),
			func(_ context.Context, p struct {
				ID string `path:"id"`
			}) (map[string]string, error) {
				return nil, nil
			}); err != nil {
			t.Fatal(err)
		}
		if err := GetNone(s, "/users/by-id", JSON[map[string]string](),
			func(context.Context) (map[string]string, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		if err := GetNone(s, "/users/by/id", JSON[map[string]string](),
			func(context.Context) (map[string]string, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	})
	seen := map[string]string{}
	paths := dig(t, doc, "paths").(map[string]any)
	for p, item := range paths {
		im, _ := item.(map[string]any)
		for method, op := range im {
			om, _ := op.(map[string]any)
			id, _ := om["operationId"].(string)
			if id == "" {
				t.Errorf("%s %s has no operationId", method, p)
				continue
			}
			if prev, dup := seen[id]; dup {
				t.Errorf("operationId %q is shared by %q and %q %s", id, prev, p, method)
			}
			seen[id] = p
		}
	}
	if len(seen) != 3 {
		t.Errorf("got %d distinct operationIds, want 3", len(seen))
	}
}

// TestUniqueOperationID 直测去重。
func TestUniqueOperationID(t *testing.T) {
	used := map[string]bool{}
	if got := uniqueOperationID(used, "getUsers"); got != "getUsers" {
		t.Errorf("first=%q, want getUsers", got)
	}
	if got := uniqueOperationID(used, "getUsers"); got != "getUsers2" {
		t.Errorf("second=%q, want getUsers2", got)
	}
	if got := uniqueOperationID(used, "getUsers"); got != "getUsers3" {
		t.Errorf("third=%q, want getUsers3", got)
	}
	if got := uniqueOperationID(used, ""); got != "operation" {
		t.Errorf("empty id=%q, want operation", got)
	}
}

// TestOpenAPI_EmbeddedShadowingDeduplicates 锁定同名遮蔽不产生重复键。
//
// encoding/json 的浅深度优先规则让外层显式字段覆盖内嵌提升的同名字段。此前直接 append
// 会让 properties 与 required 同时出现两个 "name",那是非法的 JSON Schema。
func TestOpenAPI_EmbeddedShadowingDeduplicates(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/emb", JSONBody[oaShadowOuter](), JSON[map[string]string](),
			func(_ context.Context, b oaShadowOuter) (map[string]string, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	})
	schema := dig(t, doc, "components", "schemas", "oaShadowOuter").(map[string]any)

	// required 里不得有重复项。
	required := toStringSlice(dig(t, schema, "required"))
	count := map[string]int{}
	for _, r := range required {
		count[r]++
	}
	for name, n := range count {
		if n > 1 {
			t.Errorf("required lists %q %d times (duplicate keys are invalid JSON Schema): %v", name, n, required)
		}
	}
	// 三个字段都在,且被提升的 tag 也保留。
	props := dig(t, schema, "properties").(map[string]any)
	for _, want := range []string{"name", "tag", "age"} {
		if _, ok := props[want]; !ok {
			t.Errorf("properties missing %q: %v", want, keysOf(props))
		}
	}
	if len(props) != 3 {
		t.Errorf("got %d properties, want exactly 3: %v", len(props), keysOf(props))
	}

	// 原始 JSON 文本层面也不得有重复键——map 解码会掩盖重复。
	raw := rawSpecOf(t, func(s *Server) {
		if err := PostBody(s, "/emb", JSONBody[oaShadowOuter](), JSON[map[string]string](),
			func(_ context.Context, b oaShadowOuter) (map[string]string, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	})
	if n := strings.Count(raw, `"name"`); n > 2 {
		t.Errorf("the raw spec repeats \"name\" %d times, indicating duplicate keys:\n%s", n, raw)
	}
}

// TestOpenAPI_NullableRefUsesOneOf 锁定可空引用用 oneOf 表达。
//
// 可空引用不能在 $ref 同级加 type:JSON Schema 在 3.1 之前会忽略 $ref 的兄弟关键字,
// 3.1 虽允许但组合 $ref 与 type 语义含混。此前渲染 ref 时直接 return,nullable 被丢弃,
// 指针字段在文档里表现为"必定非空",与实际契约相反。
func TestOpenAPI_NullableRefUsesOneOf(t *testing.T) {
	type inner struct {
		V string `json:"v"`
	}
	type outer struct {
		Ptr  *inner  `json:"ptr"`
		Val  inner   `json:"val"`
		PStr *string `json:"pstr"`
	}
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/p", JSONBody[outer](), JSON[map[string]string](),
			func(_ context.Context, b outer) (map[string]string, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	})
	props := dig(t, doc, "components", "schemas", "outer", "properties").(map[string]any)

	// 指针引用:oneOf[$ref, {type:null}]。
	alts, ok := dig(t, props["ptr"], "oneOf").([]any)
	if !ok || len(alts) != 2 {
		t.Fatalf("nullable ref should be a 2-element oneOf, got %v", props["ptr"])
	}
	if got := dig(t, alts[0], "$ref"); got != "#/components/schemas/inner" {
		t.Errorf("oneOf[0] $ref=%v", got)
	}
	if got := dig(t, alts[1], "type"); got != "null" {
		t.Errorf("oneOf[1] type=%v, want null", got)
	}

	// 值类型引用:仍是裸 $ref,不该被包进 oneOf。
	if got := dig(t, props["val"], "$ref"); got != "#/components/schemas/inner" {
		t.Errorf("a non-pointer ref must stay a bare $ref, got %v", props["val"])
	}
	// 指针标量:3.1 的 type 数组形式。
	if got := dig(t, props["pstr"], "type"); !anyEquals(got, []any{"string", "null"}) {
		t.Errorf("nullable scalar type=%v, want [string null]", got)
	}
}

// TestOpenAPI_ServeWSAppearsInSpec 锁定 WebSocket 端点出现在文档里。
//
// ServeWS 原先直接调 r.register,绕过 noteRoute,于是 WS 路径从 spec 中彻底消失,
// 与 RawHandle"路径存在即出现"的口径不一致——读文档的人无从知道该端点存在。
func TestOpenAPI_ServeWSAppearsInSpec(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/typed", JSON[map[string]string](),
			func(context.Context) (map[string]string, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		if err := ServeWS(s, "/ws", nil, func(context.Context, *Request, *websocket.Conn) error {
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	paths := dig(t, doc, "paths").(map[string]any)
	if _, ok := paths["/ws"].(map[string]any); !ok {
		t.Errorf("the WebSocket endpoint is missing from the spec: %v", keysOf(paths))
	}
	// WS 端点按 GET 记录(升级请求就是 GET)。
	if _, ok := dig(t, paths["/ws"], "get").(map[string]any); !ok {
		t.Error("a WS endpoint should be documented as GET")
	}
}

// TestOpenAPI_SpecIsValidJSONAndRefsResolve 端到端确认整份 spec 自洽。
func TestOpenAPI_SpecIsValidJSONAndRefsResolve(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/a", JSONBody[oaGenericPage[oaErrorClash]](), JSON[oaShadowOuter](),
			func(_ context.Context, b oaGenericPage[oaErrorClash]) (oaShadowOuter, error) {
				return oaShadowOuter{}, nil
			}); err != nil {
			t.Fatal(err)
		}
		if err := GetParams(s, "/b/{rest...}", JSON[map[string]string](),
			func(_ context.Context, p struct {
				Rest string `path:"rest"`
			}) (map[string]string, error) {
				return nil, nil
			}); err != nil {
			t.Fatal(err)
		}
	})
	schemas, _ := dig(t, doc, "components", "schemas").(map[string]any)
	assertRefsResolve(t, doc, schemas)
	// 路径键里的模板变量必须全部被声明。
	for p, item := range dig(t, doc, "paths").(map[string]any) {
		if strings.Contains(p, "...") {
			t.Errorf("path key %q still uses the router's catch-all syntax", p)
		}
		im, _ := item.(map[string]any)
		for method, op := range im {
			om, _ := op.(map[string]any)
			declared := map[string]bool{}
			if ps, ok := om["parameters"].([]any); ok {
				for _, pp := range ps {
					if pm, ok := pp.(map[string]any); ok && pm["in"] == "path" {
						declared[pm["name"].(string)] = true
					}
				}
			}
			for _, v := range templateVars(p) {
				if !declared[v] {
					t.Errorf("%s %s: template variable %q has no path parameter declaration", method, p, v)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

// rawSpecOf 返回未解码的 spec JSON 文本,用于检查 map 解码会掩盖的重复键。
func rawSpecOf(t *testing.T, build func(*Server)) string {
	t.Helper()
	s := New(WithOpenAPI(OpenAPIInfo{Title: "T", Version: "1"}))
	build(s)
	return string(s.SpecJSON())
}

// assertRefsResolve 递归检查每个 $ref 都指向真实存在的组件。
func assertRefsResolve(t *testing.T, node any, schemas map[string]any) {
	t.Helper()
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "$ref" {
				ref, _ := child.(string)
				name := strings.TrimPrefix(ref, "#/components/schemas/")
				if name == ref {
					t.Errorf("unexpected $ref target %q", ref)
					continue
				}
				if _, ok := schemas[name]; !ok {
					t.Errorf("$ref %q is dangling: no such component", ref)
				}
				continue
			}
			assertRefsResolve(t, child, schemas)
		}
	case []any:
		for _, child := range v {
			assertRefsResolve(t, child, schemas)
		}
	}
}

// templateVars 提取路径模板里的 {name} 变量。
func templateVars(p string) []string {
	var out []string
	for {
		i := strings.IndexByte(p, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(p[i:], '}')
		if j < 0 {
			return out
		}
		out = append(out, p[i+1:i+j])
		p = p[i+j+1:]
	}
}

// anyEquals 比较一个 any 是否等于给定的 []any(用于 3.1 的 type 数组)。
func anyEquals(got any, want []any) bool {
	arr, ok := got.([]any)
	if !ok || len(arr) != len(want) {
		return false
	}
	for i := range arr {
		if arr[i] != want[i] {
			return false
		}
	}
	return true
}
