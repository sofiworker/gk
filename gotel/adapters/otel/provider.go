package otel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sofiworker/gk/gotel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Provider owns SDK providers, its configured exporter and readers. It does not
// install global providers. Use NewTracer/NewMeter when the caller owns the SDK.
type Provider struct {
	*Tracer
	*Meter
	traceProvider  *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	traceExporter  *ownedSpanExporter
	batchProcessor sdktrace.SpanProcessor
	shutdownOnce   sync.Once
	shutdownErr    error
}

var (
	_ gotel.Provider = (*Provider)(nil)
	_ gotel.Flusher  = (*Provider)(nil)
)

func NewProvider(ctx context.Context, opts ...ProviderOption) (*Provider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := newProviderConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("gotel/otel: provider options: %w", err)
	}
	attrs := attributes(c.attributes)
	if c.serviceName != "" {
		attrs = append(attrs, attribute.String("service.name", c.serviceName))
	}
	if c.serviceVersion != "" {
		attrs = append(attrs, attribute.String("service.version", c.serviceVersion))
	}
	res, err := resource.Merge(c.resource, resource.NewSchemaless(attrs...))
	if err != nil {
		return nil, fmt.Errorf("gotel/otel: merge resource: %w", err)
	}
	traceOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res), sdktrace.WithSampler(c.sampler)}
	var traceExporter *ownedSpanExporter
	var batchProcessor sdktrace.SpanProcessor
	if c.traceExporter != nil {
		traceExporter = &ownedSpanExporter{SpanExporter: c.traceExporter}
		batchProcessor = sdktrace.NewBatchSpanProcessor(traceExporter, c.batchOptions...)
		traceOpts = append(traceOpts, sdktrace.WithSpanProcessor(batchProcessor))
	}
	traceProvider := sdktrace.NewTracerProvider(traceOpts...)
	meterOpts := append(c.metricOptions, sdkmetric.WithResource(res))
	for _, reader := range c.readers {
		meterOpts = append(meterOpts, sdkmetric.WithReader(reader))
	}
	meterProvider := sdkmetric.NewMeterProvider(meterOpts...)
	return &Provider{
		Tracer:         &Tracer{tracer: traceProvider.Tracer(c.instrumentationName), propagator: c.propagator},
		Meter:          &Meter{meter: meterProvider.Meter(c.instrumentationName)},
		traceProvider:  traceProvider,
		meterProvider:  meterProvider,
		traceExporter:  traceExporter,
		batchProcessor: batchProcessor,
	}, nil
}

// TracerProvider exposes native SDK capabilities, including links and processors.
func (p *Provider) TracerProvider() *sdktrace.TracerProvider { return p.traceProvider }

// MeterProvider exposes native instruments, including asynchronous instruments.
func (p *Provider) MeterProvider() *sdkmetric.MeterProvider { return p.meterProvider }

func (p *Provider) ForceFlush(ctx context.Context) error {
	return errors.Join(p.traceProvider.ForceFlush(ctx), p.meterProvider.ForceFlush(ctx))
}

// Shutdown flushes both signals and attempts both shutdowns even if one fails.
// Concurrent callers wait for the first shutdown and receive the same result.
func (p *Provider) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		// The batch processor reports failures while draining on Shutdown through
		// the global error handler. Flush first so the caller receives those
		// errors, and close all owned components even when flushing fails.
		traceErr := errors.Join(p.traceProvider.ForceFlush(ctx), p.traceProvider.Shutdown(ctx))
		// A canceled context can make the SDK return before stopping processors.
		if p.batchProcessor != nil {
			traceErr = errors.Join(traceErr, p.batchProcessor.Shutdown(ctx))
		}
		// The SDK batch processor logs exporter shutdown errors instead of
		// returning them. Retain that result and still attempt metric shutdown.
		if p.traceExporter != nil {
			traceErr = errors.Join(traceErr, p.traceExporter.Shutdown(ctx))
		}
		p.shutdownErr = errors.Join(traceErr, p.meterProvider.Shutdown(ctx))
	})
	return p.shutdownErr
}

type ownedSpanExporter struct {
	sdktrace.SpanExporter
	shutdownOnce sync.Once
	shutdownErr  error
}

func (e *ownedSpanExporter) Shutdown(ctx context.Context) error {
	e.shutdownOnce.Do(func() { e.shutdownErr = e.SpanExporter.Shutdown(ctx) })
	return e.shutdownErr
}
