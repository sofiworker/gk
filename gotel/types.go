package gotel

import (
	"context"
)

// ==================== 跨度抽象 ====================

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

// ==================== 指标抽象 ====================

// Meter 指标器抽象
type Meter interface {
	// Counter 创建计数器
	Counter(name string, options ...InstrumentOption) Counter
	// Histogram 创建直方图
	Histogram(name string, options ...InstrumentOption) Histogram
	// Gauge 创建仪表
	Gauge(name string, options ...InstrumentOption) Gauge
}

// Counter 计数器抽象
type Counter interface {
	// Add 增加计数值
	Add(ctx context.Context, value float64, attributes ...KeyValue)
	// Increment 递增1
	Increment(ctx context.Context, attributes ...KeyValue)
}

// Histogram 直方图抽象
type Histogram interface {
	// Record 记录值用于分布统计
	Record(ctx context.Context, value float64, attributes ...KeyValue)
}

// Gauge 仪表抽象
type Gauge interface {
	// Record 记录当前值
	Record(ctx context.Context, value float64, attributes ...KeyValue)
}

// ==================== 统一提供者 ====================

// Provider 可观测性提供者
type Provider interface {
	Tracer
	Meter
	// Shutdown 关闭提供者
	Shutdown(ctx context.Context) error
}

// ==================== 支持类型 ====================

type StatusCode int

const (
	StatusCodeUnset StatusCode = iota
	StatusCodeOk
	StatusCodeError
)

type KeyValue struct {
	Key   string
	Value interface{}
}

func KV(key string, value interface{}) KeyValue {
	return KeyValue{Key: key, Value: value}
}

type TextMapCarrier interface {
	Get(key string) string
	Set(key string, value string)
	Keys() []string
}

type SpanStartOption interface{}
type SpanEndOption interface{}
type InstrumentOption interface{}
