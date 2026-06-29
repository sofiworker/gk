package ghttp

import (
	"context"
	"encoding/json"
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

	Route[testInput, testOutput](app, "/users/{id}").
		POST("").
		Doc("Create user").
		Tags("Users").
		OperationID("createUser").
		Reads(testInput{}).
		Responds(http.StatusCreated).With(testOutput{}).Desc("Created").End().
		To(testHandler)

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

func TestShortcutPOST(t *testing.T) {
	app := New()

	Post[testInput, testOutput](app, "/users/{id}", testHandler)

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
