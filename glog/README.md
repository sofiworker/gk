# glog

基于 Zap 的结构化日志。
Structured logging based on Zap.

## 特性 / Features

- JSON / console 编码；JSON/console encoding
- 文件输出与轮转（lumberjack）；file output with rotation (lumberjack)
- 原子化全局重配置；atomic global reconfiguration
- Trace 上下文字段（通过可选提取器注入）；trace context fields (via optional extractor)

## 快速开始 / Quick Start

```go
import "github.com/sofiworker/gk/glog"

glog.Info("message", "key", "value")
```

## 配置 / Configure

```go
err := glog.Configure(
	glog.WithLevel(glog.InfoLevel),
	glog.WithEncoding(glog.JSONEncoding),
	glog.WithOutputPaths("/var/log/app.log"),
	glog.WithRotation(100, 30, 7, true, true),
	glog.WithInitialFields(map[string]interface{}{"service": "api"}),
	glog.WithTimeFormat("2006-01-02 15:04:05.000"),
)
if err != nil {
	// 处理错误 / handle error
}
```

## 结构化与 Context 日志 / Structured and Context Logging

```go
glog.Info("user login", "user_id", 123, "ip", "192.168.1.1")
glog.InfoContext(ctx, "request done", "path", "/v1/items")
glog.InfofContext(ctx, "user %s", "alice") // 格式化 + context / formatted + context
```

Trace 字段通过可选提取器注入（glog 核心不依赖 OpenTelemetry）：
Trace fields are injected via an optional extractor (the core does not depend on OpenTelemetry):

```go
glog.Configure(glog.WithTraceExtractor(func(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	return sc.TraceID().String(), sc.SpanID().String()
}))
```

## 错误处理 / Error Handling

结构化字段非法时记录 error 字段：`ErrInvalidKeyValuePairs`（参数个数为奇数）、`ErrKeyNotString`（key 非字符串）。
Invalid structured fields record an error field: `ErrInvalidKeyValuePairs` (odd args) and `ErrKeyNotString`.

## 刷盘 / Flush

```go
_ = glog.Sync()
```
