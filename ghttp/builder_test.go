package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func testHandler(ctx context.Context, req *testInput) (*testOutput, error) {
	return &testOutput{
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
	app := New()

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

	// With default envelope: {code, msg, data}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal envelope failed: %v", err)
	}

	var data struct {
		Status int `json:"Status"`
		Body   struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"Body"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
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
	app := New()

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

	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &envelope)
	if envelope.Code != 0 {
		t.Fatalf("expected code=0, got %d", envelope.Code)
	}
}

func TestRouteBuilderToReturnsRegisterErrorAndSkipsOpenAPI(t *testing.T) {
	wantErr := errors.New("register failed")
	router := &failingRouter{err: wantErr}
	app := New(WithOpenAPI("test", "1.0.0"), WithRouter(router))

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

type failingRouter struct {
	err error
}

func (r *failingRouter) Register(string, string, http.Handler) error {
	return r.err
}

func (r *failingRouter) ServeHTTP(http.ResponseWriter, *http.Request) {}
