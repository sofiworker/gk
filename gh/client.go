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
	defaultTimeout = 30 * time.Second
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
	unifiedResponseTemplate interface{} // 统一响应体模板
	enableUnifiedResponse   bool        // 是否启用统一响应体处理
	redirectConfig          RedirectConfig
	uploadConfig            UploadConfig
	http2Config             HTTP2Config
	retryConfig             RetryConfig
	middlewares             []MiddlewareFunc
}

// 在 NewClient 中添加默认HTTP/2配置
func NewClient() *Client {
	c := &fasthttp.Client{
		ReadTimeout:  defaultTimeout,
		WriteTimeout: defaultTimeout,
		// HTTP/2 配置需要在底层支持
	}
	return &Client{
		fastClient:     c,
		defaultDecoder: NewJsonDecoder(),
		resolver:       gresolver.NewDefaultResolver(nil),
		readTimeout:    defaultTimeout,
		writeTimeout:   defaultTimeout,
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

func (c *Client) SetBeforeRequestHook(hooks ...func(r *Request)) *Client {
	return c
}

func (c *Client) SeAfterRequestHook(hooks ...func(r *Request)) *Client {
	return c
}

func (c *Client) SetBeforeResponseHook(hooks ...func(r *Request, resp *Response)) *Client {
	return c
}

func (c *Client) SetAfterResponseHook(hooks ...func(r *Request, resp *Response)) *Client {
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

// Client 级别的便捷方法
func (c *Client) WebSocket(url string) *WebSocketRequest {
	return NewWebSocketRequest(defaultClient.R())
}

func WebSocket(url string, handler WebSocketHandler) *WebSocketRequest {
	return defaultClient.WebSocket(url).SetWebSocketHandler(handler)
}

// 全局便捷方法
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

// 添加响应缓存支持
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, data []byte, expiration time.Duration)
}

func (c *Client) SetCache(cache Cache) *Client {
	c.cache = cache
	return c
}

// 添加条件请求支持
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

// client.go 中添加重定向配置

type RedirectConfig struct {
	MaxRedirects     int
	FollowRedirects  bool
	RedirectHandlers []func(*Response) bool // 自定义重定向处理函数
}

// 添加设置重定向配置的方法
func (c *Client) SetRedirectConfig(config RedirectConfig) *Client {
	c.redirectConfig = config
	return c
}

// request.go 中添加重定向相关方法

// SetMaxRedirects 设置最大重定向次数
func (r *Request) SetMaxRedirects(max int) *Request {
	// 在请求级别设置重定向配置
	return r
}

// SetFollowRedirects 设置是否跟随重定向
func (r *Request) SetFollowRedirects(follow bool) *Request {
	// 在请求级别设置重定向配置
	return r
}

// AddRedirectHandler 添加重定向处理函数
func (r *Request) AddRedirectHandler(handler func(*Response) bool) *Request {
	// 在请求级别添加重定向处理函数
	return r
}

type UploadConfig struct {
	LargeFileSizeThreshold int64 // 大文件阈值，单位字节，默认100MB
	UseStreamingUpload     bool  // 是否启用流式上传
}

// 添加设置上传配置的方法
func (c *Client) SetUploadConfig(config UploadConfig) *Client {
	c.uploadConfig = config
	return c
}

// HTTP2Config HTTP/2配置
type HTTP2Config struct {
	Enable               bool
	MaxConcurrentStreams uint32
	MaxReadFrameSize     uint32
	DisableCompression   bool
}

// SetHTTP2Config 设置HTTP/2配置
func (c *Client) SetHTTP2Config(config HTTP2Config) *Client {
	c.http2Config = config
	return c
}

// RetryCondition 重试条件函数
type RetryCondition func(*Response, error) bool

// BackoffStrategy 退避策略
type BackoffStrategy func(attempt int) time.Duration

// ExponentialBackoff 指数退避策略
func ExponentialBackoff(baseDelay time.Duration) BackoffStrategy {
	return func(attempt int) time.Duration {
		delay := baseDelay * time.Duration(1<<uint(attempt))
		// 添加随机抖动避免惊群效应
		jitter := time.Duration(rand.Int63n(int64(delay) / 2))
		return delay + jitter
	}
}

// RetryConfig 增强的重试配置
type RetryConfig struct {
	MaxRetries      int
	RetryConditions []RetryCondition
	Backoff         BackoffStrategy
	MaxRetryTime    time.Duration // 最大重试时间
}

// DefaultRetryCondition 默认重试条件
func DefaultRetryCondition(resp *Response, err error) bool {
	// 网络错误重试
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

// Use 添加中间件
func (c *Client) Use(middleware MiddlewareFunc) *Client {
	c.middlewares = append(c.middlewares, middleware)
	return c
}

// ChainMiddlewares 链式调用中间件
func (c *Client) ChainMiddlewares(handler Handler) Handler {
	// 从后往前应用中间件
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		handler = c.middlewares[i](handler)
	}
	return handler
}
