package otel

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/sofiworker/gk/gotel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type providerConfig struct {
	serviceName         string
	serviceVersion      string
	instrumentationName string
	attributes          []gotel.KeyValue
	resource            *resource.Resource
	samplingRatio       float64
	sampler             sdktrace.Sampler
	traceExporter       sdktrace.SpanExporter
	readers             []sdkmetric.Reader
	batchOptions        []sdktrace.BatchSpanProcessorOption
	metricOptions       []sdkmetric.Option
	propagator          propagation.TextMapPropagator
}

type ProviderOption func(*providerConfig)

func WithServiceName(name string) ProviderOption {
	return func(c *providerConfig) { c.serviceName = name }
}

func WithServiceVersion(version string) ProviderOption {
	return func(c *providerConfig) { c.serviceVersion = version }
}

func WithInstrumentationName(name string) ProviderOption {
	return func(c *providerConfig) { c.instrumentationName = name }
}

func WithResourceAttributes(attrs ...gotel.KeyValue) ProviderOption {
	attrs = slices.Clone(attrs)
	return func(c *providerConfig) { c.attributes = append(c.attributes, attrs...) }
}

func WithResource(res *resource.Resource) ProviderOption {
	return func(c *providerConfig) { c.resource = res }
}

// WithSamplingRatio uses parent-based ratio sampling. The ratio must be in [0, 1].
func WithSamplingRatio(ratio float64) ProviderOption {
	return func(c *providerConfig) { c.samplingRatio = ratio }
}

// WithSampler overrides ratio sampling with a caller-supplied sampler.
func WithSampler(sampler sdktrace.Sampler) ProviderOption {
	return func(c *providerConfig) { c.sampler = sampler }
}

// WithTraceExporter transfers exporter ownership on successful construction.
func WithTraceExporter(exporter sdktrace.SpanExporter) ProviderOption {
	return func(c *providerConfig) { c.traceExporter = exporter }
}

// WithMetricReader adds an owned reader. Multiple readers may be attached.
func WithMetricReader(reader sdkmetric.Reader) ProviderOption {
	return func(c *providerConfig) { c.readers = append(c.readers, reader) }
}

func WithBatchOptions(opts ...sdktrace.BatchSpanProcessorOption) ProviderOption {
	opts = slices.Clone(opts)
	return func(c *providerConfig) { c.batchOptions = append(c.batchOptions, opts...) }
}

// WithMetricOptions configures SDK views and other advanced meter settings.
// Provider-managed resource and readers are applied after these options.
func WithMetricOptions(opts ...sdkmetric.Option) ProviderOption {
	opts = slices.Clone(opts)
	return func(c *providerConfig) { c.metricOptions = append(c.metricOptions, opts...) }
}

func WithProviderPropagator(propagator propagation.TextMapPropagator) ProviderOption {
	return func(c *providerConfig) { c.propagator = propagator }
}

func newProviderConfig(opts []ProviderOption) (providerConfig, error) {
	c := providerConfig{
		instrumentationName: "github.com/sofiworker/gk/gotel",
		resource:            resource.Default(),
		samplingRatio:       1,
		batchOptions:        []sdktrace.BatchSpanProcessorOption{sdktrace.WithBatchTimeout(5 * time.Second), sdktrace.WithExportTimeout(30 * time.Second)},
		propagator:          propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if strings.TrimSpace(c.instrumentationName) == "" || c.resource == nil || c.propagator == nil ||
		math.IsNaN(c.samplingRatio) || c.samplingRatio < 0 || c.samplingRatio > 1 {
		return providerConfig{}, ErrInvalidConfig
	}
	for _, reader := range c.readers {
		if reader == nil {
			return providerConfig{}, ErrInvalidConfig
		}
	}
	if c.sampler == nil {
		c.sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.samplingRatio))
	}
	return c, nil
}
