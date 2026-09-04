package ghttp

import (
	"context"
	"strconv"
	"strings"
)

// ===========================================================================
// 安全响应头中间件。
//
// 它只写响应头,不读请求体、不分配缓冲,是纯前处理层:未挂载时零开销,挂载后每请求
// 若干次 header 赋值。
//
// 默认值取"绝大多数 API 服务都应开启且不会破坏功能"的保守集合:nosniff、DENY 框架
// 嵌入、strict-origin-when-cross-origin。HSTS 与 CSP 默认【关闭】——前者在未全站 HTTPS 时
// 会锁死站点,后者与具体前端资源强相关,误配即白屏,必须由使用者显式声明。
//
// Security response headers middleware.
//
// It only writes response headers — no body reads, no buffer allocation — a pure
// pre-processing layer: zero overhead unmounted, a few header assignments per
// request once mounted.
//
// Defaults are the conservative set that "virtually every API service should enable
// and that breaks nothing": nosniff, DENY framing, strict-origin-when-cross-origin. HSTS
// and CSP are OFF by default — the former locks out a site not yet fully on HTTPS,
// and the latter is tightly coupled to actual front-end assets where a misconfiguration
// blanks the page, so both must be declared explicitly.
// ===========================================================================

// 安全头名常量,集中声明避免拼写漂移。
// Security header name constants, declared centrally to avoid spelling drift.
const (
	HeaderContentTypeOptions   = "X-Content-Type-Options"
	HeaderFrameOptions         = "X-Frame-Options"
	HeaderReferrerPolicy       = "Referrer-Policy"
	HeaderStrictTransport      = "Strict-Transport-Security"
	HeaderContentSecurity      = "Content-Security-Policy"
	HeaderCrossOriginOpener    = "Cross-Origin-Opener-Policy"
	HeaderCrossOriginResource  = "Cross-Origin-Resource-Policy"
	HeaderCrossOriginEmbedder  = "Cross-Origin-Embedder-Policy"
	HeaderPermissionsPolicy    = "Permissions-Policy"
	HeaderXPermittedCrossParam = "X-Permitted-Cross-Domain-Policies"
)

// SecureHeadersConfig 配置安全响应头。零值即安全默认(见 SecureHeaders 说明);
// 每个字段留空表示"用默认值",显式写 "-" 表示"不发送该头"。
// SecureHeadersConfig configures security response headers. The zero value is the
// safe default (see SecureHeaders); an empty field means "use the default" and an
// explicit "-" means "do not send this header".
type SecureHeadersConfig struct {
	// ContentTypeOptions 默认 "nosniff"(禁止浏览器 MIME 嗅探)。
	// ContentTypeOptions defaults to "nosniff" (disabling browser MIME sniffing).
	ContentTypeOptions string

	// FrameOptions 默认 "DENY"(禁止被 iframe 嵌入,防点击劫持)。需要同源嵌入时用
	// "SAMEORIGIN"。
	// FrameOptions defaults to "DENY" (no iframe embedding, anti-clickjacking). Use
	// "SAMEORIGIN" when same-origin framing is needed.
	FrameOptions string

	// ReferrerPolicy 默认 "strict-origin-when-cross-origin"(现代浏览器的默认值)。
	// 不用旧的 "no-referrer-when-downgrade":后者在同为 HTTPS 时会把【完整 URL】(含路径
	// 与查询串)发给第三方站点,足以泄漏 token、订单号一类的路径参数;前者跨源时只发源。
	// ReferrerPolicy defaults to "strict-origin-when-cross-origin" (the modern browser
	// default). The older "no-referrer-when-downgrade" is not used: it sends the FULL
	// URL (path and query included) to third-party sites as long as both are HTTPS,
	// enough to leak path parameters such as tokens or order numbers, whereas the former
	// sends only the origin cross-origin.
	ReferrerPolicy string

	// ContentSecurityPolicy 默认为空(不发送)。它与具体前端资源强相关,须由使用者
	// 按站点实际情况声明。
	// ContentSecurityPolicy defaults to empty (not sent). It is tightly coupled to
	// actual front-end assets and must be declared per site.
	ContentSecurityPolicy string

	// PermissionsPolicy 默认为空(不发送),如 "geolocation=(), camera=()"。
	// PermissionsPolicy defaults to empty (not sent), e.g. "geolocation=(), camera=()".
	PermissionsPolicy string

	// CrossOriginOpenerPolicy / CrossOriginResourcePolicy / CrossOriginEmbedderPolicy
	// 默认为空(不发送)。它们会影响跨源窗口与资源加载,须按站点显式选择。
	// These default to empty (not sent). They affect cross-origin windows and
	// resource loading and must be chosen explicitly per site.
	CrossOriginOpenerPolicy   string
	CrossOriginResourcePolicy string
	CrossOriginEmbedderPolicy string

	// HSTSMaxAge 是 Strict-Transport-Security 的 max-age 秒数。<=0(默认)时【不发送】
	// HSTS——在未全站 HTTPS 的环境下发送它会让浏览器拒绝 HTTP 访问,造成站点不可用。
	// HSTSMaxAge is Strict-Transport-Security's max-age in seconds. <=0 (the default)
	// does NOT send HSTS: sending it before a site is fully on HTTPS makes browsers
	// refuse HTTP and takes the site down.
	HSTSMaxAge int

	// HSTSIncludeSubdomains / HSTSPreload 是 HSTS 的附加指令,仅在 HSTSMaxAge>0 时生效。
	// 注意 preload 一经提交浏览器列表极难撤回,开启前请确认所有子域都已支持 HTTPS。
	// HSTSIncludeSubdomains / HSTSPreload are HSTS add-ons, effective only when
	// HSTSMaxAge>0. Note that preload is very hard to undo once submitted to browser
	// lists; confirm every subdomain serves HTTPS first.
	HSTSIncludeSubdomains bool
	HSTSPreload           bool

	// HSTSOnlyWhenTLS 为 true(默认行为)时,仅对经 TLS 到达的请求发送 HSTS。
	// 置 false 可在 TLS 卸载于上游代理的部署里强制发送。
	// HSTSOnlyWhenTLS, true by default in effect, sends HSTS only for requests that
	// arrived over TLS. Set false to force it where TLS terminates at an upstream proxy.
	HSTSOnlyWhenTLS *bool
}

// secureHeader 是一个待写出的固定头(注册期算好,请求期只赋值)。
// secureHeader is one fixed header to write (computed at registration, only
// assigned at request time).
type secureHeader struct {
	name  string
	value string
}

// SecureHeaders 返回安全响应头中间件。
//
// 默认写出:X-Content-Type-Options: nosniff、X-Frame-Options: DENY、
// Referrer-Policy: strict-origin-when-cross-origin。HSTS 与 CSP 默认不发送,须显式配置。
//
// 头在【调用 next 之前】写入,因此即使下游直接写 body 也不会丢失;下游若显式覆盖同名头,
// 以下游为准(本中间件不强制覆盖已存在的值)。
//
// SecureHeaders returns the security response headers middleware.
//
// It writes by default: X-Content-Type-Options: nosniff, X-Frame-Options: DENY, and
// Referrer-Policy: strict-origin-when-cross-origin. HSTS and CSP are not sent unless
// configured explicitly.
//
// Headers are written BEFORE calling next, so they survive a downstream that writes
// the body immediately; if downstream sets the same header explicitly, downstream
// wins (this middleware never overwrites an existing value).
func SecureHeaders(cfg SecureHeadersConfig) Middleware {
	// 注册期把配置固化为一个待写头列表:请求期不再读配置、不再判断默认值。
	// Freeze the config into a header list at registration: request time reads no
	// config and re-evaluates no defaults.
	fixed := make([]secureHeader, 0, 8)
	add := func(name, val, def string) {
		v := pickHeaderValue(val, def)
		if v != "" {
			fixed = append(fixed, secureHeader{name: name, value: v})
		}
	}
	add(HeaderContentTypeOptions, cfg.ContentTypeOptions, "nosniff")
	add(HeaderFrameOptions, cfg.FrameOptions, "DENY")
	add(HeaderReferrerPolicy, cfg.ReferrerPolicy, "strict-origin-when-cross-origin")
	add(HeaderContentSecurity, cfg.ContentSecurityPolicy, "")
	add(HeaderPermissionsPolicy, cfg.PermissionsPolicy, "")
	add(HeaderCrossOriginOpener, cfg.CrossOriginOpenerPolicy, "")
	add(HeaderCrossOriginResource, cfg.CrossOriginResourcePolicy, "")
	add(HeaderCrossOriginEmbedder, cfg.CrossOriginEmbedderPolicy, "")

	// HSTS 值也在注册期拼好(它只依赖配置,不依赖请求)。
	// The HSTS value is also assembled at registration (it depends on config only).
	var hsts string
	if cfg.HSTSMaxAge > 0 {
		var b strings.Builder
		b.WriteString("max-age=")
		b.WriteString(strconv.Itoa(cfg.HSTSMaxAge))
		if cfg.HSTSIncludeSubdomains {
			b.WriteString("; includeSubDomains")
		}
		if cfg.HSTSPreload {
			b.WriteString("; preload")
		}
		hsts = b.String()
	}
	hstsOnlyTLS := true
	if cfg.HSTSOnlyWhenTLS != nil {
		hstsOnlyTLS = *cfg.HSTSOnlyWhenTLS
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			h := resp.Header()
			for i := range fixed {
				// 不覆盖下游/上游已显式设置的值:让更具体的设置优先。
				// Never overwrite a value already set explicitly: the more specific setting wins.
				if h.Get(fixed[i].name) == "" {
					h.Set(fixed[i].name, fixed[i].value)
				}
			}
			if hsts != "" && (!hstsOnlyTLS || isTLSRequest(req)) && h.Get(HeaderStrictTransport) == "" {
				h.Set(HeaderStrictTransport, hsts)
			}
			return next(ctx, req, resp)
		}
	}
}

// SecureHeadersDefault 返回默认配置的安全头中间件,等价于 SecureHeaders(SecureHeadersConfig{})。
// SecureHeadersDefault returns the security headers middleware with defaults,
// equivalent to SecureHeaders(SecureHeadersConfig{}).
func SecureHeadersDefault() Middleware { return SecureHeaders(SecureHeadersConfig{}) }

// pickHeaderValue 解析"留空取默认、'-' 表示禁用"的三态配置。
// pickHeaderValue resolves the tri-state config of "empty takes the default, '-'
// disables".
func pickHeaderValue(val, def string) string {
	switch val {
	case "":
		return def
	case "-":
		return ""
	}
	return val
}

// isTLSRequest 报告请求是否经 TLS 到达:直连看 r.TLS,经可信代理时看
// X-Forwarded-Proto。仅在可信代理内采信转发头,复用与 ClientIP 相同的信任策略。
// isTLSRequest reports whether the request arrived over TLS: r.TLS for a direct
// connection, X-Forwarded-Proto behind a trusted proxy. The forwarded header is
// honored only within a trusted proxy, reusing the same trust policy as ClientIP.
func isTLSRequest(req *Request) bool {
	if req.TLS != nil {
		return true
	}
	if req.owner != nil && req.fromTrustedProxy() {
		if proto := req.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
			return true
		}
	}
	return false
}
