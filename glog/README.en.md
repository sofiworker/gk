# glog

English | [中文](README.md)

Structured logging based on Zap.

## Features

- JSON/console encoding
- File output with rotation (lumberjack)
- Atomic global reconfiguration
- Trace context fields (via optional extractor)

## Quick Start

```go
import "github.com/sofiworker/gk/glog"

glog.Info("message", "key", "value")
```

## Configure

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
	// handle error
}
```

## Structured and Context Logging

```go
glog.Info("user login", "user_id", 123, "ip", "192.168.1.1")
glog.InfoContext(ctx, "request done", "path", "/v1/items")
glog.InfofContext(ctx, "user %s", "alice") // formatted + context
```

Trace fields are injected via an optional extractor (the core does not depend on OpenTelemetry):

```go
glog.Configure(glog.WithTraceExtractor(func(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	return sc.TraceID().String(), sc.SpanID().String()
}))
```

## Error Handling

Invalid structured fields record an error field: `ErrInvalidKeyValuePairs` (odd args) and `ErrKeyNotString`.

## Flush

```go
_ = glog.Sync()
```
