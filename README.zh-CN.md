# Keel

[English](README.md) | **简体中文**

面向应用开发的 Go 库集合，覆盖 HTTP、网络、缓存、配置、可观测性与 Agent。
Keel 意为船的「龙骨」；各包职责独立，可以按需组合。

[![CI](https://github.com/sofiworker/keel/actions/workflows/go.yml/badge.svg?branch=master&event=push)](https://github.com/sofiworker/keel/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/sofiworker/keel/branch/master/graph/badge.svg)](https://app.codecov.io/gh/sofiworker/keel)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/keel.svg)](https://pkg.go.dev/github.com/sofiworker/keel)

> **开发中，pre-v1.0.0，禁止直接用于生产开发。**
> API 和存储格式可能发生不兼容变更，使用前请阅读[开发状态与使用限制](docs/zh-CN/development.md#开发状态)。

## 能力

| 领域 | 内容 | 文档 |
| --- | --- | --- |
| HTTP | 服务端、客户端、共享线格式、追踪与指标中间件 | [HTTP 服务端](docs/zh-CN/http.md)、[客户端](docs/zh-CN/http-client.md) |
| 网络 | Reactor、抓包、pcap/pcapng、分帧、多路复用与转发 | [网络](docs/zh-CN/networking.md)、[平台支持](docs/zh-CN/platform-support.md) |
| 工具 | 缓存、配置、压缩、加解密、错误、日志、重试与反射 | [包使用指南](docs/zh-CN/README.md#包使用指南) |
| 基础设施 | DNS、服务发现、负载均衡与 SQL | [DNS](docs/zh-CN/dns.md)、[服务发现](docs/zh-CN/discovery.md)、[SQL](docs/zh-CN/sql.md) |
| 可观测性 | 后端无关的追踪与指标接口，4 个可选后端模块 | [可观测性](docs/zh-CN/telemetry.md) |
| Agent | 模型与工具调用、会话、上下文预算与内存 Sandbox | [Agent SDK](docs/zh-CN/agent.md) |

## 安装

根模块要求 **Go 1.25.12+**；OTel、OTLP、OTel Prometheus 适配器要求 **Go 1.26.0+**。
通过 build tag 启用的 HTTP 泛型方法要求 Go 1.27+。

```sh
go get github.com/sofiworker/keel
```

可选后端模块当前通过本地 replace 联调，在其他项目中使用前请先阅读[安装说明](docs/zh-CN/installation.md)。

## 快速开始

按需导入包，与标准库并用时可以显式设置别名：

```go
import (
    stdhttp "net/http"

    keelhttp "github.com/sofiworker/keel/http"
    cache "github.com/sofiworker/keel/cache"
)
```

[使用说明](docs/zh-CN/usage.md)提供完整可运行的 HTTP 和内存缓存示例。

## 文档

| 文档 | 内容 |
| --- | --- |
| [文档目录](docs/zh-CN/README.md) | 全部指南与包文档 |
| [安装说明](docs/zh-CN/installation.md) | 工具链、根模块与可选后端 |
| [使用说明](docs/zh-CN/usage.md) | 完整示例与生命周期约定 |
| [架构](docs/zh-CN/architecture.md) | 包职责与依赖边界 |
| [迁移指南](docs/zh-CN/migration.md) | 从 gk 及旧的 g 前缀导入路径迁移 |
| [平台支持](docs/zh-CN/platform-support.md) | 网络后端、权限与 Windows Npcap |
| [开发说明](docs/zh-CN/development.md) | 目录、状态、检查项与文档约定 |
| [发布流程](docs/zh-CN/release.md) | 5 个模块的版本与发布顺序 |
| [变更记录](CHANGELOG.md) | 已记录的破坏性变更 |

英文文档位于 [docs](docs/README.md)。

## 参与开发

通过 [GitHub Issues](https://github.com/sofiworker/keel/issues) 和 Pull Request 提交问题或改进。
问题报告请包含包名、Go 版本、操作系统与最小复现；提交前执行[开发说明](docs/zh-CN/development.md)中的检查。
