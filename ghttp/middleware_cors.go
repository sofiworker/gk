package ghttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ===========================================================================
// CORS 中间件：按配置写跨域响应头，并处理预检（OPTIONS）请求。
// 默认安全：不启用通配来源，需显式声明允许的 Origin/Method/Header。
// CORS middleware: writes cross-origin headers per config and handles preflight
// (OPTIONS) requests. Safe by default: no wildcard origin is enabled; the
// allowed Origin/Method/Header must be declared explicitly.
// ===========================================================================

// CORSConfig 是跨域配置。零值不允许任何跨域；用 CORSDefault 获取常用默认。
// CORSConfig is the cross-origin configuration. The zero value allows no
// cross-origin access; use CORSDefault for a common default.
type CORSConfig struct {
	// AllowOrigins 允许的来源列表；含 "*" 表示任意来源。"*" 不能与 AllowCredentials
	// 同用：同时设置时 AllowCredentials 会被【忽略】（保留 ACAO: *），因为回显任意来源
	// 并允许携带凭证等于解除同源保护。需要凭证时请显式列出来源。
	// AllowOrigins is the list of allowed origins; "*" means any origin. "*" cannot be
	// combined with AllowCredentials: when both are set, AllowCredentials is IGNORED
	// (keeping ACAO: *), because echoing an arbitrary origin while permitting
	// credentials removes same-origin protection. List origins explicitly for
	// credentialed access.
	AllowOrigins []string
	// AllowMethods 允许的方法；空则不写 Access-Control-Allow-Methods。
	// AllowMethods is the allowed methods; empty omits Access-Control-Allow-Methods.
	AllowMethods []string
	// AllowHeaders 允许的请求头；空则回显预检请求的 Access-Control-Request-Headers。
	// AllowHeaders is the allowed request headers; empty echoes the preflight's
	// Access-Control-Request-Headers.
	AllowHeaders []string
	// ExposeHeaders 允许浏览器脚本访问的响应头。
	// ExposeHeaders lists response headers exposed to browser scripts.
	ExposeHeaders []string
	// AllowCredentials 是否允许携带凭证（cookie/authorization）。与 AllowOrigins 含 "*"
	// 同用时被忽略，详见 AllowOrigins。
	// AllowCredentials indicates whether credentials (cookie/authorization) are
	// allowed. Ignored when AllowOrigins contains "*"; see AllowOrigins.
	AllowCredentials bool
	// MaxAge 预检结果缓存时长；<=0 时不写 Max-Age。
	// MaxAge is how long a preflight result is cached; <=0 omits Max-Age.
	MaxAge time.Duration
}

// CORSDefault 返回一个宽松默认配置：任意来源、常见方法、回显请求头、无凭证。
// 适合公开只读 API；需要凭证时请显式列出 AllowOrigins 并设 AllowCredentials。
// CORSDefault returns a permissive default: any origin, common methods, echoed
// request headers, no credentials. Suitable for public read-only APIs; for
// credentials, list AllowOrigins explicitly and set AllowCredentials.
func CORSDefault() CORSConfig {
	return CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{
			http.MethodGet, http.MethodHead, http.MethodPost,
			http.MethodPut, http.MethodPatch, http.MethodDelete,
		},
		MaxAge: 12 * time.Hour,
	}
}

// CORS 用给定配置构造跨域中间件。预检 OPTIONS 请求在中间件内直接以 204 结束，不进
// 下游 handler。
// CORS builds a cross-origin middleware from the config. A preflight OPTIONS
// request is terminated with 204 inside the middleware and does not reach the
// downstream handler.
func CORS(cfg CORSConfig) Middleware {
	c := newCORS(cfg)
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			if c.apply(resp.Header(), req.Header, req.Method) == corsPreflight {
				resp.WriteHeader(http.StatusNoContent)
				return nil
			}
			return next(ctx, req, resp)
		}
	}
}

// corsCore 持有预计算的 CORS 配置,与请求期无关的字段只算一次。
// corsCore holds the precomputed CORS config; request-independent fields are
// computed once.
type corsCore struct {
	cfg             CORSConfig
	allowAllOrigins bool
	// allowCredentials 是生效后的凭证开关。它可能与 cfg.AllowCredentials 不同:通配来源
	// 与凭证并存时会被强制关闭,详见 newCORS。
	// allowCredentials is the effective credentials switch. It may differ from
	// cfg.AllowCredentials: it is forced off when a wildcard origin coexists with
	// credentials; see newCORS.
	allowCredentials bool
	// credentialsDroppedForWildcard 记录上述降级是否发生,供测试与诊断查询。
	// credentialsDroppedForWildcard records whether that downgrade happened, for
	// tests and diagnostics.
	credentialsDroppedForWildcard bool
	methods                       string
	exposeHeaders                 string
	allowHeaders                  string
}

// corsDecision 表示 apply 的处置结果：预检需调用方写 204，否则继续。
// corsDecision is the outcome of apply: preflight asks the caller to write 204,
// otherwise proceed.
type corsDecision int

const (
	corsProceed   corsDecision = iota // 继续处理请求 / continue handling
	corsPreflight                     // 预检，调用方写 204 / preflight, caller writes 204
)

// newCORS 预计算配置并返回可复用的 corsCore。
// newCORS precomputes the config and returns a reusable corsCore.
func newCORS(cfg CORSConfig) *corsCore {
	c := &corsCore{
		cfg:              cfg,
		allowCredentials: cfg.AllowCredentials,
		methods:          strings.Join(cfg.AllowMethods, ", "),
		exposeHeaders:    strings.Join(cfg.ExposeHeaders, ", "),
		allowHeaders:     strings.Join(cfg.AllowHeaders, ", "),
	}
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			c.allowAllOrigins = true
			break
		}
	}
	// 通配来源 + 凭证是被规范禁止的组合,且"回显任意 Origin 并附 Allow-Credentials"比
	// 规范禁止的写法更危险——它让任意站点都能带 cookie 读取响应,等于完全解除同源保护。
	// 这里安全降级为忽略 Credentials(保留 ACAO: *),而不是 panic:库代码不应让配置失误
	// 打挂进程,而丢弃凭证只会让需要凭证的请求失败(可察觉),不会静默扩大权限。
	// 需要凭证时必须显式列出来源,那才是唯一安全的表达。
	// Wildcard origin plus credentials is forbidden by the spec, and echoing an
	// arbitrary Origin alongside Allow-Credentials is more dangerous than the
	// forbidden form itself — it lets any site read responses with cookies, fully
	// removing same-origin protection. This degrades safely by ignoring Credentials
	// (keeping ACAO: *) rather than panicking: library code should not let a
	// misconfiguration kill the process, and dropping credentials only makes
	// credentialed requests fail visibly instead of silently widening access.
	// Credentials require explicitly listed origins, the only safe expression.
	if c.allowAllOrigins && c.allowCredentials {
		c.allowCredentials = false
		c.credentialsDroppedForWildcard = true
	}
	return c
}

// apply 依据请求头写跨域响应头，返回是否为需以 204 结束的预检。dst 是响应头，src 是
// 请求头。无 Origin（同源）或来源不在白名单时不写允许头并返回 corsProceed。
// apply writes cross-origin response headers per the request headers and reports
// whether this is a preflight to end with 204. dst is the response header, src the
// request header. With no Origin (same-origin) or a disallowed origin, it writes no
// allow header and returns corsProceed.
func (c *corsCore) apply(dst, src http.Header, method string) corsDecision {
	origin := src.Get("Origin")
	if origin == "" {
		return corsProceed
	}
	// 只要 ACAO 的取值依赖 Origin,所有分支(含拒绝分支)都必须声明 Vary: Origin。否则
	// 共享缓存可能先缓存一份不带 ACAO 的响应,再把它投给合法跨域请求(或反之),导致跨域
	// 时好时坏。拒绝分支同样"随 Origin 变化"——它变化的是"不写头"这个结果。
	// Whenever the ACAO value depends on Origin, every branch — including the reject
	// branch — must declare Vary: Origin. Otherwise a shared cache may store a
	// response without ACAO and later serve it to a legitimate cross-origin request
	// (or vice versa), making CORS work intermittently. The reject branch varies by
	// Origin too: what varies is the decision to omit the header.
	dst.Add("Vary", "Origin")

	switch {
	case c.allowAllOrigins:
		dst.Set("Access-Control-Allow-Origin", "*")
	case originAllowed(c.cfg.AllowOrigins, origin):
		dst.Set("Access-Control-Allow-Origin", origin)
	default:
		// 来源不在白名单：不写允许头，交由浏览器拦截；请求仍继续（服务端不阻断）。
		// Origin not allowed: write no allow header, let the browser block; the
		// request still proceeds (the server does not block it).
		return corsProceed
	}

	if c.allowCredentials {
		dst.Set("Access-Control-Allow-Credentials", "true")
	}
	if c.exposeHeaders != "" {
		dst.Set("Access-Control-Expose-Headers", c.exposeHeaders)
	}

	// 预检的判定必须带 Access-Control-Request-Method:按 Fetch 规范,预检一定携带该头。
	// 只看 OPTIONS 会把业务自己注册的 OPTIONS 路由(普通跨域 OPTIONS 请求)也 204 短路,
	// 使其 handler 永远不可达。
	// A preflight check must require Access-Control-Request-Method: per the Fetch
	// standard a preflight always carries it. Matching on OPTIONS alone would also
	// short-circuit a business-registered OPTIONS route (an ordinary cross-origin
	// OPTIONS request) with 204, making its handler unreachable.
	if method == http.MethodOptions && src.Get("Access-Control-Request-Method") != "" {
		if c.methods != "" {
			dst.Set("Access-Control-Allow-Methods", c.methods)
		}
		if c.allowHeaders != "" {
			dst.Set("Access-Control-Allow-Headers", c.allowHeaders)
		} else if reqHeaders := src.Get("Access-Control-Request-Headers"); reqHeaders != "" {
			dst.Set("Access-Control-Allow-Headers", reqHeaders)
		}
		if c.cfg.MaxAge > 0 {
			dst.Set("Access-Control-Max-Age", strconv.Itoa(int(c.cfg.MaxAge.Seconds())))
		}
		return corsPreflight
	}
	return corsProceed
}

// originAllowed 报告 origin 是否在白名单内（精确匹配）。
// originAllowed reports whether origin is in the allow-list (exact match).
func originAllowed(allowed []string, origin string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
	}
	return false
}
