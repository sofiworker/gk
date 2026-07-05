package ghttp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseInput_ParamsAndBody(t *testing.T) {
	type Req struct {
		Params `json:"-"`

		Body struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
	}

	r := httptest.NewRequest("POST", "/users/99?page=3", bytes.NewReader([]byte(`{"name":"Alice","age":30}`)))
	r.Header.Set("Authorization", "Bearer xxx")
	r.Header.Set("Content-Type", "application/json")

	// Set path params in context
	ctx := context.WithValue(r.Context(), pathParamsKey, map[string]string{"id": "99", "orgId": "org-42"})
	r = r.WithContext(ctx)

	var input Req
	if err := parseInput(r, &input); err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}

	if input.Path("id") != "99" {
		t.Fatalf("expected Path(id)=99, got %s", input.Path("id"))
	}
	if input.Path("orgId") != "org-42" {
		t.Fatalf("expected Path(orgId)=org-42, got %s", input.Path("orgId"))
	}
	if input.Query("page") != "3" {
		t.Fatalf("expected Query(page)=3, got %s", input.Query("page"))
	}
	if input.Header("Authorization") != "Bearer xxx" {
		t.Fatalf("expected Header(Authorization)=Bearer xxx, got %s", input.Header("Authorization"))
	}
	if input.Body.Name != "Alice" {
		t.Fatalf("expected Body.Name=Alice, got %s", input.Body.Name)
	}
	if input.Body.Age != 30 {
		t.Fatalf("expected Body.Age=30, got %d", input.Body.Age)
	}
}

func TestParseInput_BindsPathQueryHeaderCookieTags(t *testing.T) {
	type Req struct {
		ID       int    `path:"id"`
		Page     int    `query:"page"`
		Active   bool   `query:"active"`
		Token    string `header:"X-Token"`
		Session  string `cookie:"session_id"`
		Fallback string `query:"missing" default:"fallback"`

		Body struct {
			Name string `json:"name"`
		}
	}

	r := httptest.NewRequest("POST", "/users/99?page=3&active=true", bytes.NewReader([]byte(`{"name":"Alice"}`)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Token", "secret")
	r.AddCookie(&http.Cookie{Name: "session_id", Value: "s1"})
	r = r.WithContext(context.WithValue(r.Context(), pathParamsKey, map[string]string{"id": "99"}))

	var input Req
	if err := parseInput(r, &input); err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}

	if input.ID != 99 || input.Page != 3 || !input.Active || input.Token != "secret" || input.Session != "s1" || input.Fallback != "fallback" {
		t.Fatalf("bound input = %#v, want path/query/header/cookie/default values", input)
	}
	if input.Body.Name != "Alice" {
		t.Fatalf("Body.Name = %q, want Alice", input.Body.Name)
	}
}

func TestParseInput_DirectParams(t *testing.T) {
	r := httptest.NewRequest("GET", "/users/99?role=admin", nil)
	r.Header.Set("X-Token", "secret")
	r = r.WithContext(context.WithValue(r.Context(), pathParamsKey, map[string]string{"id": "99"}))

	var params Params
	if err := parseInput(r, &params); err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}

	if params.Path("id") != "99" {
		t.Fatalf("Path(id) = %q, want 99", params.Path("id"))
	}
	if params.Query("role") != "admin" {
		t.Fatalf("Query(role) = %q, want admin", params.Query("role"))
	}
	if params.Header("X-Token") != "secret" {
		t.Fatalf("Header(X-Token) = %q, want secret", params.Header("X-Token"))
	}
}

func TestParseInput_RejectsDirectPointerParams(t *testing.T) {
	var params *Params
	r := httptest.NewRequest("GET", "/users/99", nil)

	err := parseInput(r, &params)
	if !errors.Is(err, ErrInvalidParamsUsage) {
		t.Fatalf("parseInput error = %v, want ErrInvalidParamsUsage", err)
	}
}

func TestParseInput_RejectsInvalidParamsUsage(t *testing.T) {
	type namedParams struct {
		Request Params `json:"-"`
	}
	type pointerParams struct {
		*Params `json:"-"`
	}
	type commonParams struct {
		Params `json:"-"`
	}
	type indirectParams struct {
		commonParams
	}

	tests := []struct {
		name  string
		input interface{}
	}{
		{name: "named params field", input: &namedParams{}},
		{name: "pointer params field", input: &pointerParams{}},
		{name: "indirect params field", input: &indirectParams{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/users/99", nil)
			err := parseInput(r, tt.input)
			if !errors.Is(err, ErrInvalidParamsUsage) {
				t.Fatalf("parseInput error = %v, want ErrInvalidParamsUsage", err)
			}
		})
	}
}

func TestParseInput_EmptyBody(t *testing.T) {
	type Req struct {
		Body struct {
			Name string `json:"name"`
		}
	}

	r := httptest.NewRequest("POST", "/foo", bytes.NewReader([]byte(`{}`)))
	r.Header.Set("Content-Type", "application/json")

	var input Req
	if err := parseInput(r, &input); err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}
}

func TestParseInput_ParamsPathInContext(t *testing.T) {
	type Req struct {
		Params `json:"-"`

		Body struct{} `json:"-"`
	}

	r := httptest.NewRequest("GET", "/users/99", nil)

	// Set path params in context (simulates router behavior)
	ctx := context.WithValue(r.Context(), pathParamsKey, map[string]string{"id": "99"})
	r = r.WithContext(ctx)

	var input Req
	if err := parseInput(r, &input); err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}
	if input.Path("id") != "99" {
		t.Fatalf("expected Path(id)=99, got %s", input.Path("id"))
	}
}
