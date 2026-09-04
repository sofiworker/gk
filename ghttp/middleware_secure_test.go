package ghttp

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// secOKHandler 是安全头测试统一使用的终端:只提交 200,不碰任何头,因此响应里出现的头
// 全部来自被测中间件。
// secOKHandler is the shared terminal for security-header tests: it commits 200 and
// touches no header, so every header in the response comes from the middleware.
func secOKHandler(ctx context.Context, req *Request, resp *Response) error {
	resp.WriteHeader(http.StatusOK)
	return nil
}

// secServe 用一条中间件包裹终端执行一次请求。mutate 可在发出前改写请求(设置 TLS、
// RemoteAddr 或转发头),srvOpts 用于需要 Server 级配置(可信代理)的用例。
// secServe serves one request through a single middleware. mutate can rewrite the
// request before it is issued (TLS, RemoteAddr, or forwarded headers), and srvOpts
// covers cases needing server-level config (trusted proxies).
func secServe(t *testing.T, mw Middleware, mutate func(*http.Request), srvOpts ...Option) http.Header {
	t.Helper()
	s := New(srvOpts...)
	s.Use(mw)
	if err := s.RawHandle(http.MethodGet, "/x", secOKHandler); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if mutate != nil {
		mutate(r)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (the middleware must not short-circuit)", rec.Code)
	}
	return rec.Header()
}

// secBoolPtr 返回指向 v 的指针,用于填写 HSTSOnlyWhenTLS 这类三态(nil/true/false)字段。
// secBoolPtr returns a pointer to v, for tri-state fields such as HSTSOnlyWhenTLS
// (nil/true/false).
func secBoolPtr(v bool) *bool { return &v }

// secTLS 把请求标记为经 TLS 到达。零值 ConnectionState 足够:isTLSRequest 只判 nil。
// secTLS marks a request as arriving over TLS. A zero ConnectionState suffices:
// isTLSRequest only checks for nil.
func secTLS(r *http.Request) { r.TLS = &tls.ConnectionState{} }

// --- 默认值 -----------------------------------------------------------------

// TestSecureHeadersDefault_SafeDefaults 验证零配置下的保守默认集,并确认 HSTS 与 CSP
// 【不】发送:HSTS 在未全站 HTTPS 时会锁死站点,CSP 误配即白屏,二者必须显式声明。
func TestSecureHeadersDefault_SafeDefaults(t *testing.T) {
	h := secServe(t, SecureHeadersDefault(), nil)

	want := map[string]string{
		HeaderContentTypeOptions: "nosniff",
		HeaderFrameOptions:       "DENY",
		HeaderReferrerPolicy:     "strict-origin-when-cross-origin",
	}
	for name, val := range want {
		if got := h.Get(name); got != val {
			t.Errorf("%s=%q, want %q", name, got, val)
		}
	}

	// 默认不得出现的头:opt-in 语义是本中间件的安全承诺。
	for _, name := range []string{
		HeaderStrictTransport,
		HeaderContentSecurity,
		HeaderPermissionsPolicy,
		HeaderCrossOriginOpener,
		HeaderCrossOriginResource,
		HeaderCrossOriginEmbedder,
	} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s=%q, want absent by default (must be opt-in)", name, got)
		}
	}
}

// TestSecureHeaders_ZeroConfigEqualsDefault 验证 SecureHeadersDefault() 等价于
// SecureHeaders(SecureHeadersConfig{}):零值必须就是安全默认,而非"什么都不发"。
func TestSecureHeaders_ZeroConfigEqualsDefault(t *testing.T) {
	fromDefault := secServe(t, SecureHeadersDefault(), nil)
	fromZero := secServe(t, SecureHeaders(SecureHeadersConfig{}), nil)

	for _, name := range []string{HeaderContentTypeOptions, HeaderFrameOptions, HeaderReferrerPolicy, HeaderStrictTransport, HeaderContentSecurity} {
		if fromDefault.Get(name) != fromZero.Get(name) {
			t.Errorf("%s: default=%q zero-config=%q, want identical", name, fromDefault.Get(name), fromZero.Get(name))
		}
	}
}

// --- 自定义值覆盖默认 --------------------------------------------------------

// TestSecureHeaders_CustomOverridesDefaults 验证每个可配置头都能被自定义值覆盖。
func TestSecureHeaders_CustomOverridesDefaults(t *testing.T) {
	tests := []struct {
		name   string
		cfg    SecureHeadersConfig
		header string
		want   string
	}{
		{
			name:   "content type options",
			cfg:    SecureHeadersConfig{ContentTypeOptions: "nosniff-custom"},
			header: HeaderContentTypeOptions,
			want:   "nosniff-custom",
		},
		{
			// 需要同源嵌入时的典型覆盖。
			name:   "frame options sameorigin",
			cfg:    SecureHeadersConfig{FrameOptions: "SAMEORIGIN"},
			header: HeaderFrameOptions,
			want:   "SAMEORIGIN",
		},
		{
			name:   "referrer policy",
			cfg:    SecureHeadersConfig{ReferrerPolicy: "strict-origin-when-cross-origin"},
			header: HeaderReferrerPolicy,
			want:   "strict-origin-when-cross-origin",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := secServe(t, SecureHeaders(tc.cfg), nil)
			if got := h.Get(tc.header); got != tc.want {
				t.Errorf("%s=%q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

// --- pickHeaderValue 三态 ----------------------------------------------------

// TestPickHeaderValue_TriState 验证三态解析:留空取默认、"-" 表示禁用、其它按字面量。
// 这是"如何关掉一个有默认值的头"的唯一机制,若 "-" 不被识别就会被当成字面头值发出去。
func TestPickHeaderValue_TriState(t *testing.T) {
	tests := []struct {
		name string
		val  string
		def  string
		want string
	}{
		{name: "empty takes the default", val: "", def: "DENY", want: "DENY"},
		{name: "empty with empty default stays empty", val: "", def: "", want: ""},
		{name: "dash disables even with a default", val: "-", def: "DENY", want: ""},
		{name: "dash disables with no default", val: "-", def: "", want: ""},
		{name: "literal value wins over the default", val: "SAMEORIGIN", def: "DENY", want: "SAMEORIGIN"},
		// 只有恰好等于 "-" 才是禁用标记;含 "-" 的合法头值必须原样保留。
		{name: "value containing a dash is literal", val: "no-referrer", def: "DENY", want: "no-referrer"},
		{name: "double dash is literal", val: "--", def: "DENY", want: "--"},
		{name: "dash with space is literal", val: "- ", def: "DENY", want: "- "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickHeaderValue(tc.val, tc.def); got != tc.want {
				t.Errorf("pickHeaderValue(%q, %q)=%q, want %q", tc.val, tc.def, got, tc.want)
			}
		})
	}
}

// TestSecureHeaders_DashOmitsHeaderEndToEnd 验证 "-" 在真实响应里确实让头消失,
// 而不是发出一个字面值为 "-" 的头。
func TestSecureHeaders_DashOmitsHeaderEndToEnd(t *testing.T) {
	h := secServe(t, SecureHeaders(SecureHeadersConfig{
		ContentTypeOptions: "-",
		FrameOptions:       "-",
		ReferrerPolicy:     "-",
	}), nil)

	for _, name := range []string{HeaderContentTypeOptions, HeaderFrameOptions, HeaderReferrerPolicy} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s=%q, want absent (\"-\" must omit the header, not emit a literal dash)", name, got)
		}
	}
	// 头必须整体不存在,而非存在一个空值条目。
	for _, name := range []string{HeaderContentTypeOptions, HeaderFrameOptions, HeaderReferrerPolicy} {
		if _, ok := h[http.CanonicalHeaderKey(name)]; ok {
			t.Errorf("%s key present in the header map, want it fully absent", name)
		}
	}
}

// TestSecureHeaders_DashDisablesOnlyTargetHeader 验证禁用一个头不影响其它头。
func TestSecureHeaders_DashDisablesOnlyTargetHeader(t *testing.T) {
	h := secServe(t, SecureHeaders(SecureHeadersConfig{FrameOptions: "-"}), nil)

	if got := h.Get(HeaderFrameOptions); got != "" {
		t.Errorf("%s=%q, want absent", HeaderFrameOptions, got)
	}
	if got := h.Get(HeaderContentTypeOptions); got != "nosniff" {
		t.Errorf("%s=%q, want nosniff (disabling one header must not affect others)", HeaderContentTypeOptions, got)
	}
	if got := h.Get(HeaderReferrerPolicy); got != "strict-origin-when-cross-origin" {
		t.Errorf("%s=%q, want the default", HeaderReferrerPolicy, got)
	}
}

// --- 仅在显式配置时发送的头 --------------------------------------------------

// TestSecureHeaders_OptInHeaders 验证 CSP / Permissions-Policy / Cross-Origin-* 只在
// 显式配置时发送:它们与具体前端资源和跨源加载强相关,默认发送会破坏功能。
func TestSecureHeaders_OptInHeaders(t *testing.T) {
	tests := []struct {
		name   string
		cfg    SecureHeadersConfig
		header string
		want   string
	}{
		{
			name:   "csp",
			cfg:    SecureHeadersConfig{ContentSecurityPolicy: "default-src 'self'"},
			header: HeaderContentSecurity,
			want:   "default-src 'self'",
		},
		{
			name:   "permissions policy",
			cfg:    SecureHeadersConfig{PermissionsPolicy: "geolocation=(), camera=()"},
			header: HeaderPermissionsPolicy,
			want:   "geolocation=(), camera=()",
		},
		{
			name:   "cross origin opener",
			cfg:    SecureHeadersConfig{CrossOriginOpenerPolicy: "same-origin"},
			header: HeaderCrossOriginOpener,
			want:   "same-origin",
		},
		{
			name:   "cross origin resource",
			cfg:    SecureHeadersConfig{CrossOriginResourcePolicy: "same-site"},
			header: HeaderCrossOriginResource,
			want:   "same-site",
		},
		{
			name:   "cross origin embedder",
			cfg:    SecureHeadersConfig{CrossOriginEmbedderPolicy: "require-corp"},
			header: HeaderCrossOriginEmbedder,
			want:   "require-corp",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 配置后发送。
			h := secServe(t, SecureHeaders(tc.cfg), nil)
			if got := h.Get(tc.header); got != tc.want {
				t.Errorf("%s=%q, want %q when configured", tc.header, got, tc.want)
			}
			// 未配置则缺席:确认该头没有隐式默认值。
			bare := secServe(t, SecureHeaders(SecureHeadersConfig{}), nil)
			if got := bare.Get(tc.header); got != "" {
				t.Errorf("%s=%q, want absent when unconfigured", tc.header, got)
			}
		})
	}
}

// TestSecureHeaders_AllOptInTogether 验证一次配置全部可选头时都被发送(注册期固化的
// 待写头列表容量为 8,此用例确保满载也不丢头)。
func TestSecureHeaders_AllOptInTogether(t *testing.T) {
	h := secServe(t, SecureHeaders(SecureHeadersConfig{
		ContentSecurityPolicy:     "default-src 'none'",
		PermissionsPolicy:         "camera=()",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-origin",
		CrossOriginEmbedderPolicy: "require-corp",
		HSTSMaxAge:                31536000,
	}), secTLS)

	want := map[string]string{
		HeaderContentTypeOptions:  "nosniff",
		HeaderFrameOptions:        "DENY",
		HeaderReferrerPolicy:      "strict-origin-when-cross-origin",
		HeaderContentSecurity:     "default-src 'none'",
		HeaderPermissionsPolicy:   "camera=()",
		HeaderCrossOriginOpener:   "same-origin",
		HeaderCrossOriginResource: "same-origin",
		HeaderCrossOriginEmbedder: "require-corp",
		HeaderStrictTransport:     "max-age=31536000",
	}
	for name, val := range want {
		if got := h.Get(name); got != val {
			t.Errorf("%s=%q, want %q", name, got, val)
		}
	}
}

// --- HSTS 值组装 -------------------------------------------------------------

// TestSecureHeaders_HSTSComposition 验证 HSTS 值的精确拼接顺序与可选指令。
// 全部用例走 TLS 请求,把"是否发送"与"发送什么"两个关注点分开测。
func TestSecureHeaders_HSTSComposition(t *testing.T) {
	tests := []struct {
		name string
		cfg  SecureHeadersConfig
		want string // 空串表示该头不应出现 / empty means the header must be absent
	}{
		{
			// <=0 不发送:未全站 HTTPS 时发送 HSTS 会让浏览器拒绝 HTTP 访问。
			name: "zero max age omits hsts",
			cfg:  SecureHeadersConfig{HSTSMaxAge: 0},
			want: "",
		},
		{
			name: "negative max age omits hsts",
			cfg:  SecureHeadersConfig{HSTSMaxAge: -1},
			want: "",
		},
		{
			// 附加指令仅在 HSTSMaxAge>0 时生效,单独开启不得凭空造出 HSTS 头。
			name: "subdomains without max age omits hsts",
			cfg:  SecureHeadersConfig{HSTSIncludeSubdomains: true, HSTSPreload: true},
			want: "",
		},
		{
			name: "max age only",
			cfg:  SecureHeadersConfig{HSTSMaxAge: 31536000},
			want: "max-age=31536000",
		},
		{
			name: "max age one second",
			cfg:  SecureHeadersConfig{HSTSMaxAge: 1},
			want: "max-age=1",
		},
		{
			name: "with include subdomains",
			cfg:  SecureHeadersConfig{HSTSMaxAge: 600, HSTSIncludeSubdomains: true},
			want: "max-age=600; includeSubDomains",
		},
		{
			// preload 单独开启也要拼上(顺序固定在 includeSubDomains 之后)。
			name: "with preload only",
			cfg:  SecureHeadersConfig{HSTSMaxAge: 600, HSTSPreload: true},
			want: "max-age=600; preload",
		},
		{
			name: "with subdomains and preload",
			cfg:  SecureHeadersConfig{HSTSMaxAge: 63072000, HSTSIncludeSubdomains: true, HSTSPreload: true},
			want: "max-age=63072000; includeSubDomains; preload",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := secServe(t, SecureHeaders(tc.cfg), secTLS)
			if got := h.Get(HeaderStrictTransport); got != tc.want {
				t.Errorf("%s=%q, want %q", HeaderStrictTransport, got, tc.want)
			}
		})
	}
}

// --- HSTS 的 TLS 门槛 --------------------------------------------------------

// TestSecureHeaders_HSTSOnlyOverTLSByDefault 验证默认只对 TLS 请求发送 HSTS:
// 明文请求若收到 HSTS,浏览器会记住"此域只用 HTTPS",在未全站 HTTPS 时锁死站点。
func TestSecureHeaders_HSTSOnlyOverTLSByDefault(t *testing.T) {
	cfg := SecureHeadersConfig{HSTSMaxAge: 31536000}

	plain := secServe(t, SecureHeaders(cfg), nil)
	if got := plain.Get(HeaderStrictTransport); got != "" {
		t.Errorf("plain request %s=%q, want absent (HSTS over plaintext can lock out the site)", HeaderStrictTransport, got)
	}
	// 明文请求的其它安全头仍须照常发送:TLS 门槛只约束 HSTS。
	if got := plain.Get(HeaderContentTypeOptions); got != "nosniff" {
		t.Errorf("plain request %s=%q, want nosniff (the TLS gate applies to HSTS only)", HeaderContentTypeOptions, got)
	}

	overTLS := secServe(t, SecureHeaders(cfg), secTLS)
	if got := overTLS.Get(HeaderStrictTransport); got != "max-age=31536000" {
		t.Errorf("TLS request %s=%q, want max-age=31536000", HeaderStrictTransport, got)
	}
}

// TestSecureHeaders_HSTSOnlyWhenTLSFalseForces 验证显式 HSTSOnlyWhenTLS=false 时,
// 明文请求也发送 HSTS——用于 TLS 卸载在上游代理、后端只见明文的部署。
func TestSecureHeaders_HSTSOnlyWhenTLSFalseForces(t *testing.T) {
	h := secServe(t, SecureHeaders(SecureHeadersConfig{
		HSTSMaxAge:      600,
		HSTSOnlyWhenTLS: secBoolPtr(false),
	}), nil)

	if got := h.Get(HeaderStrictTransport); got != "max-age=600" {
		t.Errorf("%s=%q, want max-age=600 (HSTSOnlyWhenTLS=false forces it on plaintext)", HeaderStrictTransport, got)
	}
}

// TestSecureHeaders_HSTSOnlyWhenTLSExplicitTrue 验证显式 true 与 nil(默认)行为一致:
// 三态字段的 nil 必须等价于 true,而不是等价于零值 false。
func TestSecureHeaders_HSTSOnlyWhenTLSExplicitTrue(t *testing.T) {
	explicit := SecureHeadersConfig{HSTSMaxAge: 600, HSTSOnlyWhenTLS: secBoolPtr(true)}
	implicit := SecureHeadersConfig{HSTSMaxAge: 600}

	for _, tc := range []struct {
		name string
		cfg  SecureHeadersConfig
	}{
		{name: "explicit true", cfg: explicit},
		{name: "nil defaults to true", cfg: implicit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := secServe(t, SecureHeaders(tc.cfg), nil).Get(HeaderStrictTransport); got != "" {
				t.Errorf("plain %s=%q, want absent", HeaderStrictTransport, got)
			}
			if got := secServe(t, SecureHeaders(tc.cfg), secTLS).Get(HeaderStrictTransport); got != "max-age=600" {
				t.Errorf("TLS %s=%q, want max-age=600", HeaderStrictTransport, got)
			}
		})
	}
}

// --- X-Forwarded-Proto 与可信代理(安全关键)---------------------------------

// TestSecureHeaders_ForwardedProtoOnlyBehindTrustedProxy 是本中间件最安全关键的行为:
// X-Forwarded-Proto 可被任意客户端伪造,因此只有直连对端落在可信代理网段内才采信。
// 若不做这层判定,任何人加一个头就能诱导服务器对明文连接下发 HSTS。
func TestSecureHeaders_ForwardedProtoOnlyBehindTrustedProxy(t *testing.T) {
	cfg := SecureHeadersConfig{HSTSMaxAge: 31536000}
	const trusted = "192.0.2.0/24"

	tests := []struct {
		name     string
		remote   string
		proto    string
		wantHSTS bool
		why      string
	}{
		{
			name:     "trusted proxy with https proto",
			remote:   "192.0.2.10:443",
			proto:    "https",
			wantHSTS: true,
			why:      "the direct peer is a trusted proxy, so its forwarded proto is authoritative",
		},
		{
			name:     "untrusted peer with https proto",
			remote:   "203.0.113.10:443",
			proto:    "https",
			wantHSTS: false,
			why:      "a spoofable header from an untrusted peer must never be honored",
		},
		{
			// 边界:网段内的首尾地址都应可信。
			name:     "trusted network boundary low",
			remote:   "192.0.2.0:443",
			proto:    "https",
			wantHSTS: true,
			why:      "the network's first address is inside the prefix",
		},
		{
			name:     "trusted network boundary high",
			remote:   "192.0.2.255:443",
			proto:    "https",
			wantHSTS: true,
			why:      "the network's last address is inside the prefix",
		},
		{
			// 紧邻网段之外一个地址即不可信,验证前缀判定不放宽。
			name:     "just outside the trusted network",
			remote:   "192.0.3.1:443",
			proto:    "https",
			wantHSTS: false,
			why:      "one address outside the prefix is already untrusted",
		},
		{
			name:     "trusted proxy reporting http",
			remote:   "192.0.2.10:80",
			proto:    "http",
			wantHSTS: false,
			why:      "a trusted proxy explicitly reporting plaintext must not yield HSTS",
		},
		{
			// 大小写不敏感:HTTP 头值比较应用 EqualFold。
			name:     "trusted proxy with uppercase HTTPS",
			remote:   "192.0.2.10:443",
			proto:    "HTTPS",
			wantHSTS: true,
			why:      "the scheme comparison is case-insensitive",
		},
		{
			name:     "trusted proxy with mixed case Https",
			remote:   "192.0.2.10:443",
			proto:    "Https",
			wantHSTS: true,
			why:      "mixed case is still https",
		},
		{
			name:     "trusted proxy with no proto header",
			remote:   "192.0.2.10:443",
			proto:    "",
			wantHSTS: false,
			why:      "absent forwarded proto means nothing is known about TLS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := secServe(t, SecureHeaders(cfg), func(r *http.Request) {
				r.RemoteAddr = tc.remote
				if tc.proto != "" {
					r.Header.Set("X-Forwarded-Proto", tc.proto)
				}
			}, WithTrustedProxies(trusted))

			got := h.Get(HeaderStrictTransport) != ""
			if got != tc.wantHSTS {
				t.Errorf("HSTS present=%v, want %v: %s", got, tc.wantHSTS, tc.why)
			}
		})
	}
}

// TestSecureHeaders_ForwardedProtoIgnoredWithoutTrustedProxies 验证未配置可信代理时
// (默认)完全不采信 X-Forwarded-Proto:默认配置下伪造头必须无效。
func TestSecureHeaders_ForwardedProtoIgnoredWithoutTrustedProxies(t *testing.T) {
	h := secServe(t, SecureHeaders(SecureHeadersConfig{HSTSMaxAge: 600}), func(r *http.Request) {
		r.RemoteAddr = "192.0.2.10:443"
		r.Header.Set("X-Forwarded-Proto", "https")
	})

	if got := h.Get(HeaderStrictTransport); got != "" {
		t.Errorf("%s=%q, want absent (no trusted proxy configured means no forwarded header is trusted)", HeaderStrictTransport, got)
	}
}

// TestSecureHeaders_RealTLSBeatsUntrustedForwardedHeader 验证真实 TLS 连接不受可信代理
// 配置影响:r.TLS 非 nil 是第一手证据,无需任何转发头。
func TestSecureHeaders_RealTLSBeatsUntrustedForwardedHeader(t *testing.T) {
	h := secServe(t, SecureHeaders(SecureHeadersConfig{HSTSMaxAge: 600}), func(r *http.Request) {
		secTLS(r)
		r.RemoteAddr = "203.0.113.10:443" // 不可信对端 / untrusted peer
		r.Header.Set("X-Forwarded-Proto", "http")
	})

	if got := h.Get(HeaderStrictTransport); got != "max-age=600" {
		t.Errorf("%s=%q, want max-age=600 (a real TLS connection is first-hand evidence)", HeaderStrictTransport, got)
	}
}

// --- isTLSRequest 直测 -------------------------------------------------------

// TestIsTLSRequest_DirectAndForwarded 直测 isTLSRequest 的判定分支。owner 为 nil 时
// (miss 冷路径构造的临时 Request)不得访问可信代理配置而 panic。
func TestIsTLSRequest_DirectAndForwarded(t *testing.T) {
	// 一个配置了可信代理的 Server,借其 mux 作 owner。
	trustedSrv := New(WithTrustedProxies("192.0.2.0/24"))

	tests := []struct {
		name  string
		owner *mux
		tlsOn bool
		remot string
		proto string
		want  bool
	}{
		{name: "nil owner plain", owner: nil, want: false},
		{
			// owner 为 nil 时也必须认出真实 TLS。
			name: "nil owner with real tls", owner: nil, tlsOn: true, want: true,
		},
		{
			// owner 为 nil 时转发头不可采信(无从判断对端是否可信)。
			name: "nil owner ignores forwarded proto", owner: nil, remot: "192.0.2.10:1", proto: "https", want: false,
		},
		{name: "trusted owner plain", owner: &trustedSrv.mux, remot: "192.0.2.10:1", want: false},
		{name: "trusted owner real tls", owner: &trustedSrv.mux, tlsOn: true, remot: "203.0.113.1:1", want: true},
		{name: "trusted peer forwarded https", owner: &trustedSrv.mux, remot: "192.0.2.10:1", proto: "https", want: true},
		{name: "untrusted peer forwarded https", owner: &trustedSrv.mux, remot: "203.0.113.1:1", proto: "https", want: false},
		{name: "trusted peer forwarded http", owner: &trustedSrv.mux, remot: "192.0.2.10:1", proto: "http", want: false},
		{
			// 非法 RemoteAddr 不得被当成可信对端。
			name: "trusted owner malformed remote", owner: &trustedSrv.mux, remot: "not-an-address", proto: "https", want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.tlsOn {
				secTLS(r)
			} else {
				r.TLS = nil
			}
			if tc.remot != "" {
				r.RemoteAddr = tc.remot
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			req := &Request{Request: r, owner: tc.owner}
			if got := isTLSRequest(req); got != tc.want {
				t.Errorf("isTLSRequest()=%v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsTLSRequest_NoTrustedProxiesConfigured 验证 owner 存在但未配置可信代理时,
// 转发头依旧不被采信(fromTrustedProxy 对空网段列表返回 false)。
func TestIsTLSRequest_NoTrustedProxiesConfigured(t *testing.T) {
	s := New()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.TLS = nil
	r.RemoteAddr = "192.0.2.10:1"
	r.Header.Set("X-Forwarded-Proto", "https")

	req := &Request{Request: r, owner: &s.mux}
	if isTLSRequest(req) {
		t.Error("isTLSRequest()=true with no trusted proxies, want false (forwarded proto is spoofable)")
	}
	// 同一请求在直连 TLS 下必须为 true,证明上面的 false 来自信任判定而非解析失败。
	secTLS(r)
	if !isTLSRequest(req) {
		t.Error("isTLSRequest()=false for a real TLS connection, want true")
	}
}

// --- 不覆盖既有头 -----------------------------------------------------------

// TestSecureHeaders_DoesNotOverwriteExistingHeader 验证中间件不覆盖已显式设置的头:
// 更具体的设置优先。由于本中间件在调用 next【之前】写头,"已存在"只能由更外层中间件
// 造成,故把探针中间件排在 SecureHeaders 之前。
func TestSecureHeaders_DoesNotOverwriteExistingHeader(t *testing.T) {
	// 外层中间件先落笔,内层 SecureHeaders 随后运行。
	presetOuter := func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			resp.Header().Set(HeaderFrameOptions, "SAMEORIGIN")
			resp.Header().Set(HeaderReferrerPolicy, "unsafe-url")
			resp.Header().Set(HeaderStrictTransport, "max-age=1")
			return next(ctx, req, resp)
		}
	}

	s := New()
	s.Use(presetOuter, SecureHeaders(SecureHeadersConfig{HSTSMaxAge: 31536000}))
	if err := s.RawHandle(http.MethodGet, "/x", secOKHandler); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	secTLS(r) // 让 HSTS 本可发送,从而真正检验"不覆盖"而非"未触发"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	want := map[string]string{
		HeaderFrameOptions:    "SAMEORIGIN",
		HeaderReferrerPolicy:  "unsafe-url",
		HeaderStrictTransport: "max-age=1",
	}
	for name, val := range want {
		if got := rec.Header().Get(name); got != val {
			t.Errorf("%s=%q, want the pre-existing %q (the middleware must not overwrite)", name, got, val)
		}
	}
	// 未被预设的头仍由中间件补上:不覆盖 ≠ 全盘放弃。
	if got := rec.Header().Get(HeaderContentTypeOptions); got != "nosniff" {
		t.Errorf("%s=%q, want nosniff (unset headers are still filled in)", HeaderContentTypeOptions, got)
	}
}

// TestSecureHeaders_DownstreamHandlerCannotBePreempted 验证下游 handler 显式改写同名头
// 时以下游为准:中间件先写,handler 后写,最终以 handler 的值出站。
func TestSecureHeaders_DownstreamHandlerCannotBePreempted(t *testing.T) {
	s := New()
	s.Use(SecureHeadersDefault())
	if err := s.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		// 下游明确需要同源嵌入,覆盖中间件写下的 DENY。
		resp.Header().Set(HeaderFrameOptions, "SAMEORIGIN")
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := rec.Header().Get(HeaderFrameOptions); got != "SAMEORIGIN" {
		t.Errorf("%s=%q, want SAMEORIGIN (downstream wins)", HeaderFrameOptions, got)
	}
}

// --- 其它行为约束 -----------------------------------------------------------

// TestSecureHeaders_HeadersWrittenBeforeNext 验证头在调用 next 之前写入:下游一旦提交
// 响应就无法再补头,因此"先写头"是这些头能到达客户端的前提。
func TestSecureHeaders_HeadersWrittenBeforeNext(t *testing.T) {
	s := New()
	s.Use(SecureHeadersDefault())
	var seenInHandler string
	if err := s.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		// 立即写 body(隐式提交 200),模拟不给中间件留后处理机会的下游。
		seenInHandler = resp.Header().Get(HeaderContentTypeOptions)
		_, _ = resp.WriteString("done")
		return nil
	}); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if seenInHandler != "nosniff" {
		t.Errorf("handler saw %s=%q, want nosniff (headers must be set before next)", HeaderContentTypeOptions, seenInHandler)
	}
	if got := rec.Header().Get(HeaderContentTypeOptions); got != "nosniff" {
		t.Errorf("response %s=%q, want nosniff even though the handler committed immediately", HeaderContentTypeOptions, got)
	}
}

// TestSecureHeaders_AppliedOnMissAndError 验证安全头对未命中路由与错误响应同样生效:
// 全局中间件在 ServeHTTP 期施加,404/500 的响应体同样需要 nosniff 等防护。
func TestSecureHeaders_AppliedOnMissAndError(t *testing.T) {
	s := New()
	s.Use(SecureHeadersDefault())
	if err := s.RawHandle(http.MethodGet, "/exists", secOKHandler); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "not found", method: http.MethodGet, path: "/nope", wantStatus: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodDelete, path: "/exists", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get(HeaderContentTypeOptions); got != "nosniff" {
				t.Errorf("%s=%q, want nosniff (miss responses need protection too)", HeaderContentTypeOptions, got)
			}
			if got := rec.Header().Get(HeaderFrameOptions); got != "DENY" {
				t.Errorf("%s=%q, want DENY", HeaderFrameOptions, got)
			}
		})
	}
}

// TestSecureHeaders_SingleValuePerHeader 验证每个头只写一个值:重复 Add 会让浏览器收到
// 多值头,部分安全头在多值下行为未定义。
func TestSecureHeaders_SingleValuePerHeader(t *testing.T) {
	h := secServe(t, SecureHeaders(SecureHeadersConfig{HSTSMaxAge: 600}), secTLS)

	for _, name := range []string{HeaderContentTypeOptions, HeaderFrameOptions, HeaderReferrerPolicy, HeaderStrictTransport} {
		if n := len(h.Values(name)); n != 1 {
			t.Errorf("%s has %d values, want exactly 1", name, n)
		}
	}
}

// TestSecureHeaders_ReusableAcrossRequests 验证同一中间件实例可跨请求复用且结果稳定:
// 待写头列表在注册期固化,请求期只赋值,不应被前一请求污染。
func TestSecureHeaders_ReusableAcrossRequests(t *testing.T) {
	s := New(WithTrustedProxies("192.0.2.0/24"))
	s.Use(SecureHeaders(SecureHeadersConfig{HSTSMaxAge: 600}))
	if err := s.RawHandle(http.MethodGet, "/x", secOKHandler); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}

	serve := func(mutate func(*http.Request)) http.Header {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		if mutate != nil {
			mutate(r)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec.Header()
	}

	// 先发一个可信代理 + https 的请求(会写 HSTS)。
	if got := serve(func(r *http.Request) {
		r.RemoteAddr = "192.0.2.10:443"
		r.Header.Set("X-Forwarded-Proto", "https")
	}).Get(HeaderStrictTransport); got != "max-age=600" {
		t.Fatalf("trusted request %s=%q, want max-age=600", HeaderStrictTransport, got)
	}

	// 紧随其后的明文请求不得因上一次而拿到 HSTS(每请求独立判定)。
	if got := serve(func(r *http.Request) {
		r.RemoteAddr = "203.0.113.10:80"
	}).Get(HeaderStrictTransport); got != "" {
		t.Errorf("subsequent plain request %s=%q, want absent (per-request decision must not leak)", HeaderStrictTransport, got)
	}

	// 固定头在多次请求间保持稳定。
	for i := range 3 {
		if got := serve(nil).Get(HeaderContentTypeOptions); got != "nosniff" {
			t.Errorf("request %d: %s=%q, want nosniff", i, HeaderContentTypeOptions, got)
		}
	}
}
