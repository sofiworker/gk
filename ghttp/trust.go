package ghttp

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)

// HostResolver 从 HTTP 请求解析目标主机名。
// HostResolver resolves the target host from an HTTP request.
// 与 ClientIPResolver 对称：默认使用 r.Host（不信任任何转发头）；
// 反代场景可用 TrustedHostResolver 在信任代理后才读取 X-Forwarded-Host。
// Symmetric to ClientIPResolver: defaults to r.Host (never trusting forwarded
// headers); behind a reverse proxy use TrustedHostResolver to read
// X-Forwarded-Host only from trusted proxies.
type HostResolver func(*http.Request) string

// HostValidator 校验解析出的主机名是否放行。
// HostValidator reports whether the resolved host is allowed.
// 默认 nil（不校验）；通过 WithHostValidator 启用。
// nil by default (validation off); enable via WithHostValidator.
type HostValidator func(string) bool

// defaultTrustedCIDRs 默认信任所有代理（gin 的默认值）。
// defaultTrustedCIDRs trust every proxy by default (gin's default).
// 覆盖所有 IP 会触发 unsafe 警告，见 warnUnsafeTrustedProxies。
// covering all IPs triggers the unsafe warning, see warnUnsafeTrustedProxies.
var defaultTrustedCIDRs = func() []*net.IPNet {
	cidrs, _ := parseTrustedCIDRs([]string{"0.0.0.0/0", "::/0"})
	return cidrs
}()

// defaultForwardedIPHeaders 客户端 IP 的转发头白名单。
// defaultForwardedIPHeaders forwarded headers consulted for the client IP.
var defaultForwardedIPHeaders = []string{"X-Forwarded-For", "X-Real-IP"}

// defaultForwardedHostHeaders 主机名的转发头白名单。
// defaultForwardedHostHeaders forwarded headers consulted for the host.
var defaultForwardedHostHeaders = []string{"X-Forwarded-Host"}

// defaultHostResolver 返回请求的 Host 头。
// defaultHostResolver returns the request Host header.
func defaultHostResolver(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Host
}

// parseTrustedCIDRs 解析 CIDR 字符串列表。
// parseTrustedCIDRs parses a list of CIDR strings.
func parseTrustedCIDRs(cidrs []string) ([]*net.IPNet, error) {
	parsed := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if strings.TrimSpace(cidr) == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("ghttp: invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		parsed = append(parsed, ipNet)
	}
	return parsed, nil
}

// isTrustedProxy 报告请求来源 IP 是否在信任网段内。
// isTrustedProxy reports whether the request's source IP is within the trusted CIDRs.
func isTrustedProxy(remoteAddr string, cidrs []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// isUnsafeTrustedProxies 报告信任边界是否覆盖所有 IP（0.0.0.0 或 ::）。
// isUnsafeTrustedProxies reports whether the trust boundary covers all IPs (0.0.0.0 or ::).
func isUnsafeTrustedProxies(cidrs []*net.IPNet) bool {
	for _, cidr := range cidrs {
		if cidr.Contains(net.ParseIP("0.0.0.0")) || cidr.Contains(net.ParseIP("::")) {
			return true
		}
	}
	return false
}

var warnUnsafeOnce sync.Once

// warnUnsafeTrustedProxies 在信任边界覆盖所有 IP 时告警（gin 同款）。
// warnUnsafeTrustedProxies warns when the trust boundary covers all IPs (same as gin).
// 全局只提示一次，避免每个 Server 重复输出。
// it warns once per process to avoid repeating the noise per Server.
func warnUnsafeTrustedProxies(c *Config) {
	warnUnsafeOnce.Do(func() {
		msg := "trusted proxies cover all IPs (0.0.0.0/0 or ::/0): forwarded headers (X-Forwarded-For, X-Forwarded-Host) are trusted from any source; restrict via WithTrustedProxies when the server is directly reachable by clients"
		if c != nil && c.logger != nil {
			c.logger.WarnContext(context.Background(), msg)
			return
		}
		log.Printf("ghttp: %s", msg)
	})
}

// resolveHost 用配置的解析器解析主机名。
// resolveHost resolves the host with the configured resolver.
func (c *Config) resolveHost(r *http.Request) string {
	if c.hostResolver != nil {
		return c.hostResolver(r)
	}
	return defaultHostResolver(r)
}

// TrustedHostResolver 构造信任感知的主机名解析器。
// TrustedHostResolver builds a trust-aware host resolver.
// 仅当请求来源是信任代理时才读取转发头，否则回退 r.Host，
// 防止客户端直连时伪造 X-Forwarded-Host 绕过校验。
// forwarded headers are read only from trusted proxies, otherwise it falls
// back to r.Host, preventing clients from forging X-Forwarded-Host to bypass
// validation when connecting directly.
func TrustedHostResolver(cidrs []*net.IPNet, forwardedHeaders []string) HostResolver {
	return func(r *http.Request) string {
		if r == nil {
			return ""
		}
		if isTrustedProxy(r.RemoteAddr, cidrs) {
			for _, name := range forwardedHeaders {
				if v := r.Header.Get(name); v != "" {
					return v
				}
			}
		}
		return r.Host
	}
}

// TrustedClientIPResolver 构造信任感知的客户端 IP 解析器。
// TrustedClientIPResolver builds a trust-aware client IP resolver.
// 与 TrustedHostResolver 共享同一信任边界：信任代理时才读转发头（取首个 IP），
// 否则回退 RemoteAddr。与默认的 RemoteAddr-only 解析器互为补充。
// Sharing the same trust boundary as TrustedHostResolver: forwarded headers
// (first IP) are read only from trusted proxies, otherwise it falls back to
// RemoteAddr. Complements the default RemoteAddr-only resolver.
func TrustedClientIPResolver(cidrs []*net.IPNet, forwardedHeaders []string) ClientIPResolver {
	return func(r *http.Request) string {
		if r == nil {
			return ""
		}
		if isTrustedProxy(r.RemoteAddr, cidrs) {
			for _, name := range forwardedHeaders {
				if v := r.Header.Get(name); v != "" {
					first := strings.TrimSpace(strings.Split(v, ",")[0])
					if first != "" {
						return first
					}
				}
			}
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
}

// AllowedHosts 构造主机白名单校验器。
// AllowedHosts builds a host allow-list validator.
// 支持精确匹配与通配子域（*.example.com 匹配 foo.example.com，不匹配 example.com 本身）；
// 比较前剥离端口（example.com:8080 视为 example.com）。patterns 不应带端口。
// Supports exact matches and wildcard subdomains (*.example.com matches
// foo.example.com but not example.com itself); ports are stripped before
// comparison. patterns should not include a port.
func AllowedHosts(patterns ...string) HostValidator {
	return func(host string) bool {
		host = hostWithoutPort(host)
		for _, pattern := range patterns {
			if strings.HasPrefix(pattern, "*.") {
				suffix := strings.TrimPrefix(pattern, "*")
				if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
					return true
				}
			} else if host == pattern {
				return true
			}
		}
		return false
	}
}

// hostWithoutPort 剥离主机名的端口部分。
// hostWithoutPort strips the port from a host.
func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
