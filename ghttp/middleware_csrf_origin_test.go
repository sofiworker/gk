package ghttp

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===========================================================================
// CSRF 同源校验的回归测试(评审 M5)。
//
// 覆盖三件此前被漏掉或被写反的事:
//  1. `Origin: null` 必须【拒绝】,而"头完全缺失"必须【放行】——二者语义不同;
//  2. 同源判定必须带 scheme,只比 host 会把明文页面判为 https 站点的同源来源;
//  3. 请求自身 scheme 经 isTLSRequest 推导,故 X-Forwarded-Proto 只在可信代理后被采信。
//
// Regression tests for the CSRF same-origin check (review item M5).
// ===========================================================================

// csrfOriginServe 装一台只挂 CSRF 中间件的 Server 并发一次带正确双提交 token 的 POST,
// 返回状态码。token 恒定正确是刻意的:任何非 200 都只能来自同源校验,把"来源判定"与
// "token 判定"两个关注点彻底分开。mutate 可设置 TLS / RemoteAddr / 任意头。
// csrfOriginServe serves one POST carrying a correct double-submit token through a
// Server mounting only the CSRF middleware, returning the status. The token is always
// valid on purpose: any non-200 can then only come from the origin check, isolating
// origin decisions from token decisions. mutate can set TLS, RemoteAddr, or headers.
func csrfOriginServe(t *testing.T, cfg CSRFConfig, mutate func(*http.Request), srvOpts ...Option) (int, error) {
	t.Helper()
	var hookErr error
	s := New(append(srvOpts, WithErrorHook(func(_ *http.Request, _ int, err error) {
		hookErr = err
	}))...)
	s.Use(CSRF(cfg))
	reached := false
	if err := s.RawHandle(http.MethodPost, "/guard", RawHandlerFunc(
		func(_ context.Context, _ *Request, resp *Response) error {
			reached = true
			resp.WriteHeader(http.StatusOK)
			return nil
		})); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}

	const tok = "origin-regression-token"
	cookieName := cfg.CookieName
	if cookieName == "" {
		cookieName = DefaultCSRFCookieName
	}
	if cfg.UseHostPrefixedCookie && !strings.HasPrefix(cookieName, CSRFHostCookiePrefix) {
		cookieName = CSRFHostCookiePrefix + cookieName
	}
	r := httptest.NewRequest(http.MethodPost, "/guard", nil)
	r.Header.Set(DefaultCSRFHeaderName, tok)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	if mutate != nil {
		mutate(r)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)

	// 被拒的请求绝不能到达 handler:先执行副作用再 403 等于防护失效。
	if rec.Code != http.StatusOK && reached {
		t.Errorf("status=%d but the handler was reached; side effects already ran", rec.Code)
	}
	return rec.Code, hookErr
}

// csrfWithTLS 把请求标记为经 TLS 直连到达(零值 ConnectionState 足够:只判 nil)。
func csrfWithTLS(r *http.Request) { r.TLS = &tls.ConnectionState{} }

// TestCSRF_OriginNullRejected 验证 `Origin: null` 被拒:403 + 可 Is 到 ErrCSRFTokenInvalid。
// 沙箱 iframe(<iframe sandbox>)与 data:/blob: 文档发的正是 `Origin: null`,而这些上下文
// 恰好由攻击者构造;把它当"缺失"放行会对全部攻击者可控来源关闭第二道防线。
func TestCSRF_OriginNullRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"plain request", func(r *http.Request) { r.Header.Set("Origin", "null") }},
		{"TLS request", func(r *http.Request) {
			csrfWithTLS(r)
			r.Header.Set("Origin", "null")
		}},
		// Referer 也可能是 "null":同一条不透明来源语义,不能因承载头不同而放行。
		{"null Origin plus a cross-site Referer", func(r *http.Request) {
			r.Header.Set("Origin", "null")
			r.Header.Set("Referer", "https://evil.example/a.html")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := csrfOriginServe(t, CSRFConfig{}, tc.mutate)
			if code != http.StatusForbidden {
				t.Errorf("status=%d, want 403 for an opaque null origin", code)
			}
			if !errors.Is(err, ErrCSRFTokenInvalid) {
				t.Errorf("hook err=%v, want errors.Is(err, ErrCSRFTokenInvalid)", err)
			}
		})
	}
}

// TestCSRF_OriginAbsentStillPasses 验证头【完全缺失】仍放行,与 `Origin: null` 区别对待。
// curl / 服务间调用不发这两个头,也不携带 cookie,本就不受 CSRF 威胁;若一并拒绝,所有
// 非浏览器客户端会被打死,而防护强度并无增益(浏览器对非安全跨站请求必发 Origin)。
func TestCSRF_OriginAbsentStillPasses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"no Origin and no Referer", nil},
		{"no Origin and no Referer over TLS", csrfWithTLS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := csrfOriginServe(t, CSRFConfig{}, tc.mutate)
			if code != http.StatusOK {
				t.Errorf("status=%d err=%v, want 200: a non-browser client sends neither header", code, err)
			}
		})
	}
}

// TestCSRF_OriginSchemeMustMatch 验证同源判定带 scheme:请求自身是 https 时,http:// 来源
// 被拒。只比 host 会把中间人可任意改写的明文页面当作同源,第二道防线随之失效。
func TestCSRF_OriginSchemeMustMatch(t *testing.T) {
	cases := []struct {
		name     string
		overTLS  bool
		origin   string
		wantCode int
	}{
		{name: "http origin on a TLS request is rejected", overTLS: true, origin: "http://example.com", wantCode: http.StatusForbidden},
		{name: "https origin on a TLS request passes", overTLS: true, origin: "https://example.com", wantCode: http.StatusOK},
		{name: "https origin on a plaintext request is rejected", origin: "https://example.com", wantCode: http.StatusForbidden},
		{name: "http origin on a plaintext request passes", origin: "http://example.com", wantCode: http.StatusOK},
		// scheme 大小写不敏感(RFC 3986 §3.1),不能因大写被误拒。
		{name: "scheme comparison is case-insensitive", overTLS: true, origin: "HTTPS://example.com", wantCode: http.StatusOK},
		// Referer 走同一条 scheme 判定:它被规范化为 scheme://host 后进入同一比较。
		{name: "http Referer on a TLS request is rejected", overTLS: true, origin: "", wantCode: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := csrfOriginServe(t, CSRFConfig{}, func(r *http.Request) {
				if tc.overTLS {
					csrfWithTLS(r)
				}
				if tc.origin != "" {
					r.Header.Set("Origin", tc.origin)
				} else {
					r.Header.Set("Referer", "http://example.com/page")
				}
			})
			if code != tc.wantCode {
				t.Errorf("status=%d err=%v, want %d", code, err, tc.wantCode)
			}
			if tc.wantCode == http.StatusForbidden && !errors.Is(err, ErrCSRFTokenInvalid) {
				t.Errorf("hook err=%v, want errors.Is(err, ErrCSRFTokenInvalid)", err)
			}
		})
	}
}

// TestCSRF_OriginTrustedListBypassesScheme 验证 TrustedOrigins 是完整来源串(含 scheme)
// 的精确匹配,不受请求自身 scheme 影响:显式声明可信的跨源本就允许 scheme 不同。
// 同时锁定"仅 host 相同但 scheme 不同"的条目【不】被视为可信。
func TestCSRF_OriginTrustedListBypassesScheme(t *testing.T) {
	cfg := CSRFConfig{TrustedOrigins: []string{"https://app.example.com"}}
	t.Run("trusted https origin passes on a plaintext request", func(t *testing.T) {
		code, err := csrfOriginServe(t, cfg, func(r *http.Request) {
			r.Header.Set("Origin", "https://app.example.com")
		})
		if code != http.StatusOK {
			t.Errorf("status=%d err=%v, want 200", code, err)
		}
	})
	t.Run("same host with a different scheme is not trusted", func(t *testing.T) {
		code, _ := csrfOriginServe(t, cfg, func(r *http.Request) {
			r.Header.Set("Origin", "http://app.example.com")
		})
		if code != http.StatusForbidden {
			t.Errorf("status=%d, want 403: the trusted entry names https only", code)
		}
	})
}

// TestCSRF_OriginForwardedProtoOnlyBehindTrustedProxy 验证 X-Forwarded-Proto 只在可信代理
// 后被采信——这是复用 isTLSRequest 的直接后果,也是本修复最关键的一条:若在此另写一套
// scheme 推导,任何客户端都能靠伪造 X-Forwarded-Proto: https 让 https:// 来源被判同源,
// 把刚补上的洞重新打开。
func TestCSRF_OriginForwardedProtoOnlyBehindTrustedProxy(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		proto      string
		origin     string
		trusted    bool
		wantCode   int
		why        string
	}{
		{
			name:       "trusted proxy reports https: https origin is same-origin",
			remoteAddr: "10.1.2.3:1234", proto: "https", origin: "https://example.com",
			trusted: true, wantCode: http.StatusOK,
			why: "the proxy terminated TLS, so the request is effectively https",
		},
		{
			name:       "trusted proxy reports https: http origin is cross-scheme",
			remoteAddr: "10.1.2.3:1234", proto: "https", origin: "http://example.com",
			trusted: true, wantCode: http.StatusForbidden,
			why: "an https request must not accept a plaintext origin",
		},
		{
			name:       "untrusted peer forging https is ignored",
			remoteAddr: "9.9.9.9:1234", proto: "https", origin: "https://example.com",
			trusted: true, wantCode: http.StatusForbidden,
			why: "a spoofable header from outside the trusted range must not upgrade the scheme",
		},
		{
			name:       "no trusted proxy configured ignores the header entirely",
			remoteAddr: "10.1.2.3:1234", proto: "https", origin: "https://example.com",
			trusted: false, wantCode: http.StatusForbidden,
			why: "the default config trusts no forwarded header at all",
		},
		{
			name:       "trusted proxy reports http: http origin is same-origin",
			remoteAddr: "10.1.2.3:1234", proto: "http", origin: "http://example.com",
			trusted: true, wantCode: http.StatusOK,
			why: "the proxy explicitly reports plaintext",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opts []Option
			if tc.trusted {
				opts = append(opts, WithTrustedProxies("10.0.0.0/8"))
			}
			code, err := csrfOriginServe(t, CSRFConfig{}, func(r *http.Request) {
				r.RemoteAddr = tc.remoteAddr
				r.Header.Set("X-Forwarded-Proto", tc.proto)
				r.Header.Set("Origin", tc.origin)
			}, opts...)
			if code != tc.wantCode {
				t.Errorf("status=%d err=%v, want %d (%s)", code, err, tc.wantCode, tc.why)
			}
		})
	}
}

// TestCSRF_OriginRealTLSBeatsForwardedHeader 验证真实 TLS 连接不受转发头影响:r.TLS 非 nil
// 是第一手证据,上游代理写的 X-Forwarded-Proto: http 不能把它降级成明文。
func TestCSRF_OriginRealTLSBeatsForwardedHeader(t *testing.T) {
	code, err := csrfOriginServe(t, CSRFConfig{}, func(r *http.Request) {
		csrfWithTLS(r)
		r.RemoteAddr = "10.1.2.3:1234"
		r.Header.Set("X-Forwarded-Proto", "http")
		r.Header.Set("Origin", "https://example.com")
	}, WithTrustedProxies("10.0.0.0/8"))
	if code != http.StatusOK {
		t.Errorf("status=%d err=%v, want 200: a real TLS connection outranks any forwarded header", code, err)
	}
}

// TestCSRF_HostPrefixedCookie 验证 UseHostPrefixedCookie 的 cookie 属性:名字带 __Host-
// 前缀,且 Secure / Path=/ / 无 Domain 三条被【强制】——浏览器对该前缀做硬校验,任一条
// 不满足就丢弃整个 Set-Cookie,双提交会因"永远没有 cookie"而全量 403,故必须逐条锁定。
func TestCSRF_HostPrefixedCookie(t *testing.T) {
	issue := func(t *testing.T, cfg CSRFConfig) *http.Cookie {
		t.Helper()
		obs := serveCSRF(t, cfg, csrfRequest{method: http.MethodGet})
		cookies := obs.rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("got %d cookies, want exactly 1", len(cookies))
		}
		return cookies[0]
	}

	t.Run("default name gains the __Host- prefix", func(t *testing.T) {
		c := issue(t, CSRFConfig{UseHostPrefixedCookie: true})
		if want := CSRFHostCookiePrefix + DefaultCSRFCookieName; c.Name != want {
			t.Errorf("Name=%q, want %q", c.Name, want)
		}
	})

	t.Run("custom name gains the __Host- prefix", func(t *testing.T) {
		c := issue(t, CSRFConfig{CookieName: "_xsrf", UseHostPrefixedCookie: true})
		if c.Name != "__Host-_xsrf" {
			t.Errorf("Name=%q, want __Host-_xsrf", c.Name)
		}
	})

	t.Run("an explicit __Host- name is not doubled", func(t *testing.T) {
		c := issue(t, CSRFConfig{CookieName: "__Host-csrf", UseHostPrefixedCookie: true})
		if c.Name != "__Host-csrf" {
			t.Errorf("Name=%q, want __Host-csrf (prefixing must be idempotent)", c.Name)
		}
	})

	// 关键:配置里显式写了与 __Host- 冲突的值时,以浏览器的硬性要求为准并覆盖它们。
	t.Run("conflicting config is overridden by the prefix requirements", func(t *testing.T) {
		c := issue(t, CSRFConfig{
			UseHostPrefixedCookie: true,
			Secure:                false,
			CookiePath:            "/admin",
			CookieDomain:          "example.com",
		})
		if !c.Secure {
			t.Error("Secure=false: the browser discards a non-Secure __Host- cookie")
		}
		if c.Path != "/" {
			t.Errorf("Path=%q, want / : the browser requires Path=/ for __Host-", c.Path)
		}
		if c.Domain != "" {
			t.Errorf("Domain=%q, want empty: the browser rejects a __Host- cookie carrying Domain", c.Domain)
		}
	})

	// 属性必须真的出现在原始 Set-Cookie 头里,且绝不能出现 Domain。
	t.Run("raw Set-Cookie is well formed", func(t *testing.T) {
		obs := serveCSRF(t, CSRFConfig{UseHostPrefixedCookie: true, CookieDomain: "example.com"},
			csrfRequest{method: http.MethodGet})
		raw := obs.rec.Header().Get("Set-Cookie")
		for _, want := range []string{CSRFHostCookiePrefix + DefaultCSRFCookieName + "=", "Path=/", "Secure"} {
			if !strings.Contains(raw, want) {
				t.Errorf("Set-Cookie=%q, want it to contain %q", raw, want)
			}
		}
		if strings.Contains(raw, "Domain=") {
			t.Errorf("Set-Cookie=%q, must not carry Domain for a __Host- cookie", raw)
		}
	})

	// 写与读必须用同一个名字:否则每个非安全请求都因"没有 cookie"被 403,防护变成拒绝服务。
	t.Run("the prefixed cookie is the one verified", func(t *testing.T) {
		cfg := CSRFConfig{UseHostPrefixedCookie: true}
		code, err := csrfOriginServe(t, cfg, func(r *http.Request) {
			csrfWithTLS(r)
			r.Header.Set("Origin", "https://example.com")
		})
		if code != http.StatusOK {
			t.Errorf("status=%d err=%v, want 200: the issued and verified cookie names must match", code, err)
		}

		// 反面:只带未加前缀的旧名字必须被拒,否则等于留了一条旁路。
		var hookErr error
		s := New(WithErrorHook(func(_ *http.Request, _ int, e error) { hookErr = e }))
		s.Use(CSRF(cfg))
		if err := s.RawHandle(http.MethodPost, "/guard", RawHandlerFunc(
			func(_ context.Context, _ *Request, resp *Response) error {
				resp.WriteHeader(http.StatusOK)
				return nil
			})); err != nil {
			t.Fatalf("RawHandle: %v", err)
		}
		r := httptest.NewRequest(http.MethodPost, "/guard", nil)
		csrfWithTLS(r)
		r.Header.Set("Origin", "https://example.com")
		r.Header.Set(DefaultCSRFHeaderName, "tok")
		r.AddCookie(&http.Cookie{Name: DefaultCSRFCookieName, Value: "tok"})
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("unprefixed cookie: status=%d, want 403 (it must not be a bypass)", rec.Code)
		}
		if !errors.Is(hookErr, ErrCSRFTokenInvalid) {
			t.Errorf("hook err=%v, want errors.Is(err, ErrCSRFTokenInvalid)", hookErr)
		}
	})
}

// TestRequestScheme 直测 scheme 推导:它是同源判定的输入,必须只返回 http/https 两个值,
// 且与 isTLSRequest 完全一致(二者分叉就意味着 CSRF 与安全头的信任模型分叉)。
func TestRequestScheme(t *testing.T) {
	cases := []struct {
		name    string
		overTLS bool
		want    string
	}{
		{name: "plaintext request", want: "http"},
		{name: "TLS request", overTLS: true, want: "https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/x", nil)
			if tc.overTLS {
				csrfWithTLS(r)
			}
			req := &Request{Request: r}
			if got := requestScheme(req); got != tc.want {
				t.Errorf("requestScheme=%q, want %q", got, tc.want)
			}
			if got, want := requestScheme(req) == "https", isTLSRequest(req); got != want {
				t.Errorf("requestScheme says https=%v but isTLSRequest=%v; the two must not diverge", got, want)
			}
		})
	}
}
