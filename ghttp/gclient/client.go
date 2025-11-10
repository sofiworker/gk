package gclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sofiworker/gk/gcodec"
)

var (
	defaultClient = NewClient()
)

type HTTPExecutor interface {
	Do(req *http.Request) (*http.Response, error)
}

type ClientOption func(*Client)

type Client struct {
	name       string
	baseURLRaw string
	baseURL    *url.URL

	config *Config

	httpClient *http.Client
	executor   HTTPExecutor

	logger Logger
	tracer Tracer
	cache  Cache

	retryConfig *RetryConfig

	requestMiddlewares  []RequestMiddleware
	responseMiddlewares []ResponseMiddleware

	defaultHeaders http.Header

	cookies    []*http.Cookie
	cookieMu   sync.RWMutex
	bufferPool *MultiSizeBufferPool

	codec *gcodec.HTTPCodec

	debug bool
	mu    sync.RWMutex
}

func NewClient(opts ...ClientOption) *Client {
	cfg := DefaultConfig()
	client := &Client{
		config:              cfg,
		defaultHeaders:      make(http.Header),
		bufferPool:          NewMultiSizeBufferPool(),
		retryConfig:         cfg.RetryConfig,
		logger:              newClientLogger(),
		tracer:              &NoopTracer{},
		codec:               gcodec.NewHTTPCodec(),
		requestMiddlewares:  make([]RequestMiddleware, 0),
		responseMiddlewares: make([]ResponseMiddleware, 0),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	client.init()
	return client
}

func NewClientWithConfig(config *Config, opts ...ClientOption) *Client {
	options := append([]ClientOption{WithConfig(config)}, opts...)
	return NewClient(options...)
}

func (c *Client) init() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config == nil {
		c.config = DefaultConfig()
	}
	c.config.applyDefaults()

	if c.retryConfig == nil && c.config.RetryConfig != nil {
		rc := *c.config.RetryConfig
		rc.RetryConditions = append([]RetryCondition(nil), c.config.RetryConfig.RetryConditions...)
		c.retryConfig = &rc
	}
	if c.retryConfig == nil {
		c.retryConfig = DefaultRetryConfig()
	}

	if c.tracer == nil {
		c.tracer = &NoopTracer{}
	}

	if c.bufferPool == nil {
		c.bufferPool = NewMultiSizeBufferPool()
	}

	if c.executor == nil {
		c.httpClient = c.buildHTTPClient()
		c.executor = c.httpClient
	} else if c.httpClient == nil {
		if hc, ok := c.executor.(*http.Client); ok {
			c.httpClient = hc
		}
	}

	if c.baseURLRaw != "" && c.baseURL == nil {
		if parsed, err := url.Parse(c.baseURLRaw); err == nil {
			c.baseURL = parsed
		}
	}
}

func (c *Client) buildHTTPClient() *http.Client {
	transport := c.config.Transport
	if transport == nil {
		transport = c.config.buildTransport()
	}

	httpClient := &http.Client{
		Timeout:   c.config.Timeout,
		Transport: transport,
	}

	if cfg := c.config.RedirectConfig; cfg != nil {
		if !cfg.FollowRedirects {
			httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		} else if cfg.MaxRedirects > 0 || len(cfg.RedirectHandlers) > 0 {
			max := cfg.MaxRedirects
			if max <= 0 {
				max = 10
			}
			httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				if len(via) >= max {
					return http.ErrUseLastResponse
				}
				if len(cfg.RedirectHandlers) > 0 && req.Response != nil {
					resp := c.buildRedirectResponse(req.Response)
					for _, handler := range cfg.RedirectHandlers {
						if handler != nil && !handler(resp) {
							return http.ErrUseLastResponse
						}
					}
				}
				return nil
			}
		}
	}

	return httpClient
}

func (c *Client) buildRedirectResponse(httpResp *http.Response) *Response {
	return &Response{
		client:     c,
		StatusCode: httpResp.StatusCode,
		Status:     httpResp.Status,
		Header:     httpResp.Header.Clone(),
		Request: &Request{
			Method: httpResp.Request.Method,
			URL:    httpResp.Request.URL.String(),
		},
	}
}

func (c *Client) cloneDefaultHeaders() http.Header {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make(http.Header, len(c.defaultHeaders))
	for k, v := range c.defaultHeaders {
		cp[k] = append([]string(nil), v...)
	}
	return cp
}

func (c *Client) SetName(name string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.name = name
	return c
}

func (c *Client) Name() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.name
}

func (c *Client) SetBaseUrl(baseURL string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURLRaw = baseURL
	if baseURL == "" {
		c.baseURL = nil
		return c
	}
	if parsed, err := url.Parse(baseURL); err == nil {
		c.baseURL = parsed
	}
	return c
}

func (c *Client) BaseUrl() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURLRaw
}

func (c *Client) SetLogger(logger Logger) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if logger == nil {
		c.logger = newClientLogger()
	} else {
		c.logger = logger
	}
	return c
}

func (c *Client) SetTracer(tracer Tracer) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tracer == nil {
		c.tracer = &NoopTracer{}
	} else {
		c.tracer = tracer
	}
	return c
}

func (c *Client) SetCache(cache Cache) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = cache
	return c
}

func (c *Client) SetRetryConfig(cfg *RetryConfig) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg == nil {
		c.retryConfig = DefaultRetryConfig()
	} else {
		copyCfg := *cfg
		copyCfg.RetryConditions = append([]RetryCondition(nil), cfg.RetryConditions...)
		c.retryConfig = &copyCfg
	}
	return c
}

//func (c *Client) RegisterCodec(cdc gcodec.StreamCodec) *Client {
//	c.mu.Lock()
//	defer c.mu.Unlock()
//	if c.codecManager == nil {
//		c.codecManager = gcodec.NewCodecManager()
//	}
//	c.codecManager.RegisterCodec(cdc)
//	return c
//}

func (c *Client) UseRequest(middlewares ...RequestMiddleware) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestMiddlewares = append(c.requestMiddlewares, middlewares...)
}

func (c *Client) UseResponse(middlewares ...ResponseMiddleware) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseMiddlewares = append(c.responseMiddlewares, middlewares...)
}

func (c *Client) AddDefaultHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultHeaders.Set(key, value)
}

func (c *Client) AddCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
	c.cookieMu.Lock()
	defer c.cookieMu.Unlock()
	c.cookies = append(c.cookies, cookie)
}

func (c *Client) cookiesSnapshot() []*http.Cookie {
	c.cookieMu.RLock()
	defer c.cookieMu.RUnlock()
	if len(c.cookies) == 0 {
		return nil
	}
	out := make([]*http.Cookie, len(c.cookies))
	for i, ck := range c.cookies {
		if ck == nil {
			continue
		}
		cp := *ck
		out[i] = &cp
	}
	return out
}

func (c *Client) R() *Request {
	return newRequest(c)
}

func (c *Client) execute(r *Request) (*Response, error) {
	if r == nil {
		return nil, errors.New("request is nil")
	}

	fullURL, err := r.prepareURL()
	if err != nil {
		return nil, err
	}
	r.URL = fullURL

	if err := c.applyRequestMiddleware(r); err != nil {
		return nil, err
	}

	if cached, ok, err := c.tryCacheHit(r); err == nil && ok {
		return cached, nil
	} else if err != nil {
		return nil, err
	}

	builder := newHTTPRequestBuilder(r, c)

	attempt := 0
	start := time.Now()
	var lastErr error
	var resp *Response

	for {
		httpReq, err := builder.Build()
		if err != nil {
			return nil, err
		}

		ctx := httpReq.Context()
		if ctx == nil {
			ctx = context.Background()
			httpReq = httpReq.WithContext(ctx)
		}

		var spanEnd func()
		if c.tracer != nil {
			traceCtx, end := c.tracer.StartSpan(ctx)
			httpReq = httpReq.WithContext(traceCtx)
			spanEnd = end
			c.tracer.SetAttribute("http.method", httpReq.Method)
			c.tracer.SetAttribute("http.url", httpReq.URL.String())
		}

		httpResp, execErr := c.executor.Do(httpReq)
		if spanEnd != nil {
			spanEnd()
		}

		if execErr != nil {
			lastErr = execErr
			if !c.shouldRetry(nil, execErr, attempt, time.Since(start)) {
				return nil, execErr
			}
		} else {
			resp, lastErr = c.buildResponse(r, httpResp, time.Since(start))
			if lastErr != nil {
				if !c.shouldRetry(resp, lastErr, attempt, time.Since(start)) {
					return resp, lastErr
				}
			} else {
				break
			}
		}

		attempt++
		if !c.retryDelay(ctx, attempt) {
			break
		}
	}

	if lastErr != nil {
		return resp, lastErr
	}

	if err := c.applyResponseMiddleware(resp); err != nil {
		return resp, err
	}

	c.storeCache(r, resp)
	return resp, nil
}

func (c *Client) applyRequestMiddleware(r *Request) error {
	c.mu.RLock()
	middlewares := append([]RequestMiddleware(nil), c.requestMiddlewares...)
	c.mu.RUnlock()

	for _, mw := range middlewares {
		if mw == nil {
			continue
		}
		if err := mw(c, r); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) applyResponseMiddleware(resp *Response) error {
	c.mu.RLock()
	middlewares := append([]ResponseMiddleware(nil), c.responseMiddlewares...)
	c.mu.RUnlock()
	for _, mw := range middlewares {
		if mw == nil || resp == nil {
			continue
		}
		if err := mw(c, resp); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) shouldRetry(resp *Response, err error, attempt int, elapsed time.Duration) bool {
	if c.retryConfig == nil || c.retryConfig.MaxRetries <= 0 {
		return false
	}
	if attempt >= c.retryConfig.MaxRetries {
		return false
	}
	if c.retryConfig.MaxRetryTime > 0 && elapsed >= c.retryConfig.MaxRetryTime {
		return false
	}
	for _, cond := range c.retryConfig.RetryConditions {
		if cond != nil && cond(resp, err) {
			return true
		}
	}
	return false
}

func (c *Client) retryDelay(ctx context.Context, attempt int) bool {
	if c.retryConfig == nil || c.retryConfig.Backoff == nil {
		return true
	}
	delay := c.retryConfig.Backoff(attempt)
	if delay <= 0 {
		return true
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Client) buildResponse(r *Request, httpResp *http.Response, duration time.Duration) (*Response, error) {
	if httpResp == nil {
		return nil, errors.New("nil http response")
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	buf := bytes.NewBuffer(nil)
	if httpResp.Body != nil {
		if _, err := buf.ReadFrom(httpResp.Body); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}

	response := &Response{
		client:      c,
		Request:     r,
		StatusCode:  httpResp.StatusCode,
		Status:      httpResp.Status,
		Header:      httpResp.Header.Clone(),
		Body:        buf.Bytes(),
		Duration:    duration,
		Proto:       httpResp.Proto,
		ContentType: httpResp.Header.Get("Content-Type"),
	}

	if c.logger != nil && c.config.DumpConfig != nil && c.config.DumpConfig.DumpResponse {
		c.logger.Debugf("response %s %s -> %d (%s)", r.Method, r.URL, response.StatusCode, duration)
	}

	return response, nil
}

func (c *Client) tryCacheHit(r *Request) (*Response, bool, error) {
	if c.cache == nil || !r.useCache || !strings.EqualFold(r.Method, http.MethodGet) {
		return nil, false, nil
	}
	key := r.cacheKey
	if key == "" {
		key = r.URL
	}
	data, ok := c.cache.Get(key)
	if !ok || len(data) == 0 {
		return nil, false, nil
	}
	resp, err := c.decodeCacheEntry(data, r)
	if err != nil {
		return nil, false, err
	}
	return resp, true, nil
}

func (c *Client) storeCache(r *Request, resp *Response) {
	if c.cache == nil || !r.useCache || resp == nil || !strings.EqualFold(r.Method, http.MethodGet) {
		return
	}
	key := r.cacheKey
	if key == "" {
		key = r.URL
	}
	entry, err := c.encodeCacheEntry(resp)
	if err != nil {
		return
	}
	c.cache.Set(key, entry, r.cacheTTL)
}

type cacheEntry struct {
	Status int                 `json:"status"`
	Header map[string][]string `json:"header"`
	Body   []byte              `json:"body"`
}

func (c *Client) encodeCacheEntry(resp *Response) ([]byte, error) {
	entry := cacheEntry{
		Status: resp.StatusCode,
		Header: map[string][]string(resp.Header.Clone()),
		Body:   resp.Body,
	}
	return json.Marshal(entry)
}

func (c *Client) decodeCacheEntry(data []byte, r *Request) (*Response, error) {
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &Response{
		client:      c,
		Request:     r,
		StatusCode:  entry.Status,
		Status:      http.StatusText(entry.Status),
		Header:      http.Header(entry.Header),
		Body:        entry.Body,
		Duration:    0,
		ContentType: http.Header(entry.Header).Get("Content-Type"),
	}, nil
}

func WithConfig(cfg *Config) ClientOption {
	return func(c *Client) {
		if cfg == nil {
			return
		}
		c.config = cfg
	}
}

func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
			c.executor = client
		}
	}
}

func WithExecutor(executor HTTPExecutor) ClientOption {
	return func(c *Client) {
		if executor != nil {
			c.executor = executor
			if hc, ok := executor.(*http.Client); ok {
				c.httpClient = hc
			}
		}
	}
}

func WithBaseURL(base string) ClientOption {
	return func(c *Client) {
		c.baseURLRaw = base
		if parsed, err := url.Parse(base); err == nil {
			c.baseURL = parsed
		}
	}
}

func WithLogger(logger Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func WithTracer(tracer Tracer) ClientOption {
	return func(c *Client) {
		if tracer != nil {
			c.tracer = tracer
		}
	}
}

func WithCache(cache Cache) ClientOption {
	return func(c *Client) {
		c.cache = cache
	}
}

func WithRetry(cfg *RetryConfig) ClientOption {
	return func(c *Client) {
		if cfg != nil {
			copyCfg := *cfg
			copyCfg.RetryConditions = append([]RetryCondition(nil), cfg.RetryConditions...)
			c.retryConfig = &copyCfg
		}
	}
}

func WithDefaultHeaders(headers http.Header) ClientOption {
	return func(c *Client) {
		if headers == nil {
			return
		}
		for k, v := range headers {
			c.defaultHeaders[k] = append([]string(nil), v...)
		}
	}
}

func WithCodecManager(manager gcodec.StreamCodec) ClientOption {
	return func(c *Client) {
		if manager != nil {
			//c.codecManager = manager
		}
	}
}

func WithBufferPool(pool *MultiSizeBufferPool) ClientOption {
	return func(c *Client) {
		if pool != nil {
			c.bufferPool = pool
		}
	}
}
