package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件补充大规模路由匹配、参数提取、404/405/301(TSR)以及 fuzz 的系统性测试,
// 与 route_tree_test.go 的语义单元测试互补。所有断言对齐 gin 语义。
// This file adds systematic tests for large-scale routing, parameter extraction,
// 404/405/301 (TSR), and fuzzing, complementing the semantic unit tests in
// route_tree_test.go. All assertions align with gin semantics.

// -----------------------------------------------------------------------------
// 测试脚手架 / test scaffolding
// -----------------------------------------------------------------------------

// captureHandler 返回一个 handler:命中时把请求路径与全部命中参数写入 out。
// captureHandler returns a handler that, on a hit, writes the request path and
// all matched params into out.
func captureHandler(pattern string, out *matchResult) RawHandlerFunc {
	// 从模板里抽出参数名(便于命中后逐个读取)。
	// Extract param names from the template (to read each after a hit).
	names := paramNamesOf(pattern)
	return func(_ context.Context, req *Request, resp *Response) error {
		if out != nil {
			out.hit = true
			out.pattern = pattern
			out.params = map[string]string{}
			for _, n := range names {
				out.params[n] = req.Params.Get(n)
			}
		}
		resp.WriteHeader(http.StatusOK)
		return nil
	}
}

// matchResult 汇总一次命中的结果。
// matchResult summarizes one hit.
type matchResult struct {
	hit     bool
	pattern string
	params  map[string]string
}

// paramNamesOf 从 {name} / {name...} 模板中解析出参数名列表。
// paramNamesOf parses the list of param names from a {name} / {name...} template.
func paramNamesOf(pattern string) []string {
	var names []string
	for _, seg := range strings.Split(pattern, "/") {
		if len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			name := seg[1 : len(seg)-1]
			name = strings.TrimSuffix(name, "...")
			names = append(names, name)
		}
	}
	return names
}

// -----------------------------------------------------------------------------
// 大规模路由表(GitHub v3 API 形态,203 条),{param} 模板语法
// large-scale route table (GitHub v3 API shape, 203 routes), {param} syntax
// -----------------------------------------------------------------------------

type routeSpec struct{ method, pattern string }

// githubRoutes 是经典 GitHub v3 API 路由集(来自社区基准 julienschmidt/
// go-http-routing-benchmark),已从 :param/*name 翻译为本框架的 {param}/{name...}。
// githubRoutes is the canonical GitHub v3 API route set (from the community
// benchmark julienschmidt/go-http-routing-benchmark), translated from
// :param/*name to this framework's {param}/{name...}.
var githubRoutes = []routeSpec{
	{"GET", "/authorizations"},
	{"GET", "/authorizations/{id}"},
	{"POST", "/authorizations"},
	{"DELETE", "/authorizations/{id}"},
	{"GET", "/applications/{client_id}/tokens/{access_token}"},
	{"DELETE", "/applications/{client_id}/tokens"},
	{"DELETE", "/applications/{client_id}/tokens/{access_token}"},
	{"GET", "/events"},
	{"GET", "/repos/{owner}/{repo}/events"},
	{"GET", "/networks/{owner}/{repo}/events"},
	{"GET", "/orgs/{org}/events"},
	{"GET", "/users/{user}/received_events"},
	{"GET", "/users/{user}/received_events/public"},
	{"GET", "/users/{user}/events"},
	{"GET", "/users/{user}/events/public"},
	{"GET", "/users/{user}/events/orgs/{org}"},
	{"GET", "/feeds"},
	{"GET", "/notifications"},
	{"GET", "/repos/{owner}/{repo}/notifications"},
	{"PUT", "/notifications"},
	{"PUT", "/repos/{owner}/{repo}/notifications"},
	{"GET", "/notifications/threads/{id}"},
	{"GET", "/notifications/threads/{id}/subscription"},
	{"PUT", "/notifications/threads/{id}/subscription"},
	{"DELETE", "/notifications/threads/{id}/subscription"},
	{"GET", "/repos/{owner}/{repo}/stargazers"},
	{"GET", "/users/{user}/starred"},
	{"GET", "/user/starred"},
	{"GET", "/user/starred/{owner}/{repo}"},
	{"PUT", "/user/starred/{owner}/{repo}"},
	{"DELETE", "/user/starred/{owner}/{repo}"},
	{"GET", "/repos/{owner}/{repo}/subscribers"},
	{"GET", "/users/{user}/subscriptions"},
	{"GET", "/user/subscriptions"},
	{"GET", "/repos/{owner}/{repo}/subscription"},
	{"PUT", "/repos/{owner}/{repo}/subscription"},
	{"DELETE", "/repos/{owner}/{repo}/subscription"},
	{"GET", "/user/subscriptions/{owner}/{repo}"},
	{"PUT", "/user/subscriptions/{owner}/{repo}"},
	{"DELETE", "/user/subscriptions/{owner}/{repo}"},
	{"GET", "/gists"},
	{"GET", "/users/{user}/gists"},
	{"GET", "/gists/public"},
	{"GET", "/gists/starred"},
	{"GET", "/gists/{id}"},
	{"POST", "/gists"},
	{"PUT", "/gists/{id}/star"},
	{"DELETE", "/gists/{id}/star"},
	{"GET", "/gists/{id}/star"},
	{"POST", "/gists/{id}/forks"},
	{"DELETE", "/gists/{id}"},
	{"GET", "/repos/{owner}/{repo}/git/blobs/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/blobs"},
	{"GET", "/repos/{owner}/{repo}/git/commits/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/commits"},
	{"GET", "/repos/{owner}/{repo}/git/refs/{ref}"},
	{"GET", "/repos/{owner}/{repo}/git/refs"},
	{"POST", "/repos/{owner}/{repo}/git/refs"},
	{"GET", "/repos/{owner}/{repo}/git/tags/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/tags"},
	{"GET", "/repos/{owner}/{repo}/git/trees/{sha}"},
	{"POST", "/repos/{owner}/{repo}/git/trees"},
	{"GET", "/issues"},
	{"GET", "/user/issues"},
	{"GET", "/orgs/{org}/issues"},
	{"GET", "/repos/{owner}/{repo}/issues"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}"},
	{"POST", "/repos/{owner}/{repo}/issues"},
	{"GET", "/repos/{owner}/{repo}/assignees"},
	{"GET", "/repos/{owner}/{repo}/assignees/{assignee}"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}/comments"},
	{"GET", "/repos/{owner}/{repo}/issues/comments"},
	{"GET", "/repos/{owner}/{repo}/issues/comments/{id}"},
	{"POST", "/repos/{owner}/{repo}/issues/{number}/comments"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}/events"},
	{"GET", "/repos/{owner}/{repo}/issues/events"},
	{"GET", "/repos/{owner}/{repo}/issues/events/{id}"},
	{"GET", "/repos/{owner}/{repo}/labels"},
	{"GET", "/repos/{owner}/{repo}/labels/{name}"},
	{"POST", "/repos/{owner}/{repo}/labels"},
	{"DELETE", "/repos/{owner}/{repo}/labels/{name}"},
	{"GET", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"POST", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"DELETE", "/repos/{owner}/{repo}/issues/{number}/labels/{name}"},
	{"PUT", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"DELETE", "/repos/{owner}/{repo}/issues/{number}/labels"},
	{"GET", "/repos/{owner}/{repo}/milestones/{number}/labels"},
	{"GET", "/repos/{owner}/{repo}/milestones"},
	{"GET", "/repos/{owner}/{repo}/milestones/{number}"},
	{"POST", "/repos/{owner}/{repo}/milestones"},
	{"DELETE", "/repos/{owner}/{repo}/milestones/{number}"},
	{"GET", "/emojis"},
	{"GET", "/gitignore/templates"},
	{"GET", "/gitignore/templates/{name}"},
	{"POST", "/markdown"},
	{"POST", "/markdown/raw"},
	{"GET", "/meta"},
	{"GET", "/rate_limit"},
	{"GET", "/users/{user}/orgs"},
	{"GET", "/user/orgs"},
	{"GET", "/orgs/{org}"},
	{"GET", "/orgs/{org}/members"},
	{"GET", "/orgs/{org}/members/{user}"},
	{"DELETE", "/orgs/{org}/members/{user}"},
	{"GET", "/orgs/{org}/public_members"},
	{"GET", "/orgs/{org}/public_members/{user}"},
	{"PUT", "/orgs/{org}/public_members/{user}"},
	{"DELETE", "/orgs/{org}/public_members/{user}"},
	{"GET", "/orgs/{org}/teams"},
	{"GET", "/teams/{id}"},
	{"POST", "/orgs/{org}/teams"},
	{"DELETE", "/teams/{id}"},
	{"GET", "/teams/{id}/members"},
	{"GET", "/teams/{id}/members/{user}"},
	{"PUT", "/teams/{id}/members/{user}"},
	{"DELETE", "/teams/{id}/members/{user}"},
	{"GET", "/teams/{id}/repos"},
	{"GET", "/teams/{id}/repos/{owner}/{repo}"},
	{"PUT", "/teams/{id}/repos/{owner}/{repo}"},
	{"DELETE", "/teams/{id}/repos/{owner}/{repo}"},
	{"GET", "/user/teams"},
	{"GET", "/repos/{owner}/{repo}/pulls"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}"},
	{"POST", "/repos/{owner}/{repo}/pulls"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/commits"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/files"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/merge"},
	{"PUT", "/repos/{owner}/{repo}/pulls/{number}/merge"},
	{"GET", "/repos/{owner}/{repo}/pulls/{number}/comments"},
	{"PUT", "/repos/{owner}/{repo}/pulls/{number}/comments"},
	{"GET", "/user/repos"},
	{"GET", "/users/{user}/repos"},
	{"GET", "/orgs/{org}/repos"},
	{"GET", "/repositories"},
	{"POST", "/user/repos"},
	{"POST", "/orgs/{org}/repos"},
	{"GET", "/repos/{owner}/{repo}"},
	{"GET", "/repos/{owner}/{repo}/contributors"},
	{"GET", "/repos/{owner}/{repo}/languages"},
	{"GET", "/repos/{owner}/{repo}/teams"},
	{"GET", "/repos/{owner}/{repo}/tags"},
	{"GET", "/repos/{owner}/{repo}/branches"},
	{"GET", "/repos/{owner}/{repo}/branches/{branch}"},
	{"DELETE", "/repos/{owner}/{repo}"},
	{"GET", "/repos/{owner}/{repo}/collaborators"},
	{"GET", "/repos/{owner}/{repo}/collaborators/{user}"},
	{"PUT", "/repos/{owner}/{repo}/collaborators/{user}"},
	{"DELETE", "/repos/{owner}/{repo}/collaborators/{user}"},
	{"GET", "/repos/{owner}/{repo}/comments"},
	{"GET", "/repos/{owner}/{repo}/commits/{sha}/comments"},
	{"POST", "/repos/{owner}/{repo}/commits/{sha}/comments"},
	{"GET", "/repos/{owner}/{repo}/comments/{id}"},
	{"DELETE", "/repos/{owner}/{repo}/comments/{id}"},
	{"GET", "/repos/{owner}/{repo}/commits"},
	{"GET", "/repos/{owner}/{repo}/commits/{sha}"},
	{"GET", "/repos/{owner}/{repo}/readme"},
	{"GET", "/repos/{owner}/{repo}/keys"},
	{"GET", "/repos/{owner}/{repo}/keys/{id}"},
	{"POST", "/repos/{owner}/{repo}/keys"},
	{"DELETE", "/repos/{owner}/{repo}/keys/{id}"},
	{"GET", "/repos/{owner}/{repo}/downloads"},
	{"GET", "/repos/{owner}/{repo}/downloads/{id}"},
	{"DELETE", "/repos/{owner}/{repo}/downloads/{id}"},
	{"GET", "/repos/{owner}/{repo}/forks"},
	{"POST", "/repos/{owner}/{repo}/forks"},
	{"GET", "/repos/{owner}/{repo}/hooks"},
	{"GET", "/repos/{owner}/{repo}/hooks/{id}"},
	{"POST", "/repos/{owner}/{repo}/hooks"},
	{"POST", "/repos/{owner}/{repo}/hooks/{id}/tests"},
	{"DELETE", "/repos/{owner}/{repo}/hooks/{id}"},
	{"POST", "/repos/{owner}/{repo}/merges"},
	{"GET", "/repos/{owner}/{repo}/releases"},
	{"GET", "/repos/{owner}/{repo}/releases/{id}"},
	{"POST", "/repos/{owner}/{repo}/releases"},
	{"DELETE", "/repos/{owner}/{repo}/releases/{id}"},
	{"GET", "/repos/{owner}/{repo}/releases/{id}/assets"},
	{"GET", "/repos/{owner}/{repo}/stats/contributors"},
	{"GET", "/repos/{owner}/{repo}/stats/commit_activity"},
	{"GET", "/repos/{owner}/{repo}/stats/code_frequency"},
	{"GET", "/repos/{owner}/{repo}/stats/participation"},
	{"GET", "/repos/{owner}/{repo}/stats/punch_card"},
	{"GET", "/repos/{owner}/{repo}/statuses/{ref}"},
	{"POST", "/repos/{owner}/{repo}/statuses/{ref}"},
	{"GET", "/search/repositories"},
	{"GET", "/search/code"},
	{"GET", "/search/issues"},
	{"GET", "/search/users"},
	{"GET", "/legacy/issues/search/{owner}/{repository}/{state}/{keyword}"},
	{"GET", "/legacy/repos/search/{keyword}"},
	{"GET", "/legacy/user/search/{keyword}"},
	{"GET", "/legacy/user/email/{email}"},
	{"GET", "/users/{user}"},
	{"GET", "/user"},
	{"GET", "/users"},
	{"GET", "/user/emails"},
	{"POST", "/user/emails"},
	{"DELETE", "/user/emails"},
	{"GET", "/users/{user}/followers"},
	{"GET", "/user/followers"},
	{"GET", "/users/{user}/following"},
	{"GET", "/user/following"},
	{"GET", "/user/following/{user}"},
	{"GET", "/users/{user}/following/{target_user}"},
	{"PUT", "/user/following/{user}"},
	{"DELETE", "/user/following/{user}"},
	{"GET", "/users/{user}/keys"},
	{"GET", "/user/keys"},
	{"GET", "/user/keys/{id}"},
	{"POST", "/user/keys"},
	{"DELETE", "/user/keys/{id}"},
	{"GET", "/people/{user}/receivers"},
}

// buildGithubMux 把 githubRoutes 全部注册进一个 Mux;注册失败即 fatal。
// buildGithubMux registers every githubRoutes entry into a Mux; a registration
// error is fatal.
func buildGithubMux(t testing.TB) *Server {
	t.Helper()
	m := New()
	for _, r := range githubRoutes {
		if err := m.RawHandle(r.method, r.pattern, captureHandler(r.pattern, nil)); err != nil {
			t.Fatalf("register %s %s: %v", r.method, r.pattern, err)
		}
	}
	return m
}

// concretePath 把模板路径实例化成一条可请求的具体路径:{name} → "x{i}",
// {name...} → "x{i}/y{i}/z{i}"(多段)。返回具体路径与期望的参数键值。
// concretePath instantiates a template path into a concrete requestable path:
// {name} → "x{i}", {name...} → "x{i}/y{i}/z{i}" (multi-segment). It returns the
// concrete path and the expected param key/values.
func concretePath(pattern string, i int) (string, map[string]string) {
	want := map[string]string{}
	var b strings.Builder
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "" {
			continue
		}
		b.WriteByte('/')
		switch {
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "...}"):
			name := seg[1 : len(seg)-4]
			// catch-all 的值含前导 "/"(gin 语义):这里给出多段。
			// catch-all value includes the leading "/" (gin semantics); multi-seg.
			v := fmt.Sprintf("x%d/y%d/z%d", i, i, i)
			b.WriteString(v)
			want[name] = "/" + v
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			name := seg[1 : len(seg)-1]
			v := fmt.Sprintf("val%d", i)
			b.WriteString(v)
			want[name] = v
		default:
			b.WriteString(seg)
		}
	}
	return b.String(), want
}

// -----------------------------------------------------------------------------
// 1. 大规模路由匹配:全表命中 + 参数正确
// 1. large-scale routing: every route hits with correct params
// -----------------------------------------------------------------------------

func TestLargeScaleAllRoutesHit(t *testing.T) {
	// 独立注册每条路由并逐一请求,验证参数提取正确。用独立 Mux 避免同 method+path
	// 因参数名不同产生的冲突(表中确有仅参数名不同的重复形状)。
	// Register each route on its own Mux and request it, verifying param
	// extraction. Separate Muxes avoid conflicts from same method+path shapes
	// that differ only by param name.
	for i, r := range githubRoutes {
		m := New()
		var got matchResult
		if err := m.RawHandle(r.method, r.pattern, captureHandler(r.pattern, &got)); err != nil {
			t.Fatalf("register %s %s: %v", r.method, r.pattern, err)
		}
		target, want := concretePath(r.pattern, i)
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(r.method, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("[%d] %s %s -> %s: code=%d, want 200", i, r.method, r.pattern, target, rec.Code)
		}
		if !got.hit {
			t.Fatalf("[%d] %s %s: handler not invoked", i, r.method, r.pattern)
		}
		for k, v := range want {
			if got.params[k] != v {
				t.Fatalf("[%d] %s %s -> %s: param %q=%q, want %q", i, r.method, r.pattern, target, k, got.params[k], v)
			}
		}
	}
}

func TestLargeScaleSharedTreeHits(t *testing.T) {
	// 把全部 GET 路由塞进同一棵树(共享前缀密集),抽样请求验证共存命中无串扰。
	// Register all GET routes into one shared tree (dense shared prefixes) and
	// sample requests to verify coexisting hits without cross-talk.
	m := New()
	seen := map[string]bool{}
	for _, r := range githubRoutes {
		if r.method != http.MethodGet {
			continue
		}
		key := r.pattern
		if seen[key] {
			continue // 跳过形状重复(仅参数名不同)/ skip duplicate shapes
		}
		seen[key] = true
		if err := m.RawHandle(r.method, r.pattern, captureHandler(r.pattern, nil)); err != nil {
			t.Fatalf("register %s: %v", r.pattern, err)
		}
	}
	// 抽样若干深路径,确认全部命中 200。
	// Sample several deep paths and confirm 200 hits.
	samples := []string{
		"/repos/golang/go/git/refs/main", // {ref} 是单段参数 / {ref} is a single-segment param
		"/repos/a/b/issues/42/comments",
		"/teams/7/repos/o/r",
		"/legacy/issues/search/o/r/open/bug",
		"/users/torvalds/received_events/public",
		"/repos/a/b/stats/punch_card",
	}
	for _, s := range samples {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, s, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("shared-tree %s: code=%d, want 200", s, rec.Code)
		}
	}
}

// -----------------------------------------------------------------------------
// 2. 参数提取:多参数、空值、特殊字符、Unicode、catch-all
// 2. parameter extraction: multi-param, empty, special chars, Unicode, catch-all
// -----------------------------------------------------------------------------

func TestParamExtractionMatrix(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		target  string
		want    map[string]string
	}{
		{
			name:    "single",
			pattern: "/users/{id}",
			target:  "/users/12345",
			want:    map[string]string{"id": "12345"},
		},
		{
			name:    "five",
			pattern: "/orgs/{o}/teams/{t}/members/{m}/roles/{r}/perms/{p}",
			target:  "/orgs/O/teams/T/members/M/roles/R/perms/P",
			want:    map[string]string{"o": "O", "t": "T", "m": "M", "r": "R", "p": "P"},
		},
		{
			name:    "adjacent-params-diff-segments",
			pattern: "/{a}/{b}/{c}",
			target:  "/one/two/three",
			want:    map[string]string{"a": "one", "b": "two", "c": "three"},
		},
		{
			name:    "special-chars-dash-dot-tilde",
			pattern: "/f/{name}",
			target:  "/f/a-b.c~d_e",
			want:    map[string]string{"name": "a-b.c~d_e"},
		},
		{
			name:    "dot-in-value-not-dot-segment",
			pattern: "/f/{name}",
			target:  "/f/archive.tar.gz",
			want:    map[string]string{"name": "archive.tar.gz"},
		},
		{
			name:    "unicode-value",
			pattern: "/u/{name}",
			target:  "/u/张三",
			want:    map[string]string{"name": "张三"},
		},
		{
			name:    "percent-decoded-value",
			pattern: "/s/{q}",
			target:  "/s/hello%20world", // 解码为 "hello world" / decodes to "hello world"
			want:    map[string]string{"q": "hello world"},
		},
		{
			name:    "catchall-single-segment",
			pattern: "/files/{rest...}",
			target:  "/files/a",
			want:    map[string]string{"rest": "/a"},
		},
		{
			name:    "catchall-multi-segment",
			pattern: "/files/{rest...}",
			target:  "/files/a/b/c.txt",
			want:    map[string]string{"rest": "/a/b/c.txt"},
		},
		{
			name:    "param-then-catchall",
			pattern: "/repos/{owner}/{repo}/contents/{path...}",
			target:  "/repos/golang/go/contents/src/net/http/server.go",
			want: map[string]string{
				"owner": "golang",
				"repo":  "go",
				"path":  "/src/net/http/server.go",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			var got matchResult
			if err := m.RawHandle(http.MethodGet, tc.pattern, captureHandler(tc.pattern, &got)); err != nil {
				t.Fatalf("register: %v", err)
			}
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://t"+tc.target, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s -> code=%d, want 200", tc.target, rec.Code)
			}
			for k, v := range tc.want {
				if got.params[k] != v {
					t.Fatalf("param %q=%q, want %q (target %s)", k, got.params[k], v, tc.target)
				}
			}
		})
	}
}

func TestParamEmptyValueGinAligned(t *testing.T) {
	// gin 语义:空段作为空参数值命中(快速模式不拦空段)。
	// gin semantics: an empty segment matches as an empty param value (fast mode
	// does not reject empty segments).
	m := New()
	var got matchResult
	_ = m.RawHandle(http.MethodGet, "/a/{p}/b", captureHandler("/a/{p}/b", &got))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a//b", nil))
	if rec.Code != http.StatusOK || got.params["p"] != "" {
		t.Fatalf("/a//b -> code=%d p=%q, want 200 p=\"\"", rec.Code, got.params["p"])
	}
}

// -----------------------------------------------------------------------------
// 3. 404 Not Found 矩阵
// 3. 404 Not Found matrix
// -----------------------------------------------------------------------------

func TestNotFoundMatrix(t *testing.T) {
	m := New()
	routes := []string{
		"/users/{id}",
		"/users/{id}/repos",
		"/repos/{owner}/{repo}",
		"/files/{rest...}",
		"/static/css",
	}
	for _, p := range routes {
		if err := m.RawHandle(http.MethodGet, p, captureHandler(p, nil)); err != nil {
			t.Fatalf("register %s: %v", p, err)
		}
	}
	misses := []struct {
		name, path string
	}{
		{"unknown-root", "/nope"},
		{"partial-prefix", "/user"},          // /users 的前缀,但无此路由
		{"too-shallow", "/repos/only-owner"}, // 缺少 {repo} 段后续
		{"too-deep-static", "/static/css/extra"},
		{"deep-under-param-no-child", "/users/42/repos/extra"},
		{"sibling-miss", "/reposX/a/b"},
		{"empty-after-known", "/users"}, // /users 无路由(只有 /users/{id})
	}
	for _, mc := range misses {
		t.Run(mc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, mc.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s -> code=%d, want 404", mc.path, rec.Code)
			}
		})
	}
}

func TestNotFoundEmptyMux(t *testing.T) {
	// 完全没有任何路由时,任何请求都应 404(且不 panic)。
	// With no routes at all, any request must be 404 (and must not panic).
	m := New()
	for _, p := range []string{"/", "/anything", "/a/b/c", "/deeply/nested/path/here"} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("empty-mux %s -> code=%d, want 404", p, rec.Code)
		}
	}
}

// -----------------------------------------------------------------------------
// 4. 405 Method Not Allowed + Allow 头收集
// 4. 405 Method Not Allowed + Allow header collection
// -----------------------------------------------------------------------------

func TestMethodNotAllowedMatrix(t *testing.T) {
	m := New()
	// 同一路径注册多个 method。
	// Register multiple methods for the same path.
	path := "/repos/{owner}/{repo}"
	for _, meth := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if err := m.RawHandle(meth, path, captureHandler(path, nil)); err != nil {
			t.Fatalf("register %s %s: %v", meth, path, err)
		}
	}
	// 用未注册的 method 请求 → 405,Allow 头列出全部已注册 method。
	// Request with an unregistered method → 405, Allow header lists all methods.
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/repos/a/b", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT -> code=%d, want 405", rec.Code)
	}
	allow := rec.Header().Get("Allow")
	for _, want := range []string{"GET", "POST", "DELETE"} {
		if !strings.Contains(allow, want) {
			t.Fatalf("Allow=%q, missing %q", allow, want)
		}
	}
}

func TestMethodNotAllowedSingleMethod(t *testing.T) {
	m := New()
	_ = m.RawHandle(http.MethodPost, "/submit", captureHandler("/submit", nil))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/submit", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "POST" {
		t.Fatalf("Allow=%q, want POST", allow)
	}
}

func TestMethodNotAllowedNotConfusedWith404(t *testing.T) {
	// 路径不存在时是 404(而非 405);路径存在但 method 不符时才是 405。
	// A nonexistent path is 404 (not 405); 405 only when the path exists under
	// another method.
	m := New()
	_ = m.RawHandle(http.MethodGet, "/exists", captureHandler("/exists", nil))
	// 存在但 method 不符 → 405
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/exists", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /exists -> %d, want 405", rec.Code)
	}
	// 不存在 → 404
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /missing -> %d, want 404", rec.Code)
	}
}

// -----------------------------------------------------------------------------
// 5. TSR 尾斜杠重定向:301(GET)/308(非 GET),含 query 保留
// 5. TSR trailing-slash redirect: 301 (GET) / 308 (non-GET), query preserved
// -----------------------------------------------------------------------------

func TestTSRRedirectMatrix(t *testing.T) {
	cases := []struct {
		name      string
		register  string
		method    string
		reqTarget string
		wantCode  int
		wantLoc   string
	}{
		{"strip-slash-static-GET", "/about", http.MethodGet, "/about/", http.StatusMovedPermanently, "/about"},
		{"add-slash-static-GET", "/about/", http.MethodGet, "/about", http.StatusMovedPermanently, "/about/"},
		{"strip-slash-param-GET", "/users/{id}", http.MethodGet, "/users/42/", http.StatusMovedPermanently, "/users/42"},
		{"strip-slash-nested-GET", "/a/{p}/b", http.MethodGet, "/a/v/b/", http.StatusMovedPermanently, "/a/v/b"},
		{"add-slash-POST-uses-308", "/submit/", http.MethodPost, "/submit", http.StatusPermanentRedirect, "/submit/"},
		{"query-preserved", "/search", http.MethodGet, "/search/?q=go&n=5", http.StatusMovedPermanently, "/search?q=go&n=5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			if err := m.RawHandle(tc.method, tc.register, captureHandler(tc.register, nil)); err != nil {
				t.Fatalf("register: %v", err)
			}
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(tc.method, "http://t"+tc.reqTarget, nil))
			if rec.Code != tc.wantCode {
				t.Fatalf("%s %s -> code=%d, want %d", tc.method, tc.reqTarget, rec.Code, tc.wantCode)
			}
			if loc := rec.Header().Get("Location"); loc != tc.wantLoc {
				t.Fatalf("%s %s -> Location=%q, want %q", tc.method, tc.reqTarget, loc, tc.wantLoc)
			}
		})
	}
}

func TestTSRNoRedirectForRoot(t *testing.T) {
	// 根 "/" 不应被 TSR 处理成重定向循环。
	// The root "/" must not be turned into a redirect loop by TSR.
	m := New()
	_ = m.RawHandle(http.MethodGet, "/x", captureHandler("/x", nil))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code == http.StatusMovedPermanently || rec.Code == http.StatusPermanentRedirect {
		t.Fatalf("/ -> code=%d, must not be a TSR redirect", rec.Code)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/ -> code=%d, want 404", rec.Code)
	}
}

func TestTSRDoesNotOverrideExactMatch(t *testing.T) {
	// 同时注册 /a 与 /a/,精确匹配优先,不触发 TSR 重定向。
	// With both /a and /a/ registered, an exact match wins and no TSR fires.
	m := New()
	var gotBare, gotSlash matchResult
	_ = m.RawHandle(http.MethodGet, "/a", captureHandler("/a", &gotBare))
	_ = m.RawHandle(http.MethodGet, "/a/", captureHandler("/a/", &gotSlash))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a", nil))
	if rec.Code != http.StatusOK || !gotBare.hit {
		t.Fatalf("/a -> code=%d hit=%v, want 200 exact", rec.Code, gotBare.hit)
	}
	rec = httptest.NewRecorder()
	gotSlash.hit = false
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a/", nil))
	if rec.Code != http.StatusOK || !gotSlash.hit {
		t.Fatalf("/a/ -> code=%d hit=%v, want 200 exact", rec.Code, gotSlash.hit)
	}
}

// -----------------------------------------------------------------------------
// 6. handler 状态码透传 + 池化卫生(203 / 403 / 任意码不被路由层篡改或串扰)
// 6. handler status passthrough + pool hygiene (203 / 403 / any code is neither
//    altered by the routing layer nor leaked across pooled requests)
// -----------------------------------------------------------------------------

func TestHandlerStatusPassthrough(t *testing.T) {
	// 路由层自身只产生 404/405/301/400;命中后 handler 写的任意状态码(如 203
	// Non-Authoritative、403 Forbidden、201 Created)必须原样透传。
	// The routing layer itself emits only 404/405/301/400; once matched, any
	// status the handler writes (e.g. 203 Non-Authoritative, 403 Forbidden, 201
	// Created) must pass through unchanged.
	codes := []int{
		http.StatusOK,                   // 200
		http.StatusCreated,              // 201
		http.StatusNonAuthoritativeInfo, // 203
		http.StatusNoContent,            // 204
		http.StatusForbidden,            // 403
		http.StatusTeapot,               // 418
		http.StatusInternalServerError,  // 500
	}
	for _, want := range codes {
		want := want
		m := New()
		_ = m.RawHandle(http.MethodGet, "/r/{id}", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(want)
			return nil
		})
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r/1", nil))
		if rec.Code != want {
			t.Fatalf("handler wrote %d but response code=%d (routing layer must not alter it)", want, rec.Code)
		}
	}
}

func TestPoolHygieneNoParamLeak(t *testing.T) {
	// 池化 Request/Params 的关键风险:上一请求的参数残留到下一请求。连续打两条
	// 不同参数数量的路由,验证每次读到的参数都只属于当前请求。
	// Key risk with pooled Request/Params: the previous request's params leaking
	// into the next. Hit two routes with different param counts in sequence and
	// verify each read sees only the current request's params.
	m := New()
	_ = m.RawHandle(http.MethodGet, "/five/{a}/{b}/{c}/{d}/{e}", func(_ context.Context, req *Request, resp *Response) error {
		// 全部 5 个参数应存在且正确。
		// All five params must be present and correct.
		if req.Params.Len() != 5 {
			resp.WriteHeader(http.StatusInternalServerError)
			return nil
		}
		resp.WriteHeader(http.StatusOK)
		return nil
	})
	_ = m.RawHandle(http.MethodGet, "/one/{x}", func(_ context.Context, req *Request, resp *Response) error {
		// 只应有 1 个参数;若池未清理会看到残留的旧参数。
		// Only one param should exist; an uncleaned pool would show stale params.
		if req.Params.Len() != 1 || req.Params.Get("x") == "" {
			resp.WriteHeader(http.StatusInternalServerError)
			return nil
		}
		// 旧参数键必须已消失。
		// Old param keys must be gone.
		if req.Params.Get("a") != "" || req.Params.Get("e") != "" {
			resp.WriteHeader(http.StatusConflict)
			return nil
		}
		resp.WriteHeader(http.StatusOK)
		return nil
	})

	// 交替打 200 次,迫使池复用同一批对象。
	// Alternate 200 times to force reuse of the same pooled objects.
	for i := 0; i < 200; i++ {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/five/1/2/3/4/5", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("five-param iter %d -> code=%d", i, rec.Code)
		}
		rec = httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/one/solo", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("one-param iter %d -> code=%d (param leak from pool)", i, rec.Code)
		}
	}
}

func TestPoolHygieneConcurrent(t *testing.T) {
	// 并发命中同一 Mux,-race 下验证池化对象无数据竞争、参数无串扰。
	// Concurrent hits on the same Mux; under -race this verifies the pooled
	// objects have no data race and params do not cross between goroutines.
	m := New()
	_ = m.RawHandle(http.MethodGet, "/g/{id}", func(_ context.Context, req *Request, resp *Response) error {
		id := req.Params.Get("id")
		// 回写 id 以便调用方核对(证明该 goroutine 拿到的是自己的参数)。
		// Echo id back so the caller can verify this goroutine got its own param.
		resp.Header().Set("X-Echo", id)
		resp.WriteHeader(http.StatusOK)
		return nil
	})

	const workers, iters = 16, 300
	done := make(chan bool, workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			id := "worker" + itoaMatrix(w)
			for i := 0; i < iters; i++ {
				rec := httptest.NewRecorder()
				m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/g/"+id, nil))
				if rec.Code != http.StatusOK || rec.Header().Get("X-Echo") != id {
					done <- false
					return
				}
			}
			done <- true
		}(w)
	}
	for w := 0; w < workers; w++ {
		if !<-done {
			t.Fatal("concurrent param mismatch: pooled state leaked across goroutines")
		}
	}
}

// itoaMatrix 是本文件用的小整数转字符串(避免与 fuzz 文件的 itoa 冲突)。
// itoaMatrix converts a small int to string for this file (avoids clashing with
// the fuzz file's itoa).
func itoaMatrix(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
