package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// ===========================================================================
// Group 前缀拼接的斜杠规范化(缺陷 M7 回归)。
//
// 原缺陷是裸字符串拼接 g.prefix + path:Group("/api/") + "/v1/x" 注册成 /api//v1/x
// (于是 /api/v1/x 反而 404),Group("/api") + "users" 静默注册成 /apiusers。
//
// 本文件的断言全部走真实 ServeHTTP 而非读内部字段:注册路径是否规范只有"客户端能不能
// 用意图中的 URL 访问到"这一个可观测口径,读 prefix/树内部结构会让实现细节反过来定义
// 正确性。OpenAPI 部分同理断言 spec 的 paths 键——它必须与实际注册路径一致。
// ===========================================================================

// mountedPaths 在 prefix 分组下注册 path,返回一个"给定 URL 是否命中"的探测函数。
// 命中即回 200;未命中留给框架的 404/405/TSR 处理,故 301 与 404 都表示"没命中"。
func mountedPaths(t *testing.T, prefix, path string) (*Server, error) {
	t.Helper()
	s := New()
	g := s.Group(prefix)
	err := g.RawHandle(http.MethodGet, path, func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	})
	return s, err
}

func statusOf(t *testing.T, s *Server, url string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec.Code
}

func TestGroupPrefix_JoinNormalizesSlash(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		path   string
		// hit 是意图中的 URL,必须 200;miss 是规范化后【不应】再命中的旧拼接结果。
		hit  string
		miss []string
	}{
		{
			name:   "trailing slash on prefix collapses",
			prefix: "/api/",
			path:   "/v1/x",
			hit:    "/api/v1/x",
			miss:   []string{"/api//v1/x"},
		},
		{
			name:   "multiple trailing slashes collapse",
			prefix: "/api///",
			path:   "/v1/x",
			hit:    "/api/v1/x",
			miss:   []string{"/api//v1/x", "/api///v1/x"},
		},
		{
			name:   "missing leading slash on path is supplied",
			prefix: "/api",
			path:   "users",
			hit:    "/api/users",
			miss:   []string{"/apiusers"},
		},
		{
			name:   "both sides sloppy",
			prefix: "/api/",
			path:   "users",
			hit:    "/api/users",
			miss:   []string{"/apiusers", "/api//users"},
		},
		{
			// 既有正常用法,必须逐字保持不变。
			name:   "already canonical is unchanged",
			prefix: "/api",
			path:   "/v1/x",
			hit:    "/api/v1/x",
			miss:   []string{"/api//v1/x", "/apiv1/x"},
		},
		{
			name:   "empty root group equals bare server registration",
			prefix: "",
			path:   "/x",
			hit:    "/x",
			miss:   []string{"//x"},
		},
		{
			name:   "slash root group equals bare server registration",
			prefix: "/",
			path:   "/x",
			hit:    "/x",
			miss:   []string{"//x"},
		},
		{
			name:   "param segments survive joining",
			prefix: "/api/",
			path:   "users/{id}",
			hit:    "/api/users/7",
			miss:   []string{"/apiusers/7", "/api//users/7"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := mountedPaths(t, c.prefix, c.path)
			if err != nil {
				t.Fatalf("Group(%q).RawHandle(%q) failed: %v", c.prefix, c.path, err)
			}
			if got := statusOf(t, s, c.hit); got != http.StatusOK {
				t.Errorf("Group(%q)+%q: GET %q = %d, want 200 (intended URL must be reachable)",
					c.prefix, c.path, c.hit, got)
			}
			for _, u := range c.miss {
				if got := statusOf(t, s, u); got == http.StatusOK {
					t.Errorf("Group(%q)+%q: GET %q = 200, want a miss (unnormalized path must not be mounted)",
						c.prefix, c.path, u)
				}
			}
		})
	}
}

// TestGroupPrefix_NestedGroupsJoin 锁定嵌套分组:每层连接处都恰好一个 '/',
// 无论各层前缀自身写得多随意。
func TestGroupPrefix_NestedGroupsJoin(t *testing.T) {
	cases := []struct {
		name  string
		outer string
		inner string
		hit   string
		miss  []string
	}{
		{"canonical nesting", "/a", "/b", "/a/b/x", []string{"/a//b/x", "/ab/x"}},
		{"outer trailing slash", "/a/", "/b", "/a/b/x", []string{"/a//b/x"}},
		{"inner missing leading slash", "/a", "b", "/a/b/x", []string{"/ab/x"}},
		{"both sloppy", "/a/", "b", "/a/b/x", []string{"/ab/x", "/a//b/x"}},
		{"root outer group", "/", "/b", "/b/x", []string{"//b/x"}},
		{"empty outer group", "", "/b", "/b/x", []string{"//b/x"}},
		{"three-level canonical", "/a", "/b/c", "/a/b/c/x", []string{"/a//b/c/x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := New()
			g := s.Group(c.outer).Group(c.inner)
			if err := g.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatalf("register failed: %v", err)
			}
			if got := statusOf(t, s, c.hit); got != http.StatusOK {
				t.Errorf("Group(%q).Group(%q): GET %q = %d, want 200", c.outer, c.inner, c.hit, got)
			}
			for _, u := range c.miss {
				if got := statusOf(t, s, u); got == http.StatusOK {
					t.Errorf("Group(%q).Group(%q): GET %q = 200, want a miss", c.outer, c.inner, u)
				}
			}
		})
	}
}

// TestGroupPrefix_GroupRootSemantics 锁定"分组自身的根"的语义选择:
// Group("/api") 下注册 "" 或 "/" 都得到 /api(不带尾斜杠),/api/ 则经 TSR 301 到 /api。
// 语义理由见 joinRoutePath 的文档注释。
func TestGroupPrefix_GroupRootSemantics(t *testing.T) {
	for _, path := range []string{"/", ""} {
		t.Run("path="+path, func(t *testing.T) {
			s, err := mountedPaths(t, "/api", path)
			if err != nil {
				t.Fatalf("Group(\"/api\").RawHandle(%q) failed: %v", path, err)
			}
			if got := statusOf(t, s, "/api"); got != http.StatusOK {
				t.Errorf("GET /api = %d, want 200 (group root mounts the prefix itself)", got)
			}
			// 尾斜杠形式不是独立路由,而是经 TSR 301 收敛到规范 URL。
			if got := statusOf(t, s, "/api/"); got != http.StatusMovedPermanently {
				t.Errorf("GET /api/ = %d, want 301 to the canonical /api", got)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/", nil))
			if loc := rec.Header().Get("Location"); loc != "/api" {
				t.Errorf("GET /api/ Location = %q, want /api", loc)
			}
		})
	}
}

// TestGroupPrefix_RootGroupStillRejectsEmptyPath 锁定根分组不放宽注册期校验:
// 根分组等价于直接在 Server 上注册,故空路径照样报 ErrEmptyPath,而不是被静默接受。
func TestGroupPrefix_RootGroupStillRejectsEmptyPath(t *testing.T) {
	for _, prefix := range []string{"", "/"} {
		s := New()
		g := s.Group(prefix)
		err := g.RawHandle(http.MethodGet, "", func(_ context.Context, _ *Request, resp *Response) error {
			return nil
		})
		if !errors.Is(err, ErrEmptyPath) {
			t.Errorf("Group(%q).RawHandle(\"\") err = %v, want ErrEmptyPath", prefix, err)
		}
	}
}

// TestGroupPrefix_OpenAPIPathsNormalized 锁定 spec 的 paths 键与实际注册路径同步规范化:
// 未同步时 spec 会出现 /api//v1/x 这种非法键,读 spec 的契约测试反被误导。
func TestGroupPrefix_OpenAPIPathsNormalized(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		path   string
		want   string
	}{
		{"trailing slash on prefix", "/api/", "/v1/x", "/api/v1/x"},
		{"missing leading slash on path", "/api", "users", "/api/users"},
		{"both sloppy", "/api/", "users", "/api/users"},
		{"already canonical", "/api", "/v1/x", "/api/v1/x"},
		{"group root slash", "/api", "/", "/api"},
		{"group root empty", "/api", "", "/api"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, doc := specOf(t, func(s *Server) {
				g := s.Group(c.prefix)
				if err := GetNone(g, c.path, JSON[oaUser](), func(context.Context) (oaUser, error) {
					return oaUser{}, nil
				}); err != nil {
					t.Fatalf("GetNone(Group(%q), %q) failed: %v", c.prefix, c.path, err)
				}
			})
			paths := dig(t, doc, "paths").(map[string]any)
			keys := make([]string, 0, len(paths))
			for k := range paths {
				if k != "/openapi.json" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			if len(keys) != 1 || keys[0] != c.want {
				t.Fatalf("spec paths = %v, want exactly [%q]", keys, c.want)
			}
			// spec 声明的模板必须真的能被路由命中,否则 spec 与路由两张皮。
			if got := statusOf(t, s, c.want); got != http.StatusOK {
				t.Errorf("spec declares %q but GET it = %d, want 200", c.want, got)
			}
		})
	}
}

// TestGroupPrefix_OpenAPIMatchesRegisteredRoute 用一份含多个端点的路由表交叉校验:
// spec 的 paths 键集合必须【逐字等于】规范化后的注册路径,且每个键都能被真实路由命中。
// 这条双向断言是 spec 与路由"同一套拼接"的护栏——只要 noteRoute 与 register 有一侧漏改,
// 键集合或可达性必有一项对不上。
func TestGroupPrefix_OpenAPIMatchesRegisteredRoute(t *testing.T) {
	s, doc := specOf(t, func(s *Server) {
		sloppy := s.Group("/api/")
		if err := GetNone(sloppy, "v1/a", JSON[oaUser](), func(context.Context) (oaUser, error) {
			return oaUser{}, nil
		}); err != nil {
			t.Fatal(err)
		}
		nested := s.Group("/svc/").Group("inner/")
		if err := GetNone(nested, "b", JSON[oaUser](), func(context.Context) (oaUser, error) {
			return oaUser{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	paths := dig(t, doc, "paths").(map[string]any)
	keys := make([]string, 0, len(paths))
	for tmpl := range paths {
		if tmpl == "/openapi.json" {
			continue
		}
		keys = append(keys, tmpl)
		// 规范化后的路径不得含空段(spec 里的 //x 是非法 OpenAPI 路径)。
		for i := 1; i < len(tmpl); i++ {
			if tmpl[i] == '/' && tmpl[i-1] == '/' {
				t.Errorf("spec path %q contains an empty segment", tmpl)
			}
		}
		if got := statusOf(t, s, tmpl); got != http.StatusOK {
			t.Errorf("spec path %q is not routable: GET = %d, want 200", tmpl, got)
		}
	}
	sort.Strings(keys)
	want := []string{"/api/v1/a", "/svc/inner/b"}
	if len(keys) != len(want) {
		t.Fatalf("spec paths = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("spec paths = %v, want %v", keys, want)
			break
		}
	}
}

// TestJoinRoutePath 直接单测拼接函数,把边界语义固化成表(上面的行为测试经 HTTP 验证
// 同一批语义,这里保证纯函数本身的契约不漂移)。
func TestJoinRoutePath(t *testing.T) {
	cases := []struct {
		prefix string
		path   string
		want   string
	}{
		{"/api", "/v1/x", "/api/v1/x"},
		{"/api/", "/v1/x", "/api/v1/x"},
		{"/api///", "/v1/x", "/api/v1/x"},
		{"/api", "users", "/api/users"},
		{"/api/", "users", "/api/users"},
		{"/api", "/", "/api"},
		{"/api", "", "/api"},
		{"/api/", "/", "/api"},
		{"", "/x", "/x"},
		{"/", "/x", "/x"},
		{"///", "/x", "/x"},
		// 根分组不改写右侧:空路径要留给 translateTemplate 报 ErrEmptyPath。
		{"", "", ""},
		{"/", "", ""},
		// 右侧内部的斜杠不被折叠:只规范化连接处,路径内部由用户负责。
		{"/api", "/v1//x", "/api/v1//x"},
	}
	for _, c := range cases {
		if got := joinRoutePath(c.prefix, c.path); got != c.want {
			t.Errorf("joinRoutePath(%q, %q) = %q, want %q", c.prefix, c.path, got, c.want)
		}
	}
}
