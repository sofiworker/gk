# gk：Go 通用工具包 / Go Generic Kit

> **⚠️ 开发中 · 禁止直接用于生产开发**
>
> 本仓库整体仍处于开发阶段（pre-v1.0.0）：API 可能随时破坏性变更，行为与文档尚未冻结，也未经过生产环境验证。**禁止直接用于生产开发**。如确需使用，请先与维护者确认版本与稳定性，或等待正式发布。
>
> **⚠️ UNDER DEVELOPMENT — DO NOT USE IN PRODUCTION.**
> This repository is actively developed; APIs are unstable and unverified for production use.

[![Go](https://github.com/sofiworker/gk/actions/workflows/go.yml/badge.svg)](https://github.com/sofiworker/gk/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/gk.svg)](https://pkg.go.dev/github.com/sofiworker/gk)
[![Go Report Card](https://goreportcard.com/badge/github.com/sofiworker/gk)](https://goreportcard.com/report/github.com/sofiworker/gk)

面向构建健壮应用的 Go 库集合，各包相互独立、可积木式拼接。
A collection of Go libraries for building robust applications; packages are independent and composable.

## 模块 / Modules

- [gcache](gcache/README.md) - 缓存（内存、Redis、Valkey）/ Caching (Memory, Redis, Valkey)
- [gcompress](gcompress/README.md) - 压缩 / Compression
- [gconfig](gconfig/README.md) - 配置 / Configuration
- [gcrypt](gcrypt/README.md) - 加密 / Cryptography
- [gerr](gerr/README.md) - 错误 / Errors
- [ghttp](ghttp/README.md) - HTTP 客户端与服务端 / HTTP Client & Server
- [glog](glog/README.md) - 日志 / Logging
- [gnet](gnet/README.md) - 网络与报文分析 / Networking & Packet Analysis
- [gotel](gotel/README.md) - OpenTelemetry 抽象 / Observability abstractions
- [gresolver](gresolver/README.md) - DNS 解析 / DNS Resolver
- [gretry](gretry/README.md) - 重试逻辑 / Retry Logic
- [grx](grx/README.md) - 反射扩展 / Reflection extensions
- [gsd](gsd/README.md) - 服务发现与负载均衡 / Service Discovery & Load Balancing
- [gsql](gsql/README.md) - SQL 工具 / SQL Utilities

## 安装 / Installation

```bash
go get github.com/sofiworker/gk
```

## Go 版本 / Go Version

gk 要求 Go 1.25.0 及以上。v1.0.0 之前，最低支持的 Go 版本锁定为 Go 1.25.0；所有 Go 1.25 补丁版本都应能编译并测试本模块。
gk requires Go 1.25.0 or later. Before v1.0.0 the minimum is locked to Go 1.25.0; all Go 1.25 patch releases are expected to compile and test the module.
