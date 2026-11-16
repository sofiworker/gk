# Keel：Go 通用工具包

> **⚠️ 开发中 · 禁止直接用于生产开发**
>
> 本仓库整体仍处于开发阶段（pre-v1.0.0）：API 可能随时破坏性变更，行为与文档尚未冻结，也未经过生产环境验证。**禁止直接用于生产开发**。如确需使用，请先与维护者确认版本与稳定性，或等待正式发布。

[![CI](https://github.com/sofiworker/keel/actions/workflows/go.yml/badge.svg?branch=master&event=push)](https://github.com/sofiworker/keel/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/sofiworker/keel/branch/master/graph/badge.svg)](https://app.codecov.io/gh/sofiworker/keel)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/keel.svg)](https://pkg.go.dev/github.com/sofiworker/keel)

[English](README.en.md) | 中文

Keel 意为船的「龙骨」。这是面向构建健壮应用的 Go 库集合，各包相互独立、可积木式拼接。

## 模块

- [gai](gai/README.md) - Agent SDK 核心定义与 UUID v7 工具（运行时待实现）
- [gcache](gcache/README.md) - 缓存（进程内实现 + 注入式后端契约，零第三方依赖）
- [gcompress](gcompress/README.md) - 压缩
- [gconfig](gconfig/README.md) - 配置
- [gcrypt](gcrypt/README.md) - 加密
- [gerr](gerr/README.md) - 错误
- [ghttp](ghttp/README.md) - HTTP 客户端与服务端
- [glog](glog/README.md) - 日志
- [gnet](gnet/README.md) - 网络底座：reactor 服务器、抓包、报文解析与转发
- [gotel](gotel/README.md) - 厂商无关的追踪与指标接口，后端适配器按需引入
- [gresolver](gresolver/README.md) - DNS 解析
- [gretry](gretry/README.md) - 重试逻辑
- [grx](grx/README.md) - 反射扩展
- [gsd](gsd/README.md) - 服务发现与负载均衡
- [gsql](gsql/README.md) - SQL 工具

## 安装

根模块要求 Go 1.25.12+；可选的 `gotel/adapters/otel`、`gotel/adapters/otlp` 和 `gotel/adapters/otelprometheus` 模块因 OpenTelemetry 依赖要求 Go 1.26.0+。

```bash
go get github.com/sofiworker/keel
```

## 从 gk 迁移（破坏性变更）

项目已更名为 Keel。将 Go import 和 `go.mod`（包括 `require`、`replace`）中的 `github.com/sofiworker/gk` 前缀统一替换为 `github.com/sofiworker/keel`，然后运行 `go mod tidy` 和项目测试。4 个独立的 `gotel/adapters/*` 模块同样需要更新路径；`ghttp`、`gnet` 等子包名称保持不变。

例如：`import "github.com/sofiworker/keel/ghttp"`。

OpenTelemetry 适配器的默认 instrumentation scope 名称同步改为 `github.com/sofiworker/keel/gotel`。依赖旧 scope 名称的仪表盘或查询需要更新，也可以通过 `WithInstrumentationName` 显式配置原名称。

