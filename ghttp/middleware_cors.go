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
	// AllowOrigins 允许的来源列表；含 "*" 表示任意来源（此时不能与 AllowCredentials 同用）。
	// AllowOrigins is the list of allowed origins; "*" means any origin (which
	// cannot be combined with AllowCredentials).
	AllowOrigins []string
	// AllowMethods 允许的方法；空则回退到常见安全方法集。
	// AllowMethods is the allowed methods; empty falls back to a common safe set.
	AllowMethods []string
	// AllowHeaders 允许的请求头；空则回显预检请求的 Access-Control-Request-Headers。
	// AllowHeaders is the allowed request headers; empty echoes the preflight's
	// Access-Control-Request-Headers.
	AllowHeaders []string
	// ExposeHeaders 允许浏览器脚本访问的响应头。
	// ExposeHeaders lists response headers exposed to browser scripts.
	ExposeHeaders []string
	// AllowCredentials 是否允许携带凭证（cookie/authorization）。
	// AllowCredentials indicates whether credentials (cookie/authorization) are allowed.
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
	methods         string
	exposeHeaders   string
	allowHeaders    string
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
		cfg:           cfg,
		methods:       strings.Join(cfg.AllowMethods, ", "),
		exposeHeaders: strings.Join(cfg.ExposeHeaders, ", "),
		allowHeaders:  strings.Join(cfg.AllowHeaders, ", "),
	}
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			c.allowAllOrigins = true
			break
		}
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

	if c.allowAllOrigins {
		// 通配来源与凭证互斥：带凭证时必须回显具体 Origin。
		// Wildcard origin excludes credentials: with credentials, echo the origin.
		if c.cfg.AllowCredentials {
			dst.Set("Access-Control-Allow-Origin", origin)
			dst.Add("Vary", "Origin")
		} else {
			dst.Set("Access-Control-Allow-Origin", "*")
		}
	} else if originAllowed(c.cfg.AllowOrigins, origin) {
		dst.Set("Access-Control-Allow-Origin", origin)
		dst.Add("Vary", "Origin")
	} else {
		// 来源不在白名单：不写允许头，交由浏览器拦截；请求仍继续（服务端不阻断）。
		// Origin not allowed: write no allow header, let the browser block; the
		// request still proceeds (the server does not block it).
		return corsProceed
	}

	if c.cfg.AllowCredentials {
		dst.Set("Access-Control-Allow-Credentials", "true")
	}
	if c.exposeHeaders != "" {
		dst.Set("Access-Control-Expose-Headers", c.exposeHeaders)
	}

	if method == http.MethodOptions {
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
