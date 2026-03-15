package gclient

import (
	"crypto/tls"
	"net/http"
	"net/url"
)

func (c *Client) Transport() http.RoundTripper {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.httpClient == nil {
		return nil
	}
	return c.httpClient.Transport
}

func (c *Client) SetTransport(transport http.RoundTripper) *Client {
	if transport == nil {
		return c
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.httpClient == nil {
		c.httpClient = c.buildHTTPClient()
	}
	c.httpClient.Transport = transport
	if httpTransport, ok := transport.(*http.Transport); ok {
		c.config.Transport = httpTransport
	}
	return c
}

func (c *Client) SetProxy(proxyURL string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.ProxyConfig == nil {
		c.config.ProxyConfig = &ProxyConfig{}
	}
	c.config.ProxyConfig.NoProxy = false
	c.config.ProxyConfig.ProxyFunc = nil
	c.config.ProxyConfig.URL = proxyURL
	c.refreshHTTPTransportLocked()
	return c
}

func (c *Client) SetProxyFunc(proxyFunc func(*http.Request) (*url.URL, error)) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.ProxyConfig == nil {
		c.config.ProxyConfig = &ProxyConfig{}
	}
	c.config.ProxyConfig.NoProxy = false
	c.config.ProxyConfig.URL = ""
	c.config.ProxyConfig.ProxyFunc = proxyFunc
	c.refreshHTTPTransportLocked()
	return c
}

func (c *Client) DisableProxy() *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.ProxyConfig == nil {
		c.config.ProxyConfig = &ProxyConfig{}
	}
	c.config.ProxyConfig.NoProxy = true
	c.config.ProxyConfig.URL = ""
	c.config.ProxyConfig.ProxyFunc = nil
	c.refreshHTTPTransportLocked()
	return c
}

func (c *Client) SetTLSConfig(cfg *tls.Config) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	c.config.TLSConfig = cfg
	c.refreshHTTPTransportLocked()
	return c
}

func (c *Client) SetFollowRedirects(follow bool) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.RedirectConfig == nil {
		c.config.RedirectConfig = &RedirectConfig{}
	}
	c.config.RedirectConfig.FollowRedirects = follow
	c.refreshRedirectPolicyLocked()
	return c
}

func (c *Client) SetMaxRedirects(max int) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.RedirectConfig == nil {
		c.config.RedirectConfig = &RedirectConfig{}
	}
	c.config.RedirectConfig.MaxRedirects = max
	c.refreshRedirectPolicyLocked()
	return c
}

func (c *Client) AddRedirectHandler(handler func(*Response) bool) *Client {
	if handler == nil {
		return c
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.RedirectConfig == nil {
		c.config.RedirectConfig = &RedirectConfig{}
	}
	c.config.RedirectConfig.RedirectHandlers = append(c.config.RedirectConfig.RedirectHandlers, handler)
	c.refreshRedirectPolicyLocked()
	return c
}

func (c *Client) refreshHTTPTransportLocked() {
	if c.config == nil {
		return
	}
	transport := c.config.Transport
	if transport == nil {
		transport = c.config.buildTransport()
	}
	c.config.Transport = transport
	if c.httpClient == nil {
		c.httpClient = c.buildHTTPClient()
		return
	}
	c.httpClient.Transport = transport
}

func (c *Client) refreshRedirectPolicyLocked() {
	if c.httpClient == nil {
		c.httpClient = c.buildHTTPClient()
		return
	}
	tmp := c.buildHTTPClient()
	c.httpClient.CheckRedirect = tmp.CheckRedirect
}
