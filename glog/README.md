# glog

Structured logging based on Zap.

## Features

- JSON/console encoding
- File output with rotation (lumberjack)
- Atomic global reconfiguration
- Trace context fields (trace_id, span_id)

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

## Structured Logging

```go
glog.Info("user login", "user_id", 123, "ip", "192.168.1.1")
```

## Context Logging

```go
ctx := trace.ContextWithSpan(context.Background(), span)
glog.InfoContext(ctx, "request done", "path", "/v1/items")
```

## Error Handling

If the structured fields are invalid, glog records an error field:

- ErrInvalidKeyValuePairs: odd number of key/value arguments
- ErrKeyNotString: key is not a string

## Flush

```go
_ = glog.Sync()
```
