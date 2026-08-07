package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/sofiworker/gk/gretry"
)

// Client is an HTTP client with a go-resty-style chain API, plus typed
// generic endpoints. Request lifecycle (hooks, retry, binding, output)
// mirrors the practical features of go-resty and imroc/req.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	headers     http.Header
	queryParams url.Values
	pathParams  map[string]string
	cookies     []*http.Cookie
	authToken   string
	authScheme  string
	debug       bool
	logger      Logger
	codecMgr    *CodecManager

	retryCount       int
	retryWaitTime    time.Duration
	retryMaxWaitTime time.Duration
	retryConditions  []RetryConditionFunc
	beforeRequest    []RequestHook
	afterResponse    []ResponseHook
}

// ClientOption configures a Client.
type ClientOption func(*Client)

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		headers:          make(http.Header),
		queryParams:      make(url.Values),
		pathParams:       make(map[string]string),
		retryWaitTime:    DefaultRetryWaitTime,
		retryMaxWaitTime: DefaultRetryMaxWaitTime,
		codecMgr:         NewCodecManager(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// RetryConditionFunc decides whether a request attempt should be retried.
// It receives the response (nil on transport errors) and the error.
type RetryConditionFunc func(*Response, error) bool

// RequestHook runs before a request is sent and may mutate the request.
type RequestHook func(*Request) error

// ResponseHook runs after a response is received (body already parsed for
// non-stream responses) and may mutate the response.
type ResponseHook func(*Response) error

// DefaultRetryWaitTime is the initial retry backoff when SetRetryCount is
// used without an explicit wait time.
const DefaultRetryWaitTime = 100 * time.Millisecond

// DefaultRetryMaxWaitTime caps the exponential retry backoff.
const DefaultRetryMaxWaitTime = 2 * time.Second

func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithTransport(transport http.RoundTripper) ClientOption {
	return func(c *Client) {
		if transport == nil {
			return
		}
		if c.httpClient == nil {
			c.httpClient = &http.Client{}
		}
		c.httpClient.Transport = transport
	}
}

// WithClientLogger sets the logger used by client debug output.
func WithClientLogger(logger Logger) ClientOption {
	return func(c *Client) { c.logger = logger }
}

// R returns a new Request with the client's defaults.
func (c *Client) R() *Request {
	r := &Request{
		client:      c,
		Header:      make(http.Header),
		QueryParams: make(url.Values),
		PathParams:  make(map[string]string),
		FormData:    make(url.Values),
		Cookies:     []*http.Cookie{},
		FileFields:  []*FileField{},
	}
	for k, v := range c.headers {
		r.Header[k] = v
	}
	for k, v := range c.queryParams {
		r.QueryParams[k] = v
	}
	for k, v := range c.pathParams {
		r.PathParams[k] = v
	}
	return r
}

func (c *Client) SetBaseURL(baseURL string) *Client {
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

func (c *Client) SetHeader(key, value string) *Client {
	c.headers.Set(key, value)
	return c
}

func (c *Client) SetHeaders(headers map[string]string) *Client {
	for k, v := range headers {
		c.headers.Set(k, v)
	}
	return c
}

func (c *Client) SetAuthToken(token string) *Client {
	c.authToken = token
	c.authScheme = "Bearer"
	return c
}

func (c *Client) SetTimeout(d time.Duration) *Client {
	c.httpClient.Timeout = d
	return c
}

func (c *Client) SetDebug(debug bool) *Client {
	c.debug = debug
	return c
}

// SetLogger sets the logger used by client debug output.
func (c *Client) SetLogger(logger Logger) *Client {
	c.logger = logger
	return c
}

// SetRetryCount sets how many times a request is retried. The default retry
// condition is transport error or status >= 500; override with
// SetRetryConditions.
func (c *Client) SetRetryCount(count int) *Client {
	c.retryCount = count
	return c
}

// SetRetryWaitTime sets the initial retry backoff.
func (c *Client) SetRetryWaitTime(d time.Duration) *Client {
	if d > 0 {
		c.retryWaitTime = d
	}
	return c
}

// SetRetryMaxWaitTime caps the exponential retry backoff.
func (c *Client) SetRetryMaxWaitTime(d time.Duration) *Client {
	if d > 0 {
		c.retryMaxWaitTime = d
	}
	return c
}

// SetRetryConditions replaces the default retry condition. A request is
// retried while any condition returns true.
func (c *Client) SetRetryConditions(conditions ...RetryConditionFunc) *Client {
	c.retryConditions = append([]RetryConditionFunc(nil), conditions...)
	return c
}

// OnBeforeRequest registers request-level hooks run before each attempt.
func (c *Client) OnBeforeRequest(hooks ...RequestHook) *Client {
	c.beforeRequest = append(c.beforeRequest, hooks...)
	return c
}

// OnAfterResponse registers response-level hooks run after each attempt.
func (c *Client) OnAfterResponse(hooks ...ResponseHook) *Client {
	c.afterResponse = append(c.afterResponse, hooks...)
	return c
}

// SetCookie adds a cookie sent with every request.
func (c *Client) SetCookie(cookie *http.Cookie) *Client {
	if cookie != nil {
		c.cookies = append(c.cookies, cookie)
	}
	return c
}

// SetCookies adds multiple cookies sent with every request.
func (c *Client) SetCookies(cookies []*http.Cookie) *Client {
	for _, cookie := range cookies {
		c.SetCookie(cookie)
	}
	return c
}

// Request is a go-resty-style request builder.
type Request struct {
	client *Client
	Method string
	URL    string
	ctx    context.Context

	Header             http.Header
	QueryParams        url.Values
	PathParams         map[string]string
	FormData           url.Values
	Body               interface{}
	Result             interface{}
	ResultError        interface{}
	Error              interface{}
	Output             string
	StreamResponse     bool
	DoNotParseResponse bool
	Cookies            []*http.Cookie
	AuthToken          string
	AuthScheme         string
	BasicAuthUser      string
	BasicAuthPass      string
	Timeout            time.Duration
	FileFields         []*FileField

	RetryCount       int
	RetryWaitTime    time.Duration
	RetryMaxWaitTime time.Duration
	RetryConditions  []RetryConditionFunc
	beforeRequest    []RequestHook
	afterResponse    []ResponseHook
	queryString      string
}

// FileField represents a file upload field.
type FileField struct {
	Param    string
	FilePath string
	FileName string
	Reader   io.Reader
}

func (r *Request) SetHeader(key, value string) *Request {
	r.Header.Set(key, value)
	return r
}

func (r *Request) SetHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func (r *Request) SetQueryParam(key, value string) *Request {
	r.QueryParams.Set(key, value)
	return r
}

func (r *Request) SetQueryParams(params map[string]string) *Request {
	for k, v := range params {
		r.QueryParams.Set(k, v)
	}
	return r
}

// SetQueryParamsFromValues sets query parameters from a url.Values.
func (r *Request) SetQueryParamsFromValues(params url.Values) *Request {
	for k, values := range params {
		for _, v := range values {
			r.QueryParams.Add(k, v)
		}
	}
	return r
}

// SetQueryString appends a raw query string (already URL-encoded) to the URL.
func (r *Request) SetQueryString(query string) *Request {
	r.queryString = query
	return r
}

func (r *Request) SetPathParam(key, value string) *Request {
	r.PathParams[key] = value
	return r
}

func (r *Request) SetPathParams(params map[string]string) *Request {
	for k, v := range params {
		r.PathParams[k] = v
	}
	return r
}

func (r *Request) SetBody(body interface{}) *Request {
	r.Body = body
	return r
}

func (r *Request) SetJSONBody(body interface{}) *Request {
	r.Body = body
	r.Header.Set("Content-Type", "application/json")
	return r
}

func (r *Request) SetXMLBody(body interface{}) *Request {
	r.Body = body
	r.Header.Set("Content-Type", "application/xml")
	return r
}

func (r *Request) SetFormData(data map[string]string) *Request {
	for k, v := range data {
		r.FormData.Set(k, v)
	}
	return r
}

func (r *Request) SetFile(param, filePath string) *Request {
	r.FileFields = append(r.FileFields, &FileField{
		Param:    param,
		FilePath: filePath,
	})
	return r
}

func (r *Request) SetFileReader(param, fileName string, reader io.Reader) *Request {
	r.FileFields = append(r.FileFields, &FileField{
		Param:    param,
		FileName: fileName,
		Reader:   reader,
	})
	return r
}

func (r *Request) SetResult(result interface{}) *Request {
	r.Result = result
	return r
}

// SetError sets the target for automatic non-2xx response binding.
func (r *Request) SetError(err interface{}) *Request {
	r.Error = err
	return r
}

// SetOutput writes the response body to the given file path.
func (r *Request) SetOutput(path string) *Request {
	r.Output = path
	return r
}

func (r *Request) SetStreamResponse(stream bool) *Request {
	r.StreamResponse = stream
	return r
}

// SetDoNotParseResponse is an alias for SetStreamResponse(true).
func (r *Request) SetDoNotParseResponse(stream bool) *Request {
	r.StreamResponse = stream
	r.DoNotParseResponse = stream
	return r
}

func (r *Request) SetContext(ctx context.Context) *Request {
	r.ctx = ctx
	return r
}

// SetTimeout overrides the client timeout for this request.
func (r *Request) SetTimeout(d time.Duration) *Request {
	r.Timeout = d
	return r
}

func (r *Request) SetAuthToken(token string) *Request {
	r.AuthToken = token
	r.AuthScheme = "Bearer"
	return r
}

// SetAuthScheme overrides the auth scheme used with SetAuthToken.
func (r *Request) SetAuthScheme(scheme string) *Request {
	r.AuthScheme = scheme
	return r
}

// SetBasicAuth sets HTTP Basic authentication for this request.
func (r *Request) SetBasicAuth(user, pass string) *Request {
	r.BasicAuthUser = user
	r.BasicAuthPass = pass
	return r
}

func (r *Request) SetCookie(cookie *http.Cookie) *Request {
	if cookie != nil {
		r.Cookies = append(r.Cookies, cookie)
	}
	return r
}

// SetCookies adds multiple cookies to the request.
func (r *Request) SetCookies(cookies []*http.Cookie) *Request {
	for _, cookie := range cookies {
		r.SetCookie(cookie)
	}
	return r
}

func (r *Request) SetContentType(ct string) *Request {
	r.Header.Set("Content-Type", ct)
	return r
}

// SetRetryCount overrides the client retry count for this request.
func (r *Request) SetRetryCount(count int) *Request {
	r.RetryCount = count
	return r
}

// SetRetryWaitTime overrides the client retry wait for this request.
func (r *Request) SetRetryWaitTime(d time.Duration) *Request {
	if d > 0 {
		r.RetryWaitTime = d
	}
	return r
}

// SetRetryMaxWaitTime overrides the client retry backoff cap for this request.
func (r *Request) SetRetryMaxWaitTime(d time.Duration) *Request {
	if d > 0 {
		r.RetryMaxWaitTime = d
	}
	return r
}

// SetRetryConditions overrides the client retry conditions for this request.
func (r *Request) SetRetryConditions(conditions ...RetryConditionFunc) *Request {
	r.RetryConditions = append([]RetryConditionFunc(nil), conditions...)
	return r
}

// OnBeforeRequest registers request-level hooks for this request only.
func (r *Request) OnBeforeRequest(hooks ...RequestHook) *Request {
	r.beforeRequest = append(r.beforeRequest, hooks...)
	return r
}

// OnAfterResponse registers response-level hooks for this request only.
func (r *Request) OnAfterResponse(hooks ...ResponseHook) *Request {
	r.afterResponse = append(r.afterResponse, hooks...)
	return r
}

func (r *Request) Execute(method, url string) (*Response, error) {
	r.Method = method
	r.URL = url
	return r.client.execute(r)
}

func (r *Request) Get(url string) (*Response, error) {
	return r.Execute(http.MethodGet, url)
}

func (r *Request) Post(url string) (*Response, error) {
	return r.Execute(http.MethodPost, url)
}

func (r *Request) Put(url string) (*Response, error) {
	return r.Execute(http.MethodPut, url)
}

func (r *Request) Delete(url string) (*Response, error) {
	return r.Execute(http.MethodDelete, url)
}

func (r *Request) Patch(url string) (*Response, error) {
	return r.Execute(http.MethodPatch, url)
}

func (r *Request) Head(url string) (*Response, error) {
	return r.Execute(http.MethodHead, url)
}

func (c *Client) execute(r *Request) (*Response, error) {
	start := time.Now()

	urlStr := r.URL
	for k, v := range r.PathParams {
		urlStr = strings.ReplaceAll(urlStr, "{"+k+"}", url.PathEscape(v))
		urlStr = strings.ReplaceAll(urlStr, ":"+k, url.PathEscape(v))
	}

	if c.baseURL != "" && !strings.HasPrefix(urlStr, "http") {
		urlStr = c.baseURL + "/" + strings.TrimLeft(urlStr, "/")
	}

	if len(r.QueryParams) > 0 {
		if strings.Contains(urlStr, "?") {
			urlStr += "&" + r.QueryParams.Encode()
		} else {
			urlStr += "?" + r.QueryParams.Encode()
		}
	}
	if r.queryString != "" {
		sep := "?"
		if strings.Contains(urlStr, "?") {
			sep = "&"
		}
		urlStr += sep + strings.TrimPrefix(r.queryString, "?")
	}

	var bodyBytes []byte
	contentType := r.Header.Get("Content-Type")

	if len(r.FileFields) > 0 {
		return c.executeMultipart(r, urlStr, start)
	}

	if len(r.FormData) > 0 && contentType == "" {
		contentType = "application/x-www-form-urlencoded"
		bodyBytes = []byte(r.FormData.Encode())
	} else if r.Body != nil {
		switch contentType {
		case "application/xml":
			data, err := xml.Marshal(r.Body)
			if err != nil {
				return nil, err
			}
			bodyBytes = data
		default:
			data, err := json.Marshal(r.Body)
			if err != nil {
				return nil, err
			}
			bodyBytes = data
			if contentType == "" {
				contentType = "application/json"
			}
		}
	}
	return c.send(r, urlStr, bodyBytes, contentType, start)
}

func (c *Client) send(r *Request, urlStr string, bodyBytes []byte, contentType string, start time.Time) (*Response, error) {
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	attempts := 1 + c.retryCount
	if r.RetryCount > 0 {
		attempts = 1 + r.RetryCount
	}
	wait := c.retryWaitTime
	if r.RetryWaitTime > 0 {
		wait = r.RetryWaitTime
	}
	maxWait := c.retryMaxWaitTime
	if r.RetryMaxWaitTime > 0 {
		maxWait = r.RetryMaxWaitTime
	}
	conditions := c.retryConditions
	if len(r.RetryConditions) > 0 {
		conditions = r.RetryConditions
	}

	var lastResp *Response
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := gretry.Wait(ctx, wait); err != nil {
				return nil, err
			}
			wait = gretry.NextDelay(attempt-1, gretry.ErrorHandlingOptions{
				RetryStrategy:     gretry.RetryStrategyExponential,
				RetryDelay:        wait,
				MaxRetryDelay:     maxWait,
				BackoffMultiplier: 2,
				JitterType:        gretry.JitterNone,
			})
		}
		var body io.Reader
		if bodyBytes != nil {
			body = bytes.NewReader(bodyBytes)
		}

		httpReq, err := http.NewRequestWithContext(ctx, r.Method, urlStr, body)
		if err != nil {
			return nil, err
		}

		for _, hook := range c.beforeRequest {
			if hook != nil {
				if err := hook(r); err != nil {
					return nil, err
				}
			}
		}
		for _, hook := range r.beforeRequest {
			if hook != nil {
				if err := hook(r); err != nil {
					return nil, err
				}
			}
		}

		for k, v := range r.Header {
			for _, vv := range v {
				httpReq.Header.Add(k, vv)
			}
		}
		if contentType != "" && httpReq.Header.Get("Content-Type") == "" {
			httpReq.Header.Set("Content-Type", contentType)
		}
		if r.AuthToken != "" {
			httpReq.Header.Set("Authorization", r.AuthScheme+" "+r.AuthToken)
		}
		if r.BasicAuthUser != "" {
			httpReq.SetBasicAuth(r.BasicAuthUser, r.BasicAuthPass)
		}
		cookies := append([]*http.Cookie(nil), c.cookies...)
		cookies = append(cookies, r.Cookies...)
		for _, cookie := range cookies {
			httpReq.AddCookie(cookie)
		}

		httpResp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			if shouldRetry(nil, err, conditions) && attempt+1 < attempts {
				continue
			}
			return nil, err
		}
		resp := &Response{
			StatusCode:  httpResp.StatusCode,
			Status:      httpResp.Status,
			Header:      httpResp.Header,
			Duration:    time.Since(start),
			receivedAt:  time.Now(),
			Request:     r,
			RawResponse: httpResp,
		}
		lastResp = resp

		if r.StreamResponse || r.DoNotParseResponse {
			resp.rawBody = httpResp.Body
		} else {
			body, readErr := io.ReadAll(httpResp.Body)
			_ = httpResp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				if shouldRetry(resp, readErr, conditions) && attempt+1 < attempts {
					continue
				}
				return nil, readErr
			}
			resp.Body = body
			if r.Result != nil && resp.IsSuccess() {
				if err := resp.BindJSON(r.Result); err != nil {
					return nil, fmt.Errorf("bind response body to %T: %w", r.Result, err)
				}
			}
			if !resp.IsSuccess() && r.Error != nil {
				if bindErr := resp.BindJSON(r.Error); bindErr == nil {
					resp.errValue = r.Error
				}
			}
			if r.Output != "" {
				if err := os.WriteFile(r.Output, body, 0o644); err != nil {
					return nil, err
				}
			}
		}

		for _, hook := range c.afterResponse {
			if hook != nil {
				if err := hook(resp); err != nil {
					return nil, err
				}
			}
		}
		for _, hook := range r.afterResponse {
			if hook != nil {
				if err := hook(resp); err != nil {
					return nil, err
				}
			}
		}

		if c.debug && c.logger != nil {
			c.logger.InfoContext(ctx, "http client request",
				"method", r.Method,
				"url", urlStr,
				"status", resp.StatusCode,
				"duration", resp.Duration,
				"size", resp.Size(),
			)
		}

		if shouldRetry(resp, nil, conditions) && attempt+1 < attempts {
			if resp.rawBody != nil {
				_ = resp.rawBody.Close()
			}
			continue
		}
		return resp, nil
	}
	return lastResp, lastErr
}

func shouldRetry(resp *Response, err error, conditions []RetryConditionFunc) bool {
	if len(conditions) == 0 {
		return err != nil || (resp != nil && resp.StatusCode >= 500)
	}
	for _, cond := range conditions {
		if cond != nil && cond(resp, err) {
			return true
		}
	}
	return false
}

func (c *Client) executeMultipart(r *Request, urlStr string, start time.Time) (*Response, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for k, v := range r.FormData {
		for _, vv := range v {
			_ = writer.WriteField(k, vv)
		}
	}

	for _, f := range r.FileFields {
		var fileReader io.Reader
		var fileName string

		if f.Reader != nil {
			fileReader = f.Reader
			fileName = f.FileName
		} else {
			file, err := os.Open(f.FilePath)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			fileReader = file
			fileName = filepath.Base(f.FilePath)
		}

		part, err := writer.CreateFormFile(f.Param, fileName)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(part, fileReader)
		if err != nil {
			return nil, err
		}
	}

	_ = writer.Close()
	return c.send(r, urlStr, buf.Bytes(), writer.FormDataContentType(), start)
}

// Response represents an HTTP response.
type Response struct {
	StatusCode  int
	Status      string
	Header      http.Header
	Body        []byte
	Duration    time.Duration
	receivedAt  time.Time
	Request     *Request
	RawResponse *http.Response
	rawBody     io.ReadCloser
	errValue    interface{}
}

func (r *Response) String() string  { return string(r.Body) }
func (r *Response) Bytes() []byte   { return r.Body }
func (r *Response) IsSuccess() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }
func (r *Response) IsError() bool   { return !r.IsSuccess() }

// Time returns the total request duration.
func (r *Response) Time() time.Duration { return r.Duration }

// ReceivedAt returns when the response was received.
func (r *Response) ReceivedAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.receivedAt
}

// Size returns the buffered response body size.
func (r *Response) Size() int64 {
	if r == nil {
		return 0
	}
	return int64(len(r.Body))
}

// Cookies returns the Set-Cookie headers of the raw response.
func (r *Response) Cookies() []*http.Cookie {
	if r == nil || r.RawResponse == nil {
		return nil
	}
	return r.RawResponse.Cookies()
}

// Error returns the value bound from a non-2xx body via Request.SetError.
func (r *Response) Error() interface{} {
	if r == nil {
		return nil
	}
	return r.errValue
}

// Result returns the value bound from a 2xx body via Request.SetResult.
func (r *Response) Result() interface{} {
	if r == nil || r.Request == nil {
		return nil
	}
	return r.Request.Result
}

func (r *Response) RawBody() io.ReadCloser {
	if r == nil {
		return io.NopCloser(bytes.NewReader(nil))
	}
	if r.rawBody != nil {
		return r.rawBody
	}
	return io.NopCloser(bytes.NewReader(r.Body))
}

func (r *Response) BindJSON(target interface{}) error {
	return json.Unmarshal(r.Body, target)
}

// Unmarshal decodes the response body based on its Content-Type (XML or
// JSON; JSON is the fallback).
func (r *Response) Unmarshal(target interface{}) error {
	if r == nil {
		return fmt.Errorf("response is nil")
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if strings.Contains(contentType, "xml") {
		return xml.Unmarshal(r.Body, target)
	}
	return json.Unmarshal(r.Body, target)
}

func (r *Response) UnwrapEnvelope(target interface{}) error {
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.Body, &env); err != nil {
		return err
	}
	if env.Data != nil {
		return json.Unmarshal(env.Data, target)
	}
	return json.Unmarshal(r.Body, target)
}

// Endpoint generic callers

func Do[Req, Resp any](ctx context.Context, client *Client, method, path string, req *Req) (*Resp, error) {

	// For generic endpoints, we need to send just the Body field content
	// because the server's parseBody decodes into the Body nested struct.
	// If Req has a Body field, marshal just that; otherwise marshal the whole request.
	var bodyReader io.Reader
	if req != nil {
		v := reflect.ValueOf(req)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() == reflect.Struct {
			if bodyField := v.FieldByName("Body"); bodyField.IsValid() {
				data, err := json.Marshal(bodyField.Addr().Interface())
				if err != nil {
					return nil, err
				}
				bodyReader = bytes.NewReader(data)
			} else {
				data, err := json.Marshal(req)
				if err != nil {
					return nil, err
				}
				bodyReader = bytes.NewReader(data)
			}
		}
	}

	urlStr := path
	if client.baseURL != "" {
		urlStr = strings.TrimRight(client.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respData, &env); err != nil {
		return nil, err
	}

	var resp Resp
	if env.Data != nil {
		if err := json.Unmarshal(env.Data, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func GET[Req, Resp any](ctx context.Context, client *Client, path string) (*Resp, error) {
	return Do[Req, Resp](ctx, client, http.MethodGet, path, nil)
}

func POST[Req, Resp any](ctx context.Context, client *Client, path string, req *Req) (*Resp, error) {
	return Do[Req, Resp](ctx, client, http.MethodPost, path, req)
}

func PUT[Req, Resp any](ctx context.Context, client *Client, path string, req *Req) (*Resp, error) {
	return Do[Req, Resp](ctx, client, http.MethodPut, path, req)
}

func DELETE[Req, Resp any](ctx context.Context, client *Client, path string) (*Resp, error) {
	return Do[Req, Resp](ctx, client, http.MethodDelete, path, nil)
}
