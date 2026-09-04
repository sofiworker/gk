package ghttp

import (
	"context"
	"encoding/base64"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===========================================================================
// BasicAuth 用户名枚举时序侧信道的回归测试(评审 M15)。
//
// 刻意【不写】时序断言:一次 ~8 字节 memcmp 的差异远小于单机噪声(调度、GC、缓存),
// 任何基于墙上时钟的断言都会 flaky,还会把 CI 变成随机失败源。改为断言两件可确定验证
// 的事:
//  1. 行为正确性——修复引入的 dummy 比较绝不能让未知用户混进来,也不能拒绝合法用户;
//  2. 结构不变量——比较调用不得再被 && 短路掉(见 TestBasicAuthComparesUnconditionally),
//     这才是"两条路径工作量一致"的可验证形式。
//
// Regression tests for the BasicAuth username-enumeration timing side channel (M15).
// ===========================================================================

// basicAuthServe 装一台只挂 BasicAuth 的 Server 并发一次请求,返回状态码与下传的用户名。
// header 原样写入 Authorization(空串表示完全不设置该头)。
// basicAuthServe serves one request through a Server mounting only BasicAuth,
// returning the status and the username passed down. header goes into Authorization
// verbatim ("" means the header is not set at all).
func basicAuthServe(t *testing.T, accounts map[string]string, header string) (int, string) {
	t.Helper()
	s := New()
	s.Use(BasicAuth("My Realm", accounts))
	if err := s.RawHandle(http.MethodGet, "/private", RawHandlerFunc(
		func(ctx context.Context, _ *Request, resp *Response) error {
			resp.Header().Set("X-User", BasicAuthUser(ctx))
			resp.WriteHeader(http.StatusOK)
			return nil
		})); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec.Code, rec.Header().Get("X-User")
}

// basicAuthCreds 编码一个 Basic 凭据头。
// basicAuthCreds encodes one Basic credentials header.
func basicAuthCreds(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// TestBasicAuthUnknownUserStillRejected 表驱动锁定 dummy 比较【没有】放宽认证:未知用户
// 无论提交什么密码都必须 401。这是本修复最大的风险面——为了让两条路径工作量一致而引入的
// 对照比较,一旦其结果被误当成"认证通过",就等于把整个认证拆掉;尤其要覆盖"密码恰好等于
// dummy 值"和空密码这两个边界。
func TestBasicAuthUnknownUserStillRejected(t *testing.T) {
	accounts := map[string]string{"alice": "secret"}
	cases := []struct {
		name string
		user string
		pass string
	}{
		{name: "unknown user with an arbitrary password", user: "carol", pass: "whatever"},
		{name: "unknown user with an empty password", user: "carol", pass: ""},
		// 未知用户提交的密码恰好等于内部 dummy 对照值:比较会返回 true,认证仍必须失败。
		{name: "unknown user whose password equals the internal dummy", user: "carol", pass: basicAuthDummyPassword},
		{name: "unknown user reusing a valid password", user: "carol", pass: "secret"},
		{name: "empty username", user: "", pass: "secret"},
		// 用户名大小写敏感:map 键不做归一,Alice 不是 alice。
		{name: "known username in a different case", user: "Alice", pass: "secret"},
		// 用户名带前后空白不得被裁剪成有效用户。
		{name: "known username padded with spaces", user: " alice ", pass: "secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, user := basicAuthServe(t, accounts, basicAuthCreds(tc.user, tc.pass))
			if code != http.StatusUnauthorized {
				t.Errorf("status=%d, want 401 for user=%q pass=%q", code, tc.user, tc.pass)
			}
			if user != "" {
				t.Errorf("X-User=%q, want empty: a rejected request must not reach the handler", user)
			}
		})
	}
}

// TestBasicAuthKnownUserOutcomes 表驱动锁定已知用户的两种结果:正确密码 200 且用户名下传,
// 错误密码 401。dummy 比较的引入不得改变任何一条。
func TestBasicAuthKnownUserOutcomes(t *testing.T) {
	accounts := map[string]string{"alice": "secret", "bob": "pw", "empty": ""}
	cases := []struct {
		name     string
		user     string
		pass     string
		wantCode int
		wantUser string
	}{
		{name: "correct password authenticates", user: "alice", pass: "secret", wantCode: http.StatusOK, wantUser: "alice"},
		{name: "a second account authenticates", user: "bob", pass: "pw", wantCode: http.StatusOK, wantUser: "bob"},
		{name: "wrong password is rejected", user: "alice", pass: "wrong", wantCode: http.StatusUnauthorized},
		{name: "empty password against a real one is rejected", user: "alice", pass: "", wantCode: http.StatusUnauthorized},
		{name: "password prefix is rejected", user: "alice", pass: "secre", wantCode: http.StatusUnauthorized},
		{name: "password with a trailing byte is rejected", user: "alice", pass: "secretx", wantCode: http.StatusUnauthorized},
		// 账户配置了空密码时,只有空密码匹配——不能因为 dummy 是非空值而错误放行。
		{name: "an account with an empty password matches only an empty password", user: "empty", pass: "", wantCode: http.StatusOK, wantUser: "empty"},
		{name: "an account with an empty password rejects the dummy value", user: "empty", pass: basicAuthDummyPassword, wantCode: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, user := basicAuthServe(t, accounts, basicAuthCreds(tc.user, tc.pass))
			if code != tc.wantCode {
				t.Errorf("status=%d, want %d", code, tc.wantCode)
			}
			if user != tc.wantUser {
				t.Errorf("X-User=%q, want %q", user, tc.wantUser)
			}
		})
	}
}

// TestBasicAuthUserFromContext 验证 BasicAuthUser(ctx) 只在认证成功后返回用户名,
// 无中间件或认证失败时返回空串(下游据此判断"是否已认证",绝不能给出假阳性)。
func TestBasicAuthUserFromContext(t *testing.T) {
	t.Run("authenticated request carries the username", func(t *testing.T) {
		code, user := basicAuthServe(t, map[string]string{"alice": "secret"}, basicAuthCreds("alice", "secret"))
		if code != http.StatusOK || user != "alice" {
			t.Errorf("status=%d X-User=%q, want 200 and alice", code, user)
		}
	})
	t.Run("no middleware yields an empty username", func(t *testing.T) {
		if got := BasicAuthUser(context.Background()); got != "" {
			t.Errorf("BasicAuthUser=%q, want empty without the middleware", got)
		}
	})
	// 只有中间件写入的 string 值算数:同名类型无法从包外构造,故此处只校验类型不匹配时的降级。
	t.Run("a non-string context value yields an empty username", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), basicAuthUserKey{}, 42)
		if got := BasicAuthUser(ctx); got != "" {
			t.Errorf("BasicAuthUser=%q, want empty for a non-string value", got)
		}
	})
}

// TestBasicAuthMalformedHeaders 表驱动锁定畸形 Authorization 头一律 401,且都走同一条
// 401 出口(挑战头必须存在,否则客户端不知道该用哪种认证)。
func TestBasicAuthMalformedHeaders(t *testing.T) {
	accounts := map[string]string{"alice": "secret"}
	cases := []struct {
		name   string
		header string
	}{
		{name: "no header at all", header: ""},
		{name: "wrong scheme", header: "Bearer token"},
		{name: "scheme only", header: "Basic"},
		{name: "scheme with no payload", header: "Basic "},
		{name: "invalid base64", header: "Basic !!!notbase64"},
		{name: "no colon separator", header: "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			s.Use(BasicAuth("My Realm", accounts))
			if err := s.RawHandle(http.MethodGet, "/private", RawHandlerFunc(
				func(_ context.Context, _ *Request, resp *Response) error {
					resp.WriteHeader(http.StatusOK)
					return nil
				})); err != nil {
				t.Fatalf("RawHandle: %v", err)
			}
			r := httptest.NewRequest(http.MethodGet, "/private", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, r)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status=%d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="My Realm"` {
				t.Errorf("WWW-Authenticate=%q, want Basic realm=\"My Realm\"", got)
			}
		})
	}
}

// TestBasicAuthComparesUnconditionally 是 M15 的结构不变量测试:BasicAuth 里的恒定时间
// 比较不得被 && 短路。
//
// 为什么是结构断言而不是时序断言:一次 ~8 字节 memcmp 的耗时差远小于调度/GC/缓存噪声,
// 墙上时钟断言必然 flaky。
//
// 为什么需要它:M15 修复【不改变任何可观测行为】——未知用户修复前后都是 401。因此本文件
// 其余行为测试在实现被改回 `exists && constantTimeEqual(...)` 时会全部照旧通过,只有这条
// 会失败。它守的正是那个不可观测的差异,也是本修复唯一的回归护栏。
//
// 做法:解析 basic_auth.go 的 AST,数"位于 && 右操作数内(即会被左侧短路)"的恒定时间
// 比较调用。数量必须为 0,且至少存在一个无条件执行的比较。
func TestBasicAuthComparesUnconditionally(t *testing.T) {
	const src = "basic_auth.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", src, err)
	}

	var fn *ast.FuncDecl
	ast.Inspect(file, func(n ast.Node) bool {
		if d, ok := n.(*ast.FuncDecl); ok && d.Recv == nil && d.Name.Name == "BasicAuth" {
			fn = d
			return false
		}
		return true
	})
	if fn == nil {
		t.Fatalf("BasicAuth declaration not found in %s", src)
	}

	// isConstantTimeCall 同时识别包内 helper(constantTimeEqual)与直接调用
	// subtle.ConstantTimeCompare,以免将来内联 helper 时这条断言静默失效。
	isConstantTimeCall := func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return false
		}
		var name string
		switch f := call.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		default:
			return false
		}
		return strings.Contains(strings.ToLower(name), "constanttime")
	}

	shortCircuited := map[ast.Node]bool{}
	var forms []string
	ast.Inspect(fn, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || be.Op != token.LAND {
			return true
		}
		// && 的右操作数只在左操作数为 true 时求值,故其中的比较是"有条件的"。
		found := false
		ast.Inspect(be.Y, func(m ast.Node) bool {
			if m != nil && isConstantTimeCall(m) {
				shortCircuited[m] = true
				found = true
			}
			return true
		})
		if found {
			forms = append(forms, fset.Position(be.Pos()).String())
		}
		return true
	})

	total, unconditional := 0, 0
	ast.Inspect(fn, func(n ast.Node) bool {
		if n != nil && isConstantTimeCall(n) {
			total++
			if !shortCircuited[n] {
				unconditional++
			}
		}
		return true
	})

	if total == 0 {
		t.Fatalf("no constant-time comparison found in BasicAuth; the password check must use one")
	}
	if len(shortCircuited) != 0 {
		t.Errorf("%d constant-time comparison(s) sit behind a && short circuit at %v: an unknown user "+
			"would skip the comparison entirely, so response time reveals whether a username exists",
			len(shortCircuited), forms)
	}
	if unconditional == 0 {
		t.Errorf("BasicAuth has %d constant-time comparison(s) but none runs unconditionally; both the "+
			"known-user and unknown-user paths must perform the same work", total)
	}
}

// TestBasicAuthDummyPasswordIsUsable 锁定 dummy 对照值非空:空串会让"账户配置了空密码"
// 与"用户不存在"两条路径比较同一对内容,平白削弱区分度,也让比较退化为零字节工作量。
func TestBasicAuthDummyPasswordIsUsable(t *testing.T) {
	if basicAuthDummyPassword == "" {
		t.Error("basicAuthDummyPassword must be non-empty so the compare does real work")
	}
}
