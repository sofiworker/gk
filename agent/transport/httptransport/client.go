// Package httptransport 提供独立于模型与厂商协议的 HTTP 传输。
// Package httptransport provides HTTP transport independent of model and vendor protocols.
package httptransport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrConfig    = errors.New("invalid HTTP configuration")
	ErrResponse  = errors.New("invalid HTTP response")
	ErrLimit     = errors.New("HTTP size limit exceeded")
	ErrTransport = errors.New("HTTP transport failed")
)

type Request struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}
type config struct {
	headers   http.Header
	transport http.RoundTripper
	timeout   time.Duration
	limit     int64
}
type Option func(*config) error

func WithHeaders(h http.Header) Option {
	return func(c *config) error { c.headers = h.Clone(); return nil }
}
func WithTransport(t http.RoundTripper) Option {
	return func(c *config) error {
		if t == nil {
			return ErrConfig
		}
		c.transport = t
		return nil
	}
}
func WithLimits(timeout time.Duration, bytes int64) Option {
	return func(c *config) error {
		if timeout <= 0 || bytes < 1 || bytes > 1<<30 {
			return ErrConfig
		}
		c.timeout = timeout
		c.limit = bytes
		return nil
	}
}

type Client struct {
	cfg  config
	http *http.Client
}

func New(options ...Option) (*Client, error) {
	c := config{timeout: 60 * time.Second, limit: 8 << 20, headers: make(http.Header)}
	for _, o := range options {
		if o == nil {
			return nil, ErrConfig
		}
		if err := o(&c); err != nil {
			return nil, err
		}
	}
	if c.transport == nil {
		t := &http.Transport{ForceAttemptHTTP2: true, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second}
		t.Proxy = nil
		c.transport = t
	}
	return &Client{cfg: c, http: &http.Client{Transport: c.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Do(ctx context.Context, input Request) (Response, error) {
	u, err := url.Parse(input.URL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || input.Method == "" {
		return Response{}, ErrConfig
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.timeout)
	defer cancel()
	data := input.Body
	if int64(len(data)) > c.cfg.limit {
		return Response{}, ErrLimit
	}
	req, err := http.NewRequestWithContext(ctx, input.Method, input.URL, bytes.NewReader(data))
	if err != nil {
		return Response{}, ErrConfig
	}
	req.Header = c.cfg.headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	for k, values := range input.Header {
		req.Header[k] = append([]string(nil), values...)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, ErrTransport
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, c.cfg.limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, ErrResponse
	}
	if int64(len(data)) > c.cfg.limit {
		return Response{}, ErrLimit
	}
	return Response{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: data}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
