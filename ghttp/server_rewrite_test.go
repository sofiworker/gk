package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
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
	Params
	ID      string `path:"id"`
	Page    int    `query:"page"`
	Trace   string `header:"X-Trace"`
	Session string `cookie:"sid"`
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

type rewriteRecursiveParameter []rewriteRecursiveParameter

func TestServerRewriteTypedParamsBodyAndOpenAPI(t *testing.T) {
	server := New(
		WithOpenAPI("rewrite", "1.0.0"),
		WithConsumes(MIMEJSON),
		WithProduces(MIMEJSON),
	)
	server.MustMount(Handle(Post("/users/{id}"), StructInput[rewriteInput](), JSONOutput[rewriteOutput](), func(_ context.Context, input rewriteInput) (rewriteOutput, error) {
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
	type input struct {
		Path string `path:"path"`
	}
	server.MustMount(Handle(Get("/files/{path...}"), StructInput[input](), JSONOutput[map[string]string](), func(_ context.Context, in input) (map[string]string, error) {
		return map[string]string{"path": in.Path}, nil
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
	type input struct{ Body string }
	server.MustMount(Handle(Post("/echo"), StructInput[input](), JSONOutput[map[string]string](), func(_ context.Context, in input) (map[string]string, error) {
		return map[string]string{"body": in.Body}, nil
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
	type input struct{ Body *rewriteBody }
	server.MustMount(Handle(Post("/echo"), StructInput[input](), JSONOutput[map[string]string](), func(_ context.Context, in input) (map[string]string, error) {
		if in.Body == nil {
			return map[string]string{"body": "nil"}, nil
		}
		return map[string]string{"body": in.Body.Name}, nil
	}))
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"name":"Ada"}`))
	req.Header.Set("Content-Type", MIMEJSON)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body":"Ada"`) {
		t.Fatalf("pointer body = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerRewriteDecodesPointerBodyAcrossBuiltInCodecs(t *testing.T) {
	type formBody struct {
		Name string `form:"name"`
	}
	type plainInput struct{ Body *string }
	type formInput struct{ Body *formBody }

	server := New(WithProduces(MIMEJSON))
	server.MustMount(Handle(Post("/plain"), StructInput[plainInput](MIMEPlain), JSONOutput[map[string]string](), func(_ context.Context, in plainInput) (map[string]string, error) {
		if in.Body == nil {
			return map[string]string{"body": "nil"}, nil
		}
		return map[string]string{"body": *in.Body}, nil
	}))
	server.MustMount(Handle(Post("/form"), StructInput[formInput](MIMEPOSTForm), JSONOutput[map[string]string](), func(_ context.Context, in formInput) (map[string]string, error) {
		if in.Body == nil {
			return map[string]string{"body": "nil"}, nil
		}
		return map[string]string{"body": in.Body.Name}, nil
	}))
	server.MustMount(Handle(Post("/multipart"), StructInput[formInput](), JSONOutput[map[string]string](), func(_ context.Context, in formInput) (map[string]string, error) {
		if in.Body == nil {
			return map[string]string{"body": "nil"}, nil
		}
		return map[string]string{"body": in.Body.Name}, nil
	}))

	tests := []struct {
		name        string
		path        string
		contentType string
		body        *bytes.Buffer
		want        string
	}{
		{name: "plain", path: "/plain", contentType: MIMEPlain, body: bytes.NewBufferString("hello"), want: "hello"},
		{name: "form", path: "/form", contentType: MIMEPOSTForm, body: bytes.NewBufferString("name=Ada"), want: "Ada"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, tt.body)
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body":"`+tt.want+`"`) {
				t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
			}
		})
	}

	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	if err := writer.WriteField("name", "Ada"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/multipart", &multipartBody)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body":"Ada"`) {
		t.Fatalf("multipart response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerRewriteBindsRepeatedQueryAndHeaderValues(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	type input struct {
		Tags    []string `query:"tag"`
		Codes   [2]int   `header:"X-Code"`
		Default []string `query:"missing" default:"fallback"`
	}
	type output struct {
		Tags    []string `json:"tags"`
		Codes   [2]int   `json:"codes"`
		Default []string `json:"default"`
	}
	server.MustMount(Handle(Get("/items"), StructInput[input](), JSONOutput[output](), func(_ context.Context, in input) (output, error) {
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

func TestServerRewriteCollectionBindingErrorsNameSource(t *testing.T) {
	type input struct {
		Codes [2]int `header:"X-Code"`
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("X-Code", "7")
	var target input
	err := ParseInput(req, &target)
	if err == nil || !strings.Contains(err.Error(), `bind header parameter "X-Code"`) {
		t.Fatalf("ParseInput error = %v, want header source", err)
	}
}

func TestServerRewriteTaggedPathPopulatesEmbeddedParams(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	type input struct {
		Params
		ID string `path:"id"`
	}
	type output struct {
		Tagged string `json:"tagged"`
		Params string `json:"params"`
	}
	server.MustMount(Handle(Get("/items/{id}"), StructInput[input](), JSONOutput[output](), func(_ context.Context, in input) (output, error) {
		return output{Tagged: in.ID, Params: in.Params.Path("id")}, nil
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
	type input struct {
		ID int64 `path:"id"`
	}
	server.MustMount(Handle(Get("/items/{id}"), StructInput[input](), JSONOutput[map[string]int64](), func(_ context.Context, in input) (map[string]int64, error) {
		return map[string]int64{"id": in.ID}, nil
	}))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	if state.mux.routes[0].directHandler == nil {
		t.Fatal("path-only input did not compile a direct terminal")
	}
	matched, value, ok := state.mux.matchDirectParam(http.MethodGet, "/items/42")
	if !ok || matched != state.mux.routes[0] || value != "42" {
		t.Fatalf("direct param match = (%p, %q, %v), want (%p, %q, true)", matched, value, ok, state.mux.routes[0], "42")
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
	if state.mux.routes[0].fastHandler == nil {
		t.Fatal("generic no-input operation did not compile a static fast handler")
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("fast response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestServerRewriteNoInputGenericOperationSkipsStaticFastHandlerWithMiddleware(t *testing.T) {
	server := New()
	server.MustMount(Handle(Get("/ready"), NoInput(), TextOutput(), func(context.Context, EmptyInput) (string, error) {
		return "ready", nil
	}).WithMiddleware(func(next http.Handler) http.Handler {
		return next
	}))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	if state.mux.routes[0].fastHandler != nil {
		t.Fatal("generic operation with middleware unexpectedly compiled a static fast handler")
	}
}

func TestServerRewriteNoInputGenericOperationUsesContextTerminal(t *testing.T) {
	called := false
	server := New()
	server.MustMount(Handle(Get("/ready"), NoInput(), TextOutput(), func(context.Context, EmptyInput) (string, error) {
		return "ready", nil
	}).WithContextMiddleware(func(next ContextHandler) ContextHandler {
		return func(c *Context) error {
			called = true
			return next(c)
		}
	}))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	if state.mux.routes[0].contextHandler == nil {
		t.Fatal("generic no-input operation did not compile a context terminal")
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if !called {
		t.Fatal("context middleware was not called")
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("context response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestServerRewriteDirectPathGenericOperationUsesContextTerminal(t *testing.T) {
	called := false
	server := New()
	server.MustMount(Handle(Get("/items/{id}"), PathInt64("id"), TextOutput(), func(_ context.Context, id int64) (string, error) {
		return strconv.FormatInt(id, 10), nil
	}).WithContextMiddleware(func(next ContextHandler) ContextHandler {
		return func(c *Context) error {
			called = true
			return next(c)
		}
	}))

	server.finalizeRoutes()
	state := server.compiled.Load()
	if state == nil || len(state.mux.routes) != 1 {
		t.Fatalf("compiled routes = %#v, want one route", state)
	}
	if state.mux.routes[0].contextHandler == nil {
		t.Fatal("generic direct-path operation did not compile a context terminal")
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/items/42", nil))
	if !called {
		t.Fatal("context middleware was not called")
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "42" {
		t.Fatalf("context response = %d %q", recorder.Code, recorder.Body.String())
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
	if route.directHandler != nil {
		t.Fatal("raw route unexpectedly exposed a typed direct handler")
	}
	matched, value, ok := state.mux.matchDirectParam(http.MethodGet, "/items/42")
	if !ok || matched != route || value != "42" {
		t.Fatalf("raw direct param match = (%p, %q, %v), want (%p, %q, true)", matched, value, ok, route, "42")
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/items/42", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "/items/42" {
		t.Fatalf("raw direct response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestServerRewriteDirectParamPreservesRoutingSemantics(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	type input struct {
		ID string `path:"id"`
	}
	type output struct {
		Value string `json:"value"`
	}
	server.MustMount(Handle(Get("/items/{id}"), StructInput[input](), JSONOutput[output](), func(_ context.Context, in input) (output, error) {
		return output{Value: "param:" + in.ID}, nil
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

func TestServerRewriteMultipartRejectsNonStructBodies(t *testing.T) {
	type pointerInput struct{ Body *string }
	type sliceInput struct{ Body []byte }

	server := New(WithProduces(MIMEJSON))
	pointerCalled := false
	sliceCalled := false
	server.MustMount(Handle(Post("/pointer"), StructInput[pointerInput](), JSONOutput[struct{}](), func(_ context.Context, _ pointerInput) (struct{}, error) {
		pointerCalled = true
		return struct{}{}, nil
	}))
	server.MustMount(Handle(Post("/slice"), StructInput[sliceInput](), JSONOutput[struct{}](), func(_ context.Context, _ sliceInput) (struct{}, error) {
		sliceCalled = true
		return struct{}{}, nil
	}))

	newRequest := func(path string) *http.Request {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("value", "data"); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("Close multipart writer: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, path, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req
	}

	for _, path := range []string{"/pointer", "/slice"} {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, newRequest(path))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST %s status = %d, want 400; body = %q", path, rec.Code, rec.Body.String())
		}
	}
	if pointerCalled || sliceCalled {
		t.Fatalf("invalid multipart body reached handler: pointer=%v slice=%v", pointerCalled, sliceCalled)
	}

	var target pointerInput
	err := parseInput(newRequest("/pointer"), &target)
	if !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("parseInput error = %v, want ErrInvalidBody", err)
	}
	if target.Body != nil {
		t.Fatalf("Body = %q, want nil after rejected multipart target", *target.Body)
	}
}

func TestServerRewriteOpenAPIHandlesRecursiveSchemas(t *testing.T) {
	server := New(WithOpenAPI("rewrite", "1.0.0"), WithProduces(MIMEJSON))
	server.MustMount(Handle(Get("/nodes"), StructInput[struct{}](), JSONOutput[rewriteRecursiveOutput](), func(_ context.Context, _ struct{}) (rewriteRecursiveOutput, error) {
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

func TestServerRewriteOpenAPIHandlesRecursiveCollectionParameter(t *testing.T) {
	server := New(WithOpenAPI("rewrite", "1.0.0"), WithProduces(MIMEJSON))
	type input struct {
		Values rewriteRecursiveParameter `query:"value"`
	}
	server.MustMount(Handle(Get("/recursive-parameters"), StructInput[input](), JSONOutput[struct{}](), func(_ context.Context, _ input) (struct{}, error) {
		return struct{}{}, nil
	}))

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	for _, fragment := range []string{`"/recursive-parameters"`, `"name":"value"`, `"items":{}`} {
		if !strings.Contains(string(document), fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, document)
		}
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
	result, matched := mux.matchStatic(http.MethodHead, "/health")
	if !matched {
		t.Fatal("matchStatic did not match explicit HEAD route")
	}
	if result.kind != routeMatchFound || result.route.definition.method != http.MethodHead {
		t.Fatalf("matchStatic result = %#v, want explicit HEAD route", result)
	}
	if !result.suppressBody {
		t.Fatal("matchStatic explicit HEAD result must suppress the response body")
	}
}

func TestServerRewriteConcurrentDispatch(t *testing.T) {
	server := New(WithProduces(MIMEJSON))

	server.MustMount(Handle(Get("/items/{id}"), StructInput[struct {
		ID string `path:"id"`
	}](), JSONOutput[map[string]string](), func(_ context.Context, input struct {
		ID string `path:"id"`
	}) (map[string]string, error) {
		return map[string]string{"id": input.ID}, nil
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
