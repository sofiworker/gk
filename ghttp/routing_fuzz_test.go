package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件是路由层的 fuzz 测试。目标不是"输出正确",而是"永不崩溃、永远给出合法响应、
// 关键不变量恒成立":任意字节路径都不得让 ServeHTTP panic 或返回非法状态码;校验器
// 在两种模式下都必须终止且守住安全不变量(dot 段在两种模式下都被拒)。
// This file holds fuzz tests for the routing layer. The goal is not "correct
// output" but "never crashes, always a legal response, invariants always hold":
// any byte path must not panic ServeHTTP or yield an illegal status code; the
// validator must terminate in both modes and uphold the safety invariant (a dot
// segment is rejected in both modes).

// fuzzMux 构建一棵混合形态的树(静态 / 单参 / 多参 / catch-all / 多 method),供
// fuzz 请求打击。
// fuzzMux builds a tree of mixed shapes (static / single-param / multi-param /
// catch-all / multi-method) for fuzz requests to hammer.
func fuzzMux(tb testing.TB) *Mux {
	tb.Helper()
	m := New()
	routes := []routeSpec{
		{http.MethodGet, "/"},
		{http.MethodGet, "/ping"},
		{http.MethodGet, "/users/{id}"},
		{http.MethodGet, "/users/{id}/repos"},
		{http.MethodPost, "/users/{id}"},
		{http.MethodGet, "/a/{p1}/b/{p2}"},
		{http.MethodGet, "/a/lit/b"},
		{http.MethodGet, "/files/{rest...}"},
		{http.MethodGet, "/search"},
		{http.MethodGet, "/repos/{owner}/{repo}/git/refs/{ref}"},
		{http.MethodDelete, "/repos/{owner}/{repo}"},
	}
	for _, r := range routes {
		if err := m.RawHandle(r.method, r.pattern, func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			tb.Fatalf("register %s %s: %v", r.method, r.pattern, err)
		}
	}
	return m
}

// isLegalStatus 报告状态码是否在本路由层允许产生的集合内。
// isLegalStatus reports whether the status is within the set this routing layer
// is allowed to produce.
func isLegalStatus(code int) bool {
	switch code {
	case http.StatusOK,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusBadRequest,
		http.StatusMovedPermanently,
		http.StatusPermanentRedirect,
		http.StatusInternalServerError:
		return true
	default:
		return false
	}
}

// FuzzServeHTTPNoPanic:任意 method 与路径,ServeHTTP 都不得 panic,且只能返回合法
// 状态码。这是路由层最重要的健壮性保证。
// FuzzServeHTTPNoPanic: for any method and path, ServeHTTP must not panic and may
// only return a legal status. This is the routing layer's key robustness
// guarantee.
func FuzzServeHTTPNoPanic(f *testing.F) {
	seeds := []string{
		"/", "/ping", "/users/42", "/users/42/repos", "/a/x/b/y", "/a/lit/b",
		"/files/a/b/c", "/search/", "/users/42/", "//", "/a//b", "/a/../b",
		"/./", "/..", "/%2e%2e/", "/users/%20", "/файлы/тест", "/a/b/c/d/e/f/g",
		"", "no-leading-slash", "/users/" + strings.Repeat("x", 4096),
		"/repos/o/r/git/refs/heads/main", "/\x00/null", "/tab\t/x", "/a?b=c",
	}
	for _, s := range seeds {
		f.Add(http.MethodGet, s)
		f.Add(http.MethodPost, s)
	}
	m := fuzzMux(f)
	f.Fuzz(func(t *testing.T, method, path string) {
		// httptest.NewRequest 对非法 target 会 panic;这类输入不代表真实请求,跳过。
		// 我们要 fuzz 的是路由层,不是 net/http 的请求构造。
		// httptest.NewRequest panics on illegal targets; such inputs are not real
		// requests, so skip them. We fuzz the routing layer, not net/http's
		// request construction.
		req := safeNewRequest(method, path)
		if req == nil {
			t.Skip()
		}
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req) // 不得 panic / must not panic
		if !isLegalStatus(rec.Code) {
			t.Fatalf("method=%q path=%q -> illegal status %d", method, path, rec.Code)
		}
	})
}

// safeNewRequest 用 recover 包裹 httptest.NewRequest,非法 target 返回 nil 而非
// panic,使 fuzz 聚焦于路由层。
// safeNewRequest wraps httptest.NewRequest in recover, returning nil (instead of
// panicking) for illegal targets so the fuzz stays focused on routing.
func safeNewRequest(method, target string) (req *http.Request) {
	defer func() {
		if recover() != nil {
			req = nil
		}
	}()
	// method 为空时 NewRequest 视为 GET;非法 method 字符也可能 panic,一并兜住。
	// An empty method is treated as GET by NewRequest; illegal method chars may
	// also panic and are caught here too.
	if method == "" {
		method = http.MethodGet
	}
	return httptest.NewRequest(method, "http://example.com"+ensureLeadingSlash(target), nil)
}

// ensureLeadingSlash 保证 target 以 "/" 开头,避免被解释为 scheme/host。
// ensureLeadingSlash ensures target starts with "/", avoiding scheme/host
// interpretation.
func ensureLeadingSlash(s string) string {
	if s == "" {
		return "/"
	}
	if s[0] != '/' {
		return "/" + s
	}
	return s
}

// FuzzValidateRequestPath:校验器在两种模式下都必须终止且不 panic,并守住不变量——
//  1. 快速模式接受的路径,不含真正的 dot 段("."/".." 整段);
//  2. 快速模式接受 ⊇ 严格模式接受(严格更严):严格接受则快速也接受。
//
// FuzzValidateRequestPath: the validator must terminate without panic in both
// modes and uphold invariants — (1) a path accepted in fast mode contains no
// real dot segment; (2) strict acceptance implies fast acceptance (strict is
// stricter).
func FuzzValidateRequestPath(f *testing.F) {
	seeds := []string{
		"/", "/a", "/a/b", "/a/../b", "/a/./b", "/a//b", "/..", "/.",
		"/a/b/", "//", "/...", "/a/...", "/....", "/a/.b", "/.a", "/a.",
		"/foo/bar.txt", "/a/b/c/../../d", "", "no-slash", "/a/b/..",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, path string) {
		fastErr := validateRequestPath(path, false)  // 不得 panic / must not panic
		strictErr := validateRequestPath(path, true) // 不得 panic / must not panic

		// 不变量 1:快速模式通过 ⇒ 无真正 dot 段。
		// Invariant 1: fast-mode pass ⇒ no real dot segment.
		if fastErr == nil && hasRealDotSegment(path) {
			t.Fatalf("fast mode accepted %q but it has a dot segment", path)
		}
		// 不变量 2:严格模式通过 ⇒ 快速模式也通过(严格 ⊆ 快速)。
		// Invariant 2: strict pass ⇒ fast pass (strict ⊆ fast).
		if strictErr == nil && fastErr != nil {
			t.Fatalf("strict accepted %q but fast rejected it (%v)", path, fastErr)
		}
	})
}

// hasRealDotSegment 是独立于被测实现的参考判定:path 是否含整段为 "." 或 ".." 的段。
// 用朴素分段法,作为 checkDotSegments 快速实现的交叉校验基准。
// hasRealDotSegment is a reference oracle independent of the implementation under
// test: whether path contains a segment that is exactly "." or "..". It uses
// naive splitting as a cross-check baseline for the fast checkDotSegments.
func hasRealDotSegment(path string) bool {
	if path == "" || path[0] != '/' {
		return false // 非法前缀由另一分支处理 / illegal prefix handled elsewhere
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

// FuzzRegisterThenMatch:对随机生成的"合法模板"注册后,用其实例化路径请求,必须命中
// 200。这检验注册与匹配的一致性——凡能注册的,其规范请求必可命中。
// FuzzRegisterThenMatch: after registering a randomly generated "valid template",
// a request built from its instantiation must hit 200. This checks
// register/match consistency — whatever registers must match its canonical
// request.
func FuzzRegisterThenMatch(f *testing.F) {
	// 种子:段数与"是否参数化"的位掩码。
	// Seeds: segment count and a bitmask of "is this segment a param".
	f.Add(uint16(0b0), uint8(1))
	f.Add(uint16(0b1), uint8(2))
	f.Add(uint16(0b101), uint8(4))
	f.Add(uint16(0b11010), uint8(6))
	f.Fuzz(func(t *testing.T, mask uint16, nseg uint8) {
		n := int(nseg%6) + 1 // 1..6 段 / 1..6 segments
		pattern, target, want := synthRoute(mask, n)

		m := New()
		var got matchResult
		if err := m.RawHandle(http.MethodGet, pattern, captureHandler(pattern, &got)); err != nil {
			// 合成保证了模板合法;若仍报错,是实现问题,须暴露。
			// Synthesis guarantees a legal template; a registration error here is
			// an implementation problem and must surface.
			t.Fatalf("register %q: %v", pattern, err)
		}
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://t"+target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("pattern=%q target=%q -> code=%d, want 200", pattern, target, rec.Code)
		}
		for k, v := range want {
			if got.params[k] != v {
				t.Fatalf("pattern=%q target=%q param %q=%q want %q", pattern, target, k, got.params[k], v)
			}
		}
	})
}

// synthRoute 用位掩码合成一个必然合法的模板:第 i 段若 mask 的第 i 位为 1 则是参数
// {p{i}},否则是静态 "s{i}"。返回模板、实例化请求路径、期望参数。保证:参数名唯一、
// 无 catch-all(单独测)、无空段/ dot 段。
// synthRoute builds a guaranteed-legal template from a bitmask: segment i is a
// param {p{i}} if bit i of mask is set, else a static "s{i}". It returns the
// template, an instantiated request path, and the expected params. Guarantees:
// unique param names, no catch-all (tested separately), no empty/dot segments.
func synthRoute(mask uint16, n int) (pattern, target string, want map[string]string) {
	want = map[string]string{}
	var pb, tb strings.Builder
	for i := 0; i < n; i++ {
		pb.WriteByte('/')
		tb.WriteByte('/')
		if mask&(1<<uint(i)) != 0 {
			name := "p" + itoa(i)
			pb.WriteByte('{')
			pb.WriteString(name)
			pb.WriteByte('}')
			val := "v" + itoa(i)
			tb.WriteString(val)
			want[name] = val
		} else {
			seg := "s" + itoa(i)
			pb.WriteString(seg)
			tb.WriteString(seg)
		}
	}
	return pb.String(), tb.String(), want
}

// itoa 是不依赖 strconv 的小整数转字符串(fuzz 段数仅 0..5)。
// itoa converts a small int to string without strconv (fuzz seg index is 0..5).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [4]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
