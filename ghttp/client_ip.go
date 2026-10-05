package ghttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ErrInvalidTrustedProxy 表示可信代理配置（CIDR 或 IP）无法解析。
// ErrInvalidTrustedProxy indicates a trusted proxy entry (CIDR or IP) cannot be parsed.
var ErrInvalidTrustedProxy = errors.New("ghttp: invalid trusted proxy")

// cipKey 是 context 中保存客户端 IP 的键。
// cipKey is the context key for the client IP.
type cipKey struct{}

// cipConfig 是 RealIP 的配置。
// cipConfig is the RealIP configuration.
type cipConfig struct {
	trusted []netip.Prefix
	header  string
	errs    []error
}

// RealIPOption 配置 RealIP 中间件。
// RealIPOption configures the RealIP middleware.
type RealIPOption func(*cipConfig)

// WithTrustedProxies 设置可信代理网段，支持 CIDR（"10.0.0.0/8"）或单个 IP。
// 非法条目会让 RealIP 返回包装 ErrInvalidTrustedProxy 的错误。
// WithTrustedProxies sets trusted proxy ranges (CIDR or single IP). Invalid entries make
// RealIP return an error wrapping ErrInvalidTrustedProxy.
func WithTrustedProxies(cidrs ...string) RealIPOption {
	return func(c *cipConfig) {
		ps, err := ParseTrustedProxies(cidrs...)
		if err != nil {
			c.errs = append(c.errs, err)
			return
		}
		c.trusted = append(c.trusted, ps...)
	}
}

// WithRealIPHeader 指定可信对端携带客户端 IP 的头（如 "X-Real-IP"）；
// 默认（空）使用 X-Forwarded-For 从右向左解析。
// WithRealIPHeader names the header carrying the client IP from a trusted peer (e.g.
// "X-Real-IP"); by default X-Forwarded-For is parsed right to left.
func WithRealIPHeader(name string) RealIPOption {
	return func(c *cipConfig) { c.header = name }
}

// ParseTrustedProxies 解析 CIDR 或 IP 列表。
// ParseTrustedProxies parses a list of CIDRs or IPs.
func ParseTrustedProxies(cidrs ...string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidTrustedProxy, s)
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// RealIP 返回解析真实客户端 IP 的中间件。
// RealIP returns a middleware that resolves the real client IP.
//
// 设计取舍：返回 (Middleware, error)，使非法 CIDR 在注册期立即暴露，而不是静默降级为
// "不信任任何代理"（会让部署者误以为配置生效）。
// Design: it returns (Middleware, error) so invalid CIDRs surface at registration time
// instead of silently degrading to "trust nobody".
//
// 仅当 RemoteAddr 属于可信网段时才读取转发头，否则一律使用 RemoteAddr。
// Forwarding headers are honored only when RemoteAddr is within a trusted range.
func RealIP(opts ...RealIPOption) (Middleware, error) {
	cfg := &cipConfig{}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	if len(cfg.errs) > 0 {
		return nil, errors.Join(cfg.errs...)
	}
	header := cfg.header
	trusted := cfg.trusted
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			ip := cipResolve(req.Raw, trusted, header)
			nctx := context.WithValue(req.Raw.Context(), cipKey{}, ip)
			req.Raw = req.Raw.WithContext(nctx)
			return next(nctx, req, resp)
		}
	}, nil
}

// ClientIP 返回客户端 IP：优先使用 RealIP 中间件的结果，否则为 RemoteAddr 的 host。
// ClientIP returns the client IP: the RealIP middleware result if present, else the host of
// RemoteAddr.
func ClientIP(req *Request) string {
	if req == nil || req.Raw == nil {
		return ""
	}
	if v, ok := req.Raw.Context().Value(cipKey{}).(string); ok && v != "" {
		return v
	}
	return cipHost(req.Raw.RemoteAddr)
}

// cipHost 去掉端口与 IPv6 方括号。
// cipHost strips the port and IPv6 brackets.
func cipHost(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return strings.Trim(strings.TrimSpace(addr), "[]")
}

func cipContains(trusted []netip.Prefix, a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// cipParseAddr 解析 XFF 项（可带端口、方括号）。
// cipParseAddr parses an XFF item (may carry port or brackets).
func cipParseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}, false
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	s = strings.Trim(s, "[]")
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func cipResolve(r *http.Request, trusted []netip.Prefix, header string) string {
	peer := cipHost(r.RemoteAddr)
	pa, ok := cipParseAddr(peer)
	if !ok || len(trusted) == 0 || !cipContains(trusted, pa) {
		return peer
	}
	if header != "" && !strings.EqualFold(header, "X-Forwarded-For") {
		if a, ok := cipParseAddr(r.Header.Get(header)); ok {
			return a.String()
		}
		return peer
	}
	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		a, ok := cipParseAddr(parts[i])
		if !ok {
			// 非法项：链不可信，回退到对端
			// Invalid item: chain is untrustworthy, fall back to the peer
			return peer
		}
		if !cipContains(trusted, a) {
			return a.String()
		}
	}
	return peer
}
