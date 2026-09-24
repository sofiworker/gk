package v2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

type openAPIUser struct {
	Name string       `json:"name"`
	Next *openAPIUser `json:"next,omitempty"`
}

type openAPIInput struct {
	ID      int         `path:"id"`
	Page    int         `query:"page"`
	Token   string      `header:"X-Token"`
	Session string      `cookie:"session"`
	Payload openAPIUser `body:"json"`
}

func TestServerOpenAPIGroupsAndSchemas(t *testing.T) {
	s := NewServer()
	g := s.Group("/api").Group("/users")
	if err := g.Register(Post("/{id}", func(_ context.Context, in openAPIInput) (openAPIUser, error) {
		return in.Payload, nil
	}, WithOutput(JSONOutput[openAPIUser]().WithStatus(http.StatusCreated)))); err != nil {
		t.Fatal(err)
	}
	data, err := s.OpenAPI("Users", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("OpenAPI version = %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)
	op := paths["/api/users/{id}"].(map[string]any)["post"].(map[string]any)
	params := op["parameters"].([]any)
	if len(params) != 4 {
		t.Fatalf("parameters = %v", params)
	}
	for _, param := range params {
		p := param.(map[string]any)
		if p["in"] == "path" && (p["name"] != "id" || p["required"] != true || p["schema"].(map[string]any)["type"] != "integer") {
			t.Errorf("path parameter = %v", p)
		}
	}
	request := op["requestBody"].(map[string]any)
	if _, ok := request["content"].(map[string]any)["application/json"]; !ok {
		t.Fatalf("request body = %v", request)
	}
	response := op["responses"].(map[string]any)["201"].(map[string]any)
	if _, ok := response["content"].(map[string]any)["application/json"]; !ok {
		t.Fatalf("response = %v", response)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	user := schemas[schemaName(reflect.TypeFor[openAPIUser]())].(map[string]any)
	next := user["properties"].(map[string]any)["next"].(map[string]any)
	if next["$ref"] != "#/components/schemas/"+schemaName(reflect.TypeFor[openAPIUser]()) {
		t.Fatalf("recursive schema = %v", next)
	}
}

func TestServerOpenAPIDoesNotInventUnknownSchemas(t *testing.T) {
	s := NewServer()
	if err := s.Register(
		Get("/custom/{id}", func(context.Context, openAPIUser) (openAPIUser, error) { return openAPIUser{}, nil },
			WithInput(DecodeWith(func(context.Context, *Request) (openAPIUser, error) { return openAPIUser{}, nil })),
			WithNegotiation(JSONOutput[openAPIUser](), TextOutput[openAPIUser]())),
	); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(Get("/custom/{id}", func(context.Context, openAPIUser) (openAPIUser, error) { return openAPIUser{}, nil },
		WithInput(DecodeWith(func(context.Context, *Request) (openAPIUser, error) { return openAPIUser{}, nil })))); !errors.Is(err, root.ErrDuplicateRoute) {
		t.Fatalf("duplicate registration = %v", err)
	}
	data, err := s.OpenAPI("Dynamic", "1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"/custom/{id}"`) != 1 {
		t.Fatalf("duplicate or missing route in document: %s", data)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	op := doc["paths"].(map[string]any)["/custom/{id}"].(map[string]any)["get"].(map[string]any)
	if op["x-gk-request-schema-unavailable"] == nil || op["requestBody"] != nil {
		t.Fatalf("unknown request schema = %v", op)
	}
	response := op["responses"].(map[string]any)["200"].(map[string]any)
	if response["content"] != nil || response["x-gk-response-schema-unavailable"] == nil {
		t.Fatalf("unknown negotiated response schema = %v", response)
	}
	params := op["parameters"].([]any)
	if len(params) != 1 || params[0].(map[string]any)["name"] != "id" {
		t.Fatalf("fallback path parameter = %v", params)
	}
	if _, err := s.OpenAPI("", "1"); err == nil {
		t.Fatal("expected title validation error")
	}
}

func TestServerOpenAPIExplicitCodecs(t *testing.T) {
	type input struct {
		Search string `query:"q" json:"search"`
	}
	type form struct {
		Value string `form:"form_value" json:"json_value"`
	}
	s := NewServer()
	if err := s.Register(
		Post("/json", func(context.Context, input) (Reply[openAPIUser], error) { return Reply[openAPIUser]{}, nil },
			WithInput(JSONInput[input]()), WithOutput(ReplyOutput(JSONOutput[openAPIUser]()))),
		Post("/form", func(context.Context, form) (struct{}, error) { return struct{}{}, nil },
			WithInput(FormInput[form]()), WithOutput(EmptyOutput().WithStatus(http.StatusNoContent))),
		Post("/xml", func(context.Context, input) (input, error) { return input{}, nil },
			WithInput(XMLInput[input]()), WithOutput(XMLOutput[input]())),
		Get("/text", func(context.Context, struct{}) (input, error) { return input{}, nil },
			WithOutput(TextOutput[input]())),
		Method("PURGE", "/cache", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }, WithOutput(EmptyOutput())),
	); err != nil {
		t.Fatal(err)
	}
	data, err := s.OpenAPI("Codecs", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	jsonOp := paths["/json"].(map[string]any)["post"].(map[string]any)
	if jsonOp["parameters"] != nil {
		t.Fatalf("explicit JSON codec must not emit query parameter: %v", jsonOp)
	}
	response := jsonOp["responses"].(map[string]any)["200"].(map[string]any)
	ref := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	if ref != "#/components/schemas/"+schemaName(reflect.TypeFor[openAPIUser]()) {
		t.Fatalf("Reply schema must describe Body, got %v", ref)
	}
	formOp := paths["/form"].(map[string]any)["post"].(map[string]any)
	properties := formOp["requestBody"].(map[string]any)["content"].(map[string]any)["application/x-www-form-urlencoded"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
	if properties["form_value"] == nil || properties["json_value"] != nil {
		t.Fatalf("form field names = %v", properties)
	}
	if response := formOp["responses"].(map[string]any)["204"].(map[string]any); response["content"] != nil || response["x-gk-response-schema-unavailable"] != nil {
		t.Fatalf("no-body response = %v", response)
	}
	xmlOp := paths["/xml"].(map[string]any)["post"].(map[string]any)
	xmlBody := xmlOp["requestBody"].(map[string]any)["content"].(map[string]any)["application/xml"].(map[string]any)
	if xmlBody["schema"] != nil || xmlOp["x-gk-request-schema-unavailable"] == nil {
		t.Fatalf("unknown XML request schema = %v", xmlOp)
	}
	xmlResponse := xmlOp["responses"].(map[string]any)["200"].(map[string]any)
	if xmlResponse["content"].(map[string]any)["application/xml"].(map[string]any)["schema"] != nil {
		t.Fatalf("unknown XML response schema = %v", xmlResponse)
	}
	textResponse := paths["/text"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)
	if textResponse["content"].(map[string]any)["text/plain"].(map[string]any)["schema"].(map[string]any)["type"] != "string" {
		t.Fatalf("text response schema = %v", textResponse)
	}
	if paths["/cache"].(map[string]any)["x-gk-method-purge"] == nil {
		t.Fatalf("custom HTTP method extension = %v", paths["/cache"])
	}
}

func TestServerOpenAPICatchAllPath(t *testing.T) {
	type input struct {
		Name string `path:"name"`
	}
	s := NewServer()
	if err := s.Register(Get("/files/{name...}", func(context.Context, input) (string, error) { return "ok", nil })); err != nil {
		t.Fatal(err)
	}
	data, err := s.OpenAPI("Files", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	if paths["/files/{name}"] == nil || paths["/files/{name...}"] != nil {
		t.Fatalf("normalized catch-all path = %v", paths)
	}
	params := paths["/files/{name}"].(map[string]any)["get"].(map[string]any)["parameters"].([]any)
	if len(params) != 1 || params[0].(map[string]any)["name"] != "name" {
		t.Fatalf("catch-all parameter = %v", params)
	}
}
