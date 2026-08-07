# gotel

可观测性统一抽象（跨追踪/指标系统的接口与值类型）。
Unified observability abstractions (interfaces and value types across tracing/metrics systems).

当前仅提供抽象（`Span`/`Tracer`/`Meter`/`Provider` 等）与基础值类型，不绑定具体实现；OpenTelemetry 适配由使用方或未来的适配子包提供。
Currently provides only abstractions and value types without binding to a concrete implementation; adapters are supplied by users or future subpackages.
