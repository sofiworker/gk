package gserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
)

func TestServerServeHTTP_PathAndQuery(t *testing.T) {
	server := NewServer()
	var (
		gotID   string
		gotRole string
		arr     []string
	)
	server.GET("/users/:id", func(c *Context) {
		gotID = c.Param("id")
		gotRole = c.QueryDefault("role", "none")
		arr = c.QueryArray("role")
		c.JSON(http.StatusCreated, map[string]string{
			"id":   gotID,
			"role": gotRole,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42?role=admin&role=viewer", nil)
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.Code)
	}
	if gotID != "42" || gotRole != "admin" {
		t.Fatalf("unexpected params id=%s role=%s", gotID, gotRole)
	}
	if !reflect.DeepEqual(arr, []string{"admin", "viewer"}) {
		t.Fatalf("unexpected array values %v", arr)
	}

	var payload map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["id"] != "42" || payload["role"] != "admin" {
		t.Fatalf("unexpected body %+v", payload)
	}
	if ct := resp.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("unexpected content type %q", ct)
	}
}

func TestServerMiddlewareOrder(t *testing.T) {
	server := NewServer()
	var order []string

	server.Use(func(c *Context) {
		order = append(order, "m1-before")
		c.Next()
		order = append(order, "m1-after")
	})
	server.GET("/ping", func(c *Context) {
		order = append(order, "handler")
		c.String(http.StatusOK, "pong")
	})

	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if resp.Code != http.StatusOK || resp.Body.String() != "pong" {
		t.Fatalf("unexpected response code=%d body=%q", resp.Code, resp.Body.String())
	}
	expected := []string{"m1-before", "handler", "m1-after"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("unexpected middleware order %v", order)
	}
}

func TestServerNotFound(t *testing.T) {
	server := NewServer()
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/not-found", nil))
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func BenchmarkServerFastHandler(b *testing.B) {
	server := NewServer()
	server.GET("/bench/:id", func(c *Context) {
		c.String(http.StatusOK, "%s", c.Param("id"))
	})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := fasthttp.AcquireRequest()
		req.Header.SetMethod("GET")
		req.SetRequestURI("/bench/123")

		var ctx fasthttp.RequestCtx
		ctx.Init(req, resolveRemoteAddr("127.0.0.1:1234"), nil)
		server.FastHandler(&ctx)

		fasthttp.ReleaseRequest(req)
	}
}
