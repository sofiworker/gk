package gclient

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Tracer 定义通用的追踪器接口，支持多种不同的otel库
type Tracer interface {
	StartSpan(ctx context.Context) (context.Context, func())
	SpanName() string
	SetAttribute(key string, value interface{})
}

// OpenTelemetryTracer OpenTelemetry实现
type OpenTelemetryTracer struct {
	span trace.Span
}

// StartSpan 启动一个新的span
func (o *OpenTelemetryTracer) StartSpan(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := otel.Tracer("generic").Start(ctx, o.SpanName())
	o.span = span
	return ctx, func() { span.End() }
}

// SetAttribute 设置span属性
func (o *OpenTelemetryTracer) SetAttribute(key string, value interface{}) {
	if o.span == nil {
		return
	}

	// 根据值的类型设置不同的attribute
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
		o.span.SetAttributes(attribute.String(key, v.(string)))
	}
}

// SpanName 返回span名称
func (o *OpenTelemetryTracer) SpanName() string {
	return "ghttp_request"
}

// JaegerTracer Jaeger实现示例（需要根据实际Jaeger SDK调整）
type JaegerTracer struct {
	// Jaeger特定的字段
}

// StartSpan Jaeger实现
func (j *JaegerTracer) StartSpan(ctx context.Context) (context.Context, func()) {
	// Jaeger实现逻辑
	// 这里仅作示例，实际需要根据Jaeger SDK实现
	return ctx, func() {}
}

// SetAttribute Jaeger实现
func (j *JaegerTracer) SetAttribute(key string, value interface{}) {
	// Jaeger设置属性逻辑
}

// SpanName Jaeger实现
func (j *JaegerTracer) SpanName() string {
	return "ghttp_request"
}

// NoopTracer 空实现，用于禁用追踪
type NoopTracer struct{}

// StartSpan 空实现
func (n *NoopTracer) StartSpan(ctx context.Context) (context.Context, func()) {
	return ctx, func() {}
}

// SetAttribute 空实现
func (n *NoopTracer) SetAttribute(key string, value interface{}) {
	// 空实现
}

// SpanName 空实现
func (n *NoopTracer) SpanName() string {
	return "ghttp_request"
}
