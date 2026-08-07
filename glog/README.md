# glog

基于 Zap 的结构化日志。

## 特性

- JSON / console 编码
- 文件输出与轮转（lumberjack）
- 原子化全局重配置
- Trace 上下文字段（trace_id、span_id，通过可选提取器注入）

## 快速开始

```go
import "github.com/sofiworker/gk/glog"

glog.Info("message", "key", "value")
```

## 配置

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
	// 处理错误
}
```

## 结构化日志

```go
glog.Info("user login", "user_id", 123, "ip", "192.168.1.1")
```

## Context 日志

```go
glog.InfoContext(ctx, "request done", "path", "/v1/items")
glog.InfofContext(ctx, "user %s", "alice") // 格式化 + context
```

Trace 字段通过可选提取器注入（glog 核心不依赖 OpenTelemetry）：

```go
glog.Configure(glog.WithTraceExtractor(func(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	return sc.TraceID().String(), sc.SpanID().String()
}))
```

## 错误处理

结构化字段非法时，glog 会记录 error 字段：

- `ErrInvalidKeyValuePairs`：key/value 参数个数为奇数
- `ErrKeyNotString`：key 不是字符串

## 刷盘

```go
_ = glog.Sync()
```
