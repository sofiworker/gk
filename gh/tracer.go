package ghttp

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Tracer interface {
	StartSpan() (context.Context, func())
	SpanName() string
	SetAttribute(key string, value interface{})
}

type OpenTelemetryTracer struct {
	span trace.Span
}

func (o *OpenTelemetryTracer) StartSpan() (context.Context, func()) {
	ctx, span := otel.Tracer("generic").Start(context.Background(), o.SpanName())
	o.span = span
	return ctx, func() { span.End() }
}

func (o *OpenTelemetryTracer) SetAttribute(key string, value interface{}) {
	o.span.SetAttributes(attribute.KeyValue{
		Key:   attribute.Key(key),
		Value: attribute.StringValue(value.(string)),
	})
}

func (o *OpenTelemetryTracer) SpanName() string {
	return "ghttp_request"
}
