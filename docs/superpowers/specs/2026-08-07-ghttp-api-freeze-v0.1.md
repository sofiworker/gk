# ghttp v0.1.0 公开 API 冻结与弃用时间表

> 状态：2026-08-07 制定，随 v0.1.0 快照执行。
> 仓库仍处于开发中（见根目录 `DEVELOPMENT.md`），本文件只冻结 **API 形状与语义承诺**，不改变“禁止生产使用”状态。

## 目标

为 ghttp（server + client）建立 v0.1.0 公开 API 快照：明确哪些符号进入稳定承诺、哪些是过渡 shim、何时移除，以及破坏性变更必须经过的门禁。

## 快照范围

- 仅 ghttp 包（server 与 client 的导出面）。
- 其它 `g*` 包不在此快照内，各自独立演进。
- Go 版本：模块最低 go 1.25；`go1.27` build-tag 文件（泛型方法 API）属于 v0.1.0 的一部分，但依赖 Go 1.27 工具链。

## 稳定类别（v0.1.0 起承诺形状）

以下类别在 v0.1.0 内不删除、不改语义；命名/签名变更必须走破坏性变更流程：

- **Server 构建**：`New`、`ServerOption`、`With*` 配置项、`Server.Run/Serve/Close/Addr/Group/Use/Consumes/Produces`、`MatchedParams`。
- **路由**：1.27 根组动词链（`Server/Group.GET/POST/...`、`ANY/CUSTOM`）、`RouteBuilder` 链式方法、终结器（`To`/`ToNoInput`/`ToNoOutput`/`ToHTTP*`/`ToRedirect*`/`ToSSE`/`ToWebSocket`/`ToStatic*`/`ToHTML`）、builder 级 `Group`。
- **HTTP 语义**：405/Allow、HEAD 回退、406/415 默认、envelope 只改 body、500 收敛、`WithExposeErrorDetails`、RFC 9457 `WithProblemDetails`/`ErrorWriter`。
- **类型化响应**：`StatusCoder`/`ResponseHeaderWriter`、`.Status()`/`.ResponseHeader()`。
- **中间件**：`RequestID`/`CORS`/`Timeout`/`Recoverer`/`RequestLogger`/`Chain`/`Wrap` 及各自选项。
- **能力接口**：`Logger`/`Validator`/`Authorizer`/`RBAC`/`Renderer`/`EnvelopeFunc` 与默认实现。
- **WebSocket/SSE**：`WebSocketConn`（JSON/raw/context/子协议/deadline）、`SSEWriter`（事件/ID/comment/retry）、keepalive 与 Origin 策略选项。
- **Client**：`NewClient`/`ClientOption`、链式 `Request`、重试/钩子/错误绑定/输出/认证/超时、`Response` 访问器、包级泛型端点 `Do/GET/POST/PUT/DELETE`、1.27 `Client.Get/Post/Put/Delete`。
- **错误模型**：`HTTPError`、`Err`/`AsError`/`BadRequest`/`NotFound`/`Conflict`/`InternalError`/`WithCause`。
- **OpenAPI**：`WithOpenAPI*`、`Doc*` 选项、推断式 schema（无 go-swagger 注释）。

## 过渡与弃用

| 符号 | 状态 | 移除计划 |
|------|------|----------|
| `Route[Req,Resp](target)`（1.27 构建） | `Deprecated` 兼容 shim，忽略类型参数 | 最早 v1.0（以 Go 1.27 为最低版本时移除） |
| `WithStrictContentNegotiation()` | `Deprecated` no-op 别名 | v0.2 |
| `WithStrictContentType()` | `Deprecated` no-op 别名 | v0.2 |

## 双版本约束（v0.1.0 内固定）

- Go < 1.27：`Route[Req,Resp](target)` 是唯一注册入口（语言限制），**不标记 Deprecated**；
- Go ≥ 1.27：动词直挂为唯一推荐入口，`Route[Req,Resp]` 仅兼容；
- 两套 API 共享 `routeBuilderCore` 与全部语义，行为必须一致；两套工具链测试/vet 必须全绿。

## 破坏性变更流程

破坏性变更（删除/改名/语义翻转）必须同时满足：

1. 更新 `CHANGELOG.md` 对应版本节；
2. 更新 `ghttp/README.md` 与相关 AGENTS 约束；
3. 更新本 spec 的稳定类别/弃用表；
4. v0.x 阶段 bump minor 版本；v1.0 起按 SemVer 规则；
5. 涉及 HTTP 语义的变更附真实 HTTP 请求测试（httptest）。

## v0.1.0 快照执行清单

- [ ] CI（legacy + 1.27 预览 + race + lint + gofmt + coverage）全绿；
- [ ] `CHANGELOG.md` Unreleased 收敛为 `v0.1.0` 节；
- [ ] `git tag v0.1.0` 并发布 GitHub Release（开发版，标注“禁止生产使用”）；
- [ ] README 标注 v0.1.0 快照日期与 Go 版本要求。
