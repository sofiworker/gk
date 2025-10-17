package gserver

import (
	"fmt"
	"testing"
)

// mock handler
func h1(ctx *Context) {}
func h2(ctx *Context) {}
func h3(ctx *Context) {}
func h4(ctx *Context) {}
func h5(ctx *Context) {}

func TestMatcher_StaticAndDynamicRoutes(t *testing.T) {
	m := newServerMatcher()

	// 添加各种路由
	err := m.AddRoute("GET", "/users", h1)
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	err = m.AddRoute("GET", "/users/:id", h2)
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	err = m.AddRoute("GET", "/assets/*path", h3)
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	err = m.AddRoute("GET", "/articles/:category/:id", h4)
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	err = m.AddRoute("POST", "/submit", h5)
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	// === 测试静态路由 ===
	r1 := m.Match("GET", "/users")
	if r1 == nil {
		t.Fatal("expected /users to match")
	}
	if len(r1.Handlers) != 1 {
		t.Fatal("wrong handler count for /users")
	}

	// === 测试参数路由 ===
	r2 := m.Match("GET", "/users/42")
	if r2 == nil {
		t.Fatal("expected /users/42 to match /users/:id")
	}
	if v := r2.Params["id"]; v != "42" {
		t.Fatalf("expected param id=42, got %v", v)
	}

	// === 测试通配符 ===
	r3 := m.Match("GET", "/assets/img/logo.png")
	if r3 == nil {
		t.Fatal("expected /assets/img/logo.png to match /assets/*path")
	}
	if v := r3.Params["path"]; v != "img/logo.png" {
		t.Fatalf("expected param path=img/logo.png, got %v", v)
	}

	// === 测试多参数 ===
	r4 := m.Match("GET", "/articles/tech/123")
	if r4 == nil {
		t.Fatal("expected /articles/tech/123 to match /articles/:category/:id")
	}
	if r4.Params["category"] != "tech" || r4.Params["id"] != "123" {
		t.Fatalf("wrong params: %+v", r4.Params)
	}

	// === 测试方法区分 ===
	r5 := m.Match("POST", "/submit")
	if r5 == nil {
		t.Fatal("expected POST /submit to match")
	}
	r6 := m.Match("GET", "/submit")
	if r6 != nil {
		t.Fatal("GET /submit should not match POST route")
	}

	// === 测试不存在路径 ===
	r7 := m.Match("GET", "/notfound")
	if r7 != nil {
		t.Fatal("expected /notfound not to match anything")
	}

	fmt.Println("✅ All route matching tests passed.")
}
