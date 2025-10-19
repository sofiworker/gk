package gserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNew(t *testing.T) {
	server := NewServer()
	noop := func(*Context) {}

	group := server.Group("/v1")
	group.GET("/v1/test", noop)
	group.POST("/v1/test2", noop)

	group2 := group.Group("/v1.1/test")
	group2.GET("/v1/v1.1/test/test", noop)

	group3 := group2.Group("/v1.12/test")
	group3.POST("/v1/v1.1/test/v1.12/test/test/:id", noop)
	group3.POST("/v1/v1.1/test/v1.12/test/test/*id", noop)

	server.GET("/test/:name/:last_name/*wild", noop)

	testCases := []struct {
		method     string
		path       string
		wantPath   string
		wantParams map[string]string
	}{
		{"GET", "/v1/test", "/v1/test", nil},
		{"POST", "/v1/test2", "/v1/test2", nil},
		{"GET", "/v1/v1.1/test/test", "/v1/v1.1/test/test", nil},
		{"POST", "/v1/v1.1/test/v1.12/test/test/42", "/v1/v1.1/test/v1.12/test/test/:id", map[string]string{"id": "42"}},
		{"POST", "/v1/v1.1/test/v1.12/test/test/static/path", "/v1/v1.1/test/v1.12/test/test/*id", map[string]string{"id": "static/path"}},
		{"GET", "/test/john/doe/more/info", "/test/:name/:last_name/*wild", map[string]string{"name": "john", "last_name": "doe", "wild": "more/info"}},
	}

	for _, tc := range testCases {
		result := server.matcher.Match(tc.method, tc.path)
		if result == nil {
			t.Fatalf("expected %s %s to match", tc.method, tc.path)
		}
		if result.Path != tc.wantPath {
			t.Fatalf("expected pattern %s, got %s", tc.wantPath, result.Path)
		}
		if len(tc.wantParams) == 0 {
			if len(result.Params) != 0 {
				t.Fatalf("expected no params for %s %s, got %+v", tc.method, tc.path, result.Params)
			}
			continue
		}
		for k, v := range tc.wantParams {
			if result.Params[k] != v {
				t.Fatalf("expected param %s=%s, got %s", k, v, result.Params[k])
			}
		}
	}

	if result := server.matcher.Match("DELETE", "/v1/test"); result != nil {
		t.Fatal("unexpected match for DELETE /v1/test")
	}
	if result := server.matcher.Match("GET", "/unknown"); result != nil {
		t.Fatal("unexpected match for GET /unknown")
	}
}

func TestRoute(t *testing.T) {
	// Test routes
	routes := []string{
		"/test",
		"/test/",
		"/simple",
		"/project/:name",
		"/",
		"/news/home",
		"/news",
		"/simple-two/one",
		"/simple-two/one-two",
		"/project/:name/build/*params",
		"/project/:name/bui",
		"/user/:id/status",
		"/user/:id",
		"/user/:id/profile",
		"/a/b/c/d/e/f/g/h/i/j/k",
		"/a/b/c/d/e/f/g/h/i/j/k/:id",
		"/a/b/c/d/e/f/g/h/i/j/k/:id/*params",
		"/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z",
		"/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z/:id",
		"/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z/:id/*params",
		"/a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a",
		"/a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/*params",
		"/ffff?query=string",
		"/ffff/?query=string",
		"/ffff/static?query=string",
		"/ffff/static/?query=string",
		"/complex/:param1/static/*param2?query=string",
		"/complex/:param1/static/?query=string",
		"/complex/static/*param2?query=string",
		"/complex/static/?query=string",
		"/complex/static/path?query=string&another=param&&&test",
		"/complex/static/path/?query=string&another=param&&&test",
		"/complex/:param1/static/path?query=string&another=param&test=1",
	}
	matcher := newServerMatcher()
	for _, route := range routes {
		err := matcher.AddRoute("GET", route, func(c *Context) {})
		if err != nil {
			t.Errorf("Failed to add route %s: %v", route, err)
		}
	}

	for _, route := range routes {
		matchResult := matcher.Match("GET", route)
		bs, _ := json.Marshal(matchResult)
		fmt.Println(string(bs))
	}
}

func TestServerServeHTTP_PathAndQueryParameters(t *testing.T) {
	server := NewServer()
	var (
		called     bool
		gotID      string
		gotFoo     string
		gotDefault string
		gotArr     []string
	)

	server.GET("/users/:id", func(c *Context) {
		called = true
		gotID = c.Param("id")
		gotFoo = c.Query("foo")
		gotDefault = c.DefaultQuery("missing", "fallback")
		gotArr = c.QueryArray("arr")
		c.Writer.WriteHeader(http.StatusCreated)
		_, _ = c.Writer.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42?foo=bar&arr=a&arr=b", nil)
	resp := httptest.NewRecorder()
	server.ServeHTTP(resp, req)

	if !called {
		t.Fatal("expected handler to be called")
	}
	if gotID != "42" {
		t.Fatalf("expected id=42, got %q", gotID)
	}
	if gotFoo != "bar" {
		t.Fatalf("expected foo=bar, got %q", gotFoo)
	}
	if gotDefault != "fallback" {
		t.Fatalf("expected missing query default fallback, got %q", gotDefault)
	}
	if !reflect.DeepEqual(gotArr, []string{"a", "b"}) {
		t.Fatalf("unexpected arr values: %+v", gotArr)
	}
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, resp.Code)
	}
	if body := resp.Body.String(); body != "ok" {
		t.Fatalf("expected body 'ok', got %q", body)
	}
}

func TestServer(t *testing.T) {
	s := NewServer()
	s.GET("/test", func(ctx *Context) {
	})
	s.Start()
}
