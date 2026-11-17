package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetInto 验证 sink 泛型的核心优势：T 从 *T 推断，调用处无需写 [T]。
func TestGetInto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":11,"name":"generic"}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var user testUser
	resp, err := GetInto(context.Background(), c, "/users/{id}", &user, WithRequestPathParam("id", "11"))
	if err != nil {
		t.Fatalf("GetInto: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d", resp.StatusCode())
	}
	if user.ID != 11 || user.Name != "generic" {
		t.Fatalf("user = %+v", user)
	}
}

// TestPostInto 验证泛型 POST 同时编码请求体并解码响应体。
func TestPostInto(t *testing.T) {
	var got testUser
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":99,"name":"created"}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var out testUser
	if _, err := PostInto(context.Background(), c, "/users", testUser{Name: "sent"}, &out); err != nil {
		t.Fatalf("PostInto: %v", err)
	}
	if got.Name != "sent" {
		t.Fatalf("server received %+v", got)
	}
	if out.ID != 99 {
		t.Fatalf("out = %+v", out)
	}
}

// TestAsReturnsResult 验证返回式泛型入口（需显式实例化）与其 Result 容器。
func TestAsReturnsResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":5}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	resp, err := c.R().Get("/x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	result, err := As[testUser](resp)
	if err != nil {
		t.Fatalf("As: %v", err)
	}
	if result.Data.ID != 5 {
		t.Fatalf("Data = %+v", result.Data)
	}
	if result.Response != resp {
		t.Fatal("Result must carry the originating response")
	}
}

// TestPerRequestTimeout 验证每请求超时通过 context 生效，且不影响 Client 配置。
func TestPerRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	c := New(WithBaseURL(srv.URL))
	start := time.Now()
	_, err := c.R().SetTimeout(80 * time.Millisecond).Get("/slow")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("per-request timeout did not take effect, took %s", elapsed)
	}
	// Client 自身不应被改动。
	// The Client itself must be untouched.
	if c.HTTPClient().Timeout != 0 {
		t.Fatalf("client Timeout mutated to %v", c.HTTPClient().Timeout)
	}
}

// TestRequestOptions 验证每请求选项在泛型入口上生效。
func TestRequestOptions(t *testing.T) {
	var header, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Test")
		query = r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var user testUser
	if _, err := GetInto(context.Background(), c, "/x", &user,
		WithRequestHeader("X-Test", "v"),
		WithRequestQuery("page", 3),
	); err != nil {
		t.Fatalf("GetInto: %v", err)
	}
	if header != "v" || query != "3" {
		t.Fatalf("header=%q query=%q", header, query)
	}
}

// TestGenericEntryPropagatesStatusError 验证泛型入口不吞掉非 2xx 语义，且带回响应。
func TestGenericEntryPropagatesStatusError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	var user testUser
	resp, err := GetInto(context.Background(), c, "/missing", &user)
	if err == nil {
		t.Fatal("expected an error for 404")
	}
	if resp == nil || resp.StatusCode() != 404 {
		t.Fatalf("the response must still be returned, got %v", resp)
	}
	if user.ID != 0 {
		t.Fatalf("the target must not be decoded on failure, got %+v", user)
	}
}
