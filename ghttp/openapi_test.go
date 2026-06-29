package ghttp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestOpenAPIBuildsValidSpec(t *testing.T) {
	app := New(WithOpenAPI("My API", "1.0.0"))

	type CreateUserReq struct {
		Path struct {
			OrgID string `path:"orgId"`
		}
		Body struct {
			Name string `json:"name" minLength:"1" maxLength:"100"`
		}
	}
	type UserData struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	Post[CreateUserReq, struct{ Body UserData }](app, "/orgs/{orgId}/users", func(ctx context.Context, req *CreateUserReq) (*struct{ Body UserData }, error) {
		return nil, nil
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
