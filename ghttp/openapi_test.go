package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type oaAddr struct {
	City string `json:"city"`
}

type oaBase struct {
	ID      int64     `json:"id"`
	Created time.Time `json:"created"`
}

type oaTree struct {
	Name     string    `json:"name"`
	Children []*oaTree `json:"children,omitempty"`
}

type oaUser struct {
	oaBase
	Name     string            `json:"name"`
	Nick     *string           `json:"nick,omitempty"`
	Secret   string            `json:"-"`
	hidden   string            //nolint:unused
	Tags     map[string]string `json:"tags"`
	Avatar   []byte            `json:"avatar,omitempty"`
	Extra    json.RawMessage   `json:"extra,omitempty"`
	Addr     *oaAddr           `json:"addr,omitempty"`
	Age      int32             `json:"age"`
	Score    float64           `json:"score"`
	Active   bool              `json:"active"`
	Anything any               `json:"anything,omitempty"`
}

type oaPage[T any] struct {
	Items []T `json:"items"`
}

func oaGet[O any](v O) Endpoint[NoDataType, O] {
	return func(context.Context, RequestOf[NoDataType]) (O, error) { return v, nil }
}

func oaPostH[T, O any](v O) Endpoint[T, O] {
	return func(context.Context, RequestOf[T]) (O, error) { return v, nil }
}

func oaServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer()
	err := s.Register(
		Get("/users/:id", oaGet(oaUser{}),
			WithDoc("Get user", "Returns a user"), WithOperationID("getUser"),
			WithTags("users", "read"), WithDeprecated()),
		Post("/users", oaPostH[oaUser](&oaUser{})),
		Get("/files/*path", oaGet(FileReply{})),
		Get("/stream", oaGet(StreamReply{})),
		Get("/old", oaGet(RedirectReply{})),
		Get("/empty", oaGet(NoContentReply{})),
		Get("/reply", oaGet(Reply[oaAddr]{})),
		Get("/tree", oaGet(oaTree{})),
		Get("/page", oaGet(oaPage[oaAddr]{})),
		HandleAction[oaAddr](http.MethodPut, "/addr", func(context.Context, RequestOf[oaAddr]) error { return nil }),
		HandleProcedure(http.MethodDelete, "/proc", func(context.Context) error { return nil }),
		Raw(http.MethodGet, "/raw", func(context.Context, *Request, *Response) error { return nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func oaDoc(t *testing.T, s *Server, opts ...OpenAPIOption) map[string]any {
	t.Helper()
	b, err := OpenAPI(s, opts...)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// oaAt 按路径取嵌套值，缺失时终止测试。
// oaAt fetches a nested value by path and fails the test when missing.
func oaAt(t *testing.T, m any, path ...string) any {
	t.Helper()
	cur := m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: parent of %q is not an object", path, p)
		}
		cur, ok = mm[p]
		if !ok {
			t.Fatalf("path %v: missing %q", path, p)
		}
	}
	return cur
}

// oaMissing 报告路径是否不存在。
// oaMissing reports whether the path does not exist.
func oaMissing(m any, path ...string) bool {
	cur := m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return true
		}
		if cur, ok = mm[p]; !ok {
			return true
		}
	}
	return false
}

func TestOpenAPIBasics(t *testing.T) {
	doc := oaDoc(t, oaServer(t),
		WithOpenAPIInfo("Demo", "1.2.3", "desc"), WithOpenAPIServers("http://a", "http://b"))
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", doc["openapi"])
	}
	if oaAt(t, doc, "info", "title") != "Demo" || oaAt(t, doc, "info", "version") != "1.2.3" ||
		oaAt(t, doc, "info", "description") != "desc" {
		t.Fatalf("info = %v", doc["info"])
	}
	if len(oaAt(t, doc, "servers").([]any)) != 2 {
		t.Fatalf("servers = %v", doc["servers"])
	}

	get := oaAt(t, doc, "paths", "/users/{id}", "get").(map[string]any)
	if get["summary"] != "Get user" || get["description"] != "Returns a user" ||
		get["operationId"] != "getUser" || get["deprecated"] != true {
		t.Fatalf("doc fields = %v", get)
	}
	if tags := get["tags"].([]any); len(tags) != 2 || tags[0] != "users" {
		t.Fatalf("tags = %v", tags)
	}
	p := get["parameters"].([]any)[0].(map[string]any)
	if p["name"] != "id" || p["in"] != "path" || p["required"] != true {
		t.Fatalf("param = %v", p)
	}
	if oaAt(t, get, "responses", "200", "content", "application/json", "schema", "$ref") != "#/components/schemas/oaUser" {
		t.Fatalf("200 = %v", get["responses"])
	}
	if oaAt(t, get, "responses", "default", "content", "application/json", "schema", "$ref") != "#/components/schemas/ErrorResponse" {
		t.Fatalf("default = %v", get["responses"])
	}
	if !oaMissing(get, "requestBody") {
		t.Fatal("GET NoData must not have requestBody")
	}

	post := oaAt(t, doc, "paths", "/users", "post")
	if oaAt(t, post, "requestBody", "content", "application/json", "schema", "$ref") != "#/components/schemas/oaUser" {
		t.Fatalf("requestBody = %v", post)
	}
}

func TestOpenAPIPaths(t *testing.T) {
	doc := oaDoc(t, oaServer(t))
	f := oaAt(t, doc, "paths", "/files/{path}", "get").(map[string]any)
	if p := f["parameters"].([]any)[0].(map[string]any); p["name"] != "path" {
		t.Fatalf("param = %v", p)
	}
	if oaAt(t, f, "responses", "200", "content", "application/octet-stream", "schema", "format") != "binary" {
		t.Fatalf("file = %v", f)
	}
	if oaMissing(doc, "paths", "/stream", "get", "responses", "200", "content", "application/octet-stream") {
		t.Fatal("stream should be octet-stream")
	}
	if oaMissing(doc, "paths", "/old", "get", "responses", "302") {
		t.Fatal("redirect should be 302")
	}
	if oaMissing(doc, "paths", "/empty", "get", "responses", "204") || !oaMissing(doc, "paths", "/empty", "get", "responses", "200") {
		t.Fatal("NoContentReply should be 204 only")
	}
	if oaMissing(doc, "paths", "/addr", "put", "responses", "204") ||
		oaAt(t, doc, "paths", "/addr", "put", "requestBody", "content", "application/json", "schema", "$ref") != "#/components/schemas/oaAddr" {
		t.Fatal("action should be 204 with body")
	}
	if oaMissing(doc, "paths", "/proc", "delete", "responses", "204") {
		t.Fatal("procedure should be 204")
	}
	r := oaAt(t, doc, "paths", "/reply", "get", "responses", "200").(map[string]any)
	if !strings.Contains(r["description"].(string), "Reply.Status") ||
		oaAt(t, r, "content", "application/json", "schema", "$ref") != "#/components/schemas/oaAddr" {
		t.Fatalf("reply = %v", r)
	}
}

func TestOpenAPIRawOption(t *testing.T) {
	s := oaServer(t)
	if !oaMissing(oaDoc(t, s), "paths", "/raw") {
		t.Fatal("raw must be excluded by default")
	}
	if oaMissing(oaDoc(t, s, WithOpenAPIIncludeRaw(true)), "paths", "/raw", "get", "responses", "200") {
		t.Fatal("raw must be included when enabled")
	}
}

func TestOpenAPIHiddenRoute(t *testing.T) {
	s := NewServer()
	if err := s.Register(
		Get("/visible", oaGet(oaAddr{})),
		Get("/hidden", oaGet(oaAddr{}), WithOpenAPIHidden()),
		Get("/explicit-hidden", oaGet(oaAddr{}), WithOpenAPIExpose(false)),
	); err != nil {
		t.Fatal(err)
	}
	doc := oaDoc(t, s)
	if oaMissing(doc, "paths", "/visible", "get") {
		t.Fatal("visible route missing")
	}
	for _, p := range []string{"/hidden", "/explicit-hidden"} {
		if !oaMissing(doc, "paths", p) {
			t.Fatalf("hidden route %s was exposed", p)
		}
	}
	if rec := coreDo(s, http.MethodGet, "/hidden", ""); rec.Code != http.StatusOK {
		t.Fatalf("hidden route status = %d", rec.Code)
	}
}

func TestOpenAPISchemas(t *testing.T) {
	doc := oaDoc(t, oaServer(t))
	schemas := oaAt(t, doc, "components", "schemas")

	user := oaAt(t, schemas, "oaUser")
	props := oaAt(t, user, "properties")
	for _, name := range []string{"id", "created", "name", "nick", "tags", "avatar", "extra", "addr", "age", "score", "active", "anything"} {
		if oaMissing(props, name) {
			t.Errorf("missing property %q", name)
		}
	}
	for _, name := range []string{"Secret", "hidden", "oaBase"} {
		if !oaMissing(props, name) {
			t.Errorf("property %q should be absent", name)
		}
	}
	if oaAt(t, props, "id", "format") != "int64" || oaAt(t, props, "age", "format") != "int32" ||
		oaAt(t, props, "score", "type") != "number" || oaAt(t, props, "active", "type") != "boolean" {
		t.Errorf("primitive schemas wrong: %v", props)
	}
	if oaAt(t, props, "created", "format") != "date-time" {
		t.Error("time.Time should be date-time")
	}
	if oaAt(t, props, "avatar", "contentEncoding") != "base64" || oaAt(t, props, "avatar", "type") != "string" {
		t.Error("[]byte should be base64 string")
	}
	if len(oaAt(t, props, "extra").(map[string]any)) != 0 || len(oaAt(t, props, "anything").(map[string]any)) != 0 {
		t.Error("RawMessage/any should be empty schemas")
	}
	if oaAt(t, props, "tags", "additionalProperties", "type") != "string" {
		t.Error("map additionalProperties")
	}
	if oaAt(t, props, "addr", "$ref") != "#/components/schemas/oaAddr" {
		t.Error("pointer to struct should unwrap to $ref")
	}
	if oaAt(t, props, "nick", "type") != "string" {
		t.Error("pointer to string should unwrap")
	}

	req := map[string]bool{}
	for _, r := range oaAt(t, user, "required").([]any) {
		req[r.(string)] = true
	}
	for _, n := range []string{"id", "name", "tags", "age"} {
		if !req[n] {
			t.Errorf("%q should be required", n)
		}
	}
	for _, n := range []string{"nick", "avatar", "addr", "anything"} {
		if req[n] {
			t.Errorf("%q should not be required", n)
		}
	}

	if oaAt(t, schemas, "oaTree", "properties", "children", "items", "$ref") != "#/components/schemas/oaTree" {
		t.Error("recursive type must use $ref")
	}

	var generic string
	for name := range schemas.(map[string]any) {
		if strings.HasPrefix(name, "oaPage") {
			generic = name
		}
		if strings.ContainsAny(name, "[]*, /") {
			t.Errorf("illegal schema name %q", name)
		}
	}
	if generic == "" || generic == "oaPage" {
		t.Errorf("generic schema name = %q", generic)
	}
	if oaMissing(schemas, "ErrorResponse", "properties", "error") {
		t.Error("ErrorResponse schema missing")
	}
}

func TestOpenAPINameCollision(t *testing.T) {
	b := newOABuilder()
	// ghttp.Request 与 net/http.Request 同名不同包。
	// ghttp.Request and net/http.Request share a name across packages.
	n1, fresh1 := b.oaName(reflect.TypeFor[Request]())
	n2, fresh2 := b.oaName(reflect.TypeFor[http.Request]())
	if !fresh1 || !fresh2 || n1 == n2 || n1 != "Request" || !strings.HasPrefix(n2, "Request_") {
		t.Fatalf("names = %q, %q", n1, n2)
	}
	if again, fresh := b.oaName(reflect.TypeFor[http.Request]()); fresh || again != n2 {
		t.Fatalf("name not stable: %q fresh=%v", again, fresh)
	}
}

func TestOpenAPISanitize(t *testing.T) {
	got := oaSanitize("Page[github.com/a/b.User,*map[string]int]")
	if strings.ContainsAny(got, "[]*, /") || !strings.HasPrefix(got, "Page_") {
		t.Fatalf("sanitize = %q", got)
	}
}

func TestOpenAPIDeterministic(t *testing.T) {
	s := oaServer(t)
	a, err := OpenAPI(s)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		b, _ := OpenAPI(s)
		if !bytes.Equal(a, b) {
			t.Fatal("OpenAPI output is not deterministic")
		}
	}
}

func TestOpenAPINilServer(t *testing.T) {
	if _, err := OpenAPI(nil); err == nil {
		t.Fatal("expected error for nil server")
	}
}

func TestOpenAPIRoute(t *testing.T) {
	s := NewServer()
	// 先注册 OpenAPI 路由，再注册其他路由：惰性生成应能看到后者。
	// Register the OpenAPI route first; lazy generation must still see later routes.
	if err := s.Register(OpenAPIRoute("/openapi.json", s, WithOpenAPIInfo("Lazy", "1", ""))); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(Get("/late", oaGet(oaAddr{}))); err != nil {
		t.Fatal(err)
	}
	var first []byte
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if oaMissing(m, "paths", "/late", "get") || oaAt(t, m, "info", "title") != "Lazy" {
			t.Fatalf("doc = %s", rec.Body.String())
		}
		if !oaMissing(m, "paths", "/openapi.json") {
			t.Fatal("OpenAPI route must not describe itself by default")
		}
		if i == 0 {
			first = append([]byte(nil), rec.Body.Bytes()...)
		} else if !bytes.Equal(first, rec.Body.Bytes()) {
			t.Fatal("cached body differs")
		}
	}
}

func TestOpenAPIGolden(t *testing.T) {
	s := NewServer()
	if err := s.Register(Get("/items/:id", oaGet(oaAddr{}), WithOperationID("getItem"))); err != nil {
		t.Fatal(err)
	}
	got, err := OpenAPI(s, WithOpenAPIInfo("T", "1", ""))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{
  "components": {
    "schemas": {
      "ErrorResponse": {
        "properties": {
          "code": {
            "type": "string"
          },
          "error": {
            "type": "string"
          },
          "status": {
            "format": "int64",
            "type": "integer"
          }
        },
        "required": [
          "error"
        ],
        "type": "object"
      },
      "oaAddr": {
        "properties": {
          "city": {
            "type": "string"
          }
        },
        "required": [
          "city"
        ],
        "type": "object"
      }
    }
  },
  "info": {
    "title": "T",
    "version": "1"
  },
  "openapi": "3.1.0",
  "paths": {
    "/items/{id}": {
      "get": {
        "operationId": "getItem",
        "parameters": [
          {
            "in": "path",
            "name": "id",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/oaAddr"
                }
              }
            },
            "description": "OK"
          },
          "default": {
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/ErrorResponse"
                }
              }
            },
            "description": "Error"
          }
        }
      }
    }
  }
}`
	if string(got) != want {
		t.Fatalf("golden mismatch:\n%s", got)
	}
}
