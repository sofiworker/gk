package ghttp

import (
	"crypto/tls"
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

type Config struct {
	TLSConfig      *tls.Config
	RedirectConfig *RedirectConfig
	UploadConfig   *UploadConfig
	HTTP2Config    *HTTP2Config
	RetryConfig    *RetryConfig
}
