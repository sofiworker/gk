package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type rewriteBody struct {
	Name string `json:"name"`
}

type rewriteInput struct {
	ID      string
	Page    int
	Trace   string
	Session string
	Body    rewriteBody
}

type rewriteOutput struct {
	ID      string `json:"id"`
	Page    int    `json:"page"`
	Trace   string `json:"trace"`
	Session string `json:"session"`
	Name    string `json:"name"`
}

type rewriteRecursiveOutput struct {
	Name string                  `json:"name"`
	Next *rewriteRecursiveOutput `json:"next,omitempty"`
}

func TestServerRewriteTypedParamsBodyAndOpenAPI(t *testing.T) {
	server := New(
		WithOpenAPI("rewrite", "1.0.0"),
		WithConsumes(MIMEJSON),
		WithProduces(MIMEJSON),
	)
	server.MustMount(Handle(Post("/users/{id}"), MapInputs5(
		PathString("id"),
		QueryInt("page"),
		HeaderString("X-Trace"),
		CookieString("sid"),
		JSONBody[rewriteBody](),
		func(id string, page int, trace, session string, body rewriteBody) rewriteInput {
			return rewriteInput{ID: id, Page: page, Trace: trace, Session: session, Body: body}
		},
	), JSONOutput[rewriteOutput](), func(_ context.Context, input rewriteInput) (rewriteOutput, error) {
		return rewriteOutput{
			ID:      input.ID,
			Page:    input.Page,
			Trace:   input.Trace,
			Session: input.Session,
			Name:    input.Body.Name,
		}, nil
	}))

	req := httptest.NewRequest(http.MethodPost, "/users/u-7?page=3", strings.NewReader(`{"name":"Ada"}`))
	req.Header.Set("Content-Type", MIMEJSON)
	req.Header.Set("X-Trace", "trace-7")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "session-7"})
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	var output rewriteOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &output); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := rewriteOutput{ID: "u-7", Page: 3, Trace: "trace-7", Session: "session-7", Name: "Ada"}
	if output != want {
		t.Fatalf("output = %#v, want %#v", output, want)
	}

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	text := string(document)
	for _, fragment := range []string{
		`"/users/{id}"`,
		`"name":"id"`,
		`"name":"page"`,
		`"name":"X-Trace"`,
		`"name":"sid"`,
		`"requestBody"`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, text)
		}
	}
	// OpenAPI 元数据不可变：重复构建文档不能污染参数 schema 或重复标记 catch-all。
	// OpenAPI metadata is immutable: rebuilding must not mutate schemas or duplicate markers.
	second, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("second OpenAPI: %v", err)
	}
	if string(second) != text {
		t.Fatalf("repeated OpenAPI generation changed output\nfirst=%s\nsecond=%s", text, second)
	}
}

func TestServerRewriteCatchAllOpenAPIAndEscapedPath(t *testing.T) {
	server := New(WithOpenAPI("rewrite", "1.0.0"), WithProduces(MIMEJSON))
	server.MustMount(Handle(Get("/files/{path...}"), PathRemainder("path"), JSONOutput[map[string]string](), func(_ context.Context, path string) (map[string]string, error) {
		return map[string]string{"path": path}, nil
	}))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a/b%2Fc", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"path":"a/b/c"}` {
		t.Fatalf("catch-all response = %d %q", rec.Code, rec.Body.String())
	}
	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	text := string(document)
	if !strings.Contains(text, `"x-ghttp-catch-all":true`) {
		t.Fatalf("catch-all marker missing: %s", text)
	}
}

func TestServerRewriteDecodesScalarBodyFields(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Post("/echo"), JSONBody[string](), JSONOutput[map[string]string](), func(_ context.Context, body string) (map[string]string, error) {
		return map[string]string{"body": body}, nil
	}))
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`"hello"`))
	req.Header.Set("Content-Type", MIMEJSON)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body":"hello"`) {
		t.Fatalf("scalar body = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerRewriteDecodesPointerBodyFields(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Post("/echo"), JSONBody[*rewriteBody](), JSONOutput[map[string]string](), func(_ context.Context, body *rewriteBody) (map[string]string, error) {
		if body == nil {
			return map[string]string{"body": "nil"}, nil
		}
		return map[string]string{"body": body.Name}, nil
	}))
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"name":"Ada"}`))
	req.Header.Set("Content-Type", MIMEJSON)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body":"Ada"`) {
		t.Fatalf("pointer body = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerRewriteBindsRepeatedQueryAndHeaderValues(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	type input struct {
		Tags    []string
		Codes   [2]int
		Default []string
	}
	type output struct {
		Tags    []string `json:"tags"`
		Codes   [2]int   `json:"codes"`
		Default []string `json:"default"`
	}
	server.MustMount(Handle(Get("/items"), MapInputs3(
		QueryStrings("tag"),
		InputFunc(func(v RequestView) ([2]int, error) {
			values := v.Header().Values("X-Code")
			var codes [2]int
			for i := 0; i < len(codes) && i < len(values); i++ {
				n, err := strconv.Atoi(values[i])
				if err != nil {
					return codes, BadRequest("invalid X-Code header")
				}
				codes[i] = n
			}
			return codes, nil
		}),
		InputFunc(func(v RequestView) ([]string, error) {
			if values, ok := v.Query()["missing"]; ok {
				return values, nil
			}
			return []string{"fallback"}, nil
		}),
		func(tags []string, codes [2]int, fallback []string) input {
			return input{Tags: tags, Codes: codes, Default: fallback}
		},
	), JSONOutput[output](), func(_ context.Context, in input) (output, error) {
		return output{Tags: in.Tags, Codes: in.Codes, Default: in.Default}, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "/items?tag=one&tag=two", nil)
	req.Header.Add("X-Code", "7")
	req.Header.Add("X-Code", "9")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	var got output
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := output{Tags: []string{"one", "two"}, Codes: [2]int{7, 9}, Default: []string{"fallback"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output = %#v, want %#v", got, want)
	}
}

func TestServerRewriteTaggedPathPopulatesEmbeddedParams(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	type input struct {
		ID     string
		Params string
	}
	type output struct {
		Tagged string `json:"tagged"`
		Params string `json:"params"`
	}
	server.MustMount(Handle(Get("/items/{id}"), MapInputs(PathString("id"), HTTPRequest(), func(id string, r *http.Request) input {
		return input{ID: id, Params: server.MatchedParams(r).Path("id")}
	}), JSONOutput[output](), func(_ context.Context, in input) (output, error) {
		return output{Tagged: in.ID, Params: in.Params}, nil
	}))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/a%20b", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	var got output
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if want := (output{Tagged: "a b", Params: "a b"}); got != want {
		t.Fatalf("output = %#v, want %#v", got, want)
	}
}

func TestServerRewritePathOnlyInputCompilesDirectTerminal(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Get("/items/{id}"), PathInt64("id"), JSONOutput[map[string]int64](), func(_ context.Context, id int64) (map[string]int64, error) {
		return map[string]int64{"id": id}, nil
	}))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	r := state.mux.routes[0]
	if len(r.compiledHandlers) == 0 || r.needsState {
		t.Fatal("path-only input did not compile a stateless direct call")
	}
	matched := state.mux.match(http.MethodGet, "/items/42", false)
	if matched.kind != routeMatchFound || matched.route != r {
		t.Fatalf("direct param match = %#v, want route %p", matched, r)
	}
	if matched.params.Get("id") != "42" {
		t.Fatalf("matched params id = %q, want 42", matched.params.Get("id"))
	}
}

func TestServerRewriteNoInputGenericOperationCompilesStaticFastHandler(t *testing.T) {
	server := New()
	server.MustMount(Handle(Get("/ready"), NoInput(), TextOutput(), func(context.Context, EmptyInput) (string, error) {
		return "ready", nil
	}))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	r := state.mux.routes[0]
	if len(r.compiledHandlers) == 0 || r.needsState {
		t.Fatal("generic no-input operation did not compile a stateless direct call")
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("fast response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestServerRewriteNoInputGenericOperationKeepsStatelessDirectWithMiddleware(t *testing.T) {
	// 新执行模型下中间件永远经 Ctx 切片链运行(c.server 直接挂在 Ctx 上,
	// 不再依赖 requestState 注入);无参数无 body 的 fastBuild 路由不再为
	// 中间件强制 attachCtx——needsState 只由错误模型/参数/typed 输入决定,
	// 中间件调用顺序与响应结果不变。
	// under the new execution model middleware always runs via the Ctx slice
	// chain (c.server sits on the Ctx directly; no requestState injection).
	// fastBuild routes without params or body no longer force attachCtx for
	// middleware: needsState depends only on the error model / params / typed
	// inputs, while middleware order and the response stay identical.
	called := false
	server := New()
	server.MustMount(Handle(Get("/ready"), NoInput(), TextOutput(), func(context.Context, EmptyInput) (string, error) {
		return "ready", nil
	}).WithMiddleware(func(c *Ctx) {
		called = true
		c.Next()
	}))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	r := state.mux.routes[0]
	if r.needsState {
		t.Fatal("paramless fastBuild route with middleware forced state attach, want stateless")
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if !called {
		t.Fatal("middleware was not called")
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestServerRewriteRawSimpleParamCompilesDirectTerminal(t *testing.T) {
	server := New()
	server.MustMount(RawOperation(http.MethodGet, "/items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	})))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	route := state.mux.routes[0]
	if len(route.compiledHandlers) == 0 || route.needsState {
		t.Fatal("raw route did not compile a stateless direct call")
	}
	matched := state.mux.match(http.MethodGet, "/items/42", false)
	if matched.kind != routeMatchFound || matched.route != route {
		t.Fatalf("raw direct param match = %#v, want route %p", matched, route)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/items/42", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "/items/42" {
		t.Fatalf("raw direct response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestServerRewriteDirectParamPreservesRoutingSemantics(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	type output struct {
		Value string `json:"value"`
	}
	server.MustMount(Handle(Get("/items/{id}"), PathString("id"), JSONOutput[output](), func(_ context.Context, id string) (output, error) {
		return output{Value: "param:" + id}, nil
	}))
	server.MustMount(HandleNoInput(Get("/items/new"), JSONOutput[output](), func(_ context.Context) (output, error) {
		return output{Value: "static"}, nil
	}))

	tests := []struct {
		path       string
		wantStatus int
		wantValue  string
	}{
		{path: "/items/42", wantStatus: http.StatusOK, wantValue: "param:42"},
		{path: "/items/new", wantStatus: http.StatusOK, wantValue: "static"},
		{path: "/items/a%20b", wantStatus: http.StatusOK, wantValue: "param:a b"},
		{path: "/items/%2E", wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != tt.wantStatus {
			t.Fatalf("GET %s status = %d, want %d; body = %q", tt.path, rec.Code, tt.wantStatus, rec.Body.String())
		}
		if tt.wantValue == "" {
			continue
		}
		var got output
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("GET %s decode response: %v", tt.path, err)
		}
		if got.Value != tt.wantValue {
			t.Fatalf("GET %s value = %q, want %q", tt.path, got.Value, tt.wantValue)
		}
	}
}

func TestServerRewriteOpenAPIHandlesRecursiveSchemas(t *testing.T) {
	server := New(WithOpenAPI("rewrite", "1.0.0"), WithProduces(MIMEJSON))
	server.MustMount(Handle(Get("/nodes"), NoInput(), JSONOutput[rewriteRecursiveOutput](), func(_ context.Context, _ EmptyInput) (rewriteRecursiveOutput, error) {
		return rewriteRecursiveOutput{Name: "root"}, nil
	}))
	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	for _, fragment := range []string{`"/nodes"`, `"name"`, `"next"`} {
		if !strings.Contains(string(document), fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, document)
		}
	}
}

func TestServerRewriteOpenAPIParameterFormats(t *testing.T) {
	tests := []struct {
		name   string
		typeOf reflect.Type
		want   map[string]interface{}
	}{
		{name: "int32", typeOf: reflect.TypeOf(int32(0)), want: map[string]interface{}{"type": "integer", "format": "int32"}},
		{name: "int64", typeOf: reflect.TypeOf(int64(0)), want: map[string]interface{}{"type": "integer", "format": "int64"}},
		{name: "float", typeOf: reflect.TypeOf(float32(0)), want: map[string]interface{}{"type": "number", "format": "float"}},
		{name: "double", typeOf: reflect.TypeOf(float64(0)), want: map[string]interface{}{"type": "number", "format": "double"}},
		{name: "array", typeOf: reflect.TypeOf([]int64{}), want: map[string]interface{}{
			"type": "array", "items": map[string]interface{}{"type": "integer", "format": "int64"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goTypeToSchemaType(tt.typeOf); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("schema = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestServerRewriteMethodAwarePrecedence(t *testing.T) {
	server := New(WithProduces(MIMEPlain))
	server.MustMount(RawOperation(http.MethodGet, "/users/new", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("static"))
	})))
	server.MustMount(RawOperation(http.MethodPost, "/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("param:" + r.URL.Path))
	})))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/new", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "param:/users/new" {
		t.Fatalf("POST precedence = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerRewriteStaticFastPathPreservesEncodedSlashBoundary(t *testing.T) {
	server := New()
	server.MustMount(RawOperation(http.MethodGet, "/a/b", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("two-segments"))
	})))
	server.MustMount(RawOperation(http.MethodGet, "/a%2Fb", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("one-segment"))
	})))

	encoded := httptest.NewRecorder()
	server.ServeHTTP(encoded, httptest.NewRequest(http.MethodGet, "/a%2Fb", nil))
	if encoded.Code != http.StatusOK || encoded.Body.String() != "one-segment" {
		t.Fatalf("encoded slash response = %d %q", encoded.Code, encoded.Body.String())
	}
	plain := httptest.NewRecorder()
	server.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/a/b", nil))
	if plain.Code != http.StatusOK || plain.Body.String() != "two-segments" {
		t.Fatalf("plain slash response = %d %q", plain.Code, plain.Body.String())
	}
}

func TestServerRewriteKeepsDistinctEscapedStaticSegments(t *testing.T) {
	server := New(WithProduces(MIMEPlain))
	server.MustMount(RawOperation(http.MethodGet, "/p/%2F/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("slash"))
	})))
	server.MustMount(RawOperation(http.MethodGet, "/p/%252F/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("escaped-percent"))
	})))

	tests := []struct {
		path string
		want string
	}{
		{path: "/p/%2F/1", want: "slash"},
		{path: "/p/%252F/1", want: "escaped-percent"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != tt.want {
			t.Fatalf("GET %s = %d %q, want %q", tt.path, rec.Code, rec.Body.String(), tt.want)
		}
	}
}

func TestServerRewriteWideStaticNodeMatchesEscapedLeadingByte(t *testing.T) {
	server := New(WithProduces(MIMEPlain))
	for _, path := range []string{"/alpha", "/bravo", "/charlie", "/delta"} {
		path := path
		server.MustMount(RawOperation(http.MethodGet, path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(path))
		})))
	}
	server.MustMount(RawOperation(http.MethodGet, "/{value}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("parameter"))
	})))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/%61lpha", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "/alpha" {
		t.Fatalf("escaped leading byte response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerRewriteStaticIndexPreservesExplicitHEADResult(t *testing.T) {
	mux := newTestRouteMux(t, testRouteDefinition(t, http.MethodHead, "/health"))
	result := mux.match(http.MethodHead, "/health", false)
	if result.kind != routeMatchFound || result.route.definition.method != http.MethodHead {
		t.Fatalf("match result = %#v, want explicit HEAD route", result)
	}
	if !result.suppressBody {
		t.Fatal("explicit HEAD result must suppress the response body")
	}
}

func TestServerRewriteConcurrentDispatch(t *testing.T) {
	server := New(WithProduces(MIMEJSON))

	server.MustMount(Handle(Get("/items/{id}"), PathString("id"), JSONOutput[map[string]string](), func(_ context.Context, id string) (map[string]string, error) {
		return map[string]string{"id": id}, nil
	}))

	const workers = 32
	var group sync.WaitGroup
	group.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer group.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/items/"+strconv.Itoa(i), nil)
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("worker %d status = %d", i, rec.Code)
			}
		}(i)
	}
	group.Wait()
}
