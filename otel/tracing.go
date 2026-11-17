package gotel

import "context"

// Span 跨度抽象 - 所有追踪系统的统一抽象
type Span interface {
	// Context 获取跨度上下文
	Context() SpanContext
	// SetAttributes 设置属性
	SetAttributes(attributes ...KeyValue)
	// SetStatus 设置状态
	SetStatus(code StatusCode, description string)
	// RecordError 记录错误
	RecordError(err error, attributes ...KeyValue)
	// AddEvent 添加事件
	AddEvent(name string, attributes ...KeyValue)
	// End 结束跨度
	End(options ...SpanEndOption)
}

// SpanContext 跨度上下文 - 用于跨进程传播
type SpanContext interface {
	// TraceID 返回追踪ID
	TraceID() string
	// SpanID 返回跨度ID
	SpanID() string
	// IsSampled 返回是否采样
	IsSampled() bool
	// Serialize 序列化为可传输格式
	Serialize() map[string]string
}

// Tracer 追踪器抽象
type Tracer interface {
	// Start 开始新跨度
	Start(ctx context.Context, spanName string, opts ...SpanStartOption) (context.Context, Span)
	// Extract 从载体提取跨度上下文
	Extract(ctx context.Context, carrier TextMapCarrier) (context.Context, SpanContext)
	// Inject 将跨度上下文注入载体
	Inject(ctx context.Context, carrier TextMapCarrier, spanContext SpanContext) error
}

type StatusCode int

const (
	StatusCodeUnset StatusCode = iota
	StatusCodeOk
	StatusCodeError
)

type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

type TextMapCarrier interface {
	Get(key string) string
	Set(key string, value string)
	Keys() []string
}
