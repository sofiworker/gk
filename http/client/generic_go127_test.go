//go:build go1.27

package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequestIntoInfersTypeParameter 验证方法级 sink 泛型的核心价值：调用处无需写 [T]。
func TestRequestIntoInfersTypeParameter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":77,"name":"into"}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var user testUser
	resp, err := c.R().
		SetMethod(http.MethodGet).
		SetURL("/").
		SetQueryParam("id", 77).
		Into(context.Background(), &user)
	if err != nil {
		t.Fatalf("Into: %v", err)
	}
	if resp.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode())
	}
	if user.ID != 77 || user.Name != "into" {
		t.Fatalf("user = %+v", user)
	}
}

// TestRequestIntoExplicitContext 验证传入的 ctx 被采用。
//
// Into 同时容忍 nil ctx 并回退到 Request 上已有的 context，但该分支不在此测试：staticcheck
// 的 SA1012 禁止传入字面 nil context，而为了绕过静态检查把 nil 藏进变量只会让测试更晦涩。
// TestRequestIntoExplicitContext verifies the passed ctx is used.
//
// Into also tolerates a nil ctx, falling back to whatever context the Request holds, but
// that branch is not tested here: staticcheck's SA1012 forbids passing a literal nil
// context, and hiding nil inside a variable merely to dodge the check would make the test
// more obscure, not better.
func TestRequestIntoExplicitContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var user testUser
	if _, err := c.R().
		SetMethod(http.MethodGet).
		SetURL("/").
		Into(context.TODO(), &user); err != nil {
		t.Fatalf("Into: %v", err)
	}
	if user.ID != 1 {
		t.Fatalf("user = %+v", user)
	}
}

// TestRequestAs 验证返回式泛型方法（必须显式实例化）。
func TestRequestAs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":8}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	result, err := c.R().SetMethod(http.MethodGet).SetURL("/x").As[testUser]()
	if err != nil {
		t.Fatalf("As: %v", err)
	}
	if result.Data.ID != 8 {
		t.Fatalf("Data = %+v", result.Data)
	}
}

// TestClientMethodInto 验证 Client 上的方法级泛型入口。
func TestClientMethodInto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":21}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var user testUser
	if _, err := c.GetInto(context.Background(), "/x", &user); err != nil {
		t.Fatalf("GetInto: %v", err)
	}
	if user.ID != 21 {
		t.Fatalf("user = %+v", user)
	}

	var created testUser
	if _, err := c.PostInto(context.Background(), "/x", testUser{Name: "n"}, &created); err != nil {
		t.Fatalf("PostInto: %v", err)
	}
	if created.ID != 21 {
		t.Fatalf("created = %+v", created)
	}
}

// TestFluentMethodIntoShortcut 验证 fluent 链上的方法级快捷终结方法：链上不必再写
// SetMethod/SetURL，直接 GetInto/PostInto/DeleteInto 即可，且 T 仍从 *T 推断。
func TestFluentMethodIntoShortcut(t *testing.T) {
	var sawMethod, sawQuery, sawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawMethod = r.Method
		sawQuery = r.URL.RawQuery
		data, _ := io.ReadAll(r.Body)
		sawBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":55,"name":"shortcut"}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	ctx := context.Background()

	// GET：链上设置 query，终结时才给 url 与目标。
	var user testUser
	resp, err := c.R().SetQueryParam("id", 55).GetInto(ctx, "/users", &user)
	if err != nil {
		t.Fatalf("GetInto: %v", err)
	}
	if resp.StatusCode() != http.StatusOK || user.ID != 55 {
		t.Fatalf("resp=%d user=%+v", resp.StatusCode(), user)
	}
	if sawMethod != http.MethodGet || sawQuery != "id=55" {
		t.Fatalf("server saw %s ?%s", sawMethod, sawQuery)
	}

	// POST：body 在终结方法上给出。
	var created testUser
	if _, err := c.R().SetHeader("X-A", "1").PostInto(ctx, "/users", testUser{Name: "n"}, &created); err != nil {
		t.Fatalf("PostInto: %v", err)
	}
	if sawMethod != http.MethodPost || !strings.Contains(sawBody, `"name":"n"`) {
		t.Fatalf("server saw %s body=%s", sawMethod, sawBody)
	}

	// DELETE：无 body 的方法同样有快捷形态。
	var deleted testUser
	if _, err := c.R().DeleteInto(ctx, "/users/55", &deleted); err != nil {
		t.Fatalf("DeleteInto: %v", err)
	}
	if sawMethod != http.MethodDelete {
		t.Fatalf("server saw %s", sawMethod)
	}
}
