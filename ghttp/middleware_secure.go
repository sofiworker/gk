package ghttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 安全响应头名称常量。
// Security response header name constants.
const (
	HeaderXContentTypeOptions     = "X-Content-Type-Options"
	HeaderXFrameOptions           = "X-Frame-Options"
	HeaderReferrerPolicy          = "Referrer-Policy"
	HeaderCrossOriginOpenerPolicy = "Cross-Origin-Opener-Policy"
	HeaderStrictTransportSecurity = "Strict-Transport-Security"
	HeaderContentSecurityPolicy   = "Content-Security-Policy"
)

// 安全响应头的默认值。
// Default values of the security headers.
const (
	DefaultContentTypeOptions = "nosniff"
	DefaultFrameOptions       = "DENY"
	DefaultReferrerPolicy     = "strict-origin-when-cross-origin"
	DefaultOpenerPolicy       = "same-origin"
)

// secConfig 是 SecureHeaders 的配置。
// secConfig is the SecureHeaders configuration.
type secConfig struct {
	headers    map[string]string
	removed    map[string]struct{}
	hsts       string
	trustProto bool
}

// SecureOption 配置 SecureHeaders。
// SecureOption configures SecureHeaders.
type SecureOption func(*secConfig)

// WithHSTS 启用 Strict-Transport-Security，仅在 TLS 请求（或信任 X-Forwarded-Proto 时的 https 请求）上发送。
// maxAge 小于等于 0 时不启用。
// WithHSTS enables Strict-Transport-Security, sent only on TLS requests (or https requests
// when X-Forwarded-Proto is trusted). It is disabled when maxAge <= 0.
func WithHSTS(maxAge time.Duration, includeSubDomains, preload bool) SecureOption {
	return func(c *secConfig) {
		if maxAge <= 0 {
			c.hsts = ""
			return
		}
		v := "max-age=" + strconv.FormatInt(int64(maxAge/time.Second), 10)
		if includeSubDomains {
			v += "; includeSubDomains"
		}
		if preload {
			v += "; preload"
		}
		c.hsts = v
	}
}

// WithSecureTrustForwardedProto 信任 X-Forwarded-Proto: https（仅在可信反向代理之后启用）。
// WithSecureTrustForwardedProto trusts X-Forwarded-Proto: https (enable only behind a trusted proxy).
func WithSecureTrustForwardedProto() SecureOption {
	return func(c *secConfig) { c.trustProto = true }
}

// WithCSP 设置 Content-Security-Policy；空串表示不发送。
// WithCSP sets Content-Security-Policy; an empty string means not sent.
func WithCSP(policy string) SecureOption {
	return func(c *secConfig) { c.headers[HeaderContentSecurityPolicy] = policy }
}

// WithFrameOptions 设置 X-Frame-Options（如 DENY、SAMEORIGIN）；空串表示不发送。
// WithFrameOptions sets X-Frame-Options (e.g. DENY, SAMEORIGIN); empty means not sent.
func WithFrameOptions(v string) SecureOption {
	return func(c *secConfig) { c.headers[HeaderXFrameOptions] = v }
}

// WithReferrerPolicy 设置 Referrer-Policy；空串表示不发送。
// WithReferrerPolicy sets Referrer-Policy; empty means not sent.
func WithReferrerPolicy(v string) SecureOption {
	return func(c *secConfig) { c.headers[HeaderReferrerPolicy] = v }
}

// WithoutHeader 禁止发送指定名称的安全头（与选项顺序无关）。
// WithoutHeader suppresses the named security header (independent of option order).
func WithoutHeader(name string) SecureOption {
	return func(c *secConfig) { c.removed[http.CanonicalHeaderKey(name)] = struct{}{} }
}

// SecureHeaders 返回设置常见安全响应头的中间件。头在调用 next 之前设置，handler 可以覆盖。
// SecureHeaders returns a middleware that sets common security response headers. Headers are
// set before next runs, so handlers may override them.
func SecureHeaders(opts ...SecureOption) Middleware {
	cfg := &secConfig{
		headers: map[string]string{
			HeaderXContentTypeOptions:     DefaultContentTypeOptions,
			HeaderXFrameOptions:           DefaultFrameOptions,
			HeaderReferrerPolicy:          DefaultReferrerPolicy,
			HeaderCrossOriginOpenerPolicy: DefaultOpenerPolicy,
		},
		removed: map[string]struct{}{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	type kv struct{ k, v string }
	var fixed []kv
	for k, v := range cfg.headers {
		if _, off := cfg.removed[k]; off || v == "" {
			continue
		}
		fixed = append(fixed, kv{k, v})
	}
	hsts := cfg.hsts
	if _, off := cfg.removed[HeaderStrictTransportSecurity]; off {
		hsts = ""
	}
	trust := cfg.trustProto

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			h := resp.Header()
			for _, e := range fixed {
				h.Set(e.k, e.v)
			}
			if hsts != "" && secIsSecure(req.Raw, trust) {
				h.Set(HeaderStrictTransportSecurity, hsts)
			}
			return next(ctx, req, resp)
		}
	}
}

// secIsSecure 判断请求是否经由 HTTPS 到达。
// secIsSecure reports whether the request arrived over HTTPS.
func secIsSecure(r *http.Request, trustProto bool) bool {
	if r.TLS != nil {
		return true
	}
	return trustProto && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
