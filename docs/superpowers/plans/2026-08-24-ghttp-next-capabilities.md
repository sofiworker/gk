# ghttp 候选功能评估报告

> 日期：2026-08-24
>
> 状态：**已实施**（原评审草案；下列 P0/P1/P2 候选项已全部落地）
>
> **实施说明（回填）**：本报告评估的候选能力已全部实现，实际取舍与原评估的差异记录在此，正文保留原始评估内容以便追溯决策过程。
>
> | 候选项 | 原评级 | 实际状态 | 实现要点与偏差 |
> |--------|--------|----------|----------------|
> | OpenAPI/文档生成 | P0 推荐 | ✅ 已实现 | 选项名为 `WithOpenAPI(info, opts...)`（非草案里的 `WithOpenAPISpec`）。产出 OpenAPI **3.1**（非 3.0）。与草案的关键差异：spec 在**首次请求时**构建并缓存字节，而非注册期立即构建——注册期只收集元数据，避免 `New` 返回前就付出构建成本。参数文档复用请求期同一份 `BindPlan`，故文档不会与绑定行为漂移；输出为手工序列化的确定性字节（`map` 遍历顺序随机会破坏字节稳定性，使 spec 无法 diff review）。 |
> | Rate limiting | P1 备选 | ✅ 已实现 | `RateLimit(cfg)` / `RateLimitByRoute(rps, burst)`。分 16 片令牌桶 + 空闲桶回收（草案未提及回收，但不回收会被随机 IP 打爆内存）。`RPS<=0` 返回透传中间件。 |
> | CSRF | P1 | ✅ 已实现 | `CSRF(cfg)`，双提交 cookie + Origin/Referer 校验。 |
> | 安全头 | P1 | ✅ 已实现 | `SecureHeaders(cfg)` / `SecureHeadersDefault()`，字段三态（默认/`"-"` 省略/自定义）。HSTS 与 CSP 默认关闭。 |
> | Compression 预压缩 (static) | P2 可选 | ✅ 已实现 | `WithPrecompressed()` / `WithPrecompressedEncodings(...)`。`File` 因此新增变参选项（破坏性变更，已记入 CHANGELOG）。 |
> | 绑定能力扩展（§见正文） | 单列计划 | ✅ 已实现 | 未单列计划，与本批一并完成：列表/映射/指针可选/嵌套与内嵌递归/`TextUnmarshaler`/`[]byte`，且 params 与 form 收敛到同一套引擎以保证能力对等。 |
> | typed 入口矩阵补全 | 未在本报告评估 | ✅ 已实现 | 补齐 `*None`、`HeadParams`/`OptionsParams`、`DeleteBody`/`DeleteParamsBody`。 |
>
> **⚠️ 正文勘误**：§0"当前能力基线"把校验列为"已完备"，但内置校验（`validate` tag 全族）已在提交 `9d8bffa` 中**整体移除**——params 现只做类型绑定，业务规则由 handler 自行判断。阅读正文时请以此为准。
>
> 关联文档：
> - `docs/superpowers/plans/2026-08-06-ghttp-design-goals-and-rework.md`（设计目标与横评结论）
> - `docs/superpowers/specs/2026-08-23-ghttp-production-readiness-design.md`（生产就绪能力）
> - `ghttp/doc.go`（当前对外能力清单）
>
> 评估原则：
> 1. **默认零成本红线**：不挂载的功能不得修改热路径、不得引入每请求分配或开销
> 2. **边际价值递减**：功能面已覆盖生产核心（路由、错误链、校验、可观测、优雅关闭、WebSocket/SSE、Gzip、静态资源、健康检查），继续加功能的 ROI 下降
> 3. **只做 2 项以内**：优先推荐满足"默认零成本"且市场差异化高的能力

---

## 0. 当前能力基线（已完备，不动）

ghttp 当前已具备生产级 HTTP 框架的核心能力：

- **路由与执行模型**：gin v1.12.0 tree 移植 + 池化上下文 + 零中间件快路径（0 alloc）+ typed 泛型入口 + RawHandler 逃生
- **统一错误链**：StatusCoder 小接口 + 哨兵映射 + 默认脱敏 + JSON 错误体 + 415 校验
- **可观测性**：MatchedRoute 低基数模板 + ClientIP/可信代理 + 结构化 AccessLog + Prometheus 文本格式指标
- **参数校验**：validate tag 注册期编译 + Validator 接口 + ErrValidation → 400
- **中间件**：Recovery、CORS、Timeout、RequestID、LimitBody、BasicAuth、Gzip（池化 Writer）
- **静态资源**：Static/StaticFS（os.DirFS + embed.FS）+ File + SPA 回退
- **健康检查**：Health（liveness）+ Ready（readiness）+ ReadinessGate
- **生命周期**：Run/RunTLS/Serve/ServeTLS + Shutdown/Close + RunGraceful（信号编排）
- **协议**：WebSocket 生产化（CHANGELOG 提到 Timeout 支持 Hijack/Flush、keepalive、Origin 覆盖）+ SSE（WriteJSONWithID）

横评结论（13 框架）：功能面已覆盖生产核心，继续加功能的边际价值下降。本轮只评估**默认零成本**的能力。

---

## 1. 候选功能评估表

| 候选功能 | 热路径侵入度 (0-3) | 默认成本 | 市场差异化 | 实现复杂度 | 推荐优先级 |
|---------|------------------|---------|-----------|-----------|-----------|
| **OpenAPI/文档生成** | 0（注册期静态构建） | 零（不挂路由零开销；可选 endpoint 暴露时 0 alloc JSON marshal） | **高**（gin 生态最常被问的缺口；fuego/huma 以 OpenAPI-first 为卖点） | 中高（20-40h） | **P0 推荐** |
| **Rate limiting / Token bucket** | 0（纯中间件） | 零（不挂载零开销） | 中（gin 核心无，但 echo/hertz 有；属生态能力） | 中（12-20h） | P1 备选 |
| **Circuit breaker / Retry (服务端)** | 0（独立包 + 中间件适配器） | 零（不挂载零开销） | 低（属客户端能力，ghttp client 已延后；服务端熔断非框架核心职责） | 高（30-50h） | **不建议** |
| **Compression 预压缩 (static)** | 0（仅 static 路由挂载时） | 零（不挂 static 零开销；挂 static 时多一次 fs.Stat 查 .gz/.br） | 低（nginx/Caddy 默认能力；Go 框架 rarely 内建） | 低（6-10h） | P2 可选 |
| **其他协议支持** | 0-1（WebSocket/SSE 已有） | 零（不挂载零开销） | 低（WebSocket/SSE 已生产化；long-polling/grpc-web 价值有限） | 中-高（15-30h） | **不建议** |

---

## 2. 详细评估

### 2.1 OpenAPI/文档生成（P0 推荐）

**热路径侵入度：0**
- 注册期静态构建 OpenAPI spec（遍历路由树，收集 typed handler 的输入/输出描述器元数据）
- 请求期零开销（spec 已驻留内存）
- 可选 `WithOpenAPIRoute(path)` 暴露 `/openapi.json` endpoint（每请求 0 alloc JSON marshal）

**默认成本：零**
- 不调用 `WithOpenAPISpec()` 时，不构建 spec，零 CPU/Memory 开销
- 构建 spec 是注册期一次性成本（O(路由数)），不在请求路径

**市场差异化：高**
- gin 生态最常被问的缺口（gin-swagger 是第三方插件，非内建）
- fuego/huma 以 OpenAPI-first 为卖点（huma 从 Go 类型自动生成 OpenAPI 3.1）
- echo/hertz 有第三方 OpenAPI 生成器，但非框架内建
- ghttp 已删除基于结构体类型的 OpenAPI 反推死代码（CHANGELOG 提到），现完全由显式输入/输出描述器元数据生成（`codec.go:13` 注释："供 415 校验与 OpenAPI 使用"）

**实现复杂度：中高（20-40h）**
- 需遍历路由树，收集每个端点的 method/path/输入描述器/输出描述器/状态码
- 生成 OpenAPI 3.0/3.1 JSON（paths、components/schemas、responses）
- typed handler 的 `InputSource`（Path/Query/Header/Body）已携带元数据（`bind_plan.go` 的 `BindPlan` 含字段信息）
- 输出 `OutputSpec[O]` 已携带 encoder 元数据（`codec.go` 的 `ResponseEncoder.ContentType()`）
- 需处理 envelope 包装（`WithEnvelope`）、错误响应模型（400/404/405/500）
- 测试风险：需验证 spec 与实际路由行为一致（端到端测试）

**落地方案**：
```go
// Option 注入 spec 构建器
func WithOpenAPISpec(info Info, opts ...OpenAPIOption) Option

// 可选暴露 endpoint
func WithOpenAPIRoute(path string) Option // 默认 "/openapi.json"

// 注册期构建
func (s *Server) buildOpenAPISpec() {
    // 遍历 m.trees，收集每个路由的 fullPath + compiledHandler 元数据
    // 生成 OpenAPI 3.0 JSON
}
```

**结论**：满足"默认零成本"红线，市场差异化高（gin 生态缺口），推荐为 P0。

---

### 2.2 Rate limiting / Token bucket（P1 备选）

**热路径侵入度：0**
- 纯中间件层（`func RateLimit(cfg RateLimitConfig) Middleware`）
- 不挂载时零开销

**默认成本：零**
- 不挂载 `RateLimit()` 中间件时，零 CPU/Memory 开销
- 挂载后，每请求一次令牌桶检查（atomic 操作 + 时间戳比较）

**市场差异化：中**
- gin 核心无内建限流（gin-contrib/ratelimit 是第三方）
- echo/hertz 有内建限流中间件
- 属"生态能力"而非框架核心（设计文档 §9 明确"Gzip/限流/CSRF/安全头中间件（gin 核心亦无，属生态；可后续独立计划）"）

**实现复杂度：中（12-20h）**
- 令牌桶算法（sync.Pool 池化桶状态 + atomic 操作）
- 按路由/IP 限流（需读 `Request.ClientIP()`，已实现）
- 可选：按路由模板聚合（低基数，复用 `MatchedRoute()`）
- 测试风险：并发安全、时钟依赖、边界条件

**落地方案**：
```go
type RateLimitConfig struct {
    RPS     float64       // 每秒令牌数
    Burst   int           // 桶容量
    KeyFunc func(*Request) string // 限流键（默认 ClientIP）
}

func RateLimit(cfg RateLimitConfig) Middleware
```

**结论**：满足"默认零成本"红线，但市场差异化中（属生态能力），推荐为 P1 备选。

---

### 2.3 Circuit breaker / Retry (服务端)（不建议）

**热路径侵入度：0**
- 独立 `ghttp/clienthelper` 包 + 中间件适配器
- 不挂载时零开销

**默认成本：零**
- 不挂载时零开销

**市场差异化：低**
- 属客户端能力（对 downstream http.Client 做熔断/重试）
- ghttp client 已延后（设计文档 WS6："统一 client 实现由用户后续单独安排"）
- 服务端熔断非框架核心职责（属服务网格/SDK 层能力）

**实现复杂度：高（30-50h）**
- 需实现熔断器状态机（closed/open/half-open）
- 需实现重试策略（指数退避 + 抖动，可复用 `gretry`）
- 需与 http.Client 集成（自定义 Transport 或 RoundTripper）
- 测试风险：状态转换、并发安全、时钟依赖

**结论**：不满足"框架核心职责"定位，且 client 已延后，**不建议做**。

---

### 2.4 Compression 预压缩 (static)（P2 可选）

**热路径侵入度：0**
- 仅 `StaticFS(dir, opts)` 挂载时生效
- 不挂 static 时零开销

**默认成本：零**
- 不挂 `Static()`/`StaticFS()` 时零开销
- 挂载后，每请求多一次 `fs.Stat(name + ".gz")` 检查（约 100-200ns）

**市场差异化：低**
- nginx/Caddy 默认能力（`gzip_static on;`）
- Go 框架 rarely 内建（gin/echo/hertz 均无）
- 属运维优化，非框架核心能力

**实现复杂度：低（6-10h）**
- `serveStatic` 内先查 `fs.Stat(name + ".gz")`，存在则设 `Content-Encoding: gzip` 并服务
- 需检查请求 `Accept-Encoding: gzip`
- 可选支持 `.br`（Brotli）
- 测试风险：文件系统兼容性、缓存控制

**落地方案**：
```go
type StaticOption func(*staticConfig)

func WithPrecompressed() StaticOption // 启用预压缩查找

func serveStatic(...) error {
    if cfg.precompressed && acceptsGzip(req.Header) {
        if _, err := fs.Stat(fsys, name + ".gz"); err == nil {
            resp.Header().Set("Content-Encoding", "gzip")
            resp.Header().Set("Vary", "Accept-Encoding")
            http.ServeFileFS(resp, req, fsys, name + ".gz")
            return nil
        }
    }
    // 回退到原文件
}
```

**结论**：满足"默认零成本"红线，但市场差异化低（运维优化），推荐为 P2 可选。

---

### 2.5 其他协议支持（不建议）

**现状**：
- WebSocket 已生产化（CHANGELOG："Timeout 中间件支持 Hijack/Flush、raw 消息、context 感知读写、子协议协商、keepalive、路由级 Origin 覆盖"）
- SSE 已实现（`WriteJSONWithID` 支持 Last-Event-ID 续传）
- Gzip 中间件已支持 Flush 透传（SSE 等流式场景）

**候选扩展**：
- **Long-polling**：HTTP/1.1 已有，无需框架支持（业务层用 `http.Flusher` 即可）
- **gRPC-Web**：需第三方库（improbable-eng/grpc-web），违零依赖原则
- **Server-push (HTTP/2)**：Go 1.19+ 已弃用 `http.Pusher`
- **WebSocket 扩展**（压缩、多路复用）：属协议层优化，非框架核心

**结论**：WebSocket/SSE 已生产化，继续扩展的边际价值低，**不建议做**。

---

## 3. 明确不建议做的功能

### 3.1 Content-Type 校验变种

**理由**：
- 已实现严格 Content-Type 校验（`strictContentType` 默认 true，415 校验）
- 再加变种（如宽松模式、按路由覆盖）属过度设计
- 当前设计已满足生产需求（设计文档 §2.5）

### 3.2 校验改成非泛型 tag

**理由**：
- 当前 `validate` tag 已注册期编译为闭包，请求期零反射
- 改成非泛型 tag（如 `go-playground/validator`）会引入第三方依赖，违约束
- 当前设计已满足生产需求（设计文档 §4）

### 3.3 完整响应渲染器族

**理由**：
- gin `render/` 有 20 个渲染器（IndentedJSON/SecureJSON/JSONP/PureJSON/AsciiJSON/String/Data/HTML/SSE/Stream 等）
- ghttp 已有 `ResponseEncoder` 接口，用户可自实现
- 核心不铺开（设计文档 §9 明确"完整响应渲染器族……核心不铺开"）

### 3.4 数组/map/form-urlencoded/多文件上传

**理由**：
- 属绑定能力扩展，单列计划
- 本轮仅做标量类型扩展与校验（设计文档 §4.1a）
- 不在"候选功能"范围内

---

## 4. 推荐优先级

### P0 推荐：OpenAPI/文档生成

**理由**：
1. 满足"默认零成本"红线（注册期静态构建，请求期零开销）
2. 市场差异化高（gin 生态最常被问的缺口；fuego/huma 以 OpenAPI-first 为卖点）
3. ghttp 已具备基础设施（typed handler 的输入/输出描述器元数据、`codec.go` 注释明确"供 415 校验与 OpenAPI 使用"）
4. 实现复杂度中高（20-40h），但 ROI 高

### P1 备选：Rate limiting / Token bucket

**理由**：
1. 满足"默认零成本"红线（纯中间件，不挂载零开销）
2. 市场差异化中（gin 核心无，但 echo/hertz 有）
3. 实现复杂度中（12-20h）
4. 属"生态能力"而非框架核心，优先级低于 OpenAPI

### P2 可选：Compression 预压缩 (static)

**理由**：
1. 满足"默认零成本"红线（仅 static 路由挂载时）
2. 市场差异化低（nginx/Caddy 默认能力）
3. 实现复杂度低（6-10h）
4. 属运维优化，非框架核心能力

### 不建议做

- **Circuit breaker / Retry (服务端)**：属客户端能力，client 已延后
- **其他协议支持**：WebSocket/SSE 已生产化，继续扩展边际价值低
- **Content-Type 校验变种**：已实现严格校验，再加变种属过度设计
- **校验改成非泛型 tag**：会引入第三方依赖，违约束
- **完整响应渲染器族**：用户可自实现，核心不铺开

---

## 5. 实施建议

### 批次 1（P0）：OpenAPI/文档生成

- 注册期遍历路由树，收集 typed handler 元数据
- 生成 OpenAPI 3.0 JSON（paths、components/schemas、responses）
- 可选 `WithOpenAPIRoute(path)` 暴露 endpoint
- 测试：端到端验证 spec 与实际路由行为一致
- 更新 `doc.go` 与 README

### 批次 2（P1，可选）：Rate limiting

- 令牌桶算法（sync.Pool + atomic）
- 按路由/IP 限流（复用 `ClientIP()` + `MatchedRoute()`）
- 测试：并发安全、时钟依赖、边界条件

### 不做

- Circuit breaker / Retry
- 其他协议扩展
- Content-Type 校验变种
- 非泛型 tag 校验
- 完整响应渲染器族

---

## 6. 结论

ghttp 功能面已覆盖生产核心，继续加功能的边际价值下降。本轮评估的 5 个候选功能中：

- **推荐做 1 项**：OpenAPI/文档生成（P0，市场差异化高，满足零成本红线）
- **备选做 1 项**：Rate limiting（P1，属生态能力，优先级低于 OpenAPI）
- **不建议做 3 项**：Circuit breaker / Retry（client 已延后）、其他协议扩展（WebSocket/SSE 已生产化）、Compression 预压缩（运维优化，市场差异化低）

严格遵循"默认零成本"红线，只做 OpenAPI 一项即可满足本轮目标。Rate limiting 作为备选，视资源情况决定是否实施。
