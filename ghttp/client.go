package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// Client is an HTTP client with go-resty-style chain API.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	headers     http.Header
	queryParams url.Values
	pathParams  map[string]string
	authToken   string
	authScheme  string
	timeout     time.Duration
	debug       bool
	codecMgr    *CodecManager
}

// ClientOption configures a Client.
type ClientOption func(*Client)

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		headers:     make(http.Header),
		queryParams: make(url.Values),
		pathParams:  make(map[string]string),
		codecMgr:    NewCodecManager(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

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

// Request is a go-resty-style request builder.
type Request struct {
	client *Client
	Method string
	URL    string
	ctx    context.Context

	Header         http.Header
	QueryParams    url.Values
	PathParams     map[string]string
	FormData       url.Values
	Body           interface{}
	Result         interface{}
	ResultError    interface{}
	StreamResponse bool
	Cookies        []*http.Cookie
	AuthToken      string
	AuthScheme     string
	BasicAuthUser  string
	BasicAuthPass  string
	Timeout        time.Duration
	FileFields     []*FileField
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

func (r *Request) SetStreamResponse(stream bool) *Request {
	r.StreamResponse = stream
	return r
}

func (r *Request) SetContext(ctx context.Context) *Request {
	r.ctx = ctx
	return r
}

func (r *Request) SetAuthToken(token string) *Request {
	r.AuthToken = token
	r.AuthScheme = "Bearer"
	return r
}

func (r *Request) SetCookie(cookie *http.Cookie) *Request {
	r.Cookies = append(r.Cookies, cookie)
	return r
}

func (r *Request) SetContentType(ct string) *Request {
	r.Header.Set("Content-Type", ct)
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

	var bodyReader io.Reader
	contentType := r.Header.Get("Content-Type")

	if len(r.FileFields) > 0 {
		return c.executeMultipart(r, urlStr, start)
	}

	if len(r.FormData) > 0 && contentType == "" {
		contentType = "application/x-www-form-urlencoded"
		bodyReader = strings.NewReader(r.FormData.Encode())
	} else if r.Body != nil {
		switch contentType {
		case "application/xml":
			data, err := xml.Marshal(r.Body)
			if err != nil {
				return nil, err
			}
			bodyReader = bytes.NewReader(data)
		default:
			data, err := json.Marshal(r.Body)
			if err != nil {
				return nil, err
			}
			bodyReader = bytes.NewReader(data)
			if contentType == "" {
				contentType = "application/json"
			}
		}
	}

	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	httpReq, err := http.NewRequestWithContext(ctx, r.Method, urlStr, bodyReader)
	if err != nil {
		return nil, err
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
	for _, cookie := range r.Cookies {
		httpReq.AddCookie(cookie)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	resp := &Response{
		StatusCode:  httpResp.StatusCode,
		Status:      httpResp.Status,
		Header:      httpResp.Header,
		Duration:    time.Since(start),
		Request:     r,
		RawResponse: httpResp,
	}

	if r.StreamResponse {
		resp.rawBody = httpResp.Body
		return resp, nil
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body = body

	if r.Result != nil && resp.IsSuccess() {
		resp.BindJSON(r.Result)
	}

	return resp, nil
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

	writer.Close()
	contentType := writer.FormDataContentType()

	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	httpReq, err := http.NewRequestWithContext(ctx, r.Method, urlStr, &buf)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", contentType)
	for k, v := range r.Header {
		for _, vv := range v {
			httpReq.Header.Add(k, vv)
		}
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	resp := &Response{
		StatusCode:  httpResp.StatusCode,
		Status:      httpResp.Status,
		Header:      httpResp.Header,
		Duration:    time.Since(start),
		Request:     r,
		RawResponse: httpResp,
	}
	if r.StreamResponse {
		resp.rawBody = httpResp.Body
		return resp, nil
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body = body
	return resp, nil
}

// Response represents an HTTP response.
type Response struct {
	StatusCode  int
	Status      string
	Header      http.Header
	Body        []byte
	Duration    time.Duration
	Request     *Request
	RawResponse *http.Response
	rawBody     io.ReadCloser
}

func (r *Response) String() string  { return string(r.Body) }
func (r *Response) Bytes() []byte   { return r.Body }
func (r *Response) IsSuccess() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }
func (r *Response) IsError() bool   { return !r.IsSuccess() }

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
