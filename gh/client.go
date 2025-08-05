package ghttp

import (
	"crypto/tls"
	"fmt"
	"github.com/valyala/fasthttp"
	"gk/gresolver"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
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

type Client struct {
	fastClient         *fasthttp.Client
	baseUrl            string
	tlsConfig          *tls.Config
	enableDumpRequest  bool
	enableDumpResponse bool
	commonResponse     interface{}
	beforeRequest      []func(*Request)
	afterResponse      []func(*Request, *Response)
	defaultDecoder     Decoder
	tracer             Tracer
	resolver           gresolver.Resolver
	readTimeout        time.Duration
	writeTimeout       time.Duration
}

func NewClient() *Client {
	c := &fasthttp.Client{
		ReadTimeout:  defaultTimeout,
		WriteTimeout: defaultTimeout,
	}
	return &Client{fastClient: c,
		defaultDecoder: NewJsonDecoder(),
		resolver:       gresolver.NewDefaultResolver(nil),
		readTimeout:    defaultTimeout,
		writeTimeout:   defaultTimeout,
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
