# 迁移指南

[English](../migration.md) | **简体中文** | [文档目录](README.md)

## 仓库与导入路径

仓库已更名为 `github.com/sofiworker/keel`。先替换旧的
`github.com/sofiworker/gk` 前缀，再更新包路径的第一段：

| 原路径 | 当前路径 | 原路径 | 当前路径 |
| --- | --- | --- | --- |
| `gai` | `agent` | `gcache` | `cache` |
| `gcompress` | `compress` | `gconfig` | `config` |
| `gcrypt` | `crypt` | `ghttp` | `http` |
| `glog` | `log` | `gnet` | `net` |
| `gotel` | `otel` | `gpoller` | `poller` |
| `gresolver` | `resolver` | `gretry` | `retry` |
| `grx` | `rx` | `gsd` | `sd` |
| `gsql` | `sql` | `gerr` | `gerr`（保留） |

其余路径段保持不变，例如 HTTP 客户端为 `github.com/sofiworker/keel/http/client`。
HTTP 遥测适配器仍位于 `http/adapters/gotel`，独立后端模块已迁至 `otel/adapters/*`。

同步更新 import、应用的 `require`/`replace`、脚本及 CI 工作目录，
再运行 `go mod tidy` 和应用测试。4 个后端模块也需要分别检查。

## 包标识符

部分新目录仍保留旧的 package 声明。Go 默认导入标识符取自 package 声明，不取自目录名：

| 目录 | 当前 package | 建议显式别名 |
| --- | --- | --- |
| `cache` | `gcache` | `cache` |
| `http` | `ghttp` | `keelhttp` |
| `log` | `glog` | `keellog` |
| `net` | `gnet` | `keelnet` |
| `otel` | `gotel` | `telemetry` |
| `retry` | `gretry` | `retry` |
| `http/adapters/gotel` | `ghttpotel` | `httpotel` |

详细包指南中的片段可能使用这些 package 标识符。显式别名既适配当前声明，也能避开标准库
或厂商 SDK 的同名包：

```go
import (
    stdhttp "net/http"
    keelhttp "github.com/sofiworker/keel/http"
)
```

## 遥测 scope

托管 OTel 适配器当前默认 scope 名仍为 `github.com/sofiworker/keel/gotel`；
目录改名不会自动修改这个运行时字符串。仪表盘或查询依赖 scope 时，
通过 `WithInstrumentationName` 显式配置需要的名称。

## 文档路径

根目录 `README.md` 为英文，中文为 `README.zh-CN.md`。
详细英文文档集中在 `docs/`，中文位于 `docs/zh-CN/`，两种语言使用相同文件名。
原来的包内 README 与 `README.en.md` 已集中迁入文档目录，从[目录页](README.md)进入。
