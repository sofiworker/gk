# 安装说明

[English](../installation.md) | **简体中文** | [文档目录](README.md)

## Go 工具链

| 模块或功能 | 最低 Go 版本 |
| --- | --- |
| 根模块，包括 HTTP 与遥测核心 | 1.25.12 |
| 原生 Prometheus 适配器 | 1.25.12 |
| 基础 OTel、OTLP、OTel Prometheus 适配器 | 1.26.0 |
| 通过 build tag 启用的 HTTP 泛型方法 | 1.27 |

3 个 OTel SDK 模块的最低版本由 OpenTelemetry v1.47 决定，CI 也验证 Go 1.27.1。
较低的受支持版本使用 HTTP 函数式 API，不启用带 build tag 的泛型方法。

## 根模块

```sh
go get github.com/sofiworker/keel
```

按需导入具体包，例如 `github.com/sofiworker/keel/http/client`。
改名期间，部分目录名与 Go 包声明仍不一致，当前差异见[迁移指南](migration.md#包标识符)。

## 可选遥测后端

根模块的遥测核心不引入厂商 SDK。每个后端具有独立的 Go module：

| `github.com/sofiworker/keel/` 后的导入路径 | 用途 |
| --- | --- |
| `otel/adapters/otel` | 借用已有 OTel API，或托管 SDK Provider |
| `otel/adapters/otlp` | 通过 OTLP 导出追踪与指标 |
| `otel/adapters/otelprometheus` | 托管 OTel Provider 并提供 Prometheus 抓取 handler |
| `otel/adapters/prometheus` | 直接用 Prometheus 实现指标接口 |

这些模块通过 `v0.0.0` 和本地 replace 联调，尚不能按该版本独立远程安装。
下游应用不会继承依赖模块中的 replace。

本地开发请克隆完整仓库。在另一个应用中使用后端时，需要在应用自己的 `go.mod`
中替换其用到的全部 Keel 模块。例如 OTLP 应用使用：

```gomod
replace github.com/sofiworker/keel => ../keel
replace github.com/sofiworker/keel/otel/adapters/otel => ../keel/otel/adapters/otel
replace github.com/sofiworker/keel/otel/adapters/otlp => ../keel/otel/adapters/otlp
```

路径按实际目录调整，然后运行 `go mod tidy`；选择 Prometheus 后端时增加对应模块的替换。
移除本地替换前，先发布并固定真实版本，见[发布流程](release.md)。

## 从源码开发

```sh
git clone git@github.com:sofiworker/keel.git
cd keel
go mod download
go test ./...
```

最后一条命令只覆盖根模块。独立适配器的检查方式见[开发说明](development.md)，无需全局
`go.work`。网络后端的可用范围取决于[平台](platform-support.md)。
