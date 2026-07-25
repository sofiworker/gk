# gerr

Structured error identities, wrapping, matching, multi-error traversal, and
standard library error helpers.

`gerr` follows Go's standard error tree model:

- Go 1.13: `Unwrap`, `errors.Is`, `errors.As`, and `%w` wrapping.
- Go 1.20: multi-error trees through `Unwrap() []error` and `errors.Join`.

It adds a small application error type plus convenience helpers for common
standard library shapes such as `net.Error`, `net.OpError`, `os.PathError`, and
context cancellation.

## Public errors and internal wrapping

```go
err := gerr.New(
	"user.not_found",
	gerr.KindNotFound,
	gerr.WithParam("user_id", 42),
	gerr.WithCause(cause),
)

descriptor, ok := gerr.Describe(err)
if ok && descriptor.ID == "user.not_found" {
	// send descriptor.Params through a transport-specific safety policy
}

err = gerr.Wrap(
	cause,
	gerr.WithOp("user.lookup"),
	gerr.WithMessage("query user"),
	gerr.WithMeta("user_id", 42),
)
```

`Params` are public protocol data. `Meta`, `Op`, `Message`, and the cause are
diagnostic data and are not included in `Descriptor`.

## Multi Error

```go
err := gerr.NewMulti(
	"request.validation_failed",
	gerr.KindInvalid,
	[]error{errA, errB, errC},
)

if descriptor, ok := gerr.Describe(err); ok {
	_ = descriptor // explicit top-level identity; children remain diagnostic
}

for _, leaf := range gerr.Flatten(err) {
	_ = leaf
}
```

## Standard Library Helpers

```go
if gerr.IsTimeout(err) {
	// handles context deadline, os timeout, and net.Error timeout
}

if dnsErr, ok := gerr.AsDNSError(err); ok {
	_ = dnsErr.Name
}

if gerr.IsClosed(err) {
	// handles net.ErrClosed, os.ErrClosed, fs.ErrClosed, and io.ErrClosedPipe
}
```
