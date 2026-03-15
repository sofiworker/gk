package gclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sofiworker/gk/ghttp/codec"
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

	defaultHeaders        http.Header
	queryParams           url.Values
	formData              url.Values
	pathParams            map[string]string
	ctx                   context.Context
	timeout               time.Duration
	authToken             string
	authScheme            string
	basicAuthUser         string
	basicAuthPass         string
	authHeaderKey         string
	resultError           interface{}
	responseUnwrapper     ResponseUnwrapper
	responseStatusChecker ResponseStatusChecker
	responseDir           string
	saveToFile            bool

	cookies      []*http.Cookie
	cookieMu     sync.RWMutex
	bufferPool   *MultiSizeBufferPool
	codecManager *codec.Manager

	debug bool
	mu    sync.RWMutex
}

func NewClient(opts ...ClientOption) *Client {
	cfg := DefaultConfig()
	client := &Client{
		config:              cfg,
		defaultHeaders:      make(http.Header),
		queryParams:         make(url.Values),
		formData:            make(url.Values),
		pathParams:          make(map[string]string),
		ctx:                 context.Background(),
		timeout:             cfg.Timeout,
		authScheme:          "Bearer",
		authHeaderKey:       "Authorization",
		bufferPool:          NewMultiSizeBufferPool(),
		retryConfig:         cfg.RetryConfig,
		logger:              newClientLogger(),
		tracer:              &NoopTracer{},
		requestMiddlewares:  make([]RequestMiddleware, 0),
		responseMiddlewares: make([]ResponseMiddleware, 0),
		codecManager:        codec.DefaultManager().Clone(),
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

	if c.codecManager == nil {
		c.codecManager = codec.DefaultManager().Clone()
	}

	if c.executor == nil {
		c.httpClient = c.buildHTTPClient()
		c.executor = c.httpClient
	} else if c.httpClient == nil {
		if hc, ok := c.executor.(*http.Client); ok {
			c.httpClient = hc
		} else {
			c.httpClient = c.buildHTTPClient()
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

func (c *Client) SetBaseURL(baseURL string) *Client {
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

func (c *Client) BaseURL() string {
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

func (c *Client) SetHeader(key, value string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultHeaders.Set(key, value)
	return c
}

func (c *Client) SetHeaders(headers map[string]string) *Client {
	for k, v := range headers {
		c.SetHeader(k, v)
	}
	return c
}

func (c *Client) SetHeaderValues(headers map[string][]string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, values := range headers {
		c.defaultHeaders[k] = append([]string(nil), values...)
	}
	return c
}

func (c *Client) SetUserAgent(userAgent string) *Client {
	return c.SetHeader("User-Agent", userAgent)
}

func (c *Client) SetAccept(accept string) *Client {
	return c.SetHeader("Accept", accept)
}

func (c *Client) SetContext(ctx context.Context) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	c.ctx = ctx
	return c
}

func (c *Client) SetTimeout(timeout time.Duration) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = timeout
	if c.httpClient != nil && timeout > 0 {
		c.httpClient.Timeout = timeout
	}
	return c
}

func (c *Client) SetCookie(cookie *http.Cookie) *Client {
	c.AddCookie(cookie)
	return c
}

func (c *Client) SetCookies(cookies []*http.Cookie) *Client {
	for _, cookie := range cookies {
		c.AddCookie(cookie)
	}
	return c
}

func (c *Client) SetQueryParam(key, value string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryParams.Set(key, value)
	return c
}

func (c *Client) SetQueryParams(params map[string]string) *Client {
	for k, v := range params {
		c.SetQueryParam(k, v)
	}
	return c
}

func (c *Client) AddQueryParamsFromValues(values url.Values) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, items := range values {
		for _, item := range items {
			c.queryParams.Add(key, item)
		}
	}
	return c
}

func (c *Client) SetFormData(data map[string]string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range data {
		c.formData.Set(k, v)
	}
	return c
}

func (c *Client) SetFormDataFromValues(values url.Values) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, items := range values {
		for _, item := range items {
			c.formData.Add(key, item)
		}
	}
	return c
}

func (c *Client) SetPathParam(key, value string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pathParams[key] = value
	return c
}

func (c *Client) SetPathParams(params map[string]string) *Client {
	for k, v := range params {
		c.SetPathParam(k, v)
	}
	return c
}

func (c *Client) SetBasicAuth(username, password string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.basicAuthUser = username
	c.basicAuthPass = password
	return c
}

func (c *Client) SetAuthToken(token string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authToken = token
	return c
}

func (c *Client) SetAuthScheme(scheme string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(scheme) == "" {
		scheme = "Bearer"
	}
	c.authScheme = scheme
	return c
}

func (c *Client) SetHeaderAuthorizationKey(key string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(key) == "" {
		key = "Authorization"
	}
	c.authHeaderKey = key
	return c
}

func (c *Client) SetResultError(result interface{}) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resultError = result
	return c
}

func (c *Client) SetResponseSaveDirectory(dir string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseDir = dir
	return c
}

func (c *Client) SetResponseSaveToFile(save bool) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saveToFile = save
	return c
}

// RegisterCodec 注册自定义内容类型的编解码器。
func (c *Client) RegisterCodec(contentType string, cd codec.Codec) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.codecManager == nil {
		c.codecManager = codec.DefaultManager().Clone()
	}
	c.codecManager.Register(contentType, cd)
	return c
}

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
	req := newRequest(c)

	c.mu.RLock()
	req.Header = c.cloneDefaultHeaders()
	req.QueryParams = CloneURLValues(c.queryParams)
	req.FormData = CloneURLValues(c.formData)
	req.ctx = c.ctx
	req.timeout = c.timeout
	req.AuthToken = c.authToken
	req.AuthScheme = c.authScheme
	req.basicAuthUser = c.basicAuthUser
	req.basicAuthPass = c.basicAuthPass
	req.HeaderAuthorizationKey = c.authHeaderKey
	req.ResultError = c.resultError
	req.responseUnwrapper = c.responseUnwrapper
	req.responseStatusChecker = c.responseStatusChecker
	req.responseSaveDirectory = c.responseDir
	req.isResponseSaveToFile = c.saveToFile
	for k, v := range c.pathParams {
		req.PathParams[k] = v
	}
	c.mu.RUnlock()

	req.Cookies = c.cookiesSnapshot()
	return req
}

func (c *Client) NewRequest() *Request {
	return c.R()
}

func (c *Client) HTTPClient() *http.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpClient
}

func (c *Client) Clone() *Client {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := &Client{
		name:                  c.name,
		baseURLRaw:            c.baseURLRaw,
		config:                c.config,
		httpClient:            c.httpClient,
		executor:              c.executor,
		logger:                c.logger,
		tracer:                c.tracer,
		cache:                 c.cache,
		retryConfig:           c.retryConfig,
		requestMiddlewares:    append([]RequestMiddleware(nil), c.requestMiddlewares...),
		responseMiddlewares:   append([]ResponseMiddleware(nil), c.responseMiddlewares...),
		defaultHeaders:        c.defaultHeaders.Clone(),
		queryParams:           CloneURLValues(c.queryParams),
		formData:              CloneURLValues(c.formData),
		pathParams:            cloneStringMap(c.pathParams),
		ctx:                   c.ctx,
		timeout:               c.timeout,
		authToken:             c.authToken,
		authScheme:            c.authScheme,
		basicAuthUser:         c.basicAuthUser,
		basicAuthPass:         c.basicAuthPass,
		authHeaderKey:         c.authHeaderKey,
		resultError:           c.resultError,
		responseUnwrapper:     c.responseUnwrapper,
		responseStatusChecker: c.responseStatusChecker,
		responseDir:           c.responseDir,
		saveToFile:            c.saveToFile,
		bufferPool:            c.bufferPool,
		codecManager:          c.codecManager,
		debug:                 c.debug,
	}

	if c.baseURL != nil {
		baseCopy := *c.baseURL
		clone.baseURL = &baseCopy
	}
	clone.cookies = c.cookiesSnapshot()
	return clone
}

func (c *Client) SubClient(steps ...RequestStep) *Client {
	clone := c.Clone()
	if clone == nil {
		return nil
	}
	if len(steps) == 0 {
		return clone
	}
	req := clone.R()
	if err := req.Apply(steps...); err != nil {
		return clone
	}

	clone.mu.Lock()
	clone.defaultHeaders = req.Header.Clone()
	clone.queryParams = CloneURLValues(req.QueryParams)
	clone.formData = CloneURLValues(req.FormData)
	clone.pathParams = cloneStringMap(req.PathParams)
	clone.ctx = req.Context()
	clone.timeout = req.timeout
	clone.authToken = req.AuthToken
	clone.authScheme = req.AuthScheme
	clone.basicAuthUser = req.basicAuthUser
	clone.basicAuthPass = req.basicAuthPass
	clone.authHeaderKey = req.HeaderAuthorizationKey
	clone.resultError = req.ResultError
	clone.responseUnwrapper = req.responseUnwrapper
	clone.responseStatusChecker = req.responseStatusChecker
	clone.responseDir = req.responseSaveDirectory
	clone.saveToFile = req.isResponseSaveToFile
	clone.mu.Unlock()

	clone.cookieMu.Lock()
	clone.cookies = append([]*http.Cookie(nil), req.Cookies...)
	clone.cookieMu.Unlock()

	return clone
}

func (c *Client) Executor() HTTPExecutor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.executor
}

func (c *Client) Execute(method, rawURL string) (*Response, error) {
	return c.R().Execute(method, rawURL)
}

func (c *Client) Do(httpReq *http.Request) (*Response, error) {
	if httpReq == nil {
		return nil, errors.New("http request is nil")
	}
	req := c.R().FromHTTPRequest(httpReq)
	return c.execute(req)
}

func (c *Client) Get(rawURL string) (*Response, error) {
	return c.R().Get(rawURL)
}

func (c *Client) MustGet(rawURL string) *Response {
	resp, err := c.Get(rawURL)
	if err != nil {
		panic(err)
	}
	return resp
}

func (c *Client) Post(rawURL string) (*Response, error) {
	return c.R().Post(rawURL)
}

func (c *Client) MustPost(rawURL string) *Response {
	resp, err := c.Post(rawURL)
	if err != nil {
		panic(err)
	}
	return resp
}

func (c *Client) Put(rawURL string) (*Response, error) {
	return c.R().Put(rawURL)
}

func (c *Client) MustPut(rawURL string) *Response {
	resp, err := c.Put(rawURL)
	if err != nil {
		panic(err)
	}
	return resp
}

func (c *Client) Delete(rawURL string) (*Response, error) {
	return c.R().Delete(rawURL)
}

func (c *Client) MustDelete(rawURL string) *Response {
	resp, err := c.Delete(rawURL)
	if err != nil {
		panic(err)
	}
	return resp
}

func (c *Client) Patch(rawURL string) (*Response, error) {
	return c.R().Patch(rawURL)
}

func (c *Client) MustPatch(rawURL string) *Response {
	resp, err := c.Patch(rawURL)
	if err != nil {
		panic(err)
	}
	return resp
}

func (c *Client) Head(rawURL string) (*Response, error) {
	return c.R().Head(rawURL)
}

func (c *Client) MustHead(rawURL string) *Response {
	resp, err := c.Head(rawURL)
	if err != nil {
		panic(err)
	}
	return resp
}

func (c *Client) Options(rawURL string) (*Response, error) {
	return c.R().Options(rawURL)
}

func (c *Client) MustOptions(rawURL string) *Response {
	resp, err := c.Options(rawURL)
	if err != nil {
		panic(err)
	}
	return resp
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
	executor := c.effectiveExecutorForRequest(r)

	attempt := 0
	start := time.Now()
	var lastErr error
	var resp *Response

	for {
		httpReq, err := builder.Build()
		if err != nil {
			return nil, err
		}
		r.RawRequest = httpReq

		if c.logger != nil && c.config.DumpConfig != nil && c.config.DumpConfig.DumpRequest {
			if dump, dumpErr := dumpHTTPRequest(httpReq); dumpErr == nil {
				c.logger.Debugf("request dump\n%s", dump)
			} else {
				c.logger.Warnf("dump request failed: %v", dumpErr)
			}
		}

		ctx := httpReq.Context()
		if ctx == nil {
			ctx = context.Background()
			httpReq = httpReq.WithContext(ctx)
		}

		tracer := c.tracer
		if r.tracer != nil {
			tracer = r.tracer
		}

		var spanEnd func()
		if tracer != nil {
			traceCtx, end := tracer.StartSpan(ctx)
			httpReq = httpReq.WithContext(traceCtx)
			spanEnd = end
			tracer.SetAttribute("http.method", httpReq.Method)
			tracer.SetAttribute("http.url", httpReq.URL.String())
		}

		var cancel context.CancelFunc
		if r.timeout > 0 {
			ctx, cancel = context.WithTimeout(httpReq.Context(), r.timeout)
			httpReq = httpReq.WithContext(ctx)
		}

		httpResp, execErr := executor.Do(httpReq)
		if cancel != nil {
			cancel()
		}
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
			} else if c.shouldRetry(resp, nil, attempt, time.Since(start)) {
				// continue retry loop
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
		RawResponse: httpResp,
		StatusCode:  httpResp.StatusCode,
		Status:      httpResp.Status,
		Header:      httpResp.Header.Clone(),
		Body:        buf.Bytes(),
		Duration:    duration,
		Proto:       httpResp.Proto,
		ContentType: httpResp.Header.Get("Content-Type"),
	}

	if c.logger != nil && c.config.DumpConfig != nil && c.config.DumpConfig.DumpResponse {
		c.logger.Debugf("response dump\n%s", response.Dump())
	}

	if err := response.bindResult(); err != nil {
		return response, err
	}
	if err := c.maybeSaveResponseToFile(response); err != nil {
		return response, err
	}

	return response, nil
}

func (c *Client) effectiveExecutorForRequest(r *Request) HTTPExecutor {
	if !requestNeedsHTTPClientOverride(r) {
		return c.executor
	}
	if c.executor != nil && c.executor != c.httpClient {
		return c.executor
	}
	if client := c.buildRequestHTTPClient(r); client != nil {
		return client
	}
	return c.executor
}

func (c *Client) buildRequestHTTPClient(r *Request) *http.Client {
	base := c.httpClient
	if base == nil {
		base = c.buildHTTPClient()
	}

	tmp := new(http.Client)
	*tmp = *base

	if transport, ok := base.Transport.(*http.Transport); ok && transport != nil {
		cloned := transport.Clone()
		switch {
		case r.disableProxy:
			cloned.Proxy = nil
		case r.proxyFunc != nil:
			cloned.Proxy = r.proxyFunc
		case strings.TrimSpace(r.proxyURL) != "":
			if parsed, err := url.Parse(r.proxyURL); err == nil {
				cloned.Proxy = http.ProxyURL(parsed)
			}
		}
		tmp.Transport = cloned
	}

	c.applyRequestRedirectPolicy(tmp, r)

	return tmp
}

func requestNeedsHTTPClientOverride(r *Request) bool {
	if r == nil {
		return false
	}
	return r.disableProxy ||
		r.proxyFunc != nil ||
		strings.TrimSpace(r.proxyURL) != "" ||
		r.followRedirects != nil ||
		r.maxRedirects > 0 ||
		len(r.redirectHandlers) > 0
}

func (c *Client) applyRequestRedirectPolicy(httpClient *http.Client, r *Request) {
	if httpClient == nil || r == nil {
		return
	}

	follow := true
	max := 10
	var handlers []func(*Response) bool

	if c.config != nil && c.config.RedirectConfig != nil {
		follow = c.config.RedirectConfig.FollowRedirects
		if c.config.RedirectConfig.MaxRedirects > 0 {
			max = c.config.RedirectConfig.MaxRedirects
		}
		handlers = append(handlers, c.config.RedirectConfig.RedirectHandlers...)
	}
	if r.followRedirects != nil {
		follow = *r.followRedirects
	}
	if r.maxRedirects > 0 {
		max = r.maxRedirects
	}
	if len(r.redirectHandlers) > 0 {
		handlers = append(handlers, r.redirectHandlers...)
	}

	if !follow {
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
		return
	}

	if max <= 0 {
		max = 10
	}
	if max > 0 || len(handlers) > 0 {
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= max {
				return http.ErrUseLastResponse
			}
			if len(handlers) > 0 && req.Response != nil {
				resp := c.buildRedirectResponse(req.Response)
				for _, handler := range handlers {
					if handler != nil && !handler(resp) {
						return http.ErrUseLastResponse
					}
				}
			}
			return nil
		}
	}
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

func (c *Client) maybeSaveResponseToFile(resp *Response) error {
	if resp == nil || resp.Request == nil || !resp.Request.isResponseSaveToFile {
		return nil
	}

	target := resp.Request.responseSaveFileName
	if strings.TrimSpace(target) == "" {
		target = inferResponseFilename(resp)
		if dir := strings.TrimSpace(resp.Request.responseSaveDirectory); dir != "" {
			target = filepath.Join(dir, target)
		}
	}

	if dir := filepath.Dir(target); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return os.WriteFile(target, resp.Body, 0o644)
}

func inferResponseFilename(resp *Response) string {
	if resp == nil || resp.Request == nil {
		return "response.body"
	}
	if disposition := resp.HeaderGet("Content-Disposition"); disposition != "" {
		if idx := strings.Index(strings.ToLower(disposition), "filename="); idx >= 0 {
			name := strings.TrimSpace(disposition[idx+len("filename="):])
			name = strings.Trim(name, `"`)
			if name != "" {
				return name
			}
		}
	}
	if raw := resp.Request.URL; raw != "" {
		if parsed, err := url.Parse(raw); err == nil {
			base := filepath.Base(parsed.Path)
			if base != "" && base != "." && base != "/" {
				return base
			}
		}
	}
	return "response.body"
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
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

func WithCodecManager(manager *codec.Manager) ClientOption {
	return func(c *Client) {
		if manager != nil {
			c.codecManager = manager
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
