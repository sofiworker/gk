package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type testInput struct {
	Path struct {
		ID string `path:"id"`
	}
	Body struct {
		Name string `json:"name"`
	}
}

type testOutput struct {
	Status int `default:"200"`
	Body   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
}

func testHandler(ctx context.Context, req testInput) (testOutput, error) {
	return testOutput{
		Body: struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{
			ID:   req.Path.ID,
			Name: req.Body.Name,
		},
	}, nil
}

func TestRouteBuilderWithPOST(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	if err := Route[testInput, testOutput](app).
		POST("/users/{id}").
		Doc("Create user").
		Tags("Users").
		OperationID("createUser").
		Reads(testInput{}).
		Responds(http.StatusCreated).With(testOutput{}).Desc("Created").End().
		To(testHandler); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"Alice"}`)
	r := httptest.NewRequest("POST", "/users/42", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var data struct {
		Status int `json:"Status"`
		Body   struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"Body"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if data.Body.Name != "Alice" {
		t.Fatalf("expected Name=Alice, got %s", data.Body.Name)
	}
	if data.Body.ID != "42" {
		t.Fatalf("expected ID=42, got %s", data.Body.ID)
	}
}

func TestRouteBuilderPostShortcutReplacement(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	if err := Route[testInput, testOutput](app).POST("/users/{id}").To(testHandler); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"Bob"}`)
	r := httptest.NewRequest("POST", "/users/99", body)
	r.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var data testOutput
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if data.Body.Name != "Bob" || data.Body.ID != "99" {
		t.Fatalf("data = %#v", data)
	}
}

func TestRouteBuilderPointerResponseType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		Message string `json:"message"`
	}

	if err := Route[struct{}, *output](app).GET("/pointer-response").To(func(ctx context.Context, req struct{}) (*output, error) {
		return &output{Message: "ok"}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pointer-response", nil)
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.Message != "ok" {
		t.Fatalf("message = %q, want ok", data.Message)
	}
}

func TestRouteBuilderPointerRequestType(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Query struct {
			Name string `query:"name"`
		}
	}
	type output struct {
		Name string `json:"name"`
	}

	if err := Route[*input, output](app).GET("/pointer-request").To(func(ctx context.Context, req *input) (output, error) {
		if req == nil {
			t.Fatal("request input is nil")
		}
		return output{Name: req.Query.Name}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pointer-request?name=alice", nil)
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.Name != "alice" {
		t.Fatalf("name = %q, want alice", data.Name)
	}
}

func TestRouteBuilderToRequiresProduces(t *testing.T) {
	app := New()

	err := Route[struct{}, string](app).GET("/ping").To(func(ctx context.Context, req struct{}) (string, error) {
		return "pong", nil
	})
	if !errors.Is(err, ErrRouteProducesRequired) {
		t.Fatalf("To error = %v, want ErrRouteProducesRequired", err)
	}
}

func TestRouteBuilderProducesPlainString(t *testing.T) {
	app := New()

	if err := Route[struct{}, string](app).
		GET("/ping").
		Produces(MIMEPlain).
		To(func(ctx context.Context, req struct{}) (string, error) {
			return "pong", nil
		}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "*/*")
	app.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != MIMEPlain {
		t.Fatalf("Content-Type = %q, want %s", ct, MIMEPlain)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "pong" {
		t.Fatalf("body = %q, want pong", got)
	}
}

func TestRouteBuilderProducesInheritance(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	group := app.Group("/api").Produces(MIMEPlain)

	if err := Route[struct{}, string](app).GET("/server").To(func(context.Context, struct{}) (string, error) {
		return "server", nil
	}); err != nil {
		t.Fatalf("server route To failed: %v", err)
	}

	if err := Route[struct{}, string](group).GET("/group").To(func(context.Context, struct{}) (string, error) {
		return "group", nil
	}); err != nil {
		t.Fatalf("group route To failed: %v", err)
	}

	if err := Route[struct{}, string](group).GET("/route").Produces(MIMEJSON).To(func(context.Context, struct{}) (string, error) {
		return "route", nil
	}); err != nil {
		t.Fatalf("route override To failed: %v", err)
	}

	tests := []struct {
		path string
		ct   string
		body string
	}{
		{path: "/server", ct: MIMEJSON, body: `"server"`},
		{path: "/api/group", ct: MIMEPlain, body: "group"},
		{path: "/api/route", ct: MIMEJSON, body: `"route"`},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		req.Header.Set("Accept", "*/*")
		app.ServeHTTP(rec, req)

		if ct := rec.Header().Get("Content-Type"); ct != tt.ct {
			t.Fatalf("%s Content-Type = %q, want %s", tt.path, ct, tt.ct)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != tt.body {
			t.Fatalf("%s body = %q, want %q", tt.path, got, tt.body)
		}
	}
}

func TestRouteBuilderToReturnsRegisterErrorAndSkipsOpenAPI(t *testing.T) {
	wantErr := errors.New("register failed")
	router := &failingRouter{err: wantErr}
	app := New(WithOpenAPI("test", "1.0.0"), WithRouter(router), WithProduces(MIMEJSON))

	err := Route[testInput, testOutput](app).
		POST("/users/{id}").
		Doc("Create user").
		To(testHandler)

	if !errors.Is(err, wantErr) {
		t.Fatalf("To error = %v, want %v", err, wantErr)
	}

	spec := app.openAPI.Build()
	var doc map[string]interface{}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("unmarshal openapi failed: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	if len(paths) != 0 {
		t.Fatalf("paths = %#v, want no stale openapi routes", paths)
	}
}

func TestRouteBuilderToRaw(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	err := Route[struct{}, struct{}](app).GET("/raw").ToRaw(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if err != nil {
		t.Fatalf("ToRaw failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/raw", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRouteBuilderToSSE(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	if err := Route[struct{}, struct{}](app).GET("/events").ToSSE(func(ctx Context, stream *SSEWriter) error {
		return stream.WriteEvent("message", "hello")
	}); err != nil {
		t.Fatalf("ToSSE failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	app.ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(rec.Body.String(), "data: hello") {
		t.Fatalf("body = %q, want SSE payload", rec.Body.String())
	}
}

func TestRouteBuilderToHTML(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(`<h1>{{.Title}}</h1>`), 0o600); err != nil {
		t.Fatalf("write template failed: %v", err)
	}

	app := New(WithRenderer(NewRenderer(tmpDir, ".html", template.FuncMap{}, false)))
	if err := Route[struct{}, struct{}](app).GET("/page").ToHTML(http.StatusCreated, "index", map[string]interface{}{"Title": "Hello"}); err != nil {
		t.Fatalf("ToHTML failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
}

func TestRouteBuilderPathAndQueryPopulateOpenAPI(t *testing.T) {
	app := New(WithOpenAPI("api", "1.0.0"), WithProduces(MIMEJSON))

	type pathParams struct {
		ID string `path:"id"`
	}
	type queryParams struct {
		Role string `query:"role"`
	}

	if err := Route[struct{}, struct{}](app).
		GET("/users/{id}").
		PathSchema(pathParams{}).
		QuerySchema(queryParams{}).
		To(func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, nil
		}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	spec := app.openAPI.Build()
	var doc map[string]interface{}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("unmarshal openapi failed: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	item := paths["/users/{id}"].(map[string]interface{})
	op := item["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})
	if len(params) != 2 {
		t.Fatalf("parameters = %#v, want 2 entries", params)
	}
}

func TestRouteBuilderParamsBindFlatFields(t *testing.T) {
	app := New(WithClientIPResolver(func(r *http.Request) string {
		return r.Header.Get("X-Client-IP")
	}), WithProduces(MIMEJSON))

	type input struct {
		Params `json:"-"`

		Name  string   `path:"name"`
		Role  string   `query:"role" default:"guest"`
		Age   int      `query:"age"`
		Tags  []string `query:"tag"`
		Token string   `header:"X-Token"`
	}
	type output struct {
		Name     string   `json:"name"`
		RawName  string   `json:"raw_name"`
		Role     string   `json:"role"`
		RawRole  string   `json:"raw_role"`
		Age      int      `json:"age"`
		Tags     []string `json:"tags"`
		RawTags  []string `json:"raw_tags"`
		Token    string   `json:"token"`
		RawToken string   `json:"raw_token"`
		ClientIP string   `json:"client_ip"`
	}

	if err := Route[input, output](app).
		PUT("/user/{name}").
		To(func(ctx context.Context, in input) (output, error) {
			return output{
				Name:     in.Name,
				RawName:  in.Path("name"),
				Role:     in.Role,
				RawRole:  in.DefaultQuery("role", "guest"),
				Age:      in.Age,
				Tags:     in.Tags,
				RawTags:  in.QueryList("tag"),
				Token:    in.Token,
				RawToken: in.Header("X-Token"),
				ClientIP: in.ClientIP(),
			}, nil
		}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/user/alice?age=18&tag=a&tag=b", nil)
	req.Header.Set("X-Token", "token-1")
	req.Header.Set("X-Client-IP", "203.0.113.9")
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.Name != "alice" || data.RawName != "alice" || data.Role != "guest" || data.RawRole != "guest" || data.Age != 18 || data.Token != "token-1" || data.RawToken != "token-1" || data.ClientIP != "203.0.113.9" {
		t.Fatalf("data = %#v", data)
	}
	if !reflect.DeepEqual(data.Tags, []string{"a", "b"}) || !reflect.DeepEqual(data.RawTags, []string{"a", "b"}) {
		t.Fatalf("tags = %#v raw = %#v", data.Tags, data.RawTags)
	}
}

func TestRouteBuilderHandlerContextParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Missing string `json:"missing"`
	}

	if err := Route[struct{}, output](app).GET("/users/{id}").To(func(ctx context.Context, req struct{}) (output, error) {
		return output{
			ID:      Path(ctx, "id"),
			Role:    DefaultQuery(ctx, "role", "guest"),
			Missing: DefaultPath(ctx, "missing", "fallback"),
		}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42?role=admin", nil)
	app.ServeHTTP(rec, req)

	var data output
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if data.ID != "42" || data.Role != "admin" || data.Missing != "fallback" {
		t.Fatalf("data = %#v", data)
	}
}

func TestRouteBuilderToHTTPFunc(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		Value string `query:"value"`
	}

	if err := Route[input, struct{}](app).GET("/raw-context").ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, req input) error {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
		}
		if req.Value != "yes" {
			t.Fatalf("value = %q, want yes", req.Value)
		}
		w.Header().Set("X-Raw-Context", req.Value)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("handled"))
		return nil
	}); err != nil {
		t.Fatalf("ToHTTPFunc failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/raw-context?value=yes", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if got := rec.Header().Get("X-Raw-Context"); got != "yes" {
		t.Fatalf("X-Raw-Context = %q, want yes", got)
	}
	if rec.Body.String() != "handled" {
		t.Fatalf("body = %q, want handled", rec.Body.String())
	}
}

func TestRouteBuilderToRedirect(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	if err := Route[struct{}, struct{}](app).GET("/old").ToRedirect(http.StatusFound, "/new"); err != nil {
		t.Fatalf("ToRedirect failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/new" {
		t.Fatalf("Location = %q, want /new", got)
	}
}

func TestRouteBuilderToRedirectFunc(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type input struct {
		ID string `path:"id"`
	}

	if err := Route[input, struct{}](app).GET("/old/{id}").ToRedirectFunc(http.StatusMovedPermanently, func(req input) (string, error) {
		return "/new/" + req.ID, nil
	}); err != nil {
		t.Fatalf("ToRedirectFunc failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/old/42", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if got := rec.Header().Get("Location"); got != "/new/42" {
		t.Fatalf("Location = %q, want /new/42", got)
	}
}

func TestRequestContextParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	if err := Route[struct{}, struct{}](app).GET("/events/{id}").ToSSE(func(ctx Context, stream *SSEWriter) error {
		if err := stream.WriteEvent("path", ctx.Path("id")); err != nil {
			return err
		}
		return stream.WriteEvent("query", ctx.DefaultQuery("role", "guest"))
	}); err != nil {
		t.Fatalf("ToSSE failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/7?role=admin", nil)
	app.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "data: 7") || !strings.Contains(body, "data: admin") {
		t.Fatalf("body = %q", body)
	}
}

type failingRouter struct {
	err error
}

func (r *failingRouter) Register(string, string, http.Handler) error {
	return r.err
}

func (r *failingRouter) ServeHTTP(http.ResponseWriter, *http.Request) {}
