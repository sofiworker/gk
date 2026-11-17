# Poller

[English](../poller.md) | **简体中文** | [文档目录](README.md)

[源码](../../poller/) · `github.com/sofiworker/keel/poller`

网络运行时原语包括 readiness/completion 接口、环形缓冲与时间轮，为[网络能力层](networking.md)提供基础。

## 引擎

`NewEngine` 返回 `EventEngine`，支持注册读写兴趣、修改或删除注册、
`Poll` 事件循环、`Wake` 投递任务与 `Close` 请求关闭。
Handler 在 Poll goroutine 内串行执行，需要快速返回。

Linux 使用 epoll，macOS 与受支持的 BSD 使用 kqueue，其他平台当前返回 `ErrNotSupported`。
Linux io_uring completion 接口独立，可用性受内核能力限制。

`WithPollInterval` 的负值表示阻塞等待（默认），零表示 busy polling，
正值表示定时轮询；busy polling 用空闲 CPU 换取延迟。
公开错误包括 `ErrNotSupported`、`ErrClosed` 和 `ErrNotRegistered`。

## 验证

```sh
go test ./poller
```

平台实现由 build tag 选择，使用前请阅读[平台支持](platform-support.md)。
