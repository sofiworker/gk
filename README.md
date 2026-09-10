# gk：Go 通用工具包

> **⚠️ 开发中 · 禁止直接用于生产开发**
>
> 本仓库整体仍处于开发阶段（pre-v1.0.0）：API 可能随时破坏性变更，行为与文档尚未冻结，也未经过生产环境验证。**禁止直接用于生产开发**。如确需使用，请先与维护者确认版本与稳定性，或等待正式发布。

[![Go](https://github.com/sofiworker/gk/actions/workflows/go.yml/badge.svg)](https://github.com/sofiworker/gk/actions/workflows/go.yml)
[![golangci-lint](https://github.com/sofiworker/gk/actions/workflows/go.yml/badge.svg)](https://golangci-lint.run/)
[![codecov](https://codecov.io/gh/sofiworker/gk/graph/badge.svg)](https://app.codecov.io/gh/sofiworker/gk)
[![Go Reference](https://pkg.go.dev/badge/github.com/sofiworker/gk.svg)](https://pkg.go.dev/github.com/sofiworker/gk)

[English](README.en.md) | 中文

面向构建健壮应用的 Go 库集合，各包相互独立、可积木式拼接。

## 模块

- [gai](gai/README.md) - AI 应用开发组件（包骨架；模型、智能体等能力规划中）
- [gcache](gcache/README.md) - 缓存（进程内实现 + 注入式后端契约，零第三方依赖）
- [gcompress](gcompress/README.md) - 压缩
- [gconfig](gconfig/README.md) - 配置
- [gcrypt](gcrypt/README.md) - 加密
- [gerr](gerr/README.md) - 错误
- [ghttp](ghttp/README.md) - HTTP 客户端与服务端
- [glog](glog/README.md) - 日志
- [gnet](gnet/README.md) - 网络底座：reactor 服务器、抓包、报文解析与转发
- [gotel](gotel/README.md) - OpenTelemetry 抽象
- [gresolver](gresolver/README.md) - DNS 解析
- [gretry](gretry/README.md) - 重试逻辑
- [grx](grx/README.md) - 反射扩展
- [gsd](gsd/README.md) - 服务发现与负载均衡
- [gsql](gsql/README.md) - SQL 工具

## 安装

```bash
go get github.com/sofiworker/gk
```

## Go 版本

gk 要求 Go 1.25.0 及以上。v1.0.0 之前，最低支持的 Go 版本锁定为 Go 1.25.0；所有 Go 1.25 补丁版本都应能编译并测试本模块。
