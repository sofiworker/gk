# gerr

错误包装、匹配、多错误遍历，以及标准库错误形状的辅助函数。
Error wrapping, matching, multi-error traversal, and standard library error helpers.

`gerr` 遵循 Go 的标准错误树模型：Go 1.13 的 `Unwrap`/`errors.Is`/`errors.As`/`%w`，以及 Go 1.20 的 `Unwrap() []error` 与 `errors.Join`。
`gerr` follows Go's standard error tree model: Go 1.13 `Unwrap`/`errors.Is`/`errors.As`/`%w` and Go 1.20 multi-error trees via `errors.Join`.

在标准库之上，`gerr` 提供一个小型业务错误类型和常见标准库形状的便捷判断（`net.Error`、`net.OpError`、`os.PathError`、context 取消等）。
It adds a small application error type plus helpers for common standard library shapes such as `net.Error` and context cancellation.

## 包装 / Wrapping

```go
err := gerr.Wrap(
	cause,
	"query user",
	gerr.WithCode("db.query"),
	gerr.WithKind(gerr.KindUnavailable),
	gerr.WithOp("user.lookup"),
	gerr.WithMeta("user_id", 42),
)

if gerr.IsCode(err, "db.query") {
	// 处理数据库查询失败 / handle database query failure
}
```

## 多错误 / Multi Error

```go
err := gerr.NewMulti("validate config", errA, errB, errC)

if gerr.IsKind(err, gerr.KindInvalid) {
	// 至少一个子错误为 invalid / at least one child is invalid
}

for _, leaf := range gerr.Flatten(err) {
	_ = leaf
}
```

## 标准库辅助 / Standard Library Helpers

```go
if gerr.IsTimeout(err) {
	// 覆盖 context deadline、os timeout、net.Error timeout
}

if dnsErr, ok := gerr.AsDNSError(err); ok {
	_ = dnsErr.Name
}

if gerr.IsClosed(err) {
	// 覆盖 net.ErrClosed、os.ErrClosed、fs.ErrClosed、io.ErrClosedPipe
}
```
