package ghttp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestOpenAPIBuildsValidSpec(t *testing.T) {
	app := New(WithOpenAPI("My API", "1.0.0"), WithProduces(MIMEJSON))

	type CreateUserReq struct {
		Body struct {
			Name string `json:"name" minLength:"1" maxLength:"100"`
		}
	}
	type CreateUserPath struct {
		OrgID string `path:"orgId"`
	}
	type UserData struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	Route[CreateUserReq, struct{ Body UserData }](app).POST("/orgs/{orgId}/users").PathSchema(CreateUserPath{}).To(func(ctx context.Context, req CreateUserReq) (struct{ Body UserData }, error) {
		return struct{ Body UserData }{}, nil
	})

	spec := app.openAPI.Build()
	if len(spec) == 0 {
		t.Fatal("expected non-empty OpenAPI spec")
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("invalid OpenAPI JSON: %v", err)
	}

	if doc["openapi"] != "3.1.0" {
		t.Fatalf("expected openapi 3.1.0, got %v", doc["openapi"])
	}
}

func TestOpenAPIRequestBodyUsesConsumes(t *testing.T) {
	app := New(WithOpenAPI("My API", "1.0.0"), WithProduces(MIMEJSON))

	type CreateUserReq struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	Route[CreateUserReq, struct{}](app).
		POST("/users").
		Reads(CreateUserReq{}).
		Consumes(MIMEXML, MIMEJSON).
		To(func(ctx context.Context, req CreateUserReq) (struct{}, error) {
			return struct{}{}, nil
		})

	spec := app.openAPI.Build()
	var doc map[string]interface{}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("invalid OpenAPI JSON: %v", err)
	}

	paths := doc["paths"].(map[string]interface{})
	item := paths["/users"].(map[string]interface{})
	op := item["post"].(map[string]interface{})
	requestBody := op["requestBody"].(map[string]interface{})
	content := requestBody["content"].(map[string]interface{})
	if _, ok := content[MIMEXML]; !ok {
		t.Fatalf("requestBody content missing %s: %#v", MIMEXML, content)
	}
	if _, ok := content[MIMEJSON]; !ok {
		t.Fatalf("requestBody content missing %s: %#v", MIMEJSON, content)
	}
}

func TestSchemaGeneration(t *testing.T) {
	type User struct {
		Name string `json:"name" doc:"User name" minLength:"1" maxLength:"100"`
		Age  int    `json:"age" minimum:"0" maximum:"150"`
	}

	tp := reflect.TypeOf(User{})
	schema := generateSchema(tp)
	props := schema["properties"].(map[string]interface{})

	nameProp := props["name"].(map[string]interface{})
	if nameProp["type"] != "string" {
		t.Fatalf("expected type string, got %v", nameProp["type"])
	}
	if nameProp["minLength"] != int64(1) {
		t.Fatalf("expected minLength 1, got %v (type: %T)", nameProp["minLength"], nameProp["minLength"])
	}
}
