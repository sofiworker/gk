package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// OpenAPI 3.1 生成测试。
//
// 核心承诺有三条,分别对应下面三组测试:
//  1. 不用则零成本:未开启时不收集元数据、不注册路由;
//  2. 文档不漂移:参数说明来自请求期同一份 BindPlan,不是另写一遍;
//  3. 输出确定:同一份路由表恒产出字节相同的 spec,可纳入版本控制与契约测试。
//
// OpenAPI 3.1 generation tests.
//
// Three core promises, matching the three groups below:
//  1. Zero cost when unused: nothing is collected and no route registered when off;
//  2. No doc drift: parameter docs come from the same BindPlan the request path uses;
//  3. Deterministic output: one route table always yields byte-identical bytes,
//     making the spec committable and contract-testable.
// ===========================================================================

type oaItem struct {
	ID     int64             `json:"id"`
	Name   string            `json:"name"`
	Tags   []string          `json:"tags,omitempty"`
	Meta   map[string]string `json:"meta,omitempty"`
	Owner  *oaUser           `json:"owner,omitempty"`
	When   time.Time         `json:"when"`
	Blob   []byte            `json:"blob,omitempty"`
	secret string            //nolint:unused // 不导出字段不应出现在 schema 里
}

type oaUser struct {
	Login string `json:"login"`
	Admin bool   `json:"admin"`
}

type oaCreate struct {
	Name string `json:"name"`
	Age  int    `json:"age,omitempty"`
}

type oaListParams struct {
	oaPage
	ID    int64             `path:"id"`
	Tags  []string          `query:"tags"`
	Meta  map[string]string `query:"meta"`
	Trace string            `header:"X-Trace-Id"`
	Since *time.Time        `query:"since"`
}

type oaPage struct {
	Page int `query:"page"`
}

// specOf 构建一个开启了 OpenAPI 的 Server,注册回调里的路由,并把 spec 解析成通用 map。
// specOf builds a Server with OpenAPI enabled, registers the callback's routes, and
// parses the spec into a generic map.
func specOf(t *testing.T, register func(s *Server), opts ...OpenAPIOption) (*Server, map[string]any) {
	t.Helper()
	s := New(WithOpenAPI(OpenAPIInfo{Title: "Test API", Version: "1.0.0"}, opts...))
	register(s)
	raw := s.SpecJSON()
	if len(raw) == 0 {
		t.Fatal("SpecJSON returned no bytes")
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("spec is not valid JSON: %v\n%s", err, raw)
	}
	return s, out
}

// dig 按路径逐级取嵌套 map/slice 的值,便于对 spec 做精确断言。
// dig walks nested maps/slices by path, making precise spec assertions readable.
func dig(t *testing.T, v any, path ...string) any {
	t.Helper()
	cur := v
	for i, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("dig: %v is not an object at step %d (%q)", cur, i, key)
		}
		cur, ok = m[key]
		if !ok {
			t.Fatalf("dig: key %q missing at step %d; available: %v", key, i, keysOf(m))
		}
	}
	return cur
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ——— 零成本:未开启时不做任何事 ——— //

func TestOpenAPI_DisabledByDefault(t *testing.T) {
	s := New()
	if err := GetNone(s, "/x", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	// 未开启:不收集元数据、SpecJSON 返回 nil。
	if s.collectDocs {
		t.Error("collectDocs must stay false without WithOpenAPI")
	}
	if len(s.docs) != 0 {
		t.Errorf("no route metadata should be collected, got %d entries", len(s.docs))
	}
	if s.SpecJSON() != nil {
		t.Error("SpecJSON should be nil without WithOpenAPI")
	}
	// 也不该注册 spec 端点。
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/openapi.json should not exist, got %d", rec.Code)
	}
}

func TestOpenAPI_EndpointServesSpec(t *testing.T) {
	s, _ := specOf(t, func(s *Server) {
		if err := GetNone(s, "/ping", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type=%q", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("endpoint body is not valid JSON: %v", err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi=%v want 3.1.0", doc["openapi"])
	}
	// spec 端点自身是元数据而非业务契约,不应出现在 paths 里。
	paths := dig(t, doc, "paths").(map[string]any)
	if _, listed := paths["/openapi.json"]; listed {
		t.Error("the spec endpoint must not document itself")
	}
}

func TestOpenAPI_CustomRoute(t *testing.T) {
	s := New(WithOpenAPI(OpenAPIInfo{Title: "T", Version: "1"}, WithOpenAPIRoute("/docs/spec.json")))
	if err := GetNone(s, "/a", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/spec.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("custom route code=%d", rec.Code)
	}
	// 默认路径应当不存在。
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("default route should be replaced, got %d", rec.Code)
	}
}

func TestOpenAPI_EmptyRouteBuildsWithoutExposing(t *testing.T) {
	// 空路径:只在内存构建 spec,不暴露端点(适合把 spec 写进构建产物而不对外提供)。
	s := New(WithOpenAPI(OpenAPIInfo{Title: "T", Version: "1"}, WithOpenAPIRoute("")))
	if err := GetNone(s, "/a", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(s.SpecJSON()) == 0 {
		t.Error("spec should still be built in memory")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no endpoint should be registered, got %d", rec.Code)
	}
}

// ——— info / servers ——— //

func TestOpenAPI_InfoAndServers(t *testing.T) {
	s := New(WithOpenAPI(
		OpenAPIInfo{Title: "Svc", Version: "2.3.4", Description: "desc"},
		WithOpenAPIServers(
			OpenAPIServer{URL: "https://a.example.com", Description: "prod"},
			OpenAPIServer{URL: "https://b.example.com"},
		),
	))
	if err := GetNone(s, "/a", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(s.SpecJSON(), &doc); err != nil {
		t.Fatal(err)
	}
	if got := dig(t, doc, "info", "title"); got != "Svc" {
		t.Errorf("title=%v", got)
	}
	if got := dig(t, doc, "info", "version"); got != "2.3.4" {
		t.Errorf("version=%v", got)
	}
	if got := dig(t, doc, "info", "description"); got != "desc" {
		t.Errorf("description=%v", got)
	}
	servers := dig(t, doc, "servers").([]any)
	if len(servers) != 2 {
		t.Fatalf("servers=%v", servers)
	}
	if got := dig(t, servers[0], "url"); got != "https://a.example.com" {
		t.Errorf("servers[0].url=%v", got)
	}
	if got := dig(t, servers[0], "description"); got != "prod" {
		t.Errorf("servers[0].description=%v", got)
	}
	// 无描述时不应输出空 description 字段。
	if m := servers[1].(map[string]any); len(m) != 1 {
		t.Errorf("servers[1] should carry only url, got %v", m)
	}
}

func TestOpenAPI_InfoDefaults(t *testing.T) {
	// OpenAPI 规范要求 title 与 version 必填,留空时必须补默认值而不是产出非法 spec。
	s := New(WithOpenAPI(OpenAPIInfo{}))
	if err := GetNone(s, "/a", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(s.SpecJSON(), &doc); err != nil {
		t.Fatal(err)
	}
	if got := dig(t, doc, "info", "title"); got != "API" {
		t.Errorf("default title=%v", got)
	}
	if got := dig(t, doc, "info", "version"); got != "0.0.0" {
		t.Errorf("default version=%v", got)
	}
}

// ——— 参数:与 BindPlan 同源 ——— //

func TestOpenAPI_Parameters(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetParams(s, "/items/{id}", JSON[oaItem](),
			func(_ context.Context, p oaListParams) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
	params := dig(t, doc, "paths", "/items/{id}", "get", "parameters").([]any)

	byName := make(map[string]map[string]any, len(params))
	for _, p := range params {
		m := p.(map[string]any)
		byName[m["name"].(string)] = m
	}

	// path 参数按规范必填。
	id := byName["id"]
	if id == nil {
		t.Fatalf("missing id parameter; got %v", keysOfAny(byName))
	}
	if id["in"] != "path" || id["required"] != true {
		t.Errorf("id param=%v", id)
	}
	if got := dig(t, id, "schema", "type"); got != "integer" {
		t.Errorf("id type=%v", got)
	}
	if got := dig(t, id, "schema", "format"); got != "int64" {
		t.Errorf("id format=%v", got)
	}

	// 切片参数:数组 schema + explode 提示。
	tags := byName["tags"]
	if tags["in"] != "query" || tags["required"] != false {
		t.Errorf("tags param=%v", tags)
	}
	if got := dig(t, tags, "schema", "type"); got != "array" {
		t.Errorf("tags type=%v", got)
	}
	if got := dig(t, tags, "schema", "items", "type"); got != "string" {
		t.Errorf("tags items=%v", got)
	}
	if tags["explode"] != true {
		t.Errorf("tags should declare explode, got %v", tags)
	}

	// map 参数:object + additionalProperties。
	if got := dig(t, byName["meta"], "schema", "type"); got != "object" {
		t.Errorf("meta type=%v", got)
	}
	if got := dig(t, byName["meta"], "schema", "additionalProperties", "type"); got != "string" {
		t.Errorf("meta additionalProperties=%v", got)
	}

	// header 参数保留规范化后的头名。
	if byName["X-Trace-Id"]["in"] != "header" {
		t.Errorf("trace param=%v", byName["X-Trace-Id"])
	}

	// 内嵌结构体的字段被提升为普通参数,与绑定行为一致。
	if byName["page"] == nil {
		t.Error("embedded struct field should surface as a parameter")
	}

	// 指针 + TextUnmarshaler:表达为字符串。
	if got := dig(t, byName["since"], "schema", "type"); got != "string" {
		t.Errorf("since type=%v", got)
	}
}

func keysOfAny(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestOpenAPI_ParametersMatchBindPlan(t *testing.T) {
	// 文档不漂移:spec 里的参数集合必须与请求期 BindPlan 的步集合一一对应。
	plan, err := buildBindPlan(reflectTypeOf(oaListParams{}))
	if err != nil {
		t.Fatal(err)
	}
	_, doc := specOf(t, func(s *Server) {
		if err := GetParams(s, "/items/{id}", JSON[oaItem](),
			func(_ context.Context, p oaListParams) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
	params := dig(t, doc, "paths", "/items/{id}", "get", "parameters").([]any)
	if len(params) != len(plan.steps) {
		t.Fatalf("spec documents %d parameters but the bind plan has %d steps", len(params), len(plan.steps))
	}
	for i, st := range plan.steps {
		got := params[i].(map[string]any)
		if got["name"] != st.name {
			t.Errorf("parameter %d name=%v want %q", i, got["name"], st.name)
		}
	}
}

// ——— 请求体与响应 ——— //

func TestOpenAPI_RequestBody(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/items", JSONBody[oaCreate](), JSON[oaItem]().Status(http.StatusCreated),
			func(_ context.Context, b oaCreate) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
	body := dig(t, doc, "paths", "/items", "post", "requestBody").(map[string]any)
	if body["required"] != true {
		t.Errorf("requestBody should be required: %v", body)
	}
	ref := dig(t, body, "content", "application/json", "schema", "$ref")
	if ref != "#/components/schemas/oaCreate" {
		t.Errorf("body $ref=%v", ref)
	}
	// 自定义状态码要落到响应键上。
	if _, ok := dig(t, doc, "paths", "/items", "post", "responses").(map[string]any)["201"]; !ok {
		t.Error("custom 201 status missing from responses")
	}
}

func TestOpenAPI_FormBodyContentType(t *testing.T) {
	type formIn struct {
		Name string `form:"name"`
	}
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/form", FormBody[formIn](), JSON[oaItem](),
			func(_ context.Context, b formIn) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
	// form 契约本身接受两种表单类型(故 ContentType() 为空),文档取更常见的 urlencoded。
	content := dig(t, doc, "paths", "/form", "post", "requestBody", "content").(map[string]any)
	if _, ok := content["application/x-www-form-urlencoded"]; !ok {
		t.Errorf("form body content=%v", keysOf(content))
	}
}

func TestOpenAPI_NoContentResponse(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := DeleteNone(s, "/items", NoContent[struct{}](),
			func(context.Context) (struct{}, error) { return struct{}{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
	resp := dig(t, doc, "paths", "/items", "delete", "responses").(map[string]any)
	got, ok := resp["204"].(map[string]any)
	if !ok {
		t.Fatalf("responses=%v", keysOf(resp))
	}
	// 204 不得声明 content——按 HTTP 语义它没有响应体。
	if _, hasContent := got["content"]; hasContent {
		t.Errorf("204 must not declare content: %v", got)
	}
}

func TestOpenAPI_ErrorResponses(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := PostParamsBody(s, "/items/{id}", JSONBody[oaCreate](), JSON[oaItem](),
			func(_ context.Context, p oaListParams, b oaCreate) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
	resp := dig(t, doc, "paths", "/items/{id}", "post", "responses").(map[string]any)
	for _, want := range []string{"200", "400", "500"} {
		if _, ok := resp[want]; !ok {
			t.Errorf("missing %s response; got %v", want, keysOf(resp))
		}
	}
	// 错误体引用统一的 Error schema,与 error_chain 实际写出的形状一致。
	ref := dig(t, resp["400"], "content", "application/json", "schema", "$ref")
	if ref != "#/components/schemas/Error" {
		t.Errorf("400 schema=%v", ref)
	}
	errSchema := dig(t, doc, "components", "schemas", "Error").(map[string]any)
	inner := dig(t, errSchema, "properties", "error", "properties").(map[string]any)
	for _, f := range []string{"code", "message"} {
		if _, ok := inner[f]; !ok {
			t.Errorf("Error body should document %q; got %v", f, keysOf(inner))
		}
	}
}

func TestOpenAPI_ErrorResponsesDisabled(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := PostBody(s, "/items", JSONBody[oaCreate](), JSON[oaItem](),
			func(_ context.Context, b oaCreate) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
	}, WithOpenAPIErrorResponses(false))
	resp := dig(t, doc, "paths", "/items", "post", "responses").(map[string]any)
	if len(resp) != 1 {
		t.Errorf("only the success response should remain, got %v", keysOf(resp))
	}
	// 关闭错误响应后不该再登记 Error schema。
	comps, ok := doc["components"].(map[string]any)
	if ok {
		if schemas, ok := comps["schemas"].(map[string]any); ok {
			if _, bad := schemas["Error"]; bad {
				t.Error("Error schema should not be registered when error responses are off")
			}
		}
	}
}

// ——— schema 生成 ——— //

func TestOpenAPI_SchemaShapes(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/item", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	item := dig(t, doc, "components", "schemas", "oaItem").(map[string]any)
	props := dig(t, item, "properties").(map[string]any)

	if got := dig(t, props["id"], "format"); got != "int64" {
		t.Errorf("id format=%v", got)
	}
	if got := dig(t, props["tags"], "type"); got != "array" {
		t.Errorf("tags type=%v", got)
	}
	if got := dig(t, props["meta"], "additionalProperties", "type"); got != "string" {
		t.Errorf("meta value type=%v", got)
	}
	// time.Time 走 date-time 而非展开其内部字段。
	if got := dig(t, props["when"], "format"); got != "date-time" {
		t.Errorf("when format=%v", got)
	}
	// []byte 在 JSON 里是 base64 字符串。
	if got := dig(t, props["blob"], "format"); got != "byte" {
		t.Errorf("blob format=%v", got)
	}
	// 不导出字段不得出现。
	if _, bad := props["secret"]; bad {
		t.Error("unexported fields must not appear in the schema")
	}
	// required 只收非指针且无 omitempty 的字段。
	req := toStringSlice(dig(t, item, "required"))
	if !containsString(req, "id") || !containsString(req, "name") || !containsString(req, "when") {
		t.Errorf("required=%v", req)
	}
	if containsString(req, "tags") || containsString(req, "owner") {
		t.Errorf("omitempty/pointer fields must not be required: %v", req)
	}
	// 嵌套具名类型提取为独立 schema 并以 $ref 引用。owner 是【指针】字段,故可空:
	// 按 OpenAPI 3.1,可空引用要写成 oneOf[$ref, {type:null}]——不能在 $ref 旁直接加 type
	// (JSON Schema 在 3.1 之前会忽略 $ref 的兄弟关键字,3.1 虽允许但语义含混)。
	// 早前的实现在渲染 ref 时直接 return,把 nullable 丢掉了,于是指针字段在文档里表现为
	// "必定非空",与实际契约相反。
	ownerAlts, ok := dig(t, props["owner"], "oneOf").([]any)
	if !ok || len(ownerAlts) != 2 {
		t.Fatalf("nullable owner should render as a 2-element oneOf, got %v", props["owner"])
	}
	if got := dig(t, ownerAlts[0], "$ref"); got != "#/components/schemas/oaUser" {
		t.Errorf("owner oneOf[0] $ref=%v", got)
	}
	if got := dig(t, ownerAlts[1], "type"); got != "null" {
		t.Errorf("owner oneOf[1] should be the null type, got %v", got)
	}
	if _, ok := dig(t, doc, "components", "schemas", "oaUser").(map[string]any); !ok {
		t.Error("nested named struct should be hoisted into components")
	}
}

func TestOpenAPI_RecursiveSchemaTerminates(t *testing.T) {
	type node struct {
		Name string `json:"name"`
		Next *node  `json:"next,omitempty"`
		Kids []node `json:"kids,omitempty"`
	}
	// 自引用类型必须靠 $ref 终止,而不是无限展开(否则构建期栈溢出)。
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/tree", JSON[node](), func(context.Context) (node, error) {
			return node{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	schema := dig(t, doc, "components", "schemas", "node").(map[string]any)
	props := dig(t, schema, "properties").(map[string]any)
	// Next 是指针,故是可空自引用:oneOf[$ref, null]。关键仍是它【以引用终止】而非展开。
	nextAlts, ok := dig(t, props["next"], "oneOf").([]any)
	if !ok || len(nextAlts) != 2 {
		t.Fatalf("nullable self reference should render as a 2-element oneOf, got %v", props["next"])
	}
	if got := dig(t, nextAlts[0], "$ref"); got != "#/components/schemas/node" {
		t.Errorf("self reference should be a $ref, got %v", got)
	}
	if got := dig(t, nextAlts[1], "type"); got != "null" {
		t.Errorf("nullable self reference should offer the null type, got %v", got)
	}
	// 切片元素是值类型(非指针),不可空,仍是裸 $ref。
	if got := dig(t, props["kids"], "items", "$ref"); got != "#/components/schemas/node" {
		t.Errorf("recursive slice element should be a $ref, got %v", got)
	}
}

func TestOpenAPI_EmbeddedJSONFieldsPromoted(t *testing.T) {
	type base struct {
		ID int64 `json:"id"`
	}
	type derived struct {
		base
		Name string `json:"name"`
	}
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/d", JSON[derived](), func(context.Context) (derived, error) {
			return derived{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	props := dig(t, doc, "components", "schemas", "derived", "properties").(map[string]any)
	// encoding/json 会把内嵌结构体的字段提升到外层,schema 必须与实际 JSON 一致。
	if _, ok := props["id"]; !ok {
		t.Errorf("embedded field should be promoted; got %v", keysOf(props))
	}
	if _, ok := props["name"]; !ok {
		t.Errorf("own field missing; got %v", keysOf(props))
	}
}

func TestOpenAPI_NullableForPointers(t *testing.T) {
	type withPtr struct {
		Opt *string `json:"opt"`
	}
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/p", JSON[withPtr](), func(context.Context) (withPtr, error) {
			return withPtr{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	// OpenAPI 3.1 用 type 数组表达可空,而非 3.0 的 nullable 关键字。
	got := dig(t, doc, "components", "schemas", "withPtr", "properties", "opt", "type")
	types := toStringSlice(got)
	if !containsString(types, "string") || !containsString(types, "null") {
		t.Errorf(`pointer field type=%v, want ["string","null"]`, got)
	}
}

// ——— tags / operationId / 分组 ——— //

func TestOpenAPI_TagsFromGroupPrefix(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		g := s.Group("/api/v1/orders")
		if err := GetNone(g, "/recent", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	tags := toStringSlice(dig(t, doc, "paths", "/api/v1/orders/recent", "get", "tags"))
	// api 与版本段不构成有意义的标签,应跳到资源名。
	if len(tags) != 1 || tags[0] != "orders" {
		t.Errorf("tags=%v want [orders]", tags)
	}
}

func TestOpenAPI_NoTagsWithoutGroup(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/ping", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	op := dig(t, doc, "paths", "/ping", "get").(map[string]any)
	if _, has := op["tags"]; has {
		t.Errorf("a root-registered route needs no tag: %v", op)
	}
}

func TestOperationID(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"GET", "/users", "getUsers"},
		{"GET", "/users/{id}", "getUsersById"},
		{"POST", "/api/v1/orders", "postApiV1Orders"},
		{"DELETE", "/", "delete"},
		{"GET", "/a-b/c_d", "getABCD"},
		{"GET", "/files/{filepath...}", "getFilesByFilepath"},
	}
	for _, tc := range tests {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			if got := operationID(tc.method, tc.path); got != tc.want {
				t.Errorf("operationID(%q,%q)=%q want %q", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestOpenAPI_MultipleMethodsOnOnePath(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/res", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := DeleteNone(s, "/res", NoContent[struct{}](), func(context.Context) (struct{}, error) {
			return struct{}{}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := PostBody(s, "/res", JSONBody[oaCreate](), JSON[oaItem](),
			func(_ context.Context, b oaCreate) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
	})
	item := dig(t, doc, "paths", "/res").(map[string]any)
	for _, m := range []string{"get", "delete", "post"} {
		if _, ok := item[m]; !ok {
			t.Errorf("missing %s under one path; got %v", m, keysOf(item))
		}
	}
}

// ——— 确定性 ——— //

func TestOpenAPI_Deterministic(t *testing.T) {
	build := func() string {
		s := New(WithOpenAPI(OpenAPIInfo{Title: "D", Version: "1"}))
		g := s.Group("/api/v1/items")
		if err := GetParams(g, "/{id}", JSON[oaItem](),
			func(_ context.Context, p oaListParams) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
		if err := PostBody(g, "", JSONBody[oaCreate](), JSON[oaItem](),
			func(_ context.Context, b oaCreate) (oaItem, error) { return oaItem{}, nil }); err != nil {
			t.Fatal(err)
		}
		if err := GetNone(s, "/health", JSON[oaUser](), func(context.Context) (oaUser, error) {
			return oaUser{}, nil
		}); err != nil {
			t.Fatal(err)
		}
		return string(s.SpecJSON())
	}
	// 手工序列化的意义就在这里:map 遍历顺序随机,若用 map[string]any + json.Marshal,
	// 同一份路由表每次的字节都不同,spec 无法纳入版本控制 diff 或做契约快照测试。
	first := build()
	for i := 0; i < 8; i++ {
		if got := build(); got != first {
			t.Fatalf("spec is not deterministic on build %d", i+2)
		}
	}
}

func TestOpenAPI_SpecCached(t *testing.T) {
	s, _ := specOf(t, func(s *Server) {
		if err := GetNone(s, "/a", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	a, b := s.SpecJSON(), s.SpecJSON()
	// 缓存命中应返回同一底层数组,而不是每次重新构建。
	if len(a) == 0 || &a[0] != &b[0] {
		t.Error("SpecJSON should return the same cached slice")
	}
}

// ——— spec 与实际路由行为一致 ——— //

func TestOpenAPI_SpecMatchesActualRouting(t *testing.T) {
	s, doc := specOf(t, func(s *Server) {
		g := s.Group("/api/v1/items")
		if err := GetParams(g, "/{id}", JSON[oaItem](),
			func(_ context.Context, p oaListParams) (oaItem, error) {
				return oaItem{ID: p.ID, Name: "x"}, nil
			}); err != nil {
			t.Fatal(err)
		}
		if err := DeleteNone(g, "/all", NoContent[struct{}](),
			func(context.Context) (struct{}, error) { return struct{}{}, nil }); err != nil {
			t.Fatal(err)
		}
	})

	// spec 里声明的每个 path+method,都必须能被真实路由命中(不是 404/405)。
	paths := dig(t, doc, "paths").(map[string]any)
	for tmpl, item := range paths {
		for method := range item.(map[string]any) {
			// 把 {id} 之类替换成具体值以构造真实请求。
			concrete := strings.NewReplacer("{id}", "7").Replace(tmpl)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(strings.ToUpper(method), concrete, nil))
			if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
				t.Errorf("spec declares %s %s but routing answered %d", method, tmpl, rec.Code)
			}
		}
	}

	// 反向:声明的成功状态码要与实际响应一致。
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/items/all", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("DELETE returned %d but spec declares 204", rec.Code)
	}
}

func TestOpenAPI_RoutesRegisteredBeforeOptionAreNotCollected(t *testing.T) {
	// WithOpenAPI 必须在注册路由之前应用(即作为 New 的 Option)。这里验证"之后开启"
	// 确实收不到已注册的路由——把这条限制固定成可测行为,避免用户误解。
	s := New()
	if err := GetNone(s, "/early", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	WithOpenAPI(OpenAPIInfo{Title: "T", Version: "1"})(s)
	if err := GetNone(s, "/late", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(s.SpecJSON(), &doc); err != nil {
		t.Fatal(err)
	}
	paths := dig(t, doc, "paths").(map[string]any)
	if _, ok := paths["/late"]; !ok {
		t.Error("routes registered after the option should be collected")
	}
	if _, ok := paths["/early"]; ok {
		t.Error("routes registered before the option cannot be collected")
	}
}

// ——— 辅助 ——— //

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
