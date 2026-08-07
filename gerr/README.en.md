# gerr

English | [中文](README.md)

Error wrapping, matching, multi-error traversal, and standard library error helpers.

`gerr` follows Go's standard error tree model: Go 1.13 `Unwrap`/`errors.Is`/`errors.As`/`%w` and Go 1.20 multi-error trees via `errors.Join`.

It adds a small application error type plus helpers for common standard library shapes such as `net.Error` and context cancellation.

## Wrapping

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
	// handle database query failure
}
```

## Multi Error

```go
err := gerr.NewMulti("validate config", errA, errB, errC)

if gerr.IsKind(err, gerr.KindInvalid) {
	// at least one child is invalid
}

for _, leaf := range gerr.Flatten(err) {
	_ = leaf
}
```

## Standard Library Helpers

```go
if gerr.IsTimeout(err) {
	// handles context deadline, os timeout, net.Error timeout
}

if dnsErr, ok := gerr.AsDNSError(err); ok {
	_ = dnsErr.Name
}

if gerr.IsClosed(err) {
	// handles net.ErrClosed, os.ErrClosed, fs.ErrClosed, io.ErrClosedPipe
}
```
