package gclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const (
	DefaultTimeout = 30 * time.Second
	DefaultUA      = "gk/1.0"
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
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	MaxConnsPerHost     int
	MaxIdleConnDuration time.Duration
	MaxConnDuration     time.Duration
	MaxConnWaitTimeout  time.Duration

	DialDualStack  bool
	DialContext    func(ctx context.Context, network, addr string) (net.Conn, error)
	DialTLSContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

type ProxyConfig struct {
}

type DNSConfig struct {
	Resolver *net.Resolver
}

type Config struct {
	ConConfig      *ConConfig
	ProxyConfig    *ProxyConfig
	TLSConfig      *tls.Config
	RedirectConfig *RedirectConfig
	UploadConfig   *UploadConfig
	HTTP2Config    *http.HTTP2Config
	RetryConfig    *RetryConfig
	DumpConfig     *DumpConfig
}

func DefaultConfig() *Config {
	c := &Config{}
	c.applyDefaults()
	return c
}

func (c *Config) applyDefaults() {

}
