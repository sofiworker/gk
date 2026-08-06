# ghttp 协商默认值、表单绑定与 RFC 9457 设计

> 日期：2026-08-06
> 状态：已确认（用户指示“1 补全、2 重新设计、3 按设计补全、4/5 暂不做”）

## 背景

真实横评（15 框架）显示：

- go-restful/huma 对无 Accept 匹配返回 406、对不支持 Content-Type 返回 415；
- gin/echo/fiber 忽略 Accept、未知 Content-Type 按 JSON 硬解，错误详情还会泄露；
- ghttp 当前默认是“宽松回退 + 显式严格开关”，与“满足 RFC”的第一目标不一致。

同时 ghttp 的 FormCodec 已存在但无法把 `application/x-www-form-urlencoded` 解码到 typed handler 的 struct Body（实测 400），是使用者流程上的断点。

## 1. 协商默认值翻转

### 响应协商

- 默认（RFC 正确，go-restful/huma 风格）：
  - `Accept` 为空或 `*/*` → 路由第一个 Produces；
  - `Accept` 明确列出媒体类型但无匹配 → **406 Not Acceptable**；
  - `q=0` 始终排除对应媒体类型。
- 显式宽松（gin 风格）：`WithLenientContentNegotiation()` 开启后，无匹配时回退第一个 Produces。
- 兼容：`WithStrictContentNegotiation()` 保留，语义变为“当前默认”的无操作别名，文档标注。

### 请求 Content-Type

- 默认（RFC 正确，huma/go-restful 风格）：
  - 显式但未注册的 Content-Type（且无 Consumes 匹配）→ **415 Unsupported Media Type**；
  - 缺失 Content-Type（协议级便利，gin 风格）→ 按 JSON 解析；
  - 已注册 codec 无法解码目标类型 → 400（由 codec 报错，不静默吞掉）。
- 显式宽松（gin 风格）：`WithLenientContentType()` 开启后，未知 Content-Type 按 JSON 解析。
- 兼容：`WithStrictContentType()` 保留，语义变为“当前默认”的无操作别名。

## 2. 表单绑定补全

`FormCodec.Unmarshal` 增加 struct 目标支持：

- 字段通过 `form:"name"` tag 声明；无 tag 或 `form:"-"` 跳过；
- 支持标量类型：string、int 系、uint 系、float 系、bool、指针（复用 `setValueFromString`）；
- 重复 key 取第一个值；
- 保持现有 `url.Values` / `map[string]string` / `string` / `[]byte` 目标不变；
- 不引入自动推断（字段名映射是隐式魔法），必须显式 `form` tag。

## 3. RFC 9457 problem+json（显式能力）

新增 `WithProblemDetails()` server 选项，默认关闭：

- 开启后，所有错误响应（4xx/5xx）使用 `application/problem+json`：
  - `type`：默认 `about:blank`；
  - `title`：`http.StatusText(code)`；
  - `status`：真实 HTTP 状态码；
  - `detail`：显式 `HTTPError.Message` 恒保留；普通错误按 `WithExposeErrorDetails()` 决定是否暴露；
  - `instance`：请求路径。
- 与 envelope 的关系：错误场景 problem+json 优先于 envelope（envelope 只用于成功 body 包装），避免两套错误表示并存。
- 与 `WithErrorHandler` 的关系：显式 error handler 优先级最高，problem+json 只作用于框架默认错误写入路径。

## 4. 非目标

- 性能预算（WS9 继续项）暂不做；
- RBAC 默认实现/限流/审计/指标（能力层）暂不做；
- client 侧不做。

## 5. 测试与文档

- 每个行为变化配真实 HTTP 请求测试（httptest）；
- README 增加“默认值速查表”；
- 破坏性变更（默认 406/415）在 README 与迁移说明中标注；
- 旧 `WithStrict*` 选项保留但标记为默认别名。
