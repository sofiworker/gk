package ghttp

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
)

func TestParseInput_PathQueryHeaderBody(t *testing.T) {
	type Req struct {
		Path struct {
			ID    string `path:"id"`
			OrgID string `path:"orgId"`
		}
		Query struct {
			Page int `query:"page"`
		}
		Header struct {
			Auth string `header:"Authorization"`
		}
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

	if input.Path.ID != "99" {
		t.Fatalf("expected Path.ID=99, got %s", input.Path.ID)
	}
	if input.Path.OrgID != "org-42" {
		t.Fatalf("expected Path.OrgID=org-42, got %s", input.Path.OrgID)
	}
	if input.Query.Page != 3 {
		t.Fatalf("expected Query.Page=3, got %d", input.Query.Page)
	}
	if input.Header.Auth != "Bearer xxx" {
		t.Fatalf("expected Header.Auth=Bearer xxx, got %s", input.Header.Auth)
	}
	if input.Body.Name != "Alice" {
		t.Fatalf("expected Body.Name=Alice, got %s", input.Body.Name)
	}
	if input.Body.Age != 30 {
		t.Fatalf("expected Body.Age=30, got %d", input.Body.Age)
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

func TestParseInput_PathParamsInContext(t *testing.T) {
	type Req struct {
		Path struct {
			ID string `path:"id"`
		}
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
	if input.Path.ID != "99" {
		t.Fatalf("expected Path.ID=99, got %s", input.Path.ID)
	}
}
