# 架构

[English](../architecture.md) | **简体中文** | [文档目录](README.md)

## 包边界

Keel 由一个根 Go module 和 4 个独立的遥测后端 module 组成。
应用从根模块导入所需的具体包，显式组合运行时与遥测后端；各包的实际 import 决定依赖。

| 层次 | 职责 | 文档 |
| --- | --- | --- |
| 工具 | 缓存契约、错误、配置、日志、重试、加解密与压缩 | [包目录](README.md#包使用指南) |
| 网络运行时 | Readiness/completion 契约、环形缓冲与时间轮 | [Poller](poller.md) |
| 网络能力 | Reactor、抓包、解码、分帧、多路复用与转发 | [网络](networking.md) |
| HTTP | 服务端与客户端策略、共享协议原语 | [服务端](http.md)、[客户端](http-client.md)、[线格式](http-wire.md) |
| 遥测契约 | Tracer、Meter、指标与生命周期接口 | [可观测性](telemetry.md) |
| 适配器 | HTTP 遥测与厂商 SDK 集成 | [HTTP 遥测](http-telemetry.md)、[OTel](otel-adapter.md) |
| Agent 执行 | 模型、工具、会话、上下文与 Sandbox | [Agent SDK](agent.md) |

## HTTP 与遥测

```text
http server ──→ http/wire ←── http/client
http/adapters/gotel ──→ http + otel core
otel/adapters/* ──→ otel core + vendor SDK
```

wire 层提供严格解码、SSE 解析与状态码契约；重试、路由及响应策略分别留在两端。
HTTP 遥测适配器把通用 Tracer/Meter 接口转换成中间件。

根模块不 require 可选后端；每个后端拥有自己的 `go.mod` 并独立检查。
Build tag 选择平台或工具链实现，模块边界隔离厂商依赖。

## Agent 执行

Agent 定义行为，Session 选择模型与环境，Turn 记录执行。Runtime 在外部操作前记录
模型/工具调用意图，再通过 Store 提交结果；Sandbox View 为每次调用提供受限能力。

内置会话存储与 Sandbox 均在内存中，不提供崩溃恢复或操作系统进程隔离。
宿主文件同步是显式操作，不与 Session 提交共用事务。
详见[运行时](agent-runtime.md)、[存储](agent-session.md)与 [Sandbox](agent-sandbox.md)。

## 平台边界

Readiness 实现使用 Linux epoll 或 BSD/macOS kqueue，原始抓包使用平台专用后端。
不受支持的能力返回错误，具体范围见[平台支持](platform-support.md)。
