# gerr

错误包装、匹配、多错误遍历，以及标准库错误形状的辅助函数。

`gerr` 遵循 Go 的标准错误树模型：

- Go 1.13：`Unwrap`、`errors.Is`、`errors.As`、`%w` 包装。
- Go 1.20：通过 `Unwrap() []error` 与 `errors.Join` 支持多错误树。

在标准库之上，`gerr` 提供一个小型业务错误类型和常见标准库形状的便捷判断，
例如 `net.Error`、`net.OpError`、`os.PathError` 与 context 取消。

## 包装

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
	// 处理数据库查询失败
}
```

## 多错误

```go
err := gerr.NewMulti("validate config", errA, errB, errC)

if gerr.IsKind(err, gerr.KindInvalid) {
	// 至少一个子错误为 invalid
}

for _, leaf := range gerr.Flatten(err) {
	_ = leaf
}
```

## 标准库辅助

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
