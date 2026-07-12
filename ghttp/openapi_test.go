package ghttp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestOpenAPIBuildsValidSpec(t *testing.T) {
	app := New(WithOpenAPI("My API", "1.0.0"), WithProduces(MIMEJSON))

	type CreateUserReq struct {
		OrgID string `path:"orgId"`
		Body  struct {
			Name string `json:"name" minLength:"1" maxLength:"100"`
		}
	}
	type UserData struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	Route[CreateUserReq, struct{ Body UserData }](app).POST("/orgs/{orgId}/users").To(func(ctx context.Context, req CreateUserReq) (struct{ Body UserData }, error) {
		return struct{ Body UserData }{}, nil
	})

	spec := serverOpenAPISpec(t, app)
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
		Consumes(MIMEXML, MIMEJSON).
		To(func(ctx context.Context, req CreateUserReq) (struct{}, error) {
			return struct{}{}, nil
		})

	spec := serverOpenAPISpec(t, app)
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

func TestOpenAPIDocOptionsAndInferredRouteTypes(t *testing.T) {
	app := New(WithOpenAPI("My API", "1.0.0"), WithProduces(MIMEJSON, MIMEXML))

	type createUserReq struct {
		OrgID   string `path:"orgId" doc:"organization id"`
		Page    int    `query:"page" doc:"page number"`
		TraceID string `header:"X-Trace-ID" doc:"trace id"`
		Session string `cookie:"sid" doc:"session id"`
		Body    struct {
			Name string `json:"name" doc:"user name" minLength:"1"`
		}
	}
	type userDTO struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	Route[createUserReq, userDTO](app).
		POST("/orgs/{orgId}/users").
		Consumes(MIMEJSON, MIMEXML).
		Doc(
			Summary("Create user"),
			Description("Create one user"),
			Tags("users", "admin"),
			OperationID("createUser"),
			Success(Code(0), Message("created")),
			Errors(Err(4001, "invalid input")),
		).
		To(func(ctx context.Context, req createUserReq) (userDTO, error) {
			return userDTO{ID: req.OrgID, Name: req.Body.Name}, nil
		})

	op := openAPIOperation(t, app, "/orgs/{orgId}/users", "post")
	if got := op["summary"]; got != "Create user" {
		t.Fatalf("summary = %v, want Create user", got)
	}
	if got := op["description"]; got != "Create one user" {
		t.Fatalf("description = %v, want Create one user", got)
	}
	if got := op["operationId"]; got != "createUser" {
		t.Fatalf("operationId = %v, want createUser", got)
	}

	params := op["parameters"].([]interface{})
	assertOpenAPIParameter(t, params, "path", "orgId")
	assertOpenAPIParameter(t, params, "query", "page")
	assertOpenAPIParameter(t, params, "header", "X-Trace-ID")
	assertOpenAPIParameter(t, params, "cookie", "sid")

	requestContent := op["requestBody"].(map[string]interface{})["content"].(map[string]interface{})
	if _, ok := requestContent[MIMEJSON]; !ok {
		t.Fatalf("request body content missing %s: %#v", MIMEJSON, requestContent)
	}
	if _, ok := requestContent[MIMEXML]; !ok {
		t.Fatalf("request body content missing %s: %#v", MIMEXML, requestContent)
	}

	responses := op["responses"].(map[string]interface{})
	okResp := responses["200"].(map[string]interface{})
	responseContent := okResp["content"].(map[string]interface{})
	if _, ok := responseContent[MIMEJSON]; !ok {
		t.Fatalf("response content missing %s: %#v", MIMEJSON, responseContent)
	}
	if _, ok := responseContent[MIMEXML]; !ok {
		t.Fatalf("response content missing %s: %#v", MIMEXML, responseContent)
	}

	success := op["x-ghttp-success"].(map[string]interface{})
	if got := success["code"]; got != float64(0) {
		t.Fatalf("success code = %v, want 0", got)
	}
	if got := success["message"]; got != "created" {
		t.Fatalf("success message = %v, want created", got)
	}

	errorsDoc := op["x-ghttp-errors"].([]interface{})
	if len(errorsDoc) != 1 {
		t.Fatalf("x-ghttp-errors len = %d, want 1", len(errorsDoc))
	}
	errDoc := errorsDoc[0].(map[string]interface{})
	if got := errDoc["code"]; got != float64(4001) {
		t.Fatalf("error code = %v, want 4001", got)
	}
	if got := errDoc["message"]; got != "invalid input" {
		t.Fatalf("error message = %v, want invalid input", got)
	}
}

func TestOpenAPIAutoDocumentsReqAndRespWithoutDoc(t *testing.T) {
	app := New(WithOpenAPI("My API", "1.0.0"), WithProduces(MIMEJSON))

	type getUserReq struct {
		ID     string `path:"id"`
		Expand bool   `query:"expand"`
	}
	type getUserResp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	Route[getUserReq, getUserResp](app).
		GET("/users/{id}").
		To(func(ctx context.Context, req getUserReq) (getUserResp, error) {
			return getUserResp{ID: req.ID, Name: "Alice"}, nil
		})

	op := openAPIOperation(t, app, "/users/{id}", "get")
	if _, ok := op["summary"]; ok {
		t.Fatalf("summary = %v, want omitted", op["summary"])
	}
	params := op["parameters"].([]interface{})
	assertOpenAPIParameter(t, params, "path", "id")
	assertOpenAPIParameter(t, params, "query", "expand")

	responses := op["responses"].(map[string]interface{})
	okResp := responses["200"].(map[string]interface{})
	content := okResp["content"].(map[string]interface{})
	jsonMedia := content[MIMEJSON].(map[string]interface{})
	schema := jsonMedia["schema"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	if _, ok := props["id"]; !ok {
		t.Fatalf("response schema missing id property: %#v", props)
	}
	if _, ok := props["name"]; !ok {
		t.Fatalf("response schema missing name property: %#v", props)
	}
}

func TestOpenAPIDocLifecycleOptions(t *testing.T) {
	app := New(WithOpenAPI("My API", "1.0.0"), WithProduces(MIMEJSON))
	sunset := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)

	Route[struct{}, struct{}](app).
		GET("/users").
		Doc(
			Deprecated("use /v2/users instead"),
			Sunset(sunset),
			ExternalDocs("migration guide", "https://example.com/migrate-users"),
		).
		To(func(ctx context.Context, req struct{}) (struct{}, error) {
			return struct{}{}, nil
		})

	op := openAPIOperation(t, app, "/users", "get")
	if got := op["deprecated"]; got != true {
		t.Fatalf("deprecated = %v, want true", got)
	}
	if got := op["x-ghttp-deprecated-reason"]; got != "use /v2/users instead" {
		t.Fatalf("deprecated reason = %v, want migration hint", got)
	}
	if got := op["x-ghttp-sunset"]; got != sunset.Format(time.RFC3339) {
		t.Fatalf("sunset = %v, want %s", got, sunset.Format(time.RFC3339))
	}
	externalDocs := op["externalDocs"].(map[string]interface{})
	if got := externalDocs["description"]; got != "migration guide" {
		t.Fatalf("externalDocs.description = %v, want migration guide", got)
	}
	if got := externalDocs["url"]; got != "https://example.com/migrate-users" {
		t.Fatalf("externalDocs.url = %v, want migration URL", got)
	}
}

func openAPIOperation(t *testing.T, app *Server, path, method string) map[string]interface{} {
	t.Helper()

	spec := serverOpenAPISpec(t, app)
	var doc map[string]interface{}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("invalid OpenAPI JSON: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	item := paths[path].(map[string]interface{})
	return item[method].(map[string]interface{})
}

func serverOpenAPISpec(t *testing.T, app *Server) []byte {
	t.Helper()
	spec, err := app.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI error = %v", err)
	}
	return spec
}

func assertOpenAPIParameter(t *testing.T, params []interface{}, in, name string) {
	t.Helper()
	for _, raw := range params {
		param := raw.(map[string]interface{})
		if param["in"] == in && param["name"] == name {
			return
		}
	}
	t.Fatalf("parameter %s:%s not found in %#v", in, name, params)
}
