package ghttp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrCORSWildcardCredentials 表示配置了 "*" 来源同时允许凭证，这是 CORS 规范禁止的。
// ErrCORSWildcardCredentials means a "*" origin was combined with credentials, which the
// CORS spec forbids.
var ErrCORSWildcardCredentials = errors.New("ghttp: CORS AllowOrigins \"*\" cannot be combined with AllowCredentials")

// CORSConfig 定义 CORS 中间件的配置。
// CORSConfig defines the configuration for the CORS middleware.
type CORSConfig struct {
	// AllowOrigins 是允许的来源列表，支持 "*"（任意来源）。
	// AllowOrigins lists allowed origins; "*" allows any origin.
	AllowOrigins []string
	// AllowOriginFunc 自定义来源判定，与 AllowOrigins 任一命中即放行。
	// AllowOriginFunc is a custom origin check; either it or AllowOrigins matching allows.
	AllowOriginFunc func(origin string) bool
	// AllowMethods 是预检时回写的允许方法；为空时为 GET, HEAD, POST。
	// AllowMethods are echoed on preflight; defaults to GET, HEAD, POST.
	AllowMethods []string
	// AllowHeaders 是预检时回写的允许请求头；为空时回显请求的 Access-Control-Request-Headers。
	// AllowHeaders are echoed on preflight; when empty the requested headers are reflected.
	AllowHeaders []string
	// ExposeHeaders 是非预检响应中暴露给脚本的响应头。
	// ExposeHeaders are exposed to scripts on non-preflight responses.
	ExposeHeaders []string
	// AllowCredentials 是否允许携带凭证；开启时回写具体 origin，绝不回写 "*"。
	// AllowCredentials allows credentials; the concrete origin is echoed, never "*".
	AllowCredentials bool
	// MaxAge 是预检结果缓存时间（按秒取整）。
	// MaxAge is the preflight cache duration (whole seconds).
	MaxAge time.Duration
}

// CORS 是 NewCORS 的便捷版：配置非法时 panic（启动期编程错误，尽早暴露）。
// CORS is the convenience form of NewCORS; it panics on an invalid config (a start-up
// programming error, surfaced early).
func CORS(config CORSConfig) Middleware {
	mw, err := NewCORS(config)
	if err != nil {
		panic(err)
	}
	return mw
}

// NewCORS 校验配置并返回 CORS 中间件。"*" 与 AllowCredentials 同时配置时返回
// ErrCORSWildcardCredentials。
//
// 规则：只有 Origin 命中才回写 Access-Control-Allow-Origin；始终追加 Vary: Origin；
// 只有带 Access-Control-Request-Method 的 OPTIONS 才是预检并返回 204，其余 OPTIONS 交给 next。
//
// NewCORS validates the config and returns the middleware; "*" with AllowCredentials yields
// ErrCORSWildcardCredentials. Only a matching Origin gets Access-Control-Allow-Origin;
// Vary: Origin is always added; only an OPTIONS carrying Access-Control-Request-Method is a
// preflight (answered 204), other OPTIONS go to next.
func NewCORS(config CORSConfig) (Middleware, error) {
	wildcard := false
	origins := make(map[string]struct{}, len(config.AllowOrigins))
	for _, o := range config.AllowOrigins {
		if o == "*" {
			wildcard = true
			continue
		}
		origins[strings.ToLower(o)] = struct{}{}
	}
	if wildcard && config.AllowCredentials {
		return nil, ErrCORSWildcardCredentials
	}
	methods := "GET, HEAD, POST"
	if len(config.AllowMethods) > 0 {
		methods = strings.Join(config.AllowMethods, ", ")
	}
	allowHeaders := strings.Join(config.AllowHeaders, ", ")
	expose := strings.Join(config.ExposeHeaders, ", ")
	maxAge := ""
	if config.MaxAge >= time.Second {
		maxAge = strconv.FormatInt(int64(config.MaxAge/time.Second), 10)
	}
	match := func(origin string) bool {
		if wildcard {
			return true
		}
		if _, ok := origins[strings.ToLower(origin)]; ok {
			return true
		}
		return config.AllowOriginFunc != nil && config.AllowOriginFunc(origin)
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			h := resp.Header()
			h.Add("Vary", "Origin")
			origin := req.Raw.Header.Get("Origin")
			if origin == "" || !match(origin) {
				return next(ctx, req, resp)
			}
			preflight := req.Raw.Method == http.MethodOptions &&
				req.Raw.Header.Get("Access-Control-Request-Method") != ""

			if wildcard {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			if config.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			if !preflight {
				if expose != "" {
					h.Set("Access-Control-Expose-Headers", expose)
				}
				return next(ctx, req, resp)
			}

			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Methods", methods)
			if allowHeaders != "" {
				h.Set("Access-Control-Allow-Headers", allowHeaders)
			} else if rh := req.Raw.Header.Get("Access-Control-Request-Headers"); rh != "" {
				h.Set("Access-Control-Allow-Headers", rh)
			}
			if maxAge != "" {
				h.Set("Access-Control-Max-Age", maxAge)
			}
			resp.WriteHeader(http.StatusNoContent)
			return nil
		}
	}, nil
}
