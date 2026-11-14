package otlp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"time"

	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
)

var ErrInvalidConfig = errors.New("gotel/otlp: invalid configuration")

type Protocol string

const (
	ProtocolHTTP Protocol = "http/protobuf"
	ProtocolGRPC Protocol = "grpc"
)

type config struct {
	protocol        Protocol
	endpoint        string
	traceEndpoint   string
	metricEndpoint  string
	headers         map[string]string
	tlsConfig       *tls.Config
	timeout         time.Duration
	metricInterval  time.Duration
	traces          bool
	metrics         bool
	providerOptions []oteladapter.ProviderOption
}

type Option func(*config)

func WithProtocol(protocol Protocol) Option { return func(c *config) { c.protocol = protocol } }

// WithEndpointURL sets a base collector URL. HTTP appends /v1/traces or /v1/metrics;
// gRPC uses the URL's authority. An http URL explicitly selects plaintext.
func WithEndpointURL(endpoint string) Option { return func(c *config) { c.endpoint = endpoint } }

// WithTraceEndpointURL overrides the full trace URL, including its HTTP path.
func WithTraceEndpointURL(endpoint string) Option {
	return func(c *config) { c.traceEndpoint = endpoint }
}

// WithMetricEndpointURL overrides the full metrics URL, including its HTTP path.
func WithMetricEndpointURL(endpoint string) Option {
	return func(c *config) { c.metricEndpoint = endpoint }
}

func WithHeaders(headers map[string]string) Option {
	headers = maps.Clone(headers)
	return func(c *config) { c.headers = maps.Clone(headers) }
}

func WithTLSConfig(tlsConfig *tls.Config) Option {
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	}
	return func(c *config) {
		c.tlsConfig = nil
		if tlsConfig != nil {
			c.tlsConfig = tlsConfig.Clone()
		}
	}
}

func WithTimeout(timeout time.Duration) Option { return func(c *config) { c.timeout = timeout } }
func WithMetricInterval(interval time.Duration) Option {
	return func(c *config) { c.metricInterval = interval }
}
func WithTraces(enabled bool) Option  { return func(c *config) { c.traces = enabled } }
func WithMetrics(enabled bool) Option { return func(c *config) { c.metrics = enabled } }

// WithProviderOptions configures service/resource, sampling, batch processing and
// additional readers. Exporters created by this module are applied afterwards.
func WithProviderOptions(opts ...oteladapter.ProviderOption) Option {
	opts = slices.Clone(opts)
	return func(c *config) { c.providerOptions = append(c.providerOptions, opts...) }
}

func newConfig(opts []Option) (config, error) {
	c := config{protocol: ProtocolHTTP, timeout: 10 * time.Second, metricInterval: time.Minute, traces: true, metrics: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if (c.protocol != ProtocolHTTP && c.protocol != ProtocolGRPC) || c.timeout <= 0 || c.metricInterval <= 0 || (!c.traces && !c.metrics) {
		return config{}, ErrInvalidConfig
	}
	for _, endpoint := range []string{c.endpoint, c.traceEndpoint, c.metricEndpoint} {
		if endpoint == "" {
			continue
		}
		u, err := url.Parse(endpoint)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return config{}, fmt.Errorf("%w: endpoints require an http(s) URL without credentials, query or fragment", ErrInvalidConfig)
		}
		if c.protocol == ProtocolGRPC && u.Path != "" && u.Path != "/" {
			return config{}, fmt.Errorf("%w: gRPC endpoints cannot have a path", ErrInvalidConfig)
		}
		if c.tlsConfig != nil && u.Scheme == "http" {
			return config{}, fmt.Errorf("%w: TLS configuration requires an https endpoint", ErrInvalidConfig)
		}
	}
	return c, nil
}

func (c config) signalEndpoint(override, signal string) string {
	if override != "" {
		return override
	}
	if c.endpoint == "" || c.protocol == ProtocolGRPC {
		return c.endpoint
	}
	endpoint, _ := url.JoinPath(c.endpoint, "v1", signal)
	return endpoint
}
