package gclient

import (
	"context"
	"net/http"
	"net/url"
	"strings"
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
}

func (r *Request) SetURL(url string) *Request {
	r.URL = url
	return r
}

func (r *Request) SetContext(ctx context.Context) *Request {
	r.ctx = ctx
	return r
}

func (r *Request) SetContentType(ct string) *Request {
	r.SetHeader("Content-Type", ct)
	return r
}

func (r *Request) SetHeader(header, value string) *Request {
	r.Header.Set(header, value)
	return r
}

func (r *Request) SetHeaders(headers map[string]string) *Request {
	for h, v := range headers {
		r.SetHeader(h, v)
	}
	return r
}

func (r *Request) SetHeaderMultiValues(headers map[string][]string) *Request {
	for key, values := range headers {
		r.SetHeader(key, strings.Join(values, ", "))
	}
	return r
}

func (r *Request) SetHeaderVerbatim(header, value string) *Request {
	r.Header[header] = []string{value}
	return r
}

func (r *Request) AddQueryParam(param, value string) *Request {
	r.QueryParams.Set(param, value)
	return r
}

func (r *Request) AddQueryParams(params map[string]string) *Request {
	for p, v := range params {
		r.AddQueryParam(p, v)
	}
	return r
}

func (r *Request) AddQueryParamsFromValues(params url.Values) *Request {
	for p, v := range params {
		for _, pv := range v {
			r.QueryParams.Add(p, pv)
		}
	}
	return r
}

func (r *Request) SetQueryString(query string) *Request {
	params, err := url.ParseQuery(strings.TrimSpace(query))
	if err == nil {
		for p, v := range params {
			for _, pv := range v {
				r.QueryParams.Add(p, pv)
			}
		}
	}
	return r
}

func (r *Request) AddFormData(data map[string]string) *Request {
	for k, v := range data {
		r.FormData.Set(k, v)
	}
	return r
}

func (r *Request) SetFormDataFromValues(data url.Values) *Request {
	for k, v := range data {
		for _, kv := range v {
			r.FormData.Add(k, kv)
		}
	}
	return r
}

func (r *Request) SetBody(body any) *Request {
	r.Body = body
	return r
}

func (r *Request) AddCookies(rs ...*http.Cookie) *Request {
	r.Cookies = append(r.Cookies, rs...)
	return r
}

func (r *Request) SetPathParam(key, value string) *Request {
	if r.PathParams == nil {
		r.PathParams = make(map[string]string)
	}
	r.PathParams[key] = value
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
func (r *Request) Connect(url string) (*Response, error) {
	return r.Execute(http.MethodConnect, url)
}
func (r *Request) Trace(url string) (*Response, error) {
	return r.Execute(http.MethodTrace, url)
}

func (r *Request) Execute(method, url string) (*Response, error) {
	r.Method = method
	r.URL = url

	fullUrl, err := r.prepareURL()
	if err != nil {
		return nil, err
	}
	r.URL = fullUrl

	defer func() {
		if rec := recover(); rec != nil {
			if err, ok := rec.(error); ok {
				//r.client.onPanicHooks(r, err)
				panic(err)
			}
			panic(rec)
		}
	}()

	return r.client.execute(r)
}

func (r *Request) prepareURL() (string, error) {
	finalURL := r.URL
	finalURL, err := ConstructURL(r.client.BaseUrl, r.URL)
	if err != nil {
		return "", err
	}

	if len(r.QueryParams) > 0 {
		u, err := url.Parse(finalURL)
		if err != nil {
			return "", err
		}
		q := u.Query()
		for param, values := range r.QueryParams {
			for _, v := range values {
				q.Add(param, v)
			}
		}
		u.RawQuery = q.Encode()
		finalURL = u.String()
	}

	return finalURL, nil
}

func (r *Request) Clone() *Request {
	return &Request{
		client:      r.client,
		URL:         r.URL,
		Method:      r.Method,
		ctx:         r.ctx,
		Cookies:     append([]*http.Cookie{}, r.Cookies...),
		Header:      r.Header.Clone(),
		QueryParams: CloneURLValues(r.QueryParams),
		FormData:    CloneURLValues(r.FormData),
		Body:        r.Body, // ?? 如果是指针类型，可能需要深拷贝
	}
}
