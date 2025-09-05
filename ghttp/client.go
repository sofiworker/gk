package ghttp

import (
	"crypto/tls"
	"gk/gresolver"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"time"

	"github.com/valyala/fasthttp"
)

const (
	DefaultTimeout = 30 * time.Second
)

var (
	defaultClient = NewClient()
)

type Client struct {
	config *Config

	fastClient *fasthttp.Client

	baseUrl string

	decoder Decoder

	tracer Tracer

	resolver gresolver.Resolver

	readTimeout  time.Duration
	writeTimeout time.Duration

	cache Cache

	unifiedResponseTemplate interface{}

	enableUnifiedResponse bool

	middlewares []MiddlewareFunc

	enableDumpRequest  bool
	enableDumpResponse bool

	CookieJar http.CookieJar

	UA string
}

func NewClient() *Client {
	fc := &fasthttp.Client{
		ReadTimeout:  DefaultTimeout,
		WriteTimeout: DefaultTimeout,
	}
	jar, _ := cookiejar.New(nil)
	return &Client{
		fastClient:   fc,
		decoder:      NewJsonDecoder(),
		resolver:     gresolver.NewPureGoResolver(),
		readTimeout:  DefaultTimeout,
		writeTimeout: DefaultTimeout,
		config:       &Config{},
		CookieJar:    jar,
	}
}

func (c *Client) SetBaseUrl(baseUrl string) *Client {
	c.baseUrl = baseUrl
	return c
}

func (c *Client) SetTimeout(timeout time.Duration) *Client {
	c.fastClient.ReadTimeout = timeout
	c.fastClient.WriteTimeout = timeout
	return c
}

func (c *Client) SetReadTimeout(timeout time.Duration) *Client {
	c.fastClient.ReadTimeout = timeout
	return c
}

func (c *Client) SetWriteTimeout(timeout time.Duration) *Client {
	c.fastClient.WriteTimeout = timeout
	return c
}

func (c *Client) SetDial(f func(addr string) (net.Conn, error)) *Client {
	c.fastClient.Dial = f
	return c
}

func (c *Client) SetUserAgent(ua string) *Client {
	c.UA = ua
	return c
}
func (c *Client) SetTLSConfig(tlsConfig *tls.Config) *Client {
	c.config.TLSConfig = tlsConfig
	return c
}

func (c *Client) SetEnableDumpResponse(enable bool) *Client {
	c.enableDumpResponse = enable
	return c
}

func (c *Client) SetResolver(r gresolver.Resolver) *Client {
	c.resolver = r
	return c
}

func (c *Client) SetBeforeRequestHook(hooks ...Middleware) *Client {
	return c
}

func (c *Client) SeAfterRequestHook(hooks ...Middleware) *Client {
	return c
}

func (c *Client) SetBeforeResponseHook(hooks ...Middleware) *Client {
	return c
}

func (c *Client) SetAfterResponseHook(hooks ...Middleware) *Client {
	return c
}

func (c *Client) SetTracer(tracer Tracer) *Client {
	c.tracer = tracer
	return c
}

func (c *Client) R() *Request {
	return &Request{client: c}
}

func Get(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodGet).SetUrl(url).Done()
}

func Post(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodPost).SetUrl(url).Done()
}

func Put(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodPut).SetUrl(url).Done()
}

func Delete(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodDelete).SetUrl(url).Done()
}

func Patch(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodPatch).SetUrl(url).Done()
}

func Head(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodHead).SetUrl(url).Done()
}

func Options(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodOptions).SetUrl(url).Done()
}

func Trace(url string) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodTrace).SetUrl(url).Done()
}

func SendFile(url string, file *os.File) (*Response, error) {
	return defaultClient.R().SetMethod(http.MethodPut).SetUrl(url).Done()
}

func WebSocket(url string, handler WebSocketHandler) *WebSocketRequest {
	return defaultClient.WebSocket(url).SetWebSocketHandler(handler)
}

func SSE(url string, handler SSEHandler) *SSERequest {
	return defaultClient.SSE(url).SetSSEHandler(handler)
}

func (c *Client) WebSocket(url string) *WebSocketRequest {
	return NewWebSocketRequest(defaultClient.R())
}

func (c *Client) SetRetryConfig(config RetryConfig) *Client {
	return c
}

func (c *Client) SSE(url string) *SSERequest {
	r := c.R().SetUrl(url).SetMethod(http.MethodGet).SetHeader("Accept", "text/event-stream")
	return NewSSERequest(r)
}

func (c *Client) SetConnectionPool(poolSize int, idleTimeout time.Duration) *Client {
	c.fastClient.MaxConnsPerHost = poolSize
	c.fastClient.MaxIdleConnDuration = idleTimeout
	return c
}

func (c *Client) SetCache(cache Cache) *Client {
	c.cache = cache
	return c
}

func (r *Request) SetIfModifiedSince(time time.Time) *Request {
	r.fr.Header.Set("If-Modified-Since", time.Format(http.TimeFormat))
	return r
}

func (r *Request) SetIfNoneMatch(etag string) *Request {
	r.fr.Header.Set("If-None-Match", etag)
	return r
}

func (c *Client) SetUnifiedResponse(template interface{}) *Client {
	c.unifiedResponseTemplate = template
	c.enableUnifiedResponse = true
	return c
}

func (c *Client) SetRedirectConfig(config *RedirectConfig) *Client {
	c.config.RedirectConfig = config
	return c
}

func (r *Request) SetMaxRedirects(max int) *Request {
	// 在请求级别设置重定向配置
	return r
}

func (r *Request) SetFollowRedirects(follow bool) *Request {
	// 在请求级别设置重定向配置
	return r
}

func (r *Request) AddRedirectHandler(handler func(*Response) bool) *Request {
	// 在请求级别添加重定向处理函数
	return r
}

func (c *Client) SetUploadConfig(config *UploadConfig) *Client {
	c.config.UploadConfig = config
	return c
}

func (c *Client) SetHTTP2Config(config *HTTP2Config) *Client {
	c.config.HTTP2Config = config
	return c
}

func DefaultRetryCondition(resp *Response, err error) bool {

	if err != nil {
		return true
	}

	// 5xx 服务器错误重试
	if resp.fResp.StatusCode() >= 500 {
		return true
	}

	// 429 限流重试
	if resp.fResp.StatusCode() == 429 {
		return true
	}

	return false
}

func (c *Client) Use(middleware MiddlewareFunc) *Client {
	c.middlewares = append(c.middlewares, middleware)
	return c
}

func (c *Client) ChainMiddlewares(handler Handler) Handler {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		handler = c.middlewares[i](handler)
	}
	return handler
}
