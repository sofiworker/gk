package gclient

import (
	"gk/ghttp"
	"gk/gresolver"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/valyala/fasthttp"
)

var (
	defaultClient = NewClient()
)

type DialFunc func(addr string) (net.Conn, error)

type Client struct {
	fastClient *fasthttp.Client

	Config *ghttp.Config

	BaseUrl string

	Dial DialFunc

	decoder ghttp.Decoder

	tracer ghttp.Tracer

	resolver gresolver.Resolver

	CookieJar http.CookieJar

	cache ghttp.Cache

	middlewares []ghttp.MiddlewareFunc

	logger ghttp.Log

	Version ghttp.HTTPVersion
}

func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		fastClient:  &fasthttp.Client{},
		Config:      ghttp.DefaultConfig(),
		resolver:    gresolver.NewPureGoResolver(),
		CookieJar:   jar,
		middlewares: make([]ghttp.MiddlewareFunc, 0),
		Version:     ghttp.Version11,
	}
}

func (c *Client) SetBaseUrl(baseUrl string) *Client {
	c.BaseUrl = baseUrl
	return c
}

func (c *Client) SetTimeout(timeout time.Duration) *Client {
	c.Config.ReadTimeout = timeout
	c.Config.WriteTimeout = timeout
	return c
}

func (c *Client) SetReadTimeout(timeout time.Duration) *Client {
	c.Config.ReadTimeout = timeout
	return c
}

func (c *Client) SetWriteTimeout(timeout time.Duration) *Client {
	c.Config.WriteTimeout = timeout
	return c
}

func (c *Client) SetDial(dialer DialFunc) *Client {
	c.Dial = dialer
	return c
}

func (c *Client) SetUserAgent(ua string) *Client {
	c.Config.UA = ua
	return c
}

func (c *Client) SetResolver(r gresolver.Resolver) *Client {
	c.resolver = r
	return c
}

func (c *Client) SetTracer(tracer ghttp.Tracer) *Client {
	c.tracer = tracer
	return c
}

func (c *Client) SetCache(cache ghttp.Cache) *Client {
	c.cache = cache
	return c
}

func (c *Client) SetTLSConfig(config *ghttp.TLSConfig) *Client {
	c.Config.TLSConfig = config
	return c
}

func (c *Client) SetRedirectConfig(config *ghttp.RedirectConfig) *Client {
	c.Config.RedirectConfig = config
	return c
}

func (c *Client) SetUploadConfig(config *ghttp.UploadConfig) *Client {
	c.Config.UploadConfig = config
	return c
}

func (c *Client) SetHTTP2Config(config *ghttp.HTTP2Config) *Client {
	c.Config.HTTP2Config = config
	return c
}

func (c *Client) SetRetryConfig(config *ghttp.RetryConfig) *Client {
	c.Config.RetryConfig = config
	return c
}

func (c *Client) SetDumpConfig(config *ghttp.DumpConfig) *Client {
	c.Config.DumpConfig = config
	return c
}

func (c *Client) Use(middleware ...ghttp.MiddlewareFunc) *Client {
	c.middlewares = append(c.middlewares, middleware...)
	return c
}

func (c *Client) R() *ghttp.Request {
	return &ghttp.Request{Client: c}
}

func (c *Client) WebSocket(url string) *ghttp.WebSocketRequest {
	return ghttp.NewWebSocketRequest(defaultClient.R())
}

func (c *Client) SSE(url string) *ghttp.SSERequest {
	r := c.R().SetUrl(url).SetMethod(http.MethodGet).SetHeader("Accept", "text/event-stream")
	return ghttp.NewSSERequest(r)
}

func Get(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodGet).SetUrl(url).Done()
}

func Post(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodPost).SetUrl(url).Done()
}

func Put(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodPut).SetUrl(url).Done()
}

func Delete(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodDelete).SetUrl(url).Done()
}

func Patch(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodPatch).SetUrl(url).Done()
}

func Head(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodHead).SetUrl(url).Done()
}

func Options(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodOptions).SetUrl(url).Done()
}

func Trace(url string) (*ghttp.Response, error) {
	return defaultClient.R().SetMethod(http.MethodTrace).SetUrl(url).Done()
}

func WebSocket(url string, handler ghttp.WebSocketHandler) *ghttp.WebSocketRequest {
	return defaultClient.WebSocket(url).SetWebSocketHandler(handler)
}

func SSE(url string, handler ghttp.SSEHandler) *ghttp.SSERequest {
	return defaultClient.SSE(url).SetSSEHandler(handler)
}
