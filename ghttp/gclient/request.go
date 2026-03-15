package gclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

	Body       interface{}
	RawRequest *http.Request

	Result      interface{}
	ResultError interface{}

	bodyBytes []byte

	cacheKey string
	cacheTTL time.Duration
	useCache bool

	AuthToken              string
	AuthScheme             string
	HeaderAuthorizationKey string
	proxyURL               string
	proxyFunc              func(*http.Request) (*url.URL, error)
	disableProxy           bool
	followRedirects        *bool
	maxRedirects           int
	redirectHandlers       []func(*Response) bool
	responseUnwrapper      ResponseUnwrapper
	responseStatusChecker  ResponseStatusChecker
	tracer                 Tracer
	timeout                time.Duration
	basicAuthUser          string
	basicAuthPass          string
	multipartFields        []*MultipartField
	multipartBoundary      string
	responseSaveFileName   string
	responseSaveDirectory  string
	isResponseSaveToFile   bool
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

func (r *Request) SetHeaderAny(key string, value interface{}) *Request {
	return r.SetHeader(key, formatAnyToString(value))
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

func (r *Request) SetUserAgent(userAgent string) *Request {
	return r.SetHeader("User-Agent", userAgent)
}

func (r *Request) SetAccept(accept string) *Request {
	return r.SetHeader("Accept", accept)
}

func (r *Request) SetContentType(ct string) *Request {
	return r.SetHeader(headerContentType, ct)
}

func (r *Request) AddQueryParam(param, value string) *Request {
	r.QueryParams.Set(param, value)
	return r
}

func (r *Request) SetQueryParam(param, value string) *Request {
	return r.AddQueryParam(param, value)
}

func (r *Request) SetQueryParamAny(param string, value interface{}) *Request {
	return r.AddQueryParam(param, formatAnyToString(value))
}

func (r *Request) AddQueryParams(params map[string]string) *Request {
	for p, v := range params {
		r.QueryParams.Set(p, v)
	}
	return r
}

func (r *Request) SetQueryParams(params map[string]string) *Request {
	return r.AddQueryParams(params)
}

func (r *Request) AddQueryParamsFromValues(params url.Values) *Request {
	for p, values := range params {
		for _, val := range values {
			r.QueryParams.Add(p, val)
		}
	}
	return r
}

func (r *Request) AddQueryValues(params url.Values) *Request {
	return r.AddQueryParamsFromValues(params)
}

func (r *Request) SetQueryParamsFromValues(params url.Values) *Request {
	return r.AddQueryParamsFromValues(params)
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

func (r *Request) SetFormData(data map[string]string) *Request {
	return r.AddFormData(data)
}

func (r *Request) SetFormDataFromValues(values url.Values) *Request {
	for k, v := range values {
		for _, val := range v {
			r.FormData.Add(k, val)
		}
	}
	return r
}

func (r *Request) AddFormValues(values url.Values) *Request {
	return r.SetFormDataFromValues(values)
}

func (r *Request) SetBody(body interface{}) *Request {
	r.Body = body
	r.bodyBytes = nil
	return r
}

func (r *Request) SetJSONBody(body interface{}) *Request {
	r.SetContentType(contentTypeJSON)
	return r.SetBody(body)
}

func (r *Request) SetXMLBody(body interface{}) *Request {
	r.SetContentType("application/xml")
	return r.SetBody(body)
}

func (r *Request) SetPlainBody(body string) *Request {
	r.SetContentType(contentTypePlain + "; charset=utf-8")
	return r.SetBody(body)
}

func (r *Request) SetResult(result interface{}) *Request {
	r.Result = result
	return r
}

func (r *Request) SetResultError(result interface{}) *Request {
	r.ResultError = result
	return r
}

func (r *Request) SetBytesBody(body []byte) *Request {
	r.Body = body
	r.bodyBytes = append([]byte(nil), body...)
	return r
}

func (r *Request) SetReaderBody(reader io.Reader) *Request {
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

func (r *Request) SetCookie(cookie *http.Cookie) *Request {
	return r.AddCookies(cookie)
}

func (r *Request) SetCookies(cookies []*http.Cookie) *Request {
	return r.AddCookies(cookies...)
}

func (r *Request) SetPathParam(key, value string) *Request {
	if r.PathParams == nil {
		r.PathParams = make(map[string]string)
	}
	r.PathParams[key] = value
	return r
}

func (r *Request) SetPathParamAny(key string, value interface{}) *Request {
	return r.SetPathParam(key, formatAnyToString(value))
}

func (r *Request) SetPathParams(params map[string]string) *Request {
	for k, v := range params {
		r.SetPathParam(k, v)
	}
	return r
}

func (r *Request) SetBasicAuth(username, password string) *Request {
	r.basicAuthUser = username
	r.basicAuthPass = password
	return r
}

func (r *Request) SetAuthToken(token string) *Request {
	r.AuthToken = token
	return r
}

func (r *Request) SetBearerToken(token string) *Request {
	r.AuthScheme = "Bearer"
	r.AuthToken = token
	return r
}

func (r *Request) SetAuthScheme(scheme string) *Request {
	if strings.TrimSpace(scheme) == "" {
		scheme = "Bearer"
	}
	r.AuthScheme = scheme
	return r
}

func (r *Request) SetHeaderAuthorizationKey(key string) *Request {
	if strings.TrimSpace(key) == "" {
		key = "Authorization"
	}
	r.HeaderAuthorizationKey = key
	return r
}

func (r *Request) SetTimeout(timeout time.Duration) *Request {
	r.timeout = timeout
	return r
}

func (r *Request) SetProxy(proxyURL string) *Request {
	r.proxyURL = proxyURL
	r.proxyFunc = nil
	r.disableProxy = false
	return r
}

func (r *Request) SetProxyFunc(proxyFunc func(*http.Request) (*url.URL, error)) *Request {
	r.proxyURL = ""
	r.proxyFunc = proxyFunc
	r.disableProxy = false
	return r
}

func (r *Request) DisableProxy() *Request {
	r.proxyURL = ""
	r.proxyFunc = nil
	r.disableProxy = true
	return r
}

func (r *Request) SetFollowRedirects(follow bool) *Request {
	r.followRedirects = &follow
	return r
}

func (r *Request) DisableRedirects() *Request {
	follow := false
	r.followRedirects = &follow
	return r
}

func (r *Request) SetMaxRedirects(max int) *Request {
	r.maxRedirects = max
	return r
}

func (r *Request) AddRedirectHandler(handler func(*Response) bool) *Request {
	if handler != nil {
		r.redirectHandlers = append(r.redirectHandlers, handler)
	}
	return r
}

func (r *Request) SetTracer(tracer Tracer) *Request {
	r.tracer = tracer
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

func (r *Request) MustExecute(method, url string) *Response {
	resp, err := r.Execute(method, url)
	if err != nil {
		panic(err)
	}
	return resp
}

func (r *Request) BuildHTTPRequest() (*http.Request, error) {
	if r == nil {
		return nil, errors.New("request is nil")
	}
	req := r.Clone()
	fullURL, err := req.prepareURL()
	if err != nil {
		return nil, err
	}
	req.URL = fullURL
	builder := newHTTPRequestBuilder(req, req.effectiveClient())
	httpReq, err := builder.Build()
	if err != nil {
		return nil, err
	}
	req.RawRequest = httpReq
	r.RawRequest = httpReq
	return httpReq, nil
}

func (r *Request) MustBuildHTTPRequest() *http.Request {
	req, err := r.BuildHTTPRequest()
	if err != nil {
		panic(err)
	}
	return req
}

func (r *Request) FromHTTPRequest(httpReq *http.Request) *Request {
	if httpReq == nil {
		return r
	}
	r.RawRequest = httpReq
	r.Method = httpReq.Method
	if httpReq.URL != nil {
		urlCopy := *httpReq.URL
		urlCopy.RawQuery = ""
		urlCopy.ForceQuery = false
		r.URL = urlCopy.String()
		if r.QueryParams == nil {
			r.QueryParams = make(url.Values)
		}
		for key, values := range httpReq.URL.Query() {
			for _, value := range values {
				r.QueryParams.Add(key, value)
			}
		}
	}
	r.ctx = httpReq.Context()
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	for key, values := range httpReq.Header {
		r.Header[key] = append([]string(nil), values...)
	}
	r.Cookies = append(r.Cookies, httpReq.Cookies()...)
	return r
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
		base = r.client.BaseURL()
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
	clone.RawRequest = r.RawRequest
	if r.bodyBytes != nil {
		clone.bodyBytes = append([]byte(nil), r.bodyBytes...)
	}
	clone.cacheKey = r.cacheKey
	clone.cacheTTL = r.cacheTTL
	clone.useCache = r.useCache
	clone.Result = r.Result
	clone.ResultError = r.ResultError
	clone.responseUnwrapper = r.responseUnwrapper
	clone.responseStatusChecker = r.responseStatusChecker
	clone.AuthToken = r.AuthToken
	clone.AuthScheme = r.AuthScheme
	clone.HeaderAuthorizationKey = r.HeaderAuthorizationKey
	clone.proxyURL = r.proxyURL
	clone.proxyFunc = r.proxyFunc
	clone.disableProxy = r.disableProxy
	if r.followRedirects != nil {
		follow := *r.followRedirects
		clone.followRedirects = &follow
	}
	clone.maxRedirects = r.maxRedirects
	clone.redirectHandlers = append([]func(*Response) bool(nil), r.redirectHandlers...)
	clone.tracer = r.tracer
	clone.timeout = r.timeout
	clone.basicAuthUser = r.basicAuthUser
	clone.basicAuthPass = r.basicAuthPass
	clone.multipartBoundary = r.multipartBoundary
	clone.responseSaveFileName = r.responseSaveFileName
	clone.responseSaveDirectory = r.responseSaveDirectory
	clone.isResponseSaveToFile = r.isResponseSaveToFile
	if len(r.multipartFields) > 0 {
		clone.multipartFields = make([]*MultipartField, 0, len(r.multipartFields))
		for _, field := range r.multipartFields {
			if field == nil {
				continue
			}
			cp := *field
			clone.multipartFields = append(clone.multipartFields, &cp)
		}
	}
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

func (r *Request) effectiveClient() *Client {
	if r != nil && r.client != nil {
		return r.client
	}
	return NewClient()
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

	if req.basicAuthUser != "" || req.basicAuthPass != "" {
		httpReq.SetBasicAuth(req.basicAuthUser, req.basicAuthPass)
	} else if req.AuthToken != "" {
		headerKey := req.HeaderAuthorizationKey
		if headerKey == "" {
			headerKey = "Authorization"
		}
		scheme := strings.TrimSpace(req.AuthScheme)
		if scheme == "" {
			scheme = "Bearer"
		}
		httpReq.Header.Set(headerKey, scheme+" "+req.AuthToken)
	}

	if req.bodyBytes != nil {
		httpReq.ContentLength = int64(len(req.bodyBytes))
	}

	return httpReq, nil
}

func formatAnyToString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case []byte:
		return string(v)
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case time.Duration:
		return v.String()
	default:
		return fmt.Sprint(value)
	}
}

func (b *httpRequestBuilder) prepareBody() (io.ReadCloser, string, error) {
	req := b.req
	if req.bodyBytes != nil {
		return io.NopCloser(bytes.NewReader(req.bodyBytes)), req.Header.Get(headerContentType), nil
	}

	if len(req.multipartFields) > 0 {
		return b.prepareMultipartBody()
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
		if strings.HasPrefix(strings.ToLower(contentType), contentTypeJSON) {
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
			break
		}
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
