package gclient

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Tracer 定义通用追踪接口。
type Tracer interface {
	StartSpan(ctx context.Context) (context.Context, func())
	SpanName() string
	SetAttribute(key string, value interface{})
}

// OpenTelemetryTracer 是基于 OpenTelemetry 的追踪实现。
type OpenTelemetryTracer struct {
	span trace.Span
}

// StartSpan 启动一个新的 span。
func (o *OpenTelemetryTracer) StartSpan(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := otel.Tracer("generic").Start(ctx, o.SpanName())
	o.span = span
	return ctx, func() { span.End() }
}

// SetAttribute 设置 span 属性。
func (o *OpenTelemetryTracer) SetAttribute(key string, value interface{}) {
	if o.span == nil {
		return
	}

	switch v := value.(type) {
	case string:
		o.span.SetAttributes(attribute.String(key, v))
	case int:
		o.span.SetAttributes(attribute.Int(key, v))
	case int64:
		o.span.SetAttributes(attribute.Int64(key, v))
	case float64:
		o.span.SetAttributes(attribute.Float64(key, v))
	case bool:
		o.span.SetAttributes(attribute.Bool(key, v))
	default:
		o.span.SetAttributes(attribute.String(key, fmt.Sprint(value)))
	}
}

// SpanName 返回默认 span 名称。
func (o *OpenTelemetryTracer) SpanName() string {
	return "ghttp_request"
}

// JaegerTracer 是 Jaeger 的占位实现，便于后续接入具体 SDK。
type JaegerTracer struct{}

// StartSpan 启动 Jaeger span。
func (j *JaegerTracer) StartSpan(ctx context.Context) (context.Context, func()) {
	return ctx, func() {}
}

// SetAttribute 设置 Jaeger span 属性。
func (j *JaegerTracer) SetAttribute(key string, value interface{}) {}

// SpanName 返回默认 span 名称。
func (j *JaegerTracer) SpanName() string {
	return "ghttp_request"
}

// NoopTracer 是空实现，用于禁用追踪。
type NoopTracer struct{}

// StartSpan 直接返回原始 context。
func (n *NoopTracer) StartSpan(ctx context.Context) (context.Context, func()) {
	return ctx, func() {}
}

// SetAttribute 是空操作。
func (n *NoopTracer) SetAttribute(key string, value interface{}) {}

// SpanName 返回默认 span 名称。
func (n *NoopTracer) SpanName() string {
	return "ghttp_request"
}
