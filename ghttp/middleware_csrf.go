package ghttp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ===========================================================================
// CSRF 防护:双提交 cookie(double-submit cookie)模式。
//
// 选型理由:双提交模式无需服务端会话存储,与本包"无状态、不内置会话"的取向一致;
// 同源(Origin/Referer)校验作为第二道防线补上双提交在子域被攻破时的弱点。
//
// 语义:安全方法(GET/HEAD/OPTIONS/TRACE)只负责【发放】token,不校验;非安全方法必须
// 提交与 cookie 一致的 token(经请求头或表单字段),否则 403。
//
// CSRF protection: the double-submit cookie pattern.
//
// Why: double-submit needs no server-side session store, matching this package's
// stateless, session-free stance; a same-origin (Origin/Referer) check adds a second
// line of defense covering double-submit's weakness if a subdomain is compromised.
//
// Semantics: safe methods (GET/HEAD/OPTIONS/TRACE) only ISSUE a token and are never
// verified; unsafe methods must submit a token matching the cookie (via a request
// header or form field), else 403.
// ===========================================================================

// DefaultCSRFCookieName / DefaultCSRFHeaderName / DefaultCSRFFieldName 是双提交
// token 的默认承载位置。头名沿用生态惯例 X-CSRF-Token。
// DefaultCSRFCookieName / DefaultCSRFHeaderName / DefaultCSRFFieldName are the
// default carriers of the double-submit token. The header follows the ecosystem's
// X-CSRF-Token convention.
const (
	DefaultCSRFCookieName = "csrf_token"
	DefaultCSRFHeaderName = "X-CSRF-Token"
	DefaultCSRFFieldName  = "csrf_token"
)

// csrfTokenLen 是 token 的随机字节数(32 字节 = 256 位,足以抵抗猜测)。
// csrfTokenLen is the token's random byte count (32 bytes = 256 bits, ample against guessing).
const csrfTokenLen = 32

// csrfTokenKey 是 token 在 context 中的私有键类型。
// csrfTokenKey is the private context key type for the token.
type csrfTokenKey struct{}

// CSRFConfig 配置 CSRF 防护。
// CSRFConfig configures CSRF protection.
type CSRFConfig struct {
	// CookieName / HeaderName / FieldName 是 token 的承载位置;留空各取默认值。
	// CookieName / HeaderName / FieldName carry the token; empty values take the defaults.
	CookieName string
	HeaderName string
	FieldName  string

	// CookiePath / CookieDomain 是 token cookie 的作用域;Path 留空取 "/"。
	// CookiePath / CookieDomain scope the token cookie; an empty Path becomes "/".
	CookiePath   string
	CookieDomain string

	// CookieMaxAge 是 token cookie 的存活秒数;<=0 表示会话 cookie(关闭浏览器即失效)。
	// CookieMaxAge is the token cookie's lifetime in seconds; <=0 means a session
	// cookie (expiring when the browser closes).
	CookieMaxAge int

	// Secure 为 true 时 cookie 仅经 HTTPS 传输。生产环境应置 true。
	// Secure, when true, transmits the cookie over HTTPS only. Set it in production.
	Secure bool

	// SameSite 是 cookie 的 SameSite 策略;零值取 http.SameSiteLaxMode
	// (Lax 已能阻断绝大多数跨站表单提交,又不破坏正常的站外链接跳转)。
	// SameSite is the cookie's SameSite policy; the zero value becomes
	// http.SameSiteLaxMode (Lax blocks the vast majority of cross-site form posts
	// without breaking ordinary inbound links).
	SameSite http.SameSite

	// TrustedOrigins 是允许的跨站来源(如 "https://app.example.com")。非安全方法带
	// Origin/Referer 时,其来源必须与请求主机同源或落在此列表内。
	// TrustedOrigins lists permitted cross-site origins (e.g.
	// "https://app.example.com"). For unsafe methods carrying Origin/Referer, the
	// origin must be same-origin with the request host or present in this list.
	TrustedOrigins []string

	// TrustedOriginsOnly 收紧来源判定为【仅】TrustedOrigins 列表：不再以"Origin 的 host
	// 等于请求 Host"作为同源兜底。适用于按 Host 分发（通配虚拟主机、DNS 可被指向本站）
	// 或对来源有合规硬要求的服务。代价：本站自身来源也必须显式列进 TrustedOrigins，否则
	// 同源表单提交会被拒。
	// TrustedOriginsOnly tightens origin checking to the TrustedOrigins list alone,
	// dropping the "Origin host equals request Host" same-origin fallback. Use it where
	// routing is Host-based (wildcard vhosts, DNS that can point at this server) or where
	// the source list is a hard compliance requirement. The cost: your own origin must
	// also be listed, or same-origin form posts are rejected.
	TrustedOriginsOnly bool

	// Skip 返回 true 时跳过该请求的校验,用于豁免 API token 认证的端点(它们不受
	// CSRF 威胁,因为浏览器不会自动附带 Authorization 头)。
	// Skip exempts a request from verification when it returns true, for endpoints
	// authenticated by API token (immune to CSRF since browsers do not attach an
	// Authorization header automatically).
	Skip func(req *Request) bool

	// UseHostPrefixedCookie 为 true 时给 cookie 名加 "__Host-" 前缀,并强制
	// Secure=true、Path="/"、Domain=""(覆盖 Secure/CookiePath/CookieDomain 的设置)。
	//
	// 这三条不是本包的偏好,而是浏览器对 __Host- 前缀的【强制校验】:任一条不满足,
	// 浏览器直接丢弃整个 Set-Cookie,双提交模式会因"永远没有 cookie"而全量 403。
	// 之所以值得付这个代价:__Host- 前缀使 cookie 无法被任何子域写入或覆盖,正好补上
	// 双提交在"子域被攻破"时的弱点——攻击者拿下 sub.example.com 也无法向父域种一个
	// 自己已知的 CSRF token 再配对提交。
	//
	// 代价:cookie 不再随子域共享,故跨子域共用一套 CSRF token 的部署不能开启;
	// 且 __Host- 要求 HTTPS,本地明文调试需关闭。
	//
	// UseHostPrefixedCookie, when true, prefixes the cookie name with "__Host-" and
	// forces Secure=true, Path="/", and Domain="" (overriding Secure/CookiePath/
	// CookieDomain).
	//
	// These three are not this package's preference but the browser's MANDATORY
	// validation of the __Host- prefix: violate any one and the browser discards the
	// whole Set-Cookie, making double-submit fail closed with blanket 403s because the
	// cookie never exists. It is worth the cost because the __Host- prefix makes the
	// cookie unwritable and unshadowable by any subdomain, covering exactly
	// double-submit's weakness when a subdomain is compromised: an attacker owning
	// sub.example.com still cannot plant a CSRF token it knows on the parent domain
	// and then submit the matching pair.
	//
	// Cost: the cookie is no longer shared across subdomains, so deployments sharing
	// one CSRF token across subdomains must leave this off; __Host- also requires
	// HTTPS, so plaintext local debugging must disable it.
	UseHostPrefixedCookie bool
}

// CSRFHostCookiePrefix 是 __Host- cookie 名前缀。浏览器对带此前缀的 cookie 强制要求
// Secure、Path=/ 且无 Domain,换来"任何子域都无法写入或覆盖它"的保证。
// CSRFHostCookiePrefix is the __Host- cookie name prefix. Browsers require cookies
// carrying it to be Secure, Path=/, and Domain-less, in exchange for the guarantee
// that no subdomain can write or shadow them.
const CSRFHostCookiePrefix = "__Host-"

// CSRFTokenFromContext 返回本请求的 CSRF token,供模板把它渲染进表单隐藏字段或
// meta 标签。未挂载 CSRF 中间件时返回空串。
// CSRFTokenFromContext returns this request's CSRF token so templates can render it
// into a hidden form field or meta tag. It returns "" when the CSRF middleware is
// not mounted.
func CSRFTokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(csrfTokenKey{}).(string)
	return t
}

// CSRF 返回 CSRF 防护中间件(双提交 cookie + 同源校验)。
//
// 安全方法确保 cookie 中存在 token 并经 context 下传(供模板渲染);非安全方法要求
// 请求头或表单字段提交的 token 与 cookie 恒定时间一致,不一致即 403。
//
// 注意:它读取表单字段时会解析请求体。为不与 typed body 解码冲突,只在【请求头未提交
// token】时才回退查表单——因此推荐前端统一用请求头提交(SPA 场景的常规做法)。
//
// UseHostPrefixedCookie=true 时,CookieName 加 "__Host-" 前缀并强制 Secure/Path=//无
// Domain(浏览器对该前缀的硬性要求),使 cookie 不可被子域写入或覆盖。
//
// CSRF returns the CSRF protection middleware (double-submit cookie plus same-origin
// check).
//
// Safe methods ensure a token exists in the cookie and pass it down via context (for
// template rendering); unsafe methods require the token submitted in a header or form
// field to match the cookie in constant time, yielding 403 otherwise.
//
// Note: reading the form field parses the request body. To avoid conflicting with
// typed body decoding, the form is consulted ONLY when no token was submitted in a
// header — so submitting via header (the usual SPA practice) is recommended.
//
// With UseHostPrefixedCookie=true, CookieName gains the "__Host-" prefix and Secure/
// Path=//no-Domain are forced (the browser's hard requirements for that prefix),
// making the cookie unwritable and unshadowable by subdomains.
func CSRF(cfg CSRFConfig) Middleware {
	if cfg.CookieName == "" {
		cfg.CookieName = DefaultCSRFCookieName
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = DefaultCSRFHeaderName
	}
	if cfg.FieldName == "" {
		cfg.FieldName = DefaultCSRFFieldName
	}
	if cfg.CookiePath == "" {
		cfg.CookiePath = "/"
	}
	if cfg.SameSite == 0 {
		cfg.SameSite = http.SameSiteLaxMode
	}
	// __Host- 的三条约束在注册期一次性固化:请求期读到的 cfg 已经是浏览器会接受的形态,
	// 读 cookie 与写 cookie 也因此天然用同一个名字。前缀幂等叠加,重复调用不会写成
	// "__Host-__Host-"。
	// The three __Host- constraints are frozen once at registration: the cfg seen at
	// request time is already the shape a browser accepts, so reading and writing the
	// cookie naturally share one name. Prefixing is idempotent, so repeated calls never
	// produce "__Host-__Host-".
	if cfg.UseHostPrefixedCookie {
		if !strings.HasPrefix(cfg.CookieName, CSRFHostCookiePrefix) {
			cfg.CookieName = CSRFHostCookiePrefix + cfg.CookieName
		}
		cfg.Secure = true
		cfg.CookiePath = "/"
		cfg.CookieDomain = ""
	}
	trusted := make(map[string]bool, len(cfg.TrustedOrigins))
	for _, o := range cfg.TrustedOrigins {
		trusted[strings.ToLower(strings.TrimSuffix(o, "/"))] = true
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			if cfg.Skip != nil && cfg.Skip(req) {
				return next(ctx, req, resp)
			}

			cookieToken := csrfCookieValue(req, cfg.CookieName)

			if isSafeMethod(req.Method) {
				// 安全方法:确保有 token 可用,发放给客户端并下传。
				// Safe method: ensure a usable token, issue it to the client and pass it down.
				if cookieToken == "" {
					cookieToken = newCSRFToken()
					setCSRFCookie(resp, cfg, cookieToken)
				}
				return next(context.WithValue(ctx, csrfTokenKey{}, cookieToken), req, resp)
			}

			// 非安全方法:先做同源校验(第二道防线)。
			// Unsafe method: same-origin check first (the second line of defense).
			if err := checkCSRFOrigin(req, trusted, cfg.TrustedOriginsOnly); err != nil {
				return err
			}
			if cookieToken == "" {
				return fmt.Errorf("%w: missing CSRF cookie", ErrCSRFTokenInvalid)
			}
			sent := req.Header.Get(cfg.HeaderName)
			if sent == "" {
				sent = csrfFormToken(req, cfg.FieldName)
			}
			if sent == "" {
				return fmt.Errorf("%w: no CSRF token submitted", ErrCSRFTokenInvalid)
			}
			// 恒定时间比较:避免经响应时间侧信道逐字节猜测 token。
			// Constant-time compare: prevents byte-wise token guessing via a timing side channel.
			if subtle.ConstantTimeCompare([]byte(sent), []byte(cookieToken)) != 1 {
				return fmt.Errorf("%w: CSRF token mismatch", ErrCSRFTokenInvalid)
			}
			return next(context.WithValue(ctx, csrfTokenKey{}, cookieToken), req, resp)
		}
	}
}

// isSafeMethod 报告 method 是否为 RFC 9110 定义的安全方法(不改变服务端状态)。
// isSafeMethod reports whether method is safe per RFC 9110 (does not alter server state).
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// csrfCookieValue 取出 CSRF cookie 的值;不存在时返回空串。
// csrfCookieValue extracts the CSRF cookie value, or "" when absent.
func csrfCookieValue(req *Request, name string) string {
	c, err := req.Cookie(name)
	if err != nil || c == nil {
		return ""
	}
	return c.Value
}

// setCSRFCookie 下发 token cookie。它【不是】 HttpOnly——双提交模式要求前端 JS 能读到
// token 才能回填到请求头;防护强度来自同源策略(攻击站点读不到本站 cookie),而非 HttpOnly。
// setCSRFCookie issues the token cookie. It is deliberately NOT HttpOnly: the
// double-submit pattern requires front-end JS to read the token to echo it in a
// header; the protection comes from the same-origin policy (an attacker site cannot
// read this site's cookie), not from HttpOnly.
func setCSRFCookie(resp *Response, cfg CSRFConfig, token string) {
	http.SetCookie(resp, &http.Cookie{
		Name:     cfg.CookieName,
		Value:    token,
		Path:     cfg.CookiePath,
		Domain:   cfg.CookieDomain,
		MaxAge:   cfg.CookieMaxAge,
		Secure:   cfg.Secure,
		SameSite: cfg.SameSite,
	})
}

// csrfFormToken 从表单里取 token。只在请求头未提交时调用,避免无谓地解析请求体。
// csrfFormToken reads the token from the form. Called only when no header token was
// submitted, avoiding a needless body parse.
// maxCSRFPreAuthBodyBytes 限制"仅为取 CSRF 表单 token"而解析体的声明长度（4 MiB）。
// maxCSRFPreAuthBodyBytes caps the declared length of a body parsed only to fetch a
// CSRF form token (4 MiB).
const maxCSRFPreAuthBodyBytes = 4 << 20

func csrfFormToken(req *Request, field string) string {
	// 解析请求体只为取一个可能根本不存在的 token，而这次解析发生在鉴权结论之前：超大
	// multipart 体会先吃掉内存与临时文件（ParseMultipartForm 的 32 MiB 是【驻留内存】
	// 上限，不是总大小上限），随后请求才因缺 token 被拒。攻击者因此能用"注定失败的请求"
	// 消耗资源。声明长度超限时直接不解析：真要用表单字段带 token 的正常表单远小于此，
	// 而 header 通道完全不受影响。
	// Parsing the body only to find a token that may not exist, and doing so BEFORE the
	// verdict: an oversized multipart body first eats memory and temp files
	// (ParseMultipartForm's 32 MiB caps the IN-MEMORY portion, not the total), and only
	// then is the request rejected for a missing token — so an attacker spends resources
	// on requests that were always going to fail. Skip parsing beyond this declared
	// length: real form-field tokens are far smaller, and the header path is untouched.
	if req.ContentLength > maxCSRFPreAuthBodyBytes {
		return ""
	}
	switch mediaType(req.Header.Get("Content-Type")) {
	case "application/x-www-form-urlencoded":
		if err := req.ParseForm(); err != nil {
			return ""
		}
	case "multipart/form-data":
		// multipart 必须显式走 ParseMultipartForm:标准库的 ParseForm 对 multipart 体
		// 【不解析】却仍把 PostForm 置为非 nil 空值,导致其后的 PostFormValue 认为已解析
		// 而不再补解析,token 恒为空——multipart 提交将永远被判为缺失 token。
		// multipart must go through ParseMultipartForm explicitly: the stdlib
		// ParseForm does NOT parse a multipart body yet still leaves PostForm
		// non-nil and empty, so a subsequent PostFormValue considers it parsed and
		// never re-parses, always yielding "" — multipart submissions would forever
		// be judged as missing their token.
		if err := req.ParseMultipartForm(defaultMaxMultipartMemory); err != nil {
			return ""
		}
	default:
		// 非表单请求体(如 application/json)绝不触碰:解析会消耗掉 typed 解码所需的体。
		// Never touch a non-form body (e.g. application/json): parsing would consume
		// the body that typed decoding needs.
		return ""
	}
	return req.PostFormValue(field)
}

// checkCSRFOrigin 校验非安全方法的来源:有 Origin/Referer 时必须与请求自身的 scheme+host
// 同源,或落在可信来源列表内。
//
// 【头完全缺失】与【头存在且值为 "null"】必须区别对待,二者不是同一件事:
//   - 都缺失时放行——非浏览器客户端(curl、服务间调用)不发这些头,而它们不携带 cookie、
//     本就不受 CSRF 威胁;浏览器对非安全跨站请求必发 Origin,故不影响防护强度。
//   - `Origin: null` 是浏览器【主动声明】的不透明来源:沙箱 iframe(<iframe sandbox>)、
//     data:/blob: 文档、跨源重定向后的请求都会发它,而这些上下文恰恰是攻击者可构造的。
//     把它当作"缺失"放行,等于对全部攻击者可控来源关闭这道第二道防线,与双提交 token
//     未签名(未绑定会话)组合成完整绕过链。故必须【拒绝】。
//
// 同源比较必须带 scheme:只比 host 会把 http://example.com 这个中间人可任意改写的明文
// 页面判为 https 站点的同源来源,第二道防线随之失效。服务端拿不到自身 scheme 的现成来源
// (req.URL.Scheme 在服务端通常为空),故经 isTLSRequest 推导——它已实现"仅在可信代理后
// 才采信 X-Forwarded-Proto"的策略,此处复用以保证与 ClientIP/安全头同一套信任模型。
//
// checkCSRFOrigin verifies an unsafe method's origin: a present Origin/Referer must be
// same-origin with the request's own scheme+host, or listed as trusted.
//
// A COMPLETELY ABSENT header and a header PRESENT WITH THE VALUE "null" are not the
// same thing and must be handled differently:
//   - both absent passes: non-browser clients (curl, service-to-service) omit these
//     headers, carry no cookies, and are not subject to CSRF anyway, while browsers
//     always send Origin on unsafe cross-site requests, so protection is unaffected.
//   - `Origin: null` is an opaque origin the browser ASSERTS deliberately: sandboxed
//     iframes (<iframe sandbox>), data:/blob: documents, and post-cross-origin-redirect
//     requests all send it — exactly the contexts an attacker can construct. Treating
//     it as "absent" disables this second line of defense for every attacker-controlled
//     origin and, combined with an unsigned (session-unbound) double-submit token,
//     completes a bypass chain. It must therefore be REJECTED.
//
// The same-origin comparison must include the scheme: comparing hosts only would treat
// http://example.com — a plaintext page any man-in-the-middle can rewrite — as
// same-origin with an https site, defeating the second line of defense. The server has
// no ready source for its own scheme (req.URL.Scheme is usually empty server-side), so
// it is derived via isTLSRequest, which already implements the "trust
// X-Forwarded-Proto only behind a trusted proxy" policy and is reused here to keep one
// trust model shared with ClientIP and the security headers.
func checkCSRFOrigin(req *Request, trusted map[string]bool, trustedOnly bool) error {
	origin := req.Header.Get("Origin")
	if origin == "" {
		if ref := req.Header.Get("Referer"); ref != "" {
			u, err := url.Parse(ref)
			if err != nil || u.Host == "" {
				return fmt.Errorf("%w: malformed Referer", ErrCSRFTokenInvalid)
			}
			origin = u.Scheme + "://" + u.Host
		}
	}
	// 空串只可能来自"两个头都没有";"null" 是一个真实存在的头值,二者到此已天然分开。
	// "" can only mean "neither header present"; "null" is a real header value, so the
	// two are already distinct by this point.
	if origin == "" {
		return nil
	}
	if origin == "null" {
		return fmt.Errorf("%w: opaque origin %q", ErrCSRFTokenInvalid, origin)
	}
	if trusted[strings.ToLower(strings.TrimSuffix(origin, "/"))] {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: malformed Origin", ErrCSRFTokenInvalid)
	}
	// Host 反射兜底：Origin 的 host 与请求自身 Host 相同即视为同源放行。
	//
	// 这一兜底并非无代价：Host 属于客户端可影响输入，在按 Host 分发的部署（通配虚拟主机、
	// 误配的代理、DNS 重绑定）下，`Host: evil.com` 与 `Origin: http://evil.com` 成对出现
	// 即可通过，从而绕过运维声明的白名单。但另一方面，运维配了 TrustedOrigins 往往只是
	// 为了【追加】允许的合作来源，本站自身仍是靠这个兜底放行的——直接取消会让所有同源
	// 表单提交（token 走表单字段而非 header 的那类）全部 403。
	// 权衡结果：默认保留兜底（不破坏既有部署），并给出 TrustedOriginsOnly 让对来源有硬
	// 要求的服务显式收紧。
	// The Host-reflection fallback admits any Origin whose host equals the request's
	// own Host.
	//
	// The fallback is not free: Host is client-influenced, so under Host-based routing
	// (wildcard vhosts, a misconfigured proxy, DNS rebinding) a matched
	// `Host: evil.com` + `Origin: http://evil.com` pair passes it and bypasses the
	// declared whitelist. But operators usually add TrustedOrigins only to ALLOW MORE
	// partners, relying on this fallback for their own site — removing it outright 403s
	// every same-origin form post that carries its token as a form field rather than a
	// header. Trade-off: keep the fallback by default so existing deployments stand, and
	// offer TrustedOriginsOnly for services that need a hard source list.
	if !trustedOnly && strings.EqualFold(u.Host, req.Host) && strings.EqualFold(u.Scheme, requestScheme(req)) {
		return nil
	}
	return fmt.Errorf("%w: untrusted origin %q", ErrCSRFTokenInvalid, origin)
}

// requestScheme 返回请求自身的 scheme("https" 或 "http"),供同源比较使用。
// 判定完全委托给 isTLSRequest,以复用"仅可信代理后才采信 X-Forwarded-Proto"的既有策略;
// 在此另写一套判定会让 CSRF 与安全头/ClientIP 的信任模型分叉。
// requestScheme returns the request's own scheme ("https" or "http") for the
// same-origin comparison. The decision is delegated entirely to isTLSRequest to reuse
// the existing "trust X-Forwarded-Proto only behind a trusted proxy" policy;
// re-implementing it here would fork CSRF's trust model from the security headers' and
// ClientIP's.
func requestScheme(req *Request) string {
	if isTLSRequest(req) {
		return "https"
	}
	return "http"
}

// newCSRFToken 生成一个 URL 安全的随机 token。crypto/rand 在现代 Go 上不会失败
// (失败即进程级熵源故障),故此处不做降级——降级到弱随机反而会静默削弱防护。
// newCSRFToken generates a URL-safe random token. crypto/rand does not fail on
// modern Go (a failure means a process-level entropy fault), so there is no
// fallback here — degrading to weak randomness would silently weaken protection.
func newCSRFToken() string {
	b := make([]byte, csrfTokenLen)
	if _, err := rand.Read(b); err != nil {
		panic("ghttp: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
