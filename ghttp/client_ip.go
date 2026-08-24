package ghttp

import (
	"net"
	"net/netip"
	"strings"
)

// ===========================================================================
// 客户端真实 IP 解析 / Client real-IP resolution
//
// 反向代理/LB 后端拿到的 RemoteAddr 是代理地址而非真实客户端。盲信 X-Forwarded-For
// 又会被伪造。这里采用与 gin 一致的可信代理模型:仅当【直连对端】落在配置的可信网段内,
// 才回溯转发头取真实 IP;默认不配置任何可信代理时,ClientIP() 只返回 RemoteIP(),绝不
// 采信任何转发头——安全优先。
// Behind a reverse proxy/LB, RemoteAddr is the proxy, not the real client, while
// blindly trusting X-Forwarded-For is spoofable. This uses gin's trusted-proxy
// model: forwarded headers are honored only when the DIRECT peer falls within a
// configured trusted network; with no trusted proxy configured (default),
// ClientIP() returns only RemoteIP() and never trusts any forwarded header —
// security first.
// ===========================================================================

// defaultForwardedHeaders 是解析真实 IP 时默认按序检查的转发头。
// defaultForwardedHeaders are the forwarded headers checked in order by default.
var defaultForwardedHeaders = []string{"X-Forwarded-For", "X-Real-IP"}

// RemoteIP 返回直连对端 IP(从 RemoteAddr 去掉端口),不考虑任何转发头。解析失败返回空串。
// RemoteIP returns the direct peer IP (RemoteAddr minus the port), ignoring any
// forwarded header. Empty on parse failure.
func (r *Request) RemoteIP() string {
	return remoteIP(r.RemoteAddr)
}

// ClientIP 依可信代理策略返回真实客户端 IP:
//   - 未配置可信代理(默认):返回 RemoteIP(),不采信任何转发头(防伪造);
//   - 已配置且直连对端可信:按 forwardedHeaders 顺序回溯,返回第一个"非可信代理"的 IP;
//   - 直连对端不可信:返回 RemoteIP()。
//
// ClientIP returns the real client IP per the trusted-proxy policy:
//   - no trusted proxy configured (default): returns RemoteIP(), trusting no
//     forwarded header (anti-spoofing);
//   - configured and the direct peer is trusted: walks forwardedHeaders in order,
//     returning the first non-trusted-proxy IP;
//   - the direct peer is not trusted: returns RemoteIP().
func (r *Request) ClientIP() string {
	remote := r.RemoteIP()
	// 无 owner(miss 冷路径的临时 Request)或未配置可信代理:仅用直连 IP。
	// No owner (transient miss-path Request) or no trusted proxy: direct IP only.
	if r.owner == nil || len(r.owner.trustedProxies) == 0 {
		return remote
	}
	remoteAddr, err := netip.ParseAddr(remote)
	if err != nil || !ipInAnyPrefix(remoteAddr, r.owner.trustedProxies) {
		return remote
	}

	headers := r.owner.forwardedHeaders
	if len(headers) == 0 {
		headers = defaultForwardedHeaders
	}
	for _, h := range headers {
		v := r.Header.Get(h)
		if v == "" {
			continue
		}
		if ip := firstNonTrustedIP(v, r.owner.trustedProxies); ip != "" {
			return ip
		}
	}
	return remote
}

// firstNonTrustedIP 从一个逗号分隔的转发头值(如 X-Forwarded-For)自右向左回溯,返回第一个
// 不属于可信代理网段的 IP,即真实客户端。全部可信或空则返回空串。
// firstNonTrustedIP walks a comma-separated forwarded header value (e.g.
// X-Forwarded-For) right-to-left, returning the first IP not in a trusted proxy
// network — the real client. Empty if all are trusted or the value is empty.
func firstNonTrustedIP(header string, trusted []netip.Prefix) string {
	items := strings.Split(header, ",")
	for i := len(items) - 1; i >= 0; i-- {
		s := strings.TrimSpace(items[i])
		if s == "" {
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		if !ipInAnyPrefix(addr, trusted) {
			return s
		}
	}
	return ""
}

// ipInAnyPrefix 报告 addr 是否落入任一网段。比较前统一 Unmap,兼容 IPv4-in-IPv6。
// ipInAnyPrefix reports whether addr falls in any prefix. Addresses are Unmapped
// first for IPv4-in-IPv6 compatibility.
func ipInAnyPrefix(addr netip.Addr, prefixes []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// remoteIP 从 host:port 形式的 RemoteAddr 提取 IP;无端口时按裸 IP 处理。
// remoteIP extracts the IP from a host:port RemoteAddr; treats a value with no
// port as a bare IP.
func remoteIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// parseTrustedProxies 把 CIDR 或裸 IP 字符串解析为 netip.Prefix 列表。裸 IP 视为 /32
// (IPv4) 或 /128 (IPv6) 单机网段。任一条目非法即返回错误(注册期尽早暴露配置错误)。
// parseTrustedProxies parses CIDR or bare-IP strings into netip.Prefix values. A
// bare IP becomes a /32 (IPv4) or /128 (IPv6) single-host network. Any invalid
// entry returns an error to surface config mistakes early at registration.
func parseTrustedProxies(cidrs []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.ContainsRune(c, '/') {
			p, err := netip.ParsePrefix(c)
			if err != nil {
				return nil, err
			}
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(c)
		if err != nil {
			return nil, err
		}
		out = append(out, netip.PrefixFrom(addr.Unmap(), addr.BitLen()))
	}
	return out, nil
}

// WithTrustedProxies 声明可信代理网段(CIDR 或裸 IP)。仅当请求的直连对端落入其一,
// ClientIP() 才采信转发头。非法条目会 panic(注册期配置错误应尽早暴露)。默认无可信代理。
// WithTrustedProxies declares trusted proxy networks (CIDR or bare IP). Forwarded
// headers are honored only when a request's direct peer falls within one. An
// invalid entry panics (a registration-time config error surfaced early). No
// trusted proxy by default.
func WithTrustedProxies(cidrs ...string) Option {
	return func(s *Server) {
		p, err := parseTrustedProxies(cidrs)
		if err != nil {
			panic("ghttp: WithTrustedProxies: " + err.Error())
		}
		s.trustedProxies = p
	}
}

// WithForwardedHeaders 覆盖回溯真实 IP 时检查的转发头名(按序)。默认
// X-Forwarded-For、X-Real-IP。
// WithForwardedHeaders overrides the forwarded header names checked (in order)
// when resolving the real IP. Default: X-Forwarded-For, X-Real-IP.
func WithForwardedHeaders(names ...string) Option {
	return func(s *Server) { s.forwardedHeaders = names }
}
