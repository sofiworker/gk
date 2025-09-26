package ghttp

import (
	"crypto/tls"
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

type HTTP2Config struct {
	Enable               bool
	MaxConcurrentStreams uint32
	MaxReadFrameSize     uint32
	DisableCompression   bool
}

type DumpConfig struct {
	DumpRequest  bool
	DumpResponse bool
}

type TLSConfig struct {
	GoTLSConfig *tls.Config
	KeyFile     string
	CertFile    string
}

type Config struct {
	UA             string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	TLSConfig      *TLSConfig
	RedirectConfig *RedirectConfig
	UploadConfig   *UploadConfig
	HTTP2Config    *HTTP2Config
	RetryConfig    *RetryConfig
	DumpConfig     *DumpConfig
}

func DefaultConfig() *Config {
	return &Config{
		UA:             DefaultUA,
		ReadTimeout:    DefaultTimeout,
		WriteTimeout:   DefaultTimeout,
		TLSConfig:      &TLSConfig{},
		RedirectConfig: &RedirectConfig{},
		UploadConfig:   &UploadConfig{},
		HTTP2Config:    &HTTP2Config{},
		RetryConfig:    &RetryConfig{},
		DumpConfig:     &DumpConfig{},
	}
}
