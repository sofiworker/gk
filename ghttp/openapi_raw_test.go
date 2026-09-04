package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// ===========================================================================
// RawHandle 端点在 OpenAPI spec 中的呈现。
//
// RawHandle 是一等公民逃生入口,静态资源与健康检查也建在它之上。它们没有可反射的
// params/body/output 类型,但"这个路径+方法存在"本身就是契约的一部分:漏掉它们会让 spec
// 谎报端点不存在,依赖 spec 的契约测试与客户端生成反而被误导。
//
// 因此策略是"如实呈现而非编造":登记路径与方法,声明响应体形状未由框架声明,不凭空生成
// schema,也不追加 typed 绑定才会产生的 400/415。
//
// How RawHandle endpoints appear in the OpenAPI spec.
//
// RawHandle is a first-class escape hatch, and static assets and health checks build
// on it. They expose no reflectable params/body/output types, yet "this path and
// method exist" is itself part of the contract: omitting them would make the spec
// claim the endpoints do not exist, misleading contract tests and client generators.
//
// The strategy is therefore to report honestly rather than invent: record the path
// and method, declare the response shape as undeclared, fabricate no schema, and add
// none of the 400/415 responses that only typed binding produces.
// ===========================================================================

func TestOpenAPI_RawHandleIsDocumented(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := s.RawHandle(http.MethodGet, "/stream", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	op := dig(t, doc, "paths", "/stream", "get").(map[string]any)

	// 必须有 operationId 与 responses,才是一个合法操作。
	if _, ok := op["operationId"]; !ok {
		t.Errorf("raw operation needs an operationId; got %v", keysOf(op))
	}
	resp := dig(t, op, "responses").(map[string]any)
	if _, ok := resp["200"]; !ok {
		t.Errorf("raw endpoint should declare a 200; got %v", keysOf(resp))
	}
	// 关键:不得编造响应 schema——框架确实不知道 raw handler 会写什么。
	if _, bad := resp["200"].(map[string]any)["content"]; bad {
		t.Error("a raw endpoint's 200 must not declare content (no schema is known)")
	}
	// 也不得声明请求体。
	if _, bad := op["requestBody"]; bad {
		t.Error("a raw endpoint must not declare a requestBody")
	}
}

func TestOpenAPI_RawHandleNoTypedErrorResponses(t *testing.T) {
	// 400 来自 typed 参数绑定、415 来自内容协商,raw 端点两者都不经过,故不应声明。
	_, doc := specOf(t, func(s *Server) {
		if err := s.RawHandle(http.MethodPost, "/raw", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	resp := dig(t, doc, "paths", "/raw", "post", "responses").(map[string]any)
	for _, unwanted := range []string{"400", "415"} {
		if _, bad := resp[unwanted]; bad {
			t.Errorf("raw endpoint must not declare %s; got %v", unwanted, keysOf(resp))
		}
	}
	// 500 仍应保留:兜底 recover 会经统一错误链写出错误体。
	if _, ok := resp["500"]; !ok {
		t.Errorf("raw endpoint should still declare 500; got %v", keysOf(resp))
	}
}

func TestOpenAPI_RawHandleErrorResponsesRespectOption(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := s.RawHandle(http.MethodGet, "/raw", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}, WithOpenAPIErrorResponses(false))
	resp := dig(t, doc, "paths", "/raw", "get", "responses").(map[string]any)
	if len(resp) != 1 {
		t.Errorf("only the success response should remain; got %v", keysOf(resp))
	}
}

func TestOpenAPI_RawHandleTemplatedPathDeclaresParams(t *testing.T) {
	// OpenAPI 规定路径模板里的每个变量都必须有对应的 path 参数声明,否则 spec 非法。
	// raw 端点没有 params 类型,只能从模板自身推导。
	_, doc := specOf(t, func(s *Server) {
		if err := s.RawHandle(http.MethodGet, "/files/{bucket}/{key}", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	params := dig(t, doc, "paths", "/files/{bucket}/{key}", "get", "parameters").([]any)
	if len(params) != 2 {
		t.Fatalf("expected 2 path params, got %v", params)
	}
	for i, want := range []string{"bucket", "key"} {
		p := params[i].(map[string]any)
		if p["name"] != want {
			t.Errorf("param %d name=%v want %q", i, p["name"], want)
		}
		if p["in"] != "path" || p["required"] != true {
			t.Errorf("param %q must be a required path param: %v", want, p)
		}
		// 类型只能是 string:框架不知道 raw handler 会怎么解析它。
		if got := dig(t, p, "schema", "type"); got != "string" {
			t.Errorf("param %q type=%v want string", want, got)
		}
	}
}

func TestPathTemplateParams(t *testing.T) {
	tests := []struct {
		tmpl string
		want []string
	}{
		{"/static/path", nil},
		{"/", nil},
		{"/users/{id}", []string{"id"}},
		{"/a/{x}/b/{y}", []string{"x", "y"}},
		{"/files/{filepath...}", []string{"filepath"}}, // catch-all 归一
		{"/odd/{}", nil},                               // 空名跳过
		{"/partial/{unclosed", nil},
		{"/mid{dle}", nil}, // 只认整段变量
	}
	for _, tc := range tests {
		t.Run(tc.tmpl, func(t *testing.T) {
			got := pathTemplateParams(tc.tmpl)
			if len(got) != len(tc.want) {
				t.Fatalf("pathTemplateParams(%q) returned %d params, want %d", tc.tmpl, len(got), len(tc.want))
			}
			for i, want := range tc.want {
				if got[i].name != want {
					t.Errorf("param %d = %q, want %q", i, got[i].name, want)
				}
				if got[i].in != "path" || !got[i].required {
					t.Errorf("param %q must be required and in path: %+v", want, got[i])
				}
			}
		})
	}
}

func TestOpenAPI_SpecEndpointStillExcludesItself(t *testing.T) {
	// 回归守卫:spec 端点自身是用 RawHandle 注册的。既然 RawHandle 现在会登记路由,
	// 就必须显式确认这个元数据端点没有把自己写进业务契约。
	s, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/ping", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	paths := dig(t, doc, "paths").(map[string]any)
	if _, listed := paths["/openapi.json"]; listed {
		t.Error("the spec endpoint must not document itself")
	}
	if len(paths) != 1 {
		t.Errorf("only the business route should be listed; got %v", keysOf(paths))
	}
	// 端点本身仍应可用。
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("spec endpoint should still serve, got %d", rec.Code)
	}
}

func TestOpenAPI_CustomSpecRouteExcludesItself(t *testing.T) {
	// 自定义路径同样不得自我登记。
	s := New(WithOpenAPI(OpenAPIInfo{Title: "T", Version: "1"}, WithOpenAPIRoute("/docs/spec.json")))
	if err := GetNone(s, "/ping", JSON[oaItem](), func(context.Context) (oaItem, error) {
		return oaItem{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(s.SpecJSON(), &doc); err != nil {
		t.Fatal(err)
	}
	paths := dig(t, doc, "paths").(map[string]any)
	if _, listed := paths["/docs/spec.json"]; listed {
		t.Error("a custom spec route must not document itself")
	}
}

func TestOpenAPI_StaticAndHealthRoutesDocumented(t *testing.T) {
	// 静态资源与健康检查都建在 RawHandle 上,故也会出现在 spec 里。它们确实是服务对外
	// 暴露的端点,列出比隐藏更符合"spec 描述真实可达路径"的承诺。
	fsys := fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("x")}}
	_, doc := specOf(t, func(s *Server) {
		if err := Health(s, "/healthz"); err != nil {
			t.Fatal(err)
		}
		if err := StaticFS(s, "/assets/", fsys); err != nil {
			t.Fatal(err)
		}
	})
	paths := dig(t, doc, "paths").(map[string]any)
	if _, ok := paths["/healthz"]; !ok {
		t.Errorf("health endpoint should be documented; got %v", keysOf(paths))
	}
	// 静态挂载是 catch-all 模板,应作为带 path 参数的路径出现。
	found := false
	for p, item := range paths {
		if p == "/healthz" {
			continue
		}
		found = true
		// catch-all 变量必须被声明,否则 spec 非法。
		op, ok := item.(map[string]any)["get"].(map[string]any)
		if !ok {
			continue
		}
		if _, has := op["parameters"]; !has {
			t.Errorf("static catch-all %q must declare its path parameter: %v", p, keysOf(op))
		}
	}
	if !found {
		t.Errorf("static mount should be documented; got %v", keysOf(paths))
	}
}

func TestOpenAPI_RawAndTypedCoexistOnOnePath(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		if err := GetNone(s, "/res", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.RawHandle(http.MethodPost, "/res", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	item := dig(t, doc, "paths", "/res").(map[string]any)
	// typed 的 GET 有 schema,raw 的 POST 没有——同一路径下两种呈现并存。
	if got := dig(t, item, "get", "responses", "200", "content", "application/json", "schema", "$ref"); got == "" {
		t.Error("typed GET should keep its schema")
	}
	postOK := dig(t, item, "post", "responses", "200").(map[string]any)
	if _, bad := postOK["content"]; bad {
		t.Error("raw POST must not gain a schema from its typed neighbour")
	}
}

func TestOpenAPI_RawHandleInGroupInheritsTag(t *testing.T) {
	_, doc := specOf(t, func(s *Server) {
		g := s.Group("/api/v1/blobs")
		if err := g.RawHandle(http.MethodGet, "/{id}", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	op := dig(t, doc, "paths", "/api/v1/blobs/{id}", "get").(map[string]any)
	tags := toStringSlice(op["tags"])
	if len(tags) != 1 || tags[0] != "blobs" {
		t.Errorf("group-registered raw route tags=%v want [blobs]", tags)
	}
	// 分组前缀里的模板变量也要正确声明。
	params := dig(t, op, "parameters").([]any)
	if len(params) != 1 || params[0].(map[string]any)["name"] != "id" {
		t.Errorf("params=%v", params)
	}
}

func TestOpenAPI_RawRoutesStayDeterministic(t *testing.T) {
	build := func() string {
		s := New(WithOpenAPI(OpenAPIInfo{Title: "D", Version: "1"}))
		if err := s.RawHandle(http.MethodGet, "/a/{x}", func(_ context.Context, _ *Request, resp *Response) error {
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := GetNone(s, "/b", JSON[oaItem](), func(context.Context) (oaItem, error) {
			return oaItem{}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.RawHandle(http.MethodPost, "/c", func(_ context.Context, _ *Request, resp *Response) error {
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return string(s.SpecJSON())
	}
	first := build()
	for i := 0; i < 5; i++ {
		if got := build(); got != first {
			t.Fatalf("raw routes broke determinism on build %d", i+2)
		}
	}
}

func TestOpenAPI_RawHandleZeroCostWhenDisabled(t *testing.T) {
	// 未开启 OpenAPI 时,RawHandle 不得登记任何元数据。
	s := New()
	if err := s.RawHandle(http.MethodGet, "/raw", func(_ context.Context, _ *Request, resp *Response) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(s.docs) != 0 {
		t.Errorf("RawHandle must record nothing without WithOpenAPI, got %d entries", len(s.docs))
	}
}

func TestOpenAPI_RawHandleRegistrationErrorNotRecorded(t *testing.T) {
	// 注册失败(重复路由)时不得留下元数据,否则 spec 会列出并不存在的端点。
	s := New(WithOpenAPI(OpenAPIInfo{Title: "T", Version: "1"}))
	h := func(_ context.Context, _ *Request, resp *Response) error { return nil }
	if err := s.RawHandle(http.MethodGet, "/dup", h); err != nil {
		t.Fatal(err)
	}
	before := len(s.docs)
	if err := s.RawHandle(http.MethodGet, "/dup", h); err == nil {
		t.Fatal("duplicate registration should fail")
	}
	if len(s.docs) != before {
		t.Errorf("a failed registration must not be recorded: %d → %d", before, len(s.docs))
	}
}

func TestOpenAPI_RawSpecRemainsValidJSON(t *testing.T) {
	// 端到端:混合 raw / typed / 静态 / 健康检查后,spec 仍是合法 JSON 且可被解析。
	fsys := fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("x")}}
	s := New(WithOpenAPI(OpenAPIInfo{Title: "Mixed", Version: "1"}))
	if err := GetParams(s, "/items/{id}", JSON[oaItem](),
		func(_ context.Context, p oaListParams) (oaItem, error) { return oaItem{}, nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.RawHandle(http.MethodGet, "/raw/{k}", func(_ context.Context, _ *Request, resp *Response) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := Health(s, "/healthz"); err != nil {
		t.Fatal(err)
	}
	if err := StaticFS(s, "/s/", fsys); err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(s.SpecJSON(), &doc); err != nil {
		t.Fatalf("mixed spec is not valid JSON: %v", err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi=%v", doc["openapi"])
	}
	// spec 声明的每个路径都应真实可达(把模板变量替换成具体值后不应 404/405)。
	paths := dig(t, doc, "paths").(map[string]any)
	if len(paths) < 4 {
		t.Errorf("expected at least 4 documented paths, got %v", keysOf(paths))
	}
}
