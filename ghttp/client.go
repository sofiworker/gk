package ghttp

import (
	"crypto/tls"
	"fmt"
	"gk/gresolver"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/valyala/fasthttp"
	"golang.org/x/exp/rand"
)

const (
	DefaultTimeout = 30 * time.Second
)

var (
	ErrInvalidPath    = fmt.Errorf("invalid path")
	ErrBaseUrlEmpty   = fmt.Errorf("baseurl is required when path is relative")
	ErrBaseUrlFormat  = fmt.Errorf("invalid baseurl")
	ErrUrlNotAbs      = fmt.Errorf("resulting url is not absolute")
	ErrDataFormat     = fmt.Errorf("data format error, only ptr data")
	ErrNotFoundMethod = fmt.Errorf("not found method")
)

var (
	defaultClient = NewClient()
)

type Middleware func(next Handler) Handler

type Handler func(*Request, *Response) error

type Client struct {
	fastClient              *fasthttp.Client
	baseUrl                 string
	tlsConfig               *tls.Config
	enableDumpRequest       bool
	enableDumpResponse      bool
	commonResponse          interface{}
	beforeRequest           []func(*Request)
	afterResponse           []func(*Request, *Response)
	defaultDecoder          Decoder
	tracer                  Tracer
	resolver                gresolver.Resolver
	readTimeout             time.Duration
	writeTimeout            time.Duration
	cache                   Cache
	unifiedResponseTemplate interface{}
	enableUnifiedResponse   bool
	redirectConfig          RedirectConfig
	uploadConfig            UploadConfig
	http2Config             HTTP2Config
	retryConfig             RetryConfig
	middlewares             []MiddlewareFunc
}

func NewClient() *Client {
	c := &fasthttp.Client{
		ReadTimeout:  DefaultTimeout,
		WriteTimeout: DefaultTimeout,
	}
	return &Client{
		fastClient:     c,
		defaultDecoder: NewJsonDecoder(),
		resolver:       gresolver.NewDefaultResolver(nil),
		readTimeout:    DefaultTimeout,
		writeTimeout:   DefaultTimeout,
		uploadConfig: UploadConfig{
			LargeFileSizeThreshold: 100 * 1024 * 1024,
			UseStreamingUpload:     true,
		},
		http2Config: HTTP2Config{
			Enable: true,
		},
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

func (c *Client) SetTLSConfig(tlsConfig *tls.Config) *Client {
	c.tlsConfig = tlsConfig
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

func (c *Client) SetCommonResponseBody(body interface{}) *Client {
	c.commonResponse = body
	return c
}

func (c *Client) SetTracer(tracer Tracer) *Client {
	c.tracer = tracer
	return c
}

func (c *Client) R() *Request {
	r := fasthttp.AcquireRequest()
	return &Request{fr: r, client: c}
}

func ConstructURL(baseurl, path string) (string, error) {
	pathURL, err := url.Parse(path)
	if err != nil {
		return "", ErrInvalidPath
	}

	if pathURL.IsAbs() {
		return pathURL.String(), nil
	}

	if baseurl == "" {
		return "", ErrBaseUrlEmpty
	}

	baseURL, err := url.Parse(baseurl)
	if err != nil {
		return "", ErrBaseUrlFormat
	}

	mergedURL := baseURL.ResolveReference(pathURL)

	if !mergedURL.IsAbs() {
		return "", ErrUrlNotAbs
	}

	return mergedURL.String(), nil
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

func (c *Client) WebSocket(url string) *WebSocketRequest {
	return NewWebSocketRequest(defaultClient.R())
}

func WebSocket(url string, handler WebSocketHandler) *WebSocketRequest {
	return defaultClient.WebSocket(url).SetWebSocketHandler(handler)
}

func SSE(url string, handler SSEHandler) *SSERequest {
	return defaultClient.SSE(url).SetSSEHandler(handler)
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

type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, data []byte, expiration time.Duration)
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

type RedirectConfig struct {
	MaxRedirects     int
	FollowRedirects  bool
	RedirectHandlers []func(*Response) bool
}

func (c *Client) SetRedirectConfig(config RedirectConfig) *Client {
	c.redirectConfig = config
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

type UploadConfig struct {
	LargeFileSizeThreshold int64
	UseStreamingUpload     bool
}

func (c *Client) SetUploadConfig(config UploadConfig) *Client {
	c.uploadConfig = config
	return c
}

type HTTP2Config struct {
	Enable               bool
	MaxConcurrentStreams uint32
	MaxReadFrameSize     uint32
	DisableCompression   bool
}

func (c *Client) SetHTTP2Config(config HTTP2Config) *Client {
	c.http2Config = config
	return c
}

type RetryCondition func(*Response, error) bool

type BackoffStrategy func(attempt int) time.Duration

func ExponentialBackoff(baseDelay time.Duration) BackoffStrategy {
	return func(attempt int) time.Duration {
		delay := baseDelay * time.Duration(1<<uint(attempt))
		// 添加随机抖动避免惊群效应
		jitter := time.Duration(rand.Int63n(int64(delay) / 2))
		return delay + jitter
	}
}

type RetryConfig struct {
	MaxRetries      int
	RetryConditions []RetryCondition
	Backoff         BackoffStrategy
	MaxRetryTime    time.Duration
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
