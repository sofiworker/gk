# Poller

**English** | [简体中文](zh-CN/poller.md) | [Documentation](README.md)

[Source](../poller/) · `github.com/sofiworker/keel/poller`

The network runtime primitives include readiness/completion interfaces, a ring buffer and a timing wheel.
They support the [networking layer](networking.md).

## Engines

`NewEngine` returns an `EventEngine`: register read/write interests, update or delete
registrations, run `Poll`, enqueue work with `Wake`, and request shutdown with `Close`.
Handlers execute serially on the Poll goroutine and must return quickly.

Linux uses epoll; macOS and supported BSD systems use kqueue.
Other platforms currently return `ErrNotSupported`.
The Linux io_uring completion interface is separate; availability depends on kernel support.

`WithPollInterval` selects blocking waits (negative, default), busy polling (zero),
or a positive polling interval. Busy polling trades idle CPU for latency.
The exported errors include `ErrNotSupported`, `ErrClosed` and `ErrNotRegistered`.

## Validation

```sh
go test ./poller
```

Platform backends are selected by build tags. See [Platform support](platform-support.md)
before choosing a backend.
