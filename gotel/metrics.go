package gotel

import "context"

// Meter 创建厂商无关的指标。创建错误必须在初始化阶段处理。
// Implementations and all instruments must be safe for concurrent use.
type Meter interface {
	// Counter 创建计数器
	Counter(name string, options ...InstrumentOption) (Counter, error)
	// UpDownCounter 创建可增减计数器
	UpDownCounter(name string, options ...InstrumentOption) (UpDownCounter, error)
	// Histogram 创建直方图
	Histogram(name string, options ...InstrumentOption) (Histogram, error)
	// Gauge 创建仪表
	Gauge(name string, options ...InstrumentOption) (Gauge, error)
}

// Counter 计数器抽象
type Counter interface {
	// Add 增加非负有限值；负数、NaN、Inf 被忽略。
	Add(ctx context.Context, value float64, attributes ...KeyValue)
	// Increment 递增1
	Increment(ctx context.Context, attributes ...KeyValue)
}

// UpDownCounter 记录可增减的数量，例如活跃请求数。非有限值被忽略。
type UpDownCounter interface {
	Add(ctx context.Context, value float64, attributes ...KeyValue)
}

// Histogram 记录有限值的分布；NaN、Inf 被忽略。
type Histogram interface {
	// Record 记录值用于分布统计
	Record(ctx context.Context, value float64, attributes ...KeyValue)
}

// Gauge 设置有限的当前值；NaN、Inf 被忽略。
type Gauge interface {
	// Record 记录当前值
	Record(ctx context.Context, value float64, attributes ...KeyValue)
}
