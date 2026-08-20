package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestContextTypedKeyRoundTrip 验证类型安全键的存取与缺失语义。
// TestContextTypedKeyRoundTrip verifies the typed key round trip and the
// missing-value semantics.
func TestContextTypedKeyRoundTrip(t *testing.T) {
	key := NewKey[string]("request-id")
	ctx := context.Background()

	if _, ok := key.Get(ctx); ok {
		t.Fatal("missing key should report ok=false")
	}
	ctx = key.Set(ctx, "abc")
	got, ok := key.Get(ctx)
	if !ok || got != "abc" {
		t.Fatalf("Get = (%q, %v), want (\"abc\", true)", got, ok)
	}
}

// TestContextTypedKeyFollowsRequestContext 验证键挂在 request context 上,
// 中间件写入后 handler 可读。
// TestContextTypedKeyFollowsRequestContext verifies keys ride the request
// context: a middleware writes, the handler reads.
func TestContextTypedKeyFollowsRequestContext(t *testing.T) {
	key := NewKey[string]("request-id")
	server := New()
	server.MustMount(RawOperation("GET", "/key", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := key.Get(r.Context())
		if !ok || got != "abc" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})))
	server.Use(func(c *Ctx) {
		c.R = c.R.WithContext(key.Set(c.R.Context(), "abc"))
		c.Next()
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/key", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

// TestContextTypedKeysAreDistinct 验证不同 Key 实例互不干扰。
// TestContextTypedKeysAreDistinct verifies distinct Key instances do not clash.
func TestContextTypedKeysAreDistinct(t *testing.T) {
	a := NewKey[string]("a")
	b := NewKey[string]("b")
	ctx := context.Background()
	ctx = a.Set(ctx, "value-a")
	ctx = b.Set(ctx, "value-b")
	if got, _ := a.Get(ctx); got != "value-a" {
		t.Fatalf("key a = %q", got)
	}
	if got, _ := b.Get(ctx); got != "value-b" {
		t.Fatalf("key b = %q", got)
	}
}
