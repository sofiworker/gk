// Package otlp creates managed OpenTelemetry providers that export traces and
// metrics over OTLP HTTP/protobuf or gRPC. Jaeger can receive the trace signal;
// collectors and other compatible backends can receive both signals.
package otlp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sofiworker/gk/gotel"
	oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func New(ctx context.Context, opts ...Option) (provider *oteladapter.Provider, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := newConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("gotel/otlp: options: %w", err)
	}
	var owned []gotel.Shutdowner
	defer func() {
		if err == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, item := range owned {
			err = errors.Join(err, item.Shutdown(cleanupCtx))
		}
	}()
	providerOpts := c.providerOptions
	if c.traces {
		exporter, createErr := newTraceExporter(ctx, c)
		if createErr != nil {
			return nil, fmt.Errorf("gotel/otlp: create trace exporter: %w", createErr)
		}
		owned = append(owned, exporter)
		providerOpts = append(providerOpts, oteladapter.WithTraceExporter(exporter))
	}
	if c.metrics {
		exporter, createErr := newMetricExporter(ctx, c)
		if createErr != nil {
			return nil, fmt.Errorf("gotel/otlp: create metric exporter: %w", createErr)
		}
		reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(c.metricInterval), sdkmetric.WithTimeout(c.timeout))
		owned = append(owned, reader)
		providerOpts = append(providerOpts, oteladapter.WithMetricReader(reader))
	}
	return oteladapter.NewProvider(ctx, providerOpts...)
}
