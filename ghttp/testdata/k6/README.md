# ghttp k6 针对性测试

[English](README.en.md)

> 本仓库仍处于 pre-v1.0.0 开发阶段。本测试工具仅用于开发期定向验证，禁止直接作为生产压测方案、容量结论或 SLO 依据。

本目录提供独立测试服务、k6 场景、raw-socket 对抗探针与资源快照工具，覆盖功能契约、路由、binding/codec、错误与安全、状态并发、SSE/WebSocket、稳定性和恢复行为。它追求协议与边界覆盖，不模拟某个真实业务流量模型。

## 场景

| 场景 | 主要覆盖 |
| --- | --- |
| `contract_smoke` | health、binding、output、Problem Details、OpenAPI 基础契约 |
| `routing_matrix` | 静态、参数、通配、分组与优先级路由 |
| `binding_codec` | query/body 混合绑定、内容协商、codec 错误 |
| `error_security` | validation、认证失败、panic recovery 与恢复健康 |
| `protocol` | 方法、HEAD、路由 wire 行为 |
| `errors` | 404/Problem Details 基线 |
| `security` | bearer 认证矩阵、secret 隔离、运行指标恢复 |
| `state` | namespace 隔离、幂等、ETag、CRUD 生命周期 |
| `sse` | event-stream wire、事件与取消时限 |
| `websocket` | upgrade、echo、关闭与恢复 |
| `stability` | health/error/state 的 ramping 与 arrival-rate 混合稳定性 |

## 指标和阈值

共享指标包括 `route_duration`、`route_success`、`schema_success`、`secret_leaks`、`auth_success`、`negotiation_success`、`openapi_success`。SSE、WebSocket 等场景另有自身恢复/取消指标。场景只为实际产生的指标配置阈值，避免无样本阈值；默认功能/安全 Rate 要求为 100%，secret counter 必须为 0。`smoke` 的 HTTP p95 默认小于 1 秒，较长 profile 的默认 p95/p99 更严格，但这些只是开发门禁，不是生产性能承诺。

## 运行

需要 Go、k6，以及用于静态检查的 Node.js。从仓库根目录执行：

```powershell
powershell -ExecutionPolicy Bypass -File ghttp/testdata/k6/scripts/check-js.ps1
powershell -ExecutionPolicy Bypass -File ghttp/testdata/k6/scripts/count-checks.ps1
powershell -ExecutionPolicy Bypass -File ghttp/testdata/k6/run.ps1 -Profile smoke -Scenario contract_smoke
```

常用 runner 参数：`-Profile smoke|full`、`-Scenario <name>`、`-OutputDir`、`-Addr`、`-BaseUrl`、`-StaticDir`、`-MaxBodyBytes`、`-Secret`、`-ShutdownSeconds`。若设置 `BaseUrl`，它必须与本次 server 的 `READY` URL 完全一致。k6 共享环境变量包括 `BASE_URL`、`PROFILE`、`VUS`、`MAX_VUS`、`ITERATIONS`、`RATE`、`DURATION`、`SOAK_DURATION`、`SECRET`。

runner 会构建临时 server/rawprobe/resources 可执行文件，启动服务并验证 health，然后运行资源采样、指定 k6 场景和 rawprobe。不要把本目录服务暴露到不可信网络。

## 输出和退出码

输出目录包含 server stdout/stderr、各 build 日志、`resources.json`、`k6.log`、`k6-summary.json`、`rawprobe.json` 与 `rawprobe.log`。PowerShell runner 写入 `summary.json`，Unix runner 写入 `run-summary.json`。退出码：2 参数；3 build/server；4 k6；5 rawprobe；6 resources；7 cleanup。

## 已知限制

- Windows 的 system-wide CPU 百分比当前显式标记为 unsupported；资源采样不是进程 CPU profiler。
- rawprobe 依赖操作系统 TCP 行为；部分异常报文可能在服务到达应用层前被内核或 HTTP server 拒绝。
- k6 对 SSE/WS 和底层半关闭/截断语义的观测能力有限，底层边界由 Go fixture 与 rawprobe 补充。
- 默认 runner 一次执行一个场景；完整矩阵需要外层编排逐场景运行。
- 当前阈值面向开发回归，不代表生产容量、安全审计或长期可靠性认证。

## 最终验证记录

使用 k6 v1.8.0 在 Windows 完成全部 11 个 scenario inspect，并通过可复现脚本统计出 162 个唯一 check 名称。smoke 通过；full stability 总时长约 9 分钟，共 58,173 个请求、290,868 个 checks，checks 通过率 100%，HTTP failures 为 0，HTTP duration p95 1.12ms、p99 6.11ms。rawprobe 15/15 通过，runner summary 显示 `cleanup.forced=false`。Windows 资源输出按设计将 system-wide CPU 标记为 unsupported，不能将其 0 值解释为实际 CPU 空闲。
