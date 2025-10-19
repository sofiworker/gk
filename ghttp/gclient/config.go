package gclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	DefaultTimeout = 30 * time.Second
	DefaultUA      = "gk/1.0"

	defaultMaxConnsPerHost     = 100
	defaultMaxIdleConnDuration = 90 * time.Second
)

type RedirectConfig struct {
	MaxRedirects     int
	FollowRedirects  bool
	RedirectHandlers []func(*Response) bool
}

type UploadConfig struct {
	LargeFileSizeThreshold int64
	UseStreamingUpload     bool
}

type DumpConfig struct {
	DumpRequest  bool
	DumpResponse bool
}

type ConConfig struct {
	Timeout      time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	KeepAlive    time.Duration

	MaxConnsPerHost     int
	MaxIdleConnDuration time.Duration
	MaxConnDuration     time.Duration
	MaxConnWaitTimeout  time.Duration

	DialDualStack  bool
	DialContext    func(ctx context.Context, network, addr string) (net.Conn, error)
	DialTLSContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

type ProxyConfig struct {
	URL       string
	ProxyFunc func(*http.Request) (*url.URL, error)
	NoProxy   bool
}

type Config struct {
	Timeout        time.Duration
	ConConfig      *ConConfig
	ProxyConfig    *ProxyConfig
	TLSConfig      *tls.Config
	RedirectConfig *RedirectConfig
	UploadConfig   *UploadConfig
	HTTP2Config    *http.HTTP2Config
	RetryConfig    *RetryConfig
	DumpConfig     *DumpConfig
	Transport      *http.Transport
}

func DefaultConfig() *Config {
	cfg := &Config{}
	cfg.applyDefaults()
	return cfg
}

func (c *Config) applyDefaults() {
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}

	if c.ConConfig == nil {
		c.ConConfig = &ConConfig{}
	}
	c.ConConfig.applyDefaults()

	if c.ProxyConfig == nil {
		c.ProxyConfig = &ProxyConfig{}
	}

	if c.TLSConfig == nil {
		c.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	if c.RedirectConfig == nil {
		c.RedirectConfig = &RedirectConfig{MaxRedirects: 10, FollowRedirects: true}
	}

	if c.UploadConfig == nil {
		c.UploadConfig = &UploadConfig{LargeFileSizeThreshold: 10 << 20, UseStreamingUpload: true}
	}

	if c.RetryConfig == nil {
		c.RetryConfig = DefaultRetryConfig()
	}

	if c.DumpConfig == nil {
		c.DumpConfig = &DumpConfig{}
	}
}

func (cc *ConConfig) applyDefaults() {
	if cc.Timeout <= 0 {
		cc.Timeout = DefaultTimeout
	}
	if cc.ReadTimeout <= 0 {
		cc.ReadTimeout = DefaultTimeout
	}
	if cc.WriteTimeout <= 0 {
		cc.WriteTimeout = 30 * time.Second
	}
	if cc.KeepAlive <= 0 {
		cc.KeepAlive = 30 * time.Second
	}
	if cc.MaxConnsPerHost <= 0 {
		cc.MaxConnsPerHost = defaultMaxConnsPerHost
	}
	if cc.MaxIdleConnDuration <= 0 {
		cc.MaxIdleConnDuration = defaultMaxIdleConnDuration
	}
}

func (c *Config) buildTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()

	base.MaxConnsPerHost = c.ConConfig.MaxConnsPerHost
	base.MaxIdleConns = c.ConConfig.MaxConnsPerHost * 2
	base.MaxIdleConnsPerHost = c.ConConfig.MaxConnsPerHost
	base.IdleConnTimeout = c.ConConfig.MaxIdleConnDuration
	base.ResponseHeaderTimeout = c.ConConfig.ReadTimeout
	base.ExpectContinueTimeout = c.ConConfig.WriteTimeout
	base.MaxResponseHeaderBytes = 0
	base.ForceAttemptHTTP2 = true
	base.TLSClientConfig = c.TLSConfig

	dialer := &net.Dialer{
		Timeout:   c.ConConfig.Timeout,
		KeepAlive: c.ConConfig.KeepAlive,
	}
	dialer.DualStack = c.ConConfig.DialDualStack

	if c.ConConfig.DialContext != nil {
		base.DialContext = c.ConConfig.DialContext
	} else {
		base.DialContext = dialer.DialContext
	}

	if c.ConConfig.DialTLSContext != nil {
		base.DialTLSContext = c.ConConfig.DialTLSContext
	}

	if c.ProxyConfig != nil {
		if c.ProxyConfig.NoProxy {
			base.Proxy = nil
		} else if c.ProxyConfig.ProxyFunc != nil {
			base.Proxy = c.ProxyConfig.ProxyFunc
		} else if c.ProxyConfig.URL != "" {
			if parsed, err := url.Parse(c.ProxyConfig.URL); err == nil {
				base.Proxy = http.ProxyURL(parsed)
			}
		}
	}

	return base
}
