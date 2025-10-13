package gclient

import (
	"net/http"

	"github.com/valyala/fasthttp"
)

var (
	defaultClient = NewClient()
)

type Client struct {
	Name string

	BaseUrl string

	Config *Config

	debug bool

	Cookies []*http.Cookie

	Log Logger

	fc *fasthttp.Client
}

func NewClient() *Client {
	defaultConfig := DefaultConfig()
	fastClient := &fasthttp.Client{}
	return &Client{
		Config: defaultConfig,
		fc:     fastClient,
	}
}

func NewClientWithConfig(config *Config) *Client {
	if config == nil {
		config = DefaultConfig()
	}
	config.applyDefaults()
	fastClient := &fasthttp.Client{}
	return &Client{
		Config: config,
		fc:     fastClient,
	}
}

func (c *Client) SetName(name string) *Client {
	c.Name = name
	return c
}

func (c *Client) SetBaseUrl(baseUrl string) *Client {
	c.BaseUrl = baseUrl
	return c
}

func (c *Client) R() *Request {
	return &Request{
		client: c,
	}
}

func (c *Client) execute(req *Request) (*Response, error) {

	resp := &Response{}

	return resp, nil
}
