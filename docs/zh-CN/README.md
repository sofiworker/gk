# Keel 文档

[English](../README.md) | **简体中文**

这里是 Keel 当前开发版本的文档入口。参见[项目首页](../../README.zh-CN.md)与[开发状态](development.md#开发状态)。

## 入门指南

| 文档 | 内容 |
| --- | --- |
| [安装说明](installation.md) | 工具链与模块安装 |
| [使用说明](usage.md) | 可运行示例与资源归属 |
| [架构](architecture.md) | 职责与依赖边界 |
| [迁移指南](migration.md) | 仓库、包路径与标识符 |
| [平台支持](platform-support.md) | 网络后端与使用条件 |

## 包使用指南

| 包目录 | 指南内容 |
| --- | --- |
| [`agent`](agent.md) | Agent 定义与执行 |
| [`cache`](cache.md) | 缓存契约、内存缓存、LRU/LFU/TTL |
| [`compress`](compress.md) | Gzip、zlib、flate 与归档 |
| [`config`](configuration.md) | 文件、环境变量与远程配置 |
| [`crypt`](crypt.md) | 加解密与签名 |
| [`gerr`](errors.md) | 错误树、类别与错误码 |
| [`http`](http.md) | 路由、绑定、中间件与 OpenAPI |
| [`log`](logging.md) | 结构化日志与轮转 |
| [`net`](networking.md) | Reactor、抓包、报文与转发 |
| [`otel`](telemetry.md) | 通用 Tracer/Meter 契约 |
| [`poller`](poller.md) | 事件引擎、环形缓冲与时间轮 |
| [`resolver`](dns.md) | DNS 配置与解析器 |
| [`retry`](retry.md) | 退避、抖动与取消 |
| [`rx`](reflection.md) | 反射工具 |
| [`sd`](discovery.md) | 发现、注册与负载均衡 |
| [`sql`](sql.md) | 查询、事务与迁移 |

## HTTP 与遥测

- [HTTP 客户端](http-client.md)
- [HTTP 线格式](http-wire.md)
- [HTTP 遥测](http-telemetry.md)
- [OpenTelemetry 适配器](otel-adapter.md)
- [OTLP 导出](otlp.md)
- [OTel Prometheus 导出](otel-prometheus.md)
- [原生 Prometheus 指标](prometheus.md)

## Agent 专题

- [Agent 执行器](agent-runner.md)
- [模型上下文](agent-context.md)
- [UUID v7](agent-id.md)
- [模型契约](agent-model.md)
- [Agent 运行时](agent-runtime.md)
- [Sandbox](agent-sandbox.md)
- [会话存储](agent-session.md)
- [工具](agent-tools.md)
- [内置文件工具](agent-builtin-tools.md)
- [模型 HTTP 适配器](agent-model-http.md)
- [Agent HTTP 传输](agent-http-transport.md)
- [Agent 示例](agent-examples.md)

## 开发与维护

- [开发说明](development.md)：状态、源码结构、检查与文档约定。
- [发布流程](release.md)：版本、多模块 tag 与发布顺序。
- [变更记录](../../CHANGELOG.md)：破坏性变更。
- [编码约定](../../AGENTS.md)：仓库规则。

## 阅读约定

文档标题与包目录不一定等于 Go package 声明，差异见[迁移指南](migration.md#包标识符)。
示例片段在应用上下文中使用，完整程序见[使用说明](usage.md)。
所有英文主题在 docs/ 下，中文在 docs/zh-CN/ 下使用相同文件名。
