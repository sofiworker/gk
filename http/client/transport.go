package client

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// 本文件把构造期的传输参数落实为 http.Client/Transport，并给出把 Client 当
// http.RoundTripper 使用的适配口。
// This file materializes the construction-time transport parameters into
// http.Client/Transport and provides the adapter for using a Client as an
// http.RoundTripper.

// WithTLSConfig 设置 TLS 配置（mTLS、自定义 RootCAs、ServerName 等）。内部会克隆该配置
// 并强制 ForceAttemptHTTP2=true——自定义 TLS 会让 Transport 默认放弃 HTTP/2，这是"我加了
// 自定义证书，结果 HTTP/2 悄悄没了"的经典来源。
// WithTLSConfig sets the TLS configuration (mTLS, custom RootCAs, ServerName, …). It is
// cloned internally and ForceAttemptHTTP2 is forced true — a custom TLS config makes
// the Transport give up HTTP/2 by default, the classic source of "I added a custom
// certificate and HTTP/2 silently disappeared".
func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *Client) {
		c.tlsConfig = cfg
		c.transportChanged = true
	}
}

// finalize 把构造期收集的参数落实为一个可用的 http.Client。explicitClient 为真时不做任何
// 事：WithHTTPClient 是"完全接管"，本包不再改写它的超时、Jar 与重定向策略。
// finalize materializes the collected parameters into a usable http.Client. It does
// nothing when explicitClient is true: WithHTTPClient means full takeover, so this
// package stops rewriting its timeout, jar and redirect policy.
func (c *Client) finalize() {
	if c.explicitClient {
		return
	}
	if c.httpClient != nil {
		return
	}
	transport := c.transport
	if transport == nil {
		transport = c.buildTransport()
	}
	hc := &http.Client{Transport: transport}
	if c.timeout > 0 {
		hc.Timeout = c.timeout
	}
	if c.jar != nil {
		hc.Jar = c.jar
	}
	if c.checkRedirect != nil {
		hc.CheckRedirect = c.checkRedirect
	}
	c.httpClient = hc
}

// buildTransport 以 http.DefaultTransport 的克隆为基底，仅覆盖调用方显式要求的字段。
//
// 用克隆而非从零构造有两个必须的理由：一是绝不修改全局的 http.DefaultTransport（它是
// 全进程共享的）；二是保留默认的 ProxyFromEnvironment、DialContext、ForceAttemptHTTP2、
// 连接池参数与各项超时。从零构造 &http.Transport{} 会静默丢掉 HTTP/2、代理与连接池，
// 是封装层最容易犯的错。
// buildTransport clones http.DefaultTransport as its base and overrides only what the
// caller explicitly asked for.
//
// Cloning rather than constructing from scratch matters for two reasons: never mutate
// the process-global http.DefaultTransport, and keep the default ProxyFromEnvironment,
// DialContext, ForceAttemptHTTP2, pool parameters and timeouts. A bare
// &http.Transport{} silently drops HTTP/2, proxies and pooling — the easiest mistake
// for a wrapper to make.
func (c *Client) buildTransport() *http.Transport {
	var t *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		t = base.Clone()
	} else {
		t = &http.Transport{ForceAttemptHTTP2: true}
	}

	// 拨号：显式 DialContext 优先；否则仅在给了 dialer/超时时覆盖克隆来的默认拨号器。
	// Dialing: an explicit DialContext wins; otherwise override the cloned default only
	// when a dialer/timeout was supplied.
	if c.dialContext != nil {
		t.DialContext = c.dialContext
	} else if d := c.effectiveDialer(); d != nil {
		t.DialContext = d.DialContext
	}

	if c.tlsConfig != nil {
		t.TLSClientConfig = c.tlsConfig.Clone()
		t.ForceAttemptHTTP2 = true
	}
	if c.tlsTimeout > 0 {
		t.TLSHandshakeTimeout = c.tlsTimeout
	}
	if c.headerTimeout > 0 {
		t.ResponseHeaderTimeout = c.headerTimeout
	}
	if c.idleConnTimeout > 0 {
		t.IdleConnTimeout = c.idleConnTimeout
	}
	if c.proxyURL != nil {
		t.Proxy = http.ProxyURL(c.proxyURL)
	} else if c.proxyFromEnv {
		t.Proxy = http.ProxyFromEnvironment
	}
	return t
}

// effectiveDialer 返回一个可用的拨号器副本：用户给了就复制它（绝不改调用方对象），
// 没给但设了超时就基于默认值构造一个；两者都没有则返回 nil，表示沿用克隆来的默认拨号器。
// effectiveDialer returns a usable dialer copy: the caller's object is copied (never
// mutated) when supplied, built from defaults when only a timeout was given, and nil
// when neither — meaning "keep the cloned default dialer".
func (c *Client) effectiveDialer() *net.Dialer {
	if c.dialer == nil && c.dialTimeout <= 0 {
		return nil
	}
	var d net.Dialer
	if c.dialer != nil {
		d = *c.dialer
	} else {
		d = net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	}
	if c.dialTimeout > 0 {
		d.Timeout = c.dialTimeout
	}
	return &d
}

// roundTripperFunc 让普通函数满足 http.RoundTripper。
// roundTripperFunc lets a plain function satisfy http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// RoundTripper 把本 Client 作为 http.RoundTripper 暴露，便于塞进别人的 *http.Client 或
// 任何接受 RoundTripper 的组件。
//
// 语义边界（重要）：它只转发到本 Client 已配置好的传输层，【不】执行本包的中间件、
// 状态码判定与响应解码——那些属于 DoHTTP/R() 的编排层。这样做是刻意的：RoundTripper 的
// 契约是"交出原始 *http.Response，body 归调用方"，若在这里读 body 或把非 2xx 变成 error，
// 就破坏了标准库的约定（含重定向、Cookie Jar 与连接复用）。需要完整编排时请用
// Client.DoHTTP 或 Request.Send。
// RoundTripper exposes this Client as an http.RoundTripper so it can be dropped into
// someone else's *http.Client or any component accepting a RoundTripper.
//
// Semantic boundary (important): it only forwards to this Client's configured
// transport and does NOT run this package's middleware, status decision or response
// decoding — those belong to the DoHTTP/R() orchestration layer. That is deliberate:
// a RoundTripper's contract is to hand back a raw *http.Response whose body belongs to
// the caller, so reading the body or turning non-2xx into an error here would break the
// standard library's assumptions (redirects, cookie jar, connection reuse). Use
// Client.DoHTTP or Request.Send when full orchestration is wanted.
func (c *Client) RoundTripper() http.RoundTripper {
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return c.httpClient.Transport.RoundTrip(req)
	})
}
