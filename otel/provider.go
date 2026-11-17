package gotel

import "context"

// Shutdowner 是可选的生命周期能力；由创建并拥有 SDK 的 provider 实现。
type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

// Flusher 是可选的立即导出能力；不会关闭 provider。
type Flusher interface {
	ForceFlush(ctx context.Context) error
}

// Provider 是 Tracer、Meter 与生命周期的可选组合。
// 消费方应按需依赖 Tracer 或 Meter，指标后端不必实现追踪。
type Provider interface {
	Tracer
	Meter
	Shutdowner
}
