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
	if r1.Path != "/users" {
		t.Fatalf("expected matched pattern /users, got %s", r1.Path)
	}

	// === 测试参数路由 ===
	r2 := m.Match("GET", "/users/42")
	if r2 == nil {
		t.Fatal("expected /users/42 to match /users/:id")
	}
	if r2.Path != "/users/:id" {
		t.Fatalf("expected matched pattern /users/:id, got %s", r2.Path)
	}
	if v := r2.Params["id"]; v != "42" {
		t.Fatalf("expected param id=42, got %v", v)
	}

	// === 测试通配符 ===
	r3 := m.Match("GET", "/assets/img/logo.png")
	if r3 == nil {
		t.Fatal("expected /assets/img/logo.png to match /assets/*path")
	}
	if r3.Path != "/assets/*path" {
		t.Fatalf("expected matched pattern /assets/*path, got %s", r3.Path)
	}
	if v := r3.Params["path"]; v != "img/logo.png" {
		t.Fatalf("expected param path=img/logo.png, got %v", v)
	}

	// === 测试多参数 ===
	r4 := m.Match("GET", "/articles/tech/123")
	if r4 == nil {
		t.Fatal("expected /articles/tech/123 to match /articles/:category/:id")
	}
	if r4.Path != "/articles/:category/:id" {
		t.Fatalf("expected matched pattern /articles/:category/:id, got %s", r4.Path)
	}
	if r4.Params["category"] != "tech" || r4.Params["id"] != "123" {
		t.Fatalf("wrong params: %+v", r4.Params)
	}

	// === 测试方法区分 ===
	r5 := m.Match("POST", "/submit")
	if r5 == nil {
		t.Fatal("expected POST /submit to match")
	}
	if r5.Path != "/submit" {
		t.Fatalf("expected matched pattern /submit, got %s", r5.Path)
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

func TestMatcher_StaticPrecedenceOverParam(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/files/:name", h1); err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	if err := m.AddRoute("GET", "/files/static", h2); err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	staticMatch := m.Match("GET", "/files/static")
	if staticMatch == nil {
		t.Fatal("expected static route to match first")
	}
	if staticMatch.Path != "/files/static" {
		t.Fatalf("expected static route pattern, got %s", staticMatch.Path)
	}
	if staticMatch.Params != nil && len(staticMatch.Params) > 0 {
		t.Fatalf("expected no params for static route, got %+v", staticMatch.Params)
	}

	paramMatch := m.Match("GET", "/files/readme")
	if paramMatch == nil {
		t.Fatal("expected param route to match")
	}
	if paramMatch.Path != "/files/:name" {
		t.Fatalf("expected param route pattern, got %s", paramMatch.Path)
	}
	if paramMatch.Params["name"] != "readme" {
		t.Fatalf("expected name=readme, got %+v", paramMatch.Params)
	}
}

func TestMatcher_WildcardCoverage(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/download/*file", h3); err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}
	if err := m.AddRoute("GET", "/api/:version/files/*path", h4); err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	baseMatch := m.Match("GET", "/download")
	if baseMatch == nil {
		t.Fatal("expected /download to match wildcard route")
	}
	if baseMatch.Params["file"] != "" {
		t.Fatalf("expected empty wildcard capture, got %q", baseMatch.Params["file"])
	}

	nestedMatch := m.Match("GET", "/download/archives/file.zip")
	if nestedMatch == nil {
		t.Fatal("expected nested path to match wildcard route")
	}
	if nestedMatch.Params["file"] != "archives/file.zip" {
		t.Fatalf("expected archives/file.zip, got %q", nestedMatch.Params["file"])
	}

	mixedMatch := m.Match("GET", "/api/v2/files/images/logo.png")
	if mixedMatch == nil {
		t.Fatal("expected mixed param and wildcard route to match")
	}
	if mixedMatch.Path != "/api/:version/files/*path" {
		t.Fatalf("expected /api/:version/files/*path pattern, got %s", mixedMatch.Path)
	}
	if mixedMatch.Params["version"] != "v2" {
		t.Fatalf("expected version=v2, got %q", mixedMatch.Params["version"])
	}
	if mixedMatch.Params["path"] != "images/logo.png" {
		t.Fatalf("expected path=images/logo.png, got %q", mixedMatch.Params["path"])
	}

	noMatch := m.Match("GET", "/api/v2")
	if noMatch != nil {
		t.Fatal("expected missing tail to not match param+wildcard route")
	}
}

func BenchmarkMatcherAddRoutesThenMatchWithQueryAndWildcard(b *testing.B) {
	sizes := []int{100, 1000, 5000, 10000}
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, size := range sizes {
		sz := size
		b.Run(fmt.Sprintf("routes=%d", sz), func(b *testing.B) {
			b.ReportAllocs()

			m := newServerMatcher()
			type target struct {
				method string
				path   string
			}
			var targets []target

			emptyHandler := func(ctx *Context) {}

			for i := 0; i < sz; i++ {
				method := methods[i%len(methods)]
				switch i % 4 {
				case 0:
					// 静态路由 + 查询参数
					path := fmt.Sprintf("/static/%d/page", i)
					_ = m.AddRoute(method, path, emptyHandler)
					targets = append(targets, target{method: method, path: path + "?query=param&value=test"})
				case 1:
					// 参数路由
					path := fmt.Sprintf("/users/%d/:id", i)
					_ = m.AddRoute(method, path, emptyHandler)
					targets = append(targets, target{method: method, path: fmt.Sprintf("/users/%d/123?detail=true", i)})
				case 2:
					// 多参数路由
					path := fmt.Sprintf("/org/%d/teams/:p1/members/:p2", i)
					_ = m.AddRoute(method, path, emptyHandler)
					targets = append(targets, target{method: method, path: fmt.Sprintf("/org/%d/teams/123/members/456?expand=profile", i)})
				case 3:
					// 通配符路由
					path := fmt.Sprintf("/assets/%d/*filepath", i)
					_ = m.AddRoute(method, path, emptyHandler)
					targets = append(targets, target{method: method, path: fmt.Sprintf("/assets/%d/images/logo.png?size=large", i)})
				}
			}

			if len(targets) == 0 {
				b.Fatalf("no targets generated")
			}

			sampleCount := 256
			if len(targets) < sampleCount {
				sampleCount = len(targets)
			}
			sampleTargets := targets[:sampleCount]

			b.Run("match=hit_with_query", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					t := sampleTargets[i%len(sampleTargets)]
					res := m.Match(t.method, t.path)
					_ = res
				}
			})

			b.Run("match=miss_with_query", func(b *testing.B) {
				missTargets := []target{
					{method: "GET", path: "/nonexistent/abc/000?query=missing"},
					{method: "POST", path: "/users/999999/000?invalid=true"},
					{method: "PUT", path: "/org/999999/teams/zzz/members/yyy?error=notfound"},
					{method: "DELETE", path: "/assets/999999/unknown/file.txt?status=404"},
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					t := missTargets[i%len(missTargets)]
					res := m.Match(t.method, t.path)
					_ = res
				}
			})
		})
	}
}

func BenchmarkMatcherComplexRoutes(b *testing.B) {
	sizes := []int{100, 1000, 5000, 10000}

	for _, size := range sizes {
		sz := size
		b.Run(fmt.Sprintf("routes=%d", sz), func(b *testing.B) {
			b.ReportAllocs()

			m := newServerMatcher()
			type target struct {
				method string
				path   string
			}
			var targets []target

			emptyHandler := func(ctx *Context) {}

			// 添加复杂路由模式
			for i := 0; i < sz; i++ {
				// 混合参数和通配符的复杂路由
				complexPath := fmt.Sprintf("/api/:version/%d/*resource", i)
				_ = m.AddRoute("GET", complexPath, emptyHandler)
				targets = append(targets, target{
					method: "GET",
					path: fmt.Sprintf("/api/v%d/%d/data/users/123/profile?include=details&fields=name,email&format=json",
						i%3+1, i),
				})
			}

			if len(targets) == 0 {
				b.Fatalf("no complex targets generated")
			}

			sampleCount := 256
			if len(targets) < sampleCount {
				sampleCount = len(targets)
			}
			sampleTargets := targets[:sampleCount]

			b.Run("match=complex_routes", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					t := sampleTargets[i%len(sampleTargets)]
					res := m.Match(t.method, t.path)
					_ = res
				}
			})

			b.Run("match=complex_miss", func(b *testing.B) {
				missTargets := []target{
					{method: "GET", path: "/api/v9/999999/invalid/path?error=true"},
					{method: "GET", path: "/api/wrong/12345/missing/resource?status=404"},
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					t := missTargets[i%len(missTargets)]
					res := m.Match(t.method, t.path)
					_ = res
				}
			})
		})
	}
}
