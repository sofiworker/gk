package otlp

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
)

func newTraceExporter(ctx context.Context, c config) (sdktrace.SpanExporter, error) {
	endpoint := c.signalEndpoint(c.traceEndpoint, "traces")
	if c.protocol == ProtocolGRPC {
		opts := []otlptracegrpc.Option{otlptracegrpc.WithTimeout(c.timeout)}
		if endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpointURL(endpoint))
		}
		if c.headers != nil {
			opts = append(opts, otlptracegrpc.WithHeaders(c.headers))
		}
		if c.tlsConfig != nil {
			opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(c.tlsConfig)))
		}
		return otlptracegrpc.New(ctx, opts...)
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithTimeout(c.timeout)}
	if endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
	}
	if c.headers != nil {
		opts = append(opts, otlptracehttp.WithHeaders(c.headers))
	}
	if c.tlsConfig != nil {
		opts = append(opts, otlptracehttp.WithTLSClientConfig(c.tlsConfig))
	}
	return otlptracehttp.New(ctx, opts...)
}

func newMetricExporter(ctx context.Context, c config) (sdkmetric.Exporter, error) {
	endpoint := c.signalEndpoint(c.metricEndpoint, "metrics")
	if c.protocol == ProtocolGRPC {
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithTimeout(c.timeout)}
		if endpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpointURL(endpoint))
		}
		if c.headers != nil {
			opts = append(opts, otlpmetricgrpc.WithHeaders(c.headers))
		}
		if c.tlsConfig != nil {
			opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(c.tlsConfig)))
		}
		return otlpmetricgrpc.New(ctx, opts...)
	}
	opts := []otlpmetrichttp.Option{otlpmetrichttp.WithTimeout(c.timeout)}
	if endpoint != "" {
		opts = append(opts, otlpmetrichttp.WithEndpointURL(endpoint))
	}
	if c.headers != nil {
		opts = append(opts, otlpmetrichttp.WithHeaders(c.headers))
	}
	if c.tlsConfig != nil {
		opts = append(opts, otlpmetrichttp.WithTLSClientConfig(c.tlsConfig))
	}
	return otlpmetrichttp.New(ctx, opts...)
}
