// Package httpgate 提供按规则授权、无环境代理的 HTTP 出口。
// Package httpgate provides policy-authorized HTTP without environment proxies.
package httpgate

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	s "github.com/sofiworker/gk/gai/sandbox"
)

type Rule struct {
	Scheme       string
	Host         string
	Port         uint16
	Methods      []string
	AllowPrivate bool
	Headers      http.Header
}
type config struct {
	rules                       []Rule
	timeout                     time.Duration
	requestBytes, responseBytes int64
	concurrency, redirects      int
	resolver                    *net.Resolver
}
type Option func(*config) error

func WithRules(rules ...Rule) Option {
	return func(c *config) error {
		for _, r := range rules {
			if (r.Scheme != "http" && r.Scheme != "https") || r.Host == "" || strings.ContainsAny(r.Host, "/*@%\\\x00") || len(r.Methods) == 0 {
				return s.ErrPath
			}
			r.Host = strings.ToLower(r.Host)
			if r.Port == 0 {
				r.Port = 80
				if r.Scheme == "https" {
					r.Port = 443
				}
			}
			r.Methods = append([]string(nil), r.Methods...)
			r.Headers = r.Headers.Clone()
			for _, m := range r.Methods {
				if !method(m) {
					return s.ErrPath
				}
			}
			for k := range r.Headers {
				if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Proxy-Authorization") {
					return s.ErrDenied
				}
			}
			c.rules = append(c.rules, r)
		}
		return nil
	}
}
func WithLimits(requestBytes, responseBytes int64, concurrency int, timeout time.Duration) Option {
	return func(c *config) error {
		if requestBytes <= 0 || responseBytes <= 0 || concurrency <= 0 || timeout <= 0 {
			return s.ErrQuota
		}
		c.requestBytes = requestBytes
		c.responseBytes = responseBytes
		c.concurrency = concurrency
		c.timeout = timeout
		return nil
	}
}
func WithRedirects(max int) Option {
	return func(c *config) error {
		if max < 0 || max > 20 {
			return s.ErrQuota
		}
		c.redirects = max
		return nil
	}
}

// WithResolver 只替换解析器，解析所得地址仍受策略检查。
// WithResolver only replaces name resolution; resolved addresses remain policy-checked.
func WithResolver(r *net.Resolver) Option {
	return func(c *config) error {
		if r == nil {
			return s.ErrUnsupported
		}
		c.resolver = r
		return nil
	}
}

type Client struct {
	cfg   config
	slots chan struct{}
}

func New(options ...Option) (*Client, error) {
	c := config{timeout: 30 * time.Second, requestBytes: 1 << 20, responseBytes: 8 << 20, concurrency: 8, resolver: net.DefaultResolver}
	for _, o := range options {
		if o == nil {
			return nil, s.ErrUnsupported
		}
		if err := o(&c); err != nil {
			return nil, err
		}
	}
	return &Client{cfg: c, slots: make(chan struct{}, c.concurrency)}, nil
}
func method(m string) bool {
	if m == "" {
		return false
	}
	for _, c := range m {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
func (c *Client) rule(u *url.URL, m string) (Rule, error) {
	if u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Hostname() == "" {
		return Rule{}, s.ErrDenied
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return Rule{}, s.ErrDenied
		}
		port = n
	}
	for _, r := range c.cfg.rules {
		if r.Scheme == u.Scheme && r.Host == strings.ToLower(u.Hostname()) && int(r.Port) == port {
			for _, allowed := range r.Methods {
				if allowed == m {
					return r, nil
				}
			}
		}
	}
	return Rule{}, s.ErrDenied
}
func public(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !netip.MustParsePrefix("100.64.0.0/10").Contains(ip) && !netip.MustParsePrefix("198.18.0.0/15").Contains(ip) && !netip.MustParsePrefix("192.0.0.0/24").Contains(ip) && !netip.MustParsePrefix("2001:db8::/32").Contains(ip)
}
func (c *Client) Send(ctx context.Context, in s.HTTPRequest) (s.HTTPResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return s.HTTPResponse{}, ctx.Err()
	}
	if int64(len(in.Body)) > c.cfg.requestBytes {
		return s.HTTPResponse{}, s.ErrQuota
	}
	if !method(in.Method) {
		return s.HTTPResponse{}, s.ErrPath
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return s.HTTPResponse{}, s.ErrPath
	}
	headers := in.Header.Clone()
	body := append([]byte(nil), in.Body...)
	m := in.Method
	for hop := 0; ; hop++ {
		rule, err := c.rule(u, m)
		if err != nil {
			return s.HTTPResponse{}, err
		}
		ips, err := c.cfg.resolver.LookupNetIP(ctx, "ip", u.Hostname())
		if err != nil {
			return s.HTTPResponse{}, fmt.Errorf("resolve destination: %w", s.ErrDenied)
		}
		if len(ips) == 0 {
			return s.HTTPResponse{}, s.ErrDenied
		}
		for _, ip := range ips {
			if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
				return s.HTTPResponse{}, s.ErrDenied
			}
			if !rule.AllowPrivate && !public(ip) {
				return s.HTTPResponse{}, s.ErrDenied
			}
		}
		transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, MaxResponseHeaderBytes: 64 << 10}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(host, u.Hostname()) || port != strconv.Itoa(int(rule.Port)) {
				return nil, s.ErrDenied
			}
			var last error
			for _, ip := range ips {
				conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		}
		req, err := http.NewRequestWithContext(ctx, m, u.String(), bytes.NewReader(body))
		if err != nil {
			return s.HTTPResponse{}, s.ErrPath
		}
		req.Header = headers.Clone()
		if req.Header == nil {
			req.Header = make(http.Header)
		}
		req.Header.Del("Proxy-Authorization")
		for k, values := range rule.Headers {
			req.Header.Del(k)
			for _, v := range values {
				req.Header.Add(k, v)
			}
		}
		response, err := transport.RoundTrip(req)
		if err != nil {
			transport.CloseIdleConnections()
			return s.HTTPResponse{}, s.ErrUnknown
		}
		redirect := response.StatusCode == 301 || response.StatusCode == 302 || response.StatusCode == 303 || response.StatusCode == 307 || response.StatusCode == 308
		location := response.Header.Get("Location")
		if redirect && location != "" {
			_ = response.Body.Close()
			transport.CloseIdleConnections()
			if hop >= c.cfg.redirects {
				return s.HTTPResponse{}, s.ErrDenied
			}
			next, err := u.Parse(location)
			if err != nil {
				return s.HTTPResponse{}, s.ErrPath
			}
			u = next
			// 跳转不转发调用方头，新目标按自己的规则注入凭据。
			// Redirects forward no caller headers; the destination injects its own credentials.
			headers = make(http.Header)
			if response.StatusCode == 303 && m != "HEAD" || ((response.StatusCode == 301 || response.StatusCode == 302) && m == "POST") {
				m = "GET"
				body = nil
				headers.Del("Content-Type")
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, c.cfg.responseBytes+1))
		_ = response.Body.Close()
		transport.CloseIdleConnections()
		if readErr != nil {
			return s.HTTPResponse{}, s.ErrUnknown
		}
		if int64(len(data)) > c.cfg.responseBytes {
			return s.HTTPResponse{}, s.ErrQuota
		}
		return s.HTTPResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: data}, nil
	}
}
