package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// radix 树语义等价性测试:行为契约移植自旧实现的 TestChainCollapse* 系列
// (实现全新、断言不变)。覆盖静态/参数/catch-all、in-segment 片段、转义回退、
// 转义参数值解码、405+Allow、死链 404、尾斜杠。
// radix tree semantic-equivalence tests: behavior contracts ported from the old
// TestChainCollapse* suite (fresh implementation, unchanged assertions).

// probe 注册若干路由后对 (method, target) 发一次请求,返回状态码、body 与匹配到的
// 参数快照。
// probe registers routes, issues one request for (method, target), and returns
// the status code, body, and a snapshot of matched params.
func probe(t *testing.T, reg func(*Mux), method, target string) (int, string, map[string]string) {
	t.Helper()
	m := New()
	reg(m)

	got := map[string]string{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	// 包装:捕获命中时的参数(通过在注册的 handler 内回填,见 echoParams)。
	// Params are captured by the registered handler via echoParams.
	capturedParams = got
	m.ServeHTTP(rec, req)
	capturedParams = nil
	return rec.Code, rec.Body.String(), got
}

// capturedParams 是测试期用于回传参数的临时通道(串行测试,无并发)。
// capturedParams is a test-only channel to hand back params (serial tests).
var capturedParams map[string]string

// echoParams 注册一个把匹配参数写入 capturedParams 并返回 200 "ok" 的路由。
// echoParams registers a route that copies matched params into capturedParams
// and returns 200 "ok".
func echoParams(m *Mux, method, pattern string, names ...string) {
	_ = m.RawHandle(method, pattern, func(_ context.Context, req *Request, resp *Response) error {
		if capturedParams != nil {
			for _, n := range names {
				capturedParams[n] = req.Params.Get(n)
			}
		}
		resp.WriteHeader(http.StatusOK)
		_, err := resp.Write([]byte("ok"))
		return err
	})
}

func TestTreeStaticParamAlternation(t *testing.T) {
	code, _, p := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/a/{p1}/b/{p2}", "p1", "p2")
	}, http.MethodGet, "/a/v1/b/v2")
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if p["p1"] != "v1" || p["p2"] != "v2" {
		t.Fatalf("params=%v", p)
	}
}

func TestTreeDeepStaticChain(t *testing.T) {
	code, _, _ := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/api/v1/users/lists")
	}, http.MethodGet, "/api/v1/users/lists")
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
}

func TestTreeForkMidChain(t *testing.T) {
	reg := func(m *Mux) {
		echoParams(m, http.MethodGet, "/a/{p1}/b/x")
		echoParams(m, http.MethodGet, "/a/{p1}/b/y")
	}
	if code, _, _ := probe(t, reg, http.MethodGet, "/a/v1/b/x"); code != 200 {
		t.Fatalf("branch x code=%d", code)
	}
	if code, _, _ := probe(t, reg, http.MethodGet, "/a/v1/b/y"); code != 200 {
		t.Fatalf("branch y code=%d", code)
	}
}

func TestTreeStaticBeatsParam(t *testing.T) {
	reg := func(m *Mux) {
		echoParams(m, http.MethodGet, "/a/{p1}/b", "p1")
		echoParams(m, http.MethodGet, "/a/lit/b")
	}
	// 静态字面量 "lit" 必须优先于参数分支。
	// The static literal "lit" must win over the param branch.
	code, _, p := probe(t, reg, http.MethodGet, "/a/lit/b")
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if p["p1"] != "" {
		t.Fatalf("param branch wrongly taken: %v", p)
	}
}

func TestTreeInSegmentFragments(t *testing.T) {
	reg := func(m *Mux) {
		echoParams(m, http.MethodGet, "/u/ali")
		echoParams(m, http.MethodGet, "/u/alice")
	}
	if code, _, _ := probe(t, reg, http.MethodGet, "/u/ali"); code != 200 {
		t.Fatalf("ali code=%d", code)
	}
	if code, _, _ := probe(t, reg, http.MethodGet, "/u/alice"); code != 200 {
		t.Fatalf("alice code=%d", code)
	}
	if code, _, _ := probe(t, reg, http.MethodGet, "/u/alicia"); code != 404 {
		t.Fatalf("alicia code=%d, want 404", code)
	}
}

func TestTreeEscapeFallback(t *testing.T) {
	// 策略 A:匹配基于已解码的 URL.Path。%62 已被标准库解码为 "b",故请求
	// /x/v/%62(Path=/x/v/b)天然命中静态字面量 "b",v 落入 {p1}。
	// Strategy A: matching runs on the decoded URL.Path. %62 is decoded to "b"
	// by the stdlib, so a request to /x/v/%62 (Path=/x/v/b) naturally hits the
	// static literal "b" with v captured by {p1}.
	code, _, p := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/x/{p1}/b", "p1")
	}, http.MethodGet, "http://test/x/v/%62")
	if code != 200 {
		t.Fatalf("escaped static code=%d", code)
	}
	if p["p1"] != "v" {
		t.Fatalf("p1=%q, want %q", p["p1"], "v")
	}
}

func TestTreeEscapedSlashSplitsSegment(t *testing.T) {
	// 策略 A 的已知取舍(与 gin 一致):参数值里的 %2F 被标准库解码为 "/",
	// 从而被当作段分隔符。/a/a%2Fb/b 的 Path 为 /a/a/b/b(4 段),不匹配
	// /a/{p1}/b(3 段)→ 404。这是有意的、与 gin 相同的行为。
	// Strategy A's known tradeoff (same as gin): a %2F inside a parameter value
	// is decoded to "/" by the stdlib and thus treated as a segment separator.
	// /a/a%2Fb/b has Path /a/a/b/b (4 segments) and does not match /a/{p1}/b
	// (3 segments) → 404. This is intentional and matches gin.
	code, _, _ := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/a/{p1}/b", "p1")
	}, http.MethodGet, "http://test/a/a%2Fb/b")
	if code != 404 {
		t.Fatalf("code=%d, want 404 (%%2F splits the segment under strategy A)", code)
	}
}

func TestTreeTrailingCatchAll(t *testing.T) {
	// gin 语义:catchAll 值 = 整个剩余 path,含前导 "/"。
	// gin semantics: the catch-all value = the whole remaining path, including
	// the leading "/".
	code, _, p := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/files/{rest...}", "rest")
	}, http.MethodGet, "/files/a/b/c")
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if p["rest"] != "/a/b/c" {
		t.Fatalf("rest=%q, want %q", p["rest"], "/a/b/c")
	}
}

func TestTreeCatchAllZeroSegments(t *testing.T) {
	// gin 语义:/files(catchAll 零段)不直接命中,而是 301 重定向到 /files/。
	// gin semantics: /files (zero-segment catch-all) does not hit directly; it
	// issues a 301 redirect to /files/.
	code, _, _ := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/files/{rest...}", "rest")
	}, http.MethodGet, "/files")
	if code != http.StatusMovedPermanently {
		t.Fatalf("code=%d, want 301 (TSR to /files/)", code)
	}
	// /files/ 则命中,rest 为 "/"。
	// /files/ hits, with rest = "/".
	code, _, p := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/files/{rest...}", "rest")
	}, http.MethodGet, "/files/")
	if code != 200 {
		t.Fatalf("/files/ code=%d, want 200", code)
	}
	if p["rest"] != "/" {
		t.Fatalf("rest=%q, want %q", p["rest"], "/")
	}
}

func TestTreeMethodNotAllowed(t *testing.T) {
	m := New()
	echoParams(m, http.MethodPost, "/a/{p1}/b/{p2}")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/a/v1/b/v2", nil)
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "POST" {
		t.Fatalf("Allow=%q, want POST", allow)
	}
}

func TestTreeDeadEnds(t *testing.T) {
	reg := func(m *Mux) {
		echoParams(m, http.MethodGet, "/a/{p1}/b/{p2}", "p1", "p2")
	}
	for _, path := range []string{"/a/v1", "/a/v1/c/v2", "/a/v1/b/v2/extra"} {
		if code, _, _ := probe(t, reg, http.MethodGet, path); code != 404 {
			t.Fatalf("path %q code=%d, want 404", path, code)
		}
	}
}

func TestTreeTrailingSlashRedirect(t *testing.T) {
	// gin 语义:注册 /a/{p1}/b,请求 /a/v1/b/ 触发 TSR → 301 重定向到 /a/v1/b。
	// gin semantics: with /a/{p1}/b registered, /a/v1/b/ triggers TSR → 301 to
	// /a/v1/b.
	code, _, _ := probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/a/{p1}/b", "p1")
	}, http.MethodGet, "/a/v1/b/")
	if code != http.StatusMovedPermanently {
		t.Fatalf("code=%d, want 301 (TSR)", code)
	}
	// 反向:注册带尾斜杠,请求不带,也应 301 补上。
	// Reverse: register with a trailing slash, request without → 301 to add it.
	code, _, _ = probe(t, func(m *Mux) {
		echoParams(m, http.MethodGet, "/dir/")
	}, http.MethodGet, "/dir")
	if code != http.StatusMovedPermanently {
		t.Fatalf("/dir code=%d, want 301 (TSR to /dir/)", code)
	}
}

func TestTreeInvalidSegments(t *testing.T) {
	// 默认(快速模式):dot 段 → 400(防路径遍历);空段放行交给匹配(与 gin 一致)。
	// Default (fast mode): dot segment → 400 (traversal guard); empty segment
	// passes to matching (aligned with gin).
	reg := func(m *Mux) { echoParams(m, http.MethodGet, "/a/{p1}/b", "p1") }
	if code, _, _ := probe(t, reg, http.MethodGet, "/a/../b"); code != http.StatusBadRequest {
		t.Fatalf("/a/../b code=%d, want 400 (dot segment rejected in fast mode)", code)
	}
	// 空段 /a//b:快速模式不拦,交给匹配。与 gin 一致:空段作为空参数值命中
	// /a/{p1}/b(p1=""),返回 200。
	// Empty segment /a//b: not rejected in fast mode, passes to matching. As in
	// gin, the empty segment matches /a/{p1}/b as an empty param value (p1=""),
	// returning 200.
	if code, _, p := probe(t, reg, http.MethodGet, "/a//b"); code != 200 || p["p1"] != "" {
		t.Fatalf("/a//b code=%d p1=%q, want 200 p1=\"\" (gin-aligned empty param)", code, p["p1"])
	}
}

func TestTreeStrictPathValidation(t *testing.T) {
	// 严格模式(WithStrictPath(true)):dot 段与空段均 → 400。
	// Strict mode (WithStrictPath(true)): both dot and empty segments → 400.
	probeStrict := func(target string) int {
		m := New(WithStrictPath(true))
		echoParams(m, http.MethodGet, "/a/{p1}/b", "p1")
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		m.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, path := range []string{"/a/../b", "/a//b", "/a/./b"} {
		if code := probeStrict(path); code != http.StatusBadRequest {
			t.Fatalf("strict %q code=%d, want 400", path, code)
		}
	}
	// 正常路径在严格模式下仍命中。
	// A normal path still hits under strict mode.
	if code := probeStrict("/a/v1/b"); code != 200 {
		t.Fatalf("strict /a/v1/b code=%d, want 200", code)
	}
}

// TestTreeStaticThenParamFallback 回归:静态前缀命中但更深处失败时,必须回落到
// 同层的 param 分支。/foo 与 /{p}/x 共存,请求 /foo/x 应命中 /{p}/x(p=foo)。
// 这个用例专门验证迭代 walk 的就地下降没有吞掉 param 回退。
// Regression: when a static prefix matches but fails deeper, matching must fall
// back to a param branch at the same level. With /foo and /{p}/x, request /foo/x
// must hit /{p}/x (p=foo). This guards the iterative walk's in-place descent
// against swallowing the param fallback.
func TestTreeStaticThenParamFallback(t *testing.T) {
	reg := func(m *Mux) {
		echoParams(m, http.MethodGet, "/foo")
		echoParams(m, http.MethodGet, "/{p}/x", "p")
	}
	// /foo/x:静态 /foo 命中前缀但 /foo 下无 /x,须回落到 /{p}/x。
	// /foo/x: static /foo matches the prefix but has no /x child; fall back to /{p}/x.
	code, _, p := probe(t, reg, http.MethodGet, "/foo/x")
	if code != 200 {
		t.Fatalf("/foo/x code=%d, want 200 (param fallback)", code)
	}
	if p["p"] != "foo" {
		t.Fatalf("p=%q, want %q", p["p"], "foo")
	}
	// /foo 本身仍命中静态。
	// /foo itself still hits the static route.
	if code, _, _ := probe(t, reg, http.MethodGet, "/foo"); code != 200 {
		t.Fatalf("/foo code=%d, want 200", code)
	}
}
