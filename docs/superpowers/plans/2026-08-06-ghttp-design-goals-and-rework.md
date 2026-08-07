# ghttp 设计目标落实与重构计划

> 日期：2026-08-06
>
> 状态：草案，等待评审
>
> 关联文档：`ghttp/AGENTS.md`（七条目标与约束）、`docs/superpowers/specs/2026-07-10-ghttp-server-routing-design.md`（路由设计基线）、`docs/superpowers/specs/2026-06-29-ghttp-route-builder-api.md`（API 设计基线）、`docs/superpowers/plans/2026-07-03-ghttp-design-improvements.md`（已实施/挂账问题清单）
>
> 实施任务计划：`docs/superpowers/plans/2026-08-06-ghttp-server-issues-implementation.md`（本计划的任务级拆解，待 review）

## 0. 背景与依据

本轮依据真实使用与 13 个框架横评（gin、echo、chi、huma、go-restful、stdlib ServeMux、httprouter、gorilla/mux、fiber v3、hertz、fasthttp、bunrouter、ghttp）确认差距，不依赖 README 或记忆：

- HTTP 语义：ghttp 的 HEAD 回退、405+Allow、500 错误收敛是横评中唯一做全的；缺口是 406、q=0 两条协商路径不一致、envelope 绕过路由 Produces、默认宽松 Content-Type 导致 400 而非 415。
- 协议/安全：SSE 多行 data 不拆行（实测非法事件流）；WebSocket 默认接受任意 Origin（实测跨源握手成功）。
- 类型化能力：`To` 固定 200、不能设 header，201/204/Location/ETag 必须退回 `ToHTTPFunc`；middleware 拿不到路径参数。
- 客户端：`Client.SetAuthToken` 无效、泛型 `Do` 忽略 client 默认值、`SetResult` 吞解析错误（均实测复现），07-03 文档已记录但未修。
- 性能：matcher 接近零额外分配；但全链路实测静态 26 allocs/2.2µs、参数 10 allocs/1.5µs，明显落后 gin/echo/fiber。
- 能力层：日志仅有最小 Logger 接口，无 zap/slog 适配；无 RBAC/限流/审计能力；OpenAPI 仅生成 200。

## 1. 总体约束（继承 ghttp/AGENTS.md）

1. 显式优于隐式：外部可观察行为默认保守，必须显式开启。
2. 非 200 必须返回真实 HTTP 状态码与响应内容；envelope 不得改写状态码。
3. 当前泛型 API 是 Go 1.27 泛型方法落地前的过渡形态，内部模型不得阻塞方法链迁移。
4. 所有破坏性变更需在迁移文档与 README 中标注。
5. 当前只实施 server 侧；client 侧仅允许 demo 或延后占位，统一 client 实现由用户后续单独安排。
6. server 侧内部优先级：标准 HTTP 方法（路由、协商、错误语义等）高于 WS/SSE；WS/SSE 高于 client。

## 2. 工作流与改动清单

### WS1 内容协商闭环（P0）

- 将 `selectResponseCodec`、`CodecManager.Negotiate`、`DefaultEnvelope` 收敛为单一协商实现：
  - q=0 排除、通配符匹配、q 值排序、charset 处理；
  - 无匹配时按配置返回 406（默认行为待决策，见 §6）或回退首选类型。
- Envelope 尊重路由/Group/Server 的 Produces：envelope 只包装 body，不接管协商。
- 请求侧：`parseBody` 改走 `CodecManager`（补 form-urlencoded），`FormCodec.Unmarshal` 落地；默认 Content-Type 策略待决策（§6）。

### WS2 显式行为开关（P0）

- 新增 `WithExposeErrorDetails(bool)` 或等价选项，默认关闭；开启后才允许 4xx/5xx 泄露内部错误详情。
- 服务器级 validator 改为显式启用（默认 `nil`，`WithValidator` 开启），路由级 `.Validate()` 保持显式；标注破坏性。
- envelope 保持显式 `WithEnvelope`；客户端 `Do` 的 envelope 显式化归入 WS6，不在本批实施。
- 梳理其余隐式行为清单（`Body` 字段、HEAD 回退、默认 JSON），逐项确认“明示的默认值”或“显式开关”。

### WS3 类型化状态码与 header（P1）

- 参考 huma：允许在输出结构体或终结器上显式声明状态码与 header（如 `Status`/`header` tag，或 `.Status(code)`）。
- `To` 支持 201/202/204 及动态状态；204/304/1xx 自动禁止 body。
- 保留 `ToHTTPFunc` 逃生口；OpenAPI 联动生成对应状态与 header。
- 不得引入隐式状态推断（不通过响应结构体字段名猜状态）。

### WS4 middleware 路径参数可见性（P1）

- 提供显式访问器（如 `Server.MatchedParams(r)`），默认不注入 context、不产生分配；
- 评估可选 `r.PathValue` 注入开关（默认关闭，避免热路径分配）；
- 文档明确：middleware 需要参数时使用访问器，而不是重写路由匹配语义。

### WS5 SSE / WebSocket 协议与安全（P2，server 侧低优先级）

- SSE：`data` 按规范拆分多行、`event` 字段校验、补充 `id`/`retry` 服务端 API 与 ping 辅助；
- WebSocket：`CheckOrigin` 可配置（`WithWebSocketOrigin` 等），默认同源；握手失败走统一错误管线。
- 优先级：低于标准 HTTP 方法相关改动（WS1-WS4、WS7-WS9 中标准 HTTP 部分），高于 client（WS6）。

### WS6 客户端问题（延后，仅 demo）

- 已实测的问题（`Client.SetAuthToken` 无效、泛型 `Do` 忽略默认值、`SetResult` 吞错误等）**记录不修**；
- 本计划范围内仅允许提供 demo 或最小修复演示，不得开展完整客户端改造；
- 统一 client 实现由用户后续单独安排，不在本计划内。

### WS7 能力层（P2）

- `Logger` 接口扩展为可接 zap/slog 的完整接口，提供 `slog` 适配器；zap 适配放入独立子包。
- `Authorizer` 能力接口 + 默认 RBAC 实现（策略存储抽象、角色解析接口），不依赖 casbin 库。
- 限流、审计、指标钩子作为后续增量（独立计划）。

### WS8 OpenAPI 增强（P2）

- 生成 envelope 包装 schema、错误响应模型（可选 RFC 9457 problem+json）、404/405、typed 状态码与 header；
- 支持 security/servers/examples 的显式声明；
- 保持“推断优先、宁缺毋滥”，不反向限制运行时。

### WS9 性能预算（贯穿）

- matcher/参数提取：维持零额外分配，加入回归门槛；
- 全链路目标：静态 ≤ 8 allocs/op、参数 ≤ 10 allocs/op（当前 26/10），通过消除 ResponseWriter 包装与输入构造分配达成；
- 404/405 消除临时 map/slice 分配（设计文档已记录约 64B 分配）；
- 更新 benchmarks harness 与回归门槛。

## 3. Go 1.27 泛型方法迁移路径

- 现状（过渡形态）：

```go
ghttp.Route[Req, Resp](s).GET("/users/{id}").To(handler)
```

- **已落地双版本共存（2026-08-07）**：`//go:build go1.27` / `//go:build !go1.27` 自动选择，使用者无需传 build tag。
  - `ghttp/builder_core.go`：共享 `routeBuilderCore` 与全部注册/解析/错误/协商实现；
  - `ghttp/builder_pre127.go`：Go < 1.27 的泛型 `RouteBuilder[Req, Resp]`；
  - `ghttp/builder_go127.go`：Go ≥ 1.27 的非泛型链 + 泛型终结方法 + 快捷注册。

- 目标形态（Go 1.27 泛型方法，已实现）：

```go
s.Route().GET("/users/{id}").To(func(ctx context.Context, req *GetReq) (*GetResp, error) { ... })
s.Route().GET("/health").ToNoInput(func(ctx context.Context) (*HealthResp, error) { ... })
s.Route().DELETE("/users/{id}").ToNoOutput(func(ctx context.Context, req *DeleteReq) error { ... })
s.Get("/users/{id}", handler) // 快捷注册，类型由 handler 推断
```

- 无输入/无输出使用显式终结器，不使用 `struct{}` 魔法；`Route[Req, struct{}]` 中的 `struct{}` 仅是过渡期占位。
- 包级 `Route[Req,Resp]` 在 1.27 构建中保留为源兼容 shim（忽略类型参数）；Server/Group 不泛型化。
- 两套 API 必须共享同一内部实现，且两套工具链（go1.26 与 go1.27 预览版）下测试/vet 全绿。

## 4. 验证策略

- 每个 WS 附带：单测 + 真实 HTTP 请求测试（httptest）+ 相关 fuzz/race；
- 保留横评回归场景（13 框架行为表）作为行为基线，防止 405/HEAD/Allow 等语义回退；
- 性能改动用 `-benchmem` 与 benchstat 对比；
- 最终执行 `make check`、`go test ./...`、`go test -race ./ghttp/...`。

## 5. 执行顺序与门禁

- 批次 1（P0）：WS1、WS2 —— 先修标准 HTTP 的规范/安全/显式化；
- 批次 2（P1）：WS3、WS4 —— 补 server 侧类型化能力与 middleware 参数可见性；WS6 仅 demo/延后，不进入正式实施；
- 批次 3（P2）：WS5、WS7、WS8、WS9 —— WS/SSE 协议安全、能力层、OpenAPI、性能预算（WS5 不得提前于标准 HTTP 方法相关改动）。
- 每批完成后独立评审；破坏性变更必须附迁移说明。

## 6. 待决策点

1. 无 Accept 匹配时默认 406 还是回退首选类型（横评仅 go-restful 做 406）；
2. 默认 Content-Type 策略：宽松（当前，未知类型按 JSON）还是严格（未知类型 415，huma/go-restful 风格）；
3. 服务器级 validator 默认关闭的破坏性接受度；
4. 错误响应是否引入 RFC 9457 problem+json（或保持 `{code,message}` 并显式扩展）；
5. ~~Go 1.27 方法链的具体命名形态~~ **已定（2026-08-07）**：`s.Route().GET(...).To(handler)`（类型推断）+ `ToNoInput`/`ToNoOutput`/`ToHTTPFunc`/`ToRedirectFunc` 泛型终结器 + `Server/Group.Get/Post/...` 快捷注册。
