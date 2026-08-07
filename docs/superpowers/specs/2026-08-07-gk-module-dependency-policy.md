# gk 模块依赖分层原则

> 状态：2026-08-07 制定，作为整个仓库（所有 `g*` 包）的架构约束。
> 优先级高于各包内部约定；与 ghttp API 冻结 spec（`2026-08-07-ghttp-api-freeze-v0.1.md`）并行生效。

## 1. 背景与目标

仓库设计意图是“积木式拼接”：各个 `g*` 包相互隔离，类似积木，用户按需选取并在自己的代码里注入组合（例如 ghttp 只定义 `Logger` 接口，用户希望用 glog 时才注入）。

“隔离”的正确含义不是“零引用”，而是：

1. 能力层之间互不依赖，避免把两个模块绑死；
2. 公共基础概念只实现一次，通过单向依赖复用，避免三套错误/重试模型漂移；
3. 拼接发生在接口与适配器层，而不是核心包内部。

## 2. 三层模型

| 层 | 职责 | 包含（现状） | 依赖规则 |
|----|------|--------------|----------|
| **基础契约层** | 跨包复用的最小公共概念：错误、重试、反射工具、编解码原语 | `gerr`、`gretry`、`grx`、`gcompress`、`gcrypt` | 不依赖任何 `g*` 包（只依赖标准库/第三方）；**允许被上层包引用** |
| **能力层** | 面向使用者的独立能力：HTTP、日志、缓存、配置、服务发现、DNS、SQL | `ghttp`、`glog`、`gcache`、`gconfig`、`gsd`、`gresolver`、`gsql`、`gnet/*`、`gotel` | 只依赖基础契约层 + 标准库/第三方；**能力层之间禁止互相 import** |
| **适配/拼接层** | 显式胶水：把多个能力层组合起来，或适配第三方实现 | 子包形式（如未来的 `ghttp/adapters/*`）、用户业务代码 | 可以依赖多个能力层；不得反向污染能力层核心 |

## 3. 强制规则

### 3.1 依赖方向

- 允许：能力层 → 基础契约层；适配层 → 任意层；基础契约层 → 标准库/第三方。
- 禁止：能力层 → 能力层；基础契约层 → 能力层；任何反向/循环依赖。
- 禁止在能力层核心 import 另一个能力层的实现（适配器例外，见 3.3）。

### 3.2 接口注入

- 能力层需要另一模块的能力时（日志、鉴权、限流、审计、追踪），**只定义接口或最小默认实现**，不 import 对方实现包。
- 注入发生在：用户代码（推荐）或适配层子包；能力层核心不感知具体实现。
- 标准库能力（如 `log/slog`）可以直接内置适配（示例：`ghttp.NewSlogLogger`），因为不引入第三方耦合。

### 3.3 适配器

- 适配层子包命名约定：`<能力包>/adapters/<实现名>`（如 `ghttp/adapters/glog`）。
- 适配器可以同时依赖多个能力层，但只做“转接”，不得包含业务逻辑；
- 用户不 import 适配器时，能力层核心不产生该依赖。

### 3.4 公共概念不重复

- 错误模型、重试/退避、反射工具等公共概念**只允许在基础契约层实现一次**；
- 能力层若需要，必须通过依赖基础契约层复用，不得各自重新发明（现状违规清单见第 4 节）。

## 4. 现状违规清单（2026-08-07 审计）

| 违规 | 位置 | 目标动作 |
|------|------|----------|
| 三套错误模型 | `gerr.Error` vs `ghttp.HTTPError`（互不引用） | ghttp 单向依赖 gerr：`HTTPError` 作为 HTTP 转换视图，或至少提供 `errors.As` 互操作；短期可并存但必须文档声明边界 |
| 三套重试实现 | `gretry`；ghttp client 内联 `SetRetryCount/Conditions`（[client.go](/root/workspace/golang/gk/ghttp/client.go)）；gsd 自研 `ErrorHandlingOptions/RetryStrategy`（[gsd/retry.go](/root/workspace/golang/gk/gsd/retry.go)） | backoff/jitter 下沉到 gretry；ghttp 只保留“HTTP 状态/传输错误判定”；gsd 删除自研 retry.go 并依赖 gretry |
| 日志核心耦合可观测性 | `glog/zap.go` import `go.opentelemetry.io/otel/trace` | trace 上下文改为可选注入（接口/函数选项），glog 只依赖 zap/lumberjack |
| 占位 API 当正式导出 | `gotel.OTELProvider` 空结构体 + 注释实现 | 实现或删除/降级；未实现前不得承诺为正式 API |

## 5. 实施顺序（迁移行动项）

1. **gsd → gretry**（最明确）：删除 `gsd/retry.go`，`ErrorHandlingOptions` 收敛为 gretry 的类型或别名，语义对齐；
2. **ghttp client → gretry**：复用 gretry 的 backoff/jitter/等待逻辑，删除内联实现；保留 HTTP 状态条件判定；
3. **ghttp → gerr**：建立错误互操作（`errors.As`/转换），短期并存并文档化，长期收敛；
4. **glog 去 otel**：trace 字段改为可选注入；
5. **CI 依赖方向检查**：用 depguard 或脚本禁止“能力层 → 能力层”import，把规则机器化。

每个迁移项按仓库惯例：先计划文档 → review → 实现 → 双工具链测试/lint → 本地提交（未经授权不推送）。

## 5.1 定义归属与收敛进度（2026-08-07 更新）

公共概念的**权威定义归属**：

| 概念 | 权威定义 | 现状 |
|------|----------|------|
| 领域错误 | `gerr` | ghttp 尚未接入（下一步：`HTTPError` 与 gerr 互操作/转换） |
| 重试/退避/抖动 | `gretry`（`Do`/`NextDelay`/`Wait`） | **已收敛**：ghttp client 与 gsd 均复用 `gretry.NextDelay/Wait`，不再各自实现 |
| 日志接口 | 能力层各自定义小接口（如 `ghttp.Logger`） | ghttp 定义接口、用户注入；glog 核心已去除 otel 硬依赖，改为可选 `WithTraceExtractor` |
| 反射工具 | `grx` | 保持独立基础层 |

### 已完成的收敛（2026-08-07）

- `gretry` 导出 `NextDelay(attempt, options)` 与 `Wait(ctx, delay)`，`Do` 内部统一使用；移除了每尝试一个 goroutine 的实现。
- `ghttp` client 重试改用 `gretry.Wait`/`gretry.NextDelay`（指数退避、上限、ctx 感知），删除内联 `waitWithContext`/`nextRetryWait`。
- `gsd` 删除自研 `calculateDelay`/`mathPow`，`retryWithBackoff` 委托 `gretry.Do`，`RetryStrategy` 收敛为 gretry 的类型别名。
- `glog` 核心不再 import `go.opentelemetry.io/otel/trace`，trace 字段通过 `Config.TraceExtractor` / `WithTraceExtractor` 由使用方注入。

### 待收敛

- `ghttp.HTTPError` ↔ `gerr` 互操作（`errors.As`/转换），短期并存但文档化边界；
- `gotel.OTELProvider` 空壳：实现或删除/降级；
- CI 依赖方向检查（depguard/脚本）落地。

## 6. 例外与豁免

- 只有明确标注的“适配/拼接”文件可以跨能力层依赖；
- 例外必须写入计划文档并等待 review，不得静默偏离；
- 基础契约层的第三方依赖必须轻量（优先标准库）；引入重依赖需说明理由。

## 7. 验收标准

- `golangci-lint run ./...` 通过，且 depguard（或等价检查）禁止能力层互引；
- `go list -deps` 抽查：能力层不出现其他能力层 import；
- 公共概念（错误/重试）在仓库内只有一份权威实现；
- 双工具链（go1.26 / go1.27 预览）测试与 lint 全绿。
