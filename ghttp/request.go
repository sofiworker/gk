package ghttp

import (
	"gk/gresolver"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

type RequestTime struct {
	startRequest  *time.Time
	endRequest    *time.Time
	requestCost   *time.Duration
	startResponse *time.Time
	endResponse   *time.Time
	responseCost  *time.Duration
}

type Request struct {
	Client *Client

	url    string
	method string

	Headers     http.Header
	Cookies     []*http.Cookie
	QueryParams url.Values
	FormData    url.Values

	resolver gresolver.Resolver

	body interface{}

	// config 优先于 client 的 config
	config *Config
}

func NewRequest() *Request {
	return &Request{
		Client: NewClient(),
	}
}

func (r *Request) GetClient() *Client {
	return r.Client
}

func (r *Request) SetHeader(key, value string) *Request {
	if r.Headers == nil {
		r.Headers = make(http.Header)
	}
	r.Headers.Set(key, value)
	return r
}

func (r *Request) SetHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.SetHeader(k, v)
	}
	return r
}

func (r *Request) SetUrl(url string) *Request {
	r.url = url
	return r
}

func (r *Request) SetMethod(method string) *Request {
	r.method = method
	return r
}

func (r *Request) SetCookies(cookies []*http.Cookie) *Request {
	r.Cookies = cookies
	return r
}

func (r *Request) SetQueryParams(queryParams url.Values) *Request {
	r.QueryParams = queryParams
	return r
}

func (r *Request) SetResolver(resolver gresolver.Resolver) *Request {
	r.resolver = resolver
	return r
}

func (r *Request) Get() (*Response, error) {
	return r.SetMethod(http.MethodGet).Done()
}

func (r *Request) HEAD() (*Response, error) {
	return r.SetMethod(http.MethodHead).Done()
}

func (r *Request) POST() (*Response, error) {
	return r.SetMethod(http.MethodPost).Done()
}

func (r *Request) PUT() (*Response, error) {
	return r.SetMethod(http.MethodPut).Done()
}

func (r *Request) PATCH() (*Response, error) {
	return r.SetMethod(http.MethodPatch).Done()
}

func (r *Request) DELETE() (*Response, error) {
	return r.SetMethod(http.MethodDelete).Done()
}

func (r *Request) CONNECT() (*Response, error) {
	return r.SetMethod(http.MethodConnect).Done()
}

func (r *Request) OPTIONS() (*Response, error) {
	return r.SetMethod(http.MethodOptions).Done()
}

func (r *Request) TRACE() (*Response, error) {
	return r.SetMethod(http.MethodTrace).Done()
}

func (r *Request) SetIfModifiedSince(time time.Time) *Request {
	r.SetHeader("If-Modified-Since", time.Format(http.TimeFormat))
	return r
}

func (r *Request) SetIfNoneMatch(etag string) *Request {
	r.SetHeader("If-None-Match", etag)
	return r
}

func (r *Request) JSON(data interface{}) *Request {
	r.SetHeader("Content-Type", "application/json")
	r.body = data
	return r
}

func (r *Request) XML(data interface{}) *Request {
	r.SetHeader("Content-Type", "application/xml")
	r.body = data
	return r
}

func (r *Request) File(filePath string) *Request {
	//r.file = filePath
	return r
}

func (r *Request) FilePath(filePath string) *Request {
	//r.file = filePath
	return r
}

func (r *Request) FileReader(reader io.ReadCloser) *Request {
	//r.file = filePath
	return r
}

func (r *Request) Multipart(filePath string) *Request {
	//r.file = filePath
	return r
}

func (r *Request) Form(form multipart.Form) *Request {
	//r.file = filePath
	return r
}

func (r *Request) PostForm(data url.Values) *Request {
	r.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func (r *Request) MultipartFormData(data map[string]string) *Request {
	formData := url.Values{}
	for k, v := range data {
		formData.Set(k, v)
	}
	return r.PostForm(formData)
}

func (r *Request) Done() (*Response, error) {

	return nil, nil
}
