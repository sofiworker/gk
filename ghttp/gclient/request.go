package gclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sofiworker/gk/ghttp/codec"
)

const (
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
	contentTypeForm   = "application/x-www-form-urlencoded"
	contentTypePlain  = "text/plain"
)

type Request struct {
	client *Client

	URL    string
	Method string
	ctx    context.Context

	Cookies []*http.Cookie
	Header  http.Header

	QueryParams url.Values
	FormData    url.Values
	PathParams  map[string]string

	Body interface{}

	bodyBytes []byte

	cacheKey string
	cacheTTL time.Duration
	useCache bool
}

func newRequest(client *Client) *Request {
	return &Request{
		client:      client,
		Method:      http.MethodGet,
		Header:      make(http.Header),
		QueryParams: make(url.Values),
		FormData:    make(url.Values),
		PathParams:  make(map[string]string),
		ctx:         context.Background(),
	}
}

func (r *Request) Context() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

func (r *Request) SetContext(ctx context.Context) *Request {
	if ctx == nil {
		ctx = context.Background()
	}
	r.ctx = ctx
	return r
}

func (r *Request) SetURL(u string) *Request {
	r.URL = strings.TrimSpace(u)
	return r
}

func (r *Request) SetMethod(method string) *Request {
	if method == "" {
		return r
	}
	r.Method = strings.ToUpper(method)
	return r
}

func (r *Request) ensureHeader() {
	if r.Header == nil {
		r.Header = make(http.Header)
	}
}

func (r *Request) SetHeader(key, value string) *Request {
	r.ensureHeader()
	r.Header.Set(key, value)
	return r
}

func (r *Request) AddHeader(key, value string) *Request {
	r.ensureHeader()
	r.Header.Add(key, value)
	return r
}

func (r *Request) SetHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.SetHeader(k, v)
	}
	return r
}

func (r *Request) SetHeaderValues(headers map[string][]string) *Request {
	r.ensureHeader()
	for k, v := range headers {
		cp := append([]string(nil), v...)
		r.Header[k] = cp
	}
	return r
}

func (r *Request) SetContentType(ct string) *Request {
	return r.SetHeader(headerContentType, ct)
}

func (r *Request) AddQueryParam(param, value string) *Request {
	r.QueryParams.Set(param, value)
	return r
}

func (r *Request) AddQueryParams(params map[string]string) *Request {
	for p, v := range params {
		r.QueryParams.Set(p, v)
	}
	return r
}

func (r *Request) AddQueryParamsFromValues(params url.Values) *Request {
	for p, values := range params {
		for _, val := range values {
			r.QueryParams.Add(p, val)
		}
	}
	return r
}

func (r *Request) SetQueryString(query string) *Request {
	values, err := url.ParseQuery(strings.TrimSpace(query))
	if err != nil {
		return r
	}
	return r.AddQueryParamsFromValues(values)
}

func (r *Request) AddFormData(data map[string]string) *Request {
	for k, v := range data {
		r.FormData.Set(k, v)
	}
	return r
}

func (r *Request) SetFormDataFromValues(values url.Values) *Request {
	for k, v := range values {
		for _, val := range v {
			r.FormData.Add(k, val)
		}
	}
	return r
}

func (r *Request) SetBody(body interface{}) *Request {
	r.Body = body
	r.bodyBytes = nil
	return r
}

func (r *Request) SetBodyBytes(body []byte) *Request {
	r.Body = body
	r.bodyBytes = append([]byte(nil), body...)
	return r
}

func (r *Request) SetBodyReader(reader io.Reader) *Request {
	r.Body = reader
	r.bodyBytes = nil
	return r
}

func (r *Request) AddCookies(cookies ...*http.Cookie) *Request {
	for _, ck := range cookies {
		if ck != nil {
			r.Cookies = append(r.Cookies, ck)
		}
	}
	return r
}

func (r *Request) SetPathParam(key, value string) *Request {
	if r.PathParams == nil {
		r.PathParams = make(map[string]string)
	}
	r.PathParams[key] = value
	return r
}

func (r *Request) UseCache(key string, ttl time.Duration) *Request {
	r.useCache = true
	r.cacheKey = key
	r.cacheTTL = ttl
	return r
}

func (r *Request) DisableCache() *Request {
	r.useCache = false
	r.cacheKey = ""
	r.cacheTTL = 0
	return r
}

func (r *Request) Execute(method, url string) (*Response, error) {
	r.SetMethod(method)
	r.SetURL(url)

	defer func() {
		if rec := recover(); rec != nil {
			panic(rec)
		}
	}()

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

func (r *Request) Options(url string) (*Response, error) {
	return r.Execute(http.MethodOptions, url)
}

func (r *Request) prepareURL() (string, error) {
	finalPath := r.URL
	if len(r.PathParams) > 0 {
		finalPath = replacePathParams(finalPath, r.PathParams)
	}

	base := ""
	if r.client != nil {
		base = r.client.BaseUrl()
	}

	fullURL, err := ConstructURL(base, finalPath)
	if err != nil {
		return "", err
	}

	if len(r.QueryParams) > 0 {
		parsed, err := url.Parse(fullURL)
		if err != nil {
			return "", err
		}
		query := parsed.Query()
		for k, values := range r.QueryParams {
			for _, v := range values {
				query.Add(k, v)
			}
		}
		parsed.RawQuery = query.Encode()
		fullURL = parsed.String()
	}

	if r.useCache && r.cacheKey == "" {
		r.cacheKey = fullURL
	}

	return fullURL, nil
}

func (r *Request) Clone() *Request {
	clone := newRequest(r.client)
	clone.URL = r.URL
	clone.Method = r.Method
	clone.ctx = r.ctx
	clone.Cookies = append([]*http.Cookie(nil), r.Cookies...)
	clone.Header = r.Header.Clone()
	clone.QueryParams = CloneURLValues(r.QueryParams)
	clone.FormData = CloneURLValues(r.FormData)
	clone.PathParams = make(map[string]string, len(r.PathParams))
	for k, v := range r.PathParams {
		clone.PathParams[k] = v
	}
	clone.Body = r.Body
	if r.bodyBytes != nil {
		clone.bodyBytes = append([]byte(nil), r.bodyBytes...)
	}
	clone.cacheKey = r.cacheKey
	clone.cacheTTL = r.cacheTTL
	clone.useCache = r.useCache
	return clone
}

func replacePathParams(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	result := path
	for k, v := range params {
		escaped := url.PathEscape(v)
		result = strings.ReplaceAll(result, ":"+k, escaped)
		result = strings.ReplaceAll(result, "{"+k+"}", escaped)
	}
	return result
}

type httpRequestBuilder struct {
	req    *Request
	client *Client
}

func newHTTPRequestBuilder(req *Request, client *Client) *httpRequestBuilder {
	return &httpRequestBuilder{req: req, client: client}
}

func (b *httpRequestBuilder) Build() (*http.Request, error) {
	req := b.req
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	body, contentType, err := b.prepareBody()
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(req.Context(), method, req.URL, body)
	if err != nil {
		return nil, err
	}

	headers := b.client.cloneDefaultHeaders()
	for k, values := range req.Header {
		headers[k] = append([]string(nil), values...)
	}
	if contentType != "" {
		headers.Set(headerContentType, contentType)
	} else if headers.Get(headerContentType) == "" && body != nil {
		headers.Set(headerContentType, contentTypePlain)
	}
	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", DefaultUA)
	}
	httpReq.Header = headers

	for _, ck := range b.client.cookiesSnapshot() {
		httpReq.AddCookie(ck)
	}
	for _, ck := range req.Cookies {
		if ck != nil {
			httpReq.AddCookie(ck)
		}
	}

	if req.bodyBytes != nil {
		httpReq.ContentLength = int64(len(req.bodyBytes))
	}

	return httpReq, nil
}

func (b *httpRequestBuilder) prepareBody() (io.ReadCloser, string, error) {
	req := b.req
	if req.bodyBytes != nil {
		return io.NopCloser(bytes.NewReader(req.bodyBytes)), req.Header.Get(headerContentType), nil
	}

	if len(req.FormData) > 0 && req.Body == nil {
		data := req.FormData.Encode()
		req.bodyBytes = []byte(data)
		return io.NopCloser(bytes.NewReader(req.bodyBytes)), contentTypeForm, nil
	}

	if req.Body == nil {
		return nil, "", nil
	}

	contentType := req.Header.Get(headerContentType)
	switch body := req.Body.(type) {
	case []byte:
		req.bodyBytes = append([]byte(nil), body...)
	case string:
		req.bodyBytes = []byte(body)
		if contentType == "" {
			contentType = contentTypePlain + "; charset=utf-8"
		}
	case io.Reader:
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, "", err
		}
		req.bodyBytes = data
	case url.Values:
		req.bodyBytes = []byte(body.Encode())
		if contentType == "" {
			contentType = contentTypeForm
		}
	case map[string]string:
		values := url.Values{}
		for k, v := range body {
			values.Set(k, v)
		}
		req.bodyBytes = []byte(values.Encode())
		if contentType == "" {
			contentType = contentTypeForm
		}
	default:
		if contentType == "" {
			contentType = contentTypeJSON
		}
		manager := b.client.codecManager
		if manager == nil {
			manager = codec.DefaultManager()
		}
		cd := manager.GetCodec(contentType)
		if cd == nil {
			cd = manager.DefaultCodec()
		}
		if cd == nil {
			return nil, "", fmt.Errorf("no codec available for %s", contentType)
		}
		data, err := cd.Encode(body)
		if err != nil {
			return nil, "", err
		}
		req.bodyBytes = data
	}

	if req.bodyBytes == nil {
		return nil, contentType, nil
	}

	return io.NopCloser(bytes.NewReader(req.bodyBytes)), contentType, nil
}
