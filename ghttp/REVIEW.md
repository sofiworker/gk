# ghttp 包深度代码评审报告

## 评审方法

- **不参考 README/doc.go/git 历史**，只通过代码阅读与实际测试评审（实现可能与文档有差异）
- 主审 + 三个子评审（安全中间件 / 传输功能 / 编解码与文档）交叉验证，所有关键结论都有探针测试实证
- `go test -race -count=1 ./ghttp/` 通过；`go vet` 干净；临时探针文件已全部删除
- 标注【实测】的条目均由临时测试程序验证过实际行为

---

## 严重 🔴

### S1. TSR 重定向构成开放重定向（Open Redirect）

**位置**: `mux.go:486-501 redirectTrailingSlash`

用解码后的 `r.URL.Path` 直接拼 `Location`。裸 TCP 请求 `GET //evil.com/`（存在 `/{a}/{b}` 或 `/{name}` 类路由时）触发 TSR 去尾斜杠 → `Location: //evil.com`，浏览器视为协议相对 URL 跳到 `https://evil.com/`。【实测】

连带问题（同根源，用 decoded path 重建 URL）：
- `/\evil.com/` → `Location: /\evil.com`（浏览器同样按外站处理）
- `/a%3Fx=1/b/` → `Location: /a?x=1/b`（解码 `%3F` 引入查询分割歧义）
- `/a%23frag/b/` → `/a#frag/b`（解码 `%23` 引入 fragment）

**修复**: target 以 `//` 或 `/\` 开头时拒绝重定向（直接 404）；构造 Location 用 `(&url.URL{Path: target, RawQuery: ...}).String()` 重新编码，或改用 `EscapedPath()` 为基础。

### S2. SSE ID/Event 字段换行注入

**位置**: `sse.go appendSSEField / SendMessage`

`SSEMessage.ID`/`Event` 未过滤 `\n`、`\r`，值含换行时可注入任意 SSE 字段/伪造事件（实测注入出 `data:INJECTED` 与第二个 `event:admin`）；`Data` 按 `\n` 拆分但裸 `\r` 也会透传（`\r` 在 SSE 协议中同为行终止符）。【实测】

**修复**: 对 ID/Event 拒绝或剥离 `[\r\n]`；Data 拆分时同时处理 `\r`。

### S3. CORS `AllowOrigins:["*"]` + `AllowCredentials:true` = 带凭证反射任意 Origin

**位置**: `middleware_cors.go:130-138`

该组合下回显任意请求 Origin 并附 `Access-Control-Allow-Credentials: true`——任意恶意站点可带 cookie 跨域读取响应，完全解除浏览器同源保护。注释（L23-25）声明"不能同用"但代码不阻止，反而实现成了比规范禁止写法更危险的"等效全放行"。且 `TestCORS_CredentialsEchoOrigin` 把该行为锁定为预期断言（测错了东西）。【实测】

**修复**: 构造期对该组合 panic 或忽略 Credentials；同步删改测试。

### S4. StatusCoder 返回非法状态码 → panic 逃出 ServeHTTP

**位置**: `error_chain.go classifyError`（不校验 `HTTPStatus()` 返回值）+ `mux.go`（`writeError` 在 `safeChain` 的 recover **之外**执行）

业务错误实现 `HTTPStatus() int` 返回 0（或 <100/>599）时，渲染器 `WriteHeader(0)` panic，且该 panic 发生在所有 recover 之后——直接击穿 ServeHTTP 由 net/http 兜底断连，一个实现不当的业务错误类型即可打挂连接。实测输出 `PANIC 逃出 ServeHTTP: invalid WriteHeader code 0`。【实测】

**修复**: classifyError 对 StatusCoder 结果做 `100<=s<=599` 校验，非法回退 500。

### S5. handler panic 时 Metrics inflight 永久泄漏

**位置**: `metrics.go:112-115`

`inflight.Add(1)` 后调用 `next`，`Add(-1)` 与计数不在 defer 中；panic 穿过中间件时 inflight 永久 +1 且 total 不记。实测 panic 一次后 `http_requests_in_flight{route="/p"} 1` 永不归零——监控假告警源。【实测】

**修复**: `defer func(){ inflight.Add(-1); ... }()`。

### S6. AccessLog/Logger 状态码对错误路径系统性失真

**位置**: `middleware_builtin.go:109-119 LoggerWith` + `mux.go:232-239`（错误渲染在中间件链**外**）

handler/中间件返回 error 时状态码由链外 writeError 写入，Logger 读到 `resp.Status()==0` 后按 200 占位。实测：CSRF 403、BasicAuth 401、业务 400 在访问日志中**全部记为 200**；而 404/405 在链内写入所以正确——同一日志流部分对部分错，比全错更具迷惑性。Logger+认证/限流是最常见组合，等于内置访问日志对所有被拒请求撒谎。【实测】

**修复**: 把错误渲染移进链内（如包在 dispatchTerminal 一层），或 Logger 对 `err!=nil && status==0` 用 `HTTPStatus(err)` 推断（metrics.go:117-129 已这么做）。

### S7. `http.ErrAbortHandler` 被吞成 500

**位置**: `mux.go:292-305 safeChain`、`mux.go:523-533 serve`、`middleware_recovery.go`

标准库约定 `panic(http.ErrAbortHandler)` 应向上冒泡让 net/http 静默断连（ReverseProxy 等依赖此约定）；三处 recover 均未特判，统一收敛为 500 响应 + panic 日志刷屏。【实测】

**修复**: recover 中 `if rec == http.ErrAbortHandler { panic(rec) }`（三处）。

### S8. XFF 全可信时回退 X-Real-IP，ClientIP 可被伪造

**位置**: `client_ip.go`（按 headers 顺序逐个回退 + `firstNonTrustedIP` 全可信返回 ""）

XFF 链全部落在可信网段时返回空串，循环**继续尝试下一个头**（X-Real-IP）。代理常见配置只追加 XFF 而透传客户端自带的 X-Real-IP，此时内部/直连客户端可预置 `X-Real-IP: 6.6.6.6` 完全伪造 ClientIP，绕过按 IP 限流/审计。实测 `trusted=10.0.0.0/8`、XFF 全可信、X-Real-IP=6.6.6.6 → `ClientIP()=="6.6.6.6"`。【实测】

**修复**: 某个转发头**存在且非空**但推不出不可信 IP 时，直接回退 `RemoteIP()`，不再尝试后续头。

---

## 中等 🟡

### M1. LimitBody Content-Length 分支：客户端 413 空体、hook 看到 400

**位置**: `middleware.go:81-85`

先 `resp.WriteHeader(413)`（空体、绕过统一 JSON 错误渲染）再返回 `ErrInvalidInput` 包装错误（classifyError → 400）。实测客户端收 `413 body=""`、onError hook 收 `status=400`——客户端、监控、响应体三方不一致。`errors.go` 明明导出了 `ErrRequestEntityTooLarge`（注释写着"对应 413"）却未用。【实测】

**修复**: 删掉手动 WriteHeader，`return fmt.Errorf("%w: body exceeds limit", ErrRequestEntityTooLarge)` 交错误链。
**测试掩盖**: `middleware_test.go` 只断言 `resp.Status()==413`；`TestLimitBody_NoCLWithMaxBytesReader` 全是 t.Log 无有效断言。

### M2. MaxBytesError 被 codec 打平，chunked 超限退化为 400

**位置**: `codec.go:56-57/83-84`（`fmt.Errorf("%w: %v", ErrInvalidInput, err)`）

`%v` 打平了 `*http.MaxBytesError`，`error_chain.go` 里专门的 `errors.As(MaxBytesError)` → 413 分支成为**不可达死代码**。实测 LimitBody + 无 Content-Length（chunked）超限体 → 客户端 400 而非 413。【实测】

**修复**: 解码错误先 `errors.As` 提取 MaxBytesError 单独映射，或用支持双链的包装。

### M3. CORS 预检判定：任意带 Origin 的 OPTIONS 都被 204 短路

**位置**: `middleware_cors.go:156-169`

未检查 `Access-Control-Request-Method`。注册的业务 OPTIONS 路由 + 请求带 Origin（普通跨域 OPTIONS，非预检）→ 中间件直接 204，用户 handler 永远不可达。Fetch 规范定义预检必带 ACRM；rs/cors 等主流实现均检查。实测 `status=204 handler被调=false`。【实测】

**修复**: 预检判定加 `src.Get("Access-Control-Request-Method") != ""`。

### M4. CORS 拒绝分支与无 Origin 分支缺 `Vary: Origin`

**位置**: `middleware_cors.go:126-127, 142-147`

只有 allow 分支写 Vary。共享缓存先缓存了无 ACAO 的响应后，合法跨域请求命中缓存会失败（或反向混淆）。实测非白名单请求 `Vary=[]`。【实测】

**修复**: 只要 ACAO 随 Origin 变化，所有分支统一 `Vary: Origin`。

### M5. CSRF：`Origin: null` 放行 + 同源判定忽略 scheme

**位置**: `middleware_csrf.go:277-279, 287`

- `origin == "null"` 被当作"缺失"放行：沙箱 iframe / data: URL 会发 `Origin: null`，同源校验这道"第二道防线"（注释自述用于补双提交在子域被攻破时的弱点）对 null 完全失效，与双提交 token 无签名（未绑定会话）组合成完整绕过链。
- `EqualFold(u.Host, req.Host)` 忽略 scheme：`http://example.com` 的中间人明文页面对 https 站点算"同源"。
- 两者均被 `TestCheckCSRFOrigin` 固化为预期断言。

**修复**: 拒绝 `"null"`；同源比较带 scheme；提供 `__Host-` 前缀 cookie 选项；考虑 HMAC 绑定会话的 token 方案。

### M6. Metrics Prometheus 文本输出格式非法 + 标签基数 DoS

**位置**: `metrics.go:154-202`

- HELP/TYPE 在每路由循环内重复输出、`http_requests_total` 样本被其它行打断且同名 family 混用有/无 `code` 两种标签集——标准 Prometheus 解析器报错。【实测】
- miss 时 route 归一为 `no_route` 但 **method 原样进标签**：伪造任意方法名（`FOO`/`BAR`...）可无限新建 series 撑爆 MetricsRegistry（内存 + 基数 DoS）。【实测】
- method/route 标签值未做转义；只有 `duration_seconds_sum` 没有 `_count` 与 bucket，无法算均值/分位数。

**修复**: HELP/TYPE 提到循环外；非标准方法归一为 `OTHER`；补 `_count`；标签值转义。

### M7. Group 前缀裸字符串拼接

**位置**: `group.go:47,66`（`g.prefix + path`）

无斜杠规范化：`Group("/api/")`+`"/v1/x"` 注册成 `/api//v1/x`（实测 `/api/v1/x` 404、`/api//v1/x` 200）；`Group("/api")`+`"users"` 注册成 `/apiusers` 且注册成功。【实测】

**修复**: 拼接时规范化（确保单个 `/` 连接），或注册期校验拼接结果。

### M8. 自定义 404/405 handler 的错误被吞 + miss 路径 Request 无 owner

**位置**: `mux.go:408,415,433,440`

- `_ = m.notFoundHandler(...)` 返回值丢弃：handler 返回 error 且未写响应时客户端收到隐式 200 空体。【实测】
- miss 冷路径构造 `&Request{Request: r}` 未设 `owner`：自定义 404/405 handler 内调 `ClientIP()` 不遵循 WithTrustedProxies（实测同一请求命中路由返回 "1.2.3.4"，miss 返回 "10.1.2.3"）。【实测】

**修复**: 错误交给 writeError；构造时带 `owner: m`。

### M9. FormBody 声明空 Content-Type，严格校验完全绕过

**位置**: `body_decoders.go`（`formDecoder.ContentType()==""`）

- 默认 `strictContentType=true` 对空声明直接放行：发 `application/json` CT 的 body 到 form 端点 → ParseForm 不认识该 CT 不读 body → 静默解出零值结构体 200。【实测】
- urlencoded 分支依赖 `PostForm`，标准库对 DELETE 等方法不解析 body → `DeleteBody`+FormBody 表单静默零值。（子代理实测）

**修复**: form 契约声明两个可接受 CT 并在严格模式校验；或空 CT 时至少校验请求 CT 属于两种表单类型。

### M10. `Body[B](nil)` 绕过 nil 检查，注册期 nil 解引用 panic

**位置**: `input.go` + `typed.go registerBody`

`Body[B](nil)` 返回非 nil 的 `InputSpec` 接口（内含 nil codec），绕过 `in == nil` 检查，注册期 `in.contentType()` 直接 nil pointer panic——`ErrMissingCodec` 防御失效。实测注册期 panic。【实测】

**修复**: `Body[B](nil)` 返回 nil 或注册期检查内部 codec。

### M11. 输出编码失败时客户端拿到 200 空体

**位置**: `output.go:75-97`（先 `WriteHeader(200)` 再流式 `Encode`）

编码失败（如结构体含 chan 字段）时头已提交，错误链无法改写：hook 记 500、客户端收 200 空体。（子代理实测）

**建议**: 已知无法零成本修复（流式取舍），至少在 OutputSpec 文档标注；或提供缓冲编码选项。

### M12. Response.Flush 不标记 written

**位置**: `request.go:199-203`

Flush 隐式提交了 header 但不设 `written=true`：handler Flush 后返回 error → writeError 以为未提交 → 补写触发 "superfluous WriteHeader"，客户端收 200 但 hook 记 4xx/5xx。【实测】

**修复**: Flush 时若未 written 先置 `status=200, written=true`。

### M13. Timeout 超时事件不可观测 + 503 语义

**位置**: `middleware_timeout.go:56-61`

超时写 503 后 `return nil`——onError hook 与外层中间件都看不到超时事件，无法打点告警；503 宜为 504，且 `http.Error` 的 text/plain 与统一 JSON 错误体不一致。

（正面确认：协作式设计本身合理，同步执行 + `Written()` 检查规避了 sync.Pool 场景的 use-after-free 与 double-write，实测无响应竞态。）

### M14. RateLimit 内存回收不完整、无桶数上限

**位置**: `middleware_ratelimit.go:218-228`

sweep 只清理**被访问的分片**：流量停止后其它分片的桶滞留到该分片下次被访问；攻击者短时注入大量 key（伪造 XFF/IPv6 轮换）后停手，内存长期不归还，且每分片 map 无上限。（子代理实测：12 桶注入后仅访问 1 分片 → 0 回收）

**修复**: 每分片桶数上限 + 超限全扫；或文档明确此权衡。

### M15. BasicAuth 用户名枚举时序侧信道

**位置**: `basic_auth.go:48`

`if want, exists := accounts[user]; exists && constantTimeEqual(...)`——未知用户 map miss 立即 401，已知用户才做恒定时间比较，时间差可枚举有效用户名（密码比较本身正确用了 `subtle.ConstantTimeCompare`）。另 API 只接受明文密码 map，无法用 bcrypt。

**修复**: 用户不存在时对固定 dummy 值做一次同样比较。

### M16. OpenAPI 生成的多处正确性问题

**位置**: `openapi.go` / `openapi_schema.go`

- catch-all 路由 `/files/{fp...}` 原样进 paths 键，与参数声明 `fp` 不匹配，路径模板非法。【实测】
- 错误响应固定用组件名 `Error`：用户业务类型恰好叫 `Error` 且先注册时，400/500 的 `$ref` 指向用户 schema，错误契约被替换。（子代理实测）
- 组件名直接取 `t.Name()`：泛型实例名含 `[github.com/...]`，违反 OpenAPI 3.1 组件名正则且 $ref 未转义，spec 非法。（子代理实测）
- operationID 冲突：`GET /users/{id}` 与 `GET /users/by-id` 都生成 `getUsersById`，违反全局唯一。（子代理实测）
- 内嵌 struct 字段提升不实现 json 同名遮蔽规则：properties/required 输出重复键。（子代理实测）
- 指针字段的 nullable 打在 $ref 节点上，渲染时 ref 分支先 return，null 信息全丢。（子代理实测）
- `ServeWS`（websocket.go）用 `r.register` 注册，绕过 `noteRoute`——WS 端点不出现在 spec 中，与 RawHandle"路径存在即契约"的口径不一致。【实测】

### M17. WebSocket 与优雅关闭脱节、文档不符

**位置**: `websocket.go`

Hijack 后连接脱离 `http.Server` 管理：Shutdown 不等待也不通知已升级的 WS 连接，handler 收到的 ctx 也不会因 Shutdown 取消，与"ctx cancels with the request"的注释不符；并发写保护未强制或文档化。（子代理确认）

**修复**: 文档修正 + 提供关闭通知/drain 机制（如注册活跃连接集合，Shutdown 时主动 Close）。

---

## 轻微 🟢

1. **JSON/XML 解码宽松**（`codec.go:56/83`）：`err != io.EOF` 应为 `errors.Is`；空 body 静默解出零值 200【实测】；只解首个 JSON 值，trailing garbage 被接受（`{"a":1} garbage{{` → 200）【实测】；无 DisallowUnknownFields 选项。
2. **空 path 参数静默跳过**（`bind_plan.go:256-260`）：`/users//posts` 匹配 `/users/{id}/posts` 且 `id=""` 视为缺失保留零值（int 字段也不报错）【实测】；严格模式（WithStrictPath）才 400。path 参数在 OpenAPI 中声明 required 但绑定层不强制，文档与行为矛盾。
3. **panic 值被丢弃**（`mux.go:529 `_ = rec``）：不挂 Recovery 时 panic 原因/堆栈不进日志不进 hook（hook 只见 "ghttp: handler panicked"），生产排障黑洞。建议 onError 传递含 panic 值的错误。
4. **`Use()` 在首请求后静默 no-op**（chainOnce 已折叠）【实测】；建议 panic 或返回错误提示误用。
5. **死代码/死字段**：`Request.reset`（request.go:112）无生产调用方，dispatch 手动内联重置——新增字段易漏清理；`BindPlan.needQuery/needHeader`（bind_plan.go:139-141）写而不读，`compiledParams.serve` 无条件 `req.Query()`（纯 path 参数端点也解析 query）。
6. **RequestID 未校验字符集**（`middleware_builtin.go:35-38`）：只限长度 ≤128，可注入任意字符进日志。建议限定 `[0-9A-Za-z._-]`。
7. **Referrer-Policy 默认过时**：`no-referrer-when-downgrade` 泄完整 URL 给 HTTPS 第三方；现代默认为 `strict-origin-when-cross-origin`。
8. **CORS 空 AllowMethods 注释与行为不符**（`middleware_cors.go:157-159`）：注释承诺"回退常见安全方法集"，实现为空则不写 ACAM。
9. **CSRF multipart 解析放大**：不带 token 的 multipart 体触发 32MiB 级 ParseMultipartForm；文档应提示 LimitBody 挂在 CSRF 之前。
10. **mediaType 手写解析**（`codec.go:103-108`）：按 `;` 裸切而非 `mime.ParseMediaType`，参数含引号分号的 CT 会误切（低危）。
11. **SpecJSON sync.Once 缓存**：首次调用后再注册路由 spec 不更新且无告警；注册期 docs append 无锁（注册期单线程是隐含约定，未文档化）。
12. **health 检查串行执行**（`health.go`）：多检查项总耗时可能超探针阈值，建议并发。
13. **SSE writer 固定 200** 无法定制状态码；**upload.go** Save/Bytes 无大小校验（依赖用户先挂 LimitBody）。
14. **metrics 快照非原子**：total（atomic）与 per-code（map+mutex）两套机制，抓取瞬间可不一致（可接受，建议注释）。
15. **HEAD 不回退 GET**：HEAD 请求只注册 GET 的路径返回 405（Allow: GET）【实测】；与 net/http 默认行为不同，属设计取舍，建议文档标注。
16. **`newRequestID` 熵降级可预测**（时间戳回退）与 `newCSRFToken` 的 panic 策略不一致；RequestID 非安全敏感可接受，建议注释差异。
17. **测试质量**：三处把漏洞行为固化为断言（CORS credentials 回显、CSRF null/scheme）；`TestLimitBody_NoCLWithMaxBytesReader` 无有效断言；`TestAppendJSONStringMatchesStdlib` 声称与标准库一致但避开了不一致的 HTML 字符集（`<>&`，输出仍合法仅低危）；缺失用例：Logger 错误路径状态码、ClientIP 全可信 XFF/多头回退、panic 时 inflight、Prometheus 格式合法性、static 的 Range/条件请求/遍历变体。

---

## 正面评价 ✅

- **架构一致性**：注册期编译（bind plan/中间件折叠/预构建 miss body）+ 请求期零反射零组装的设计贯彻彻底；`sync.Pool` 上下文复用、分片锁限流、gzip writer 池化等性能工程到位。
- **并发与资源**：`-race` 全绿；无 goroutine/timer 泄漏；Timeout 协作式设计对 sync.Pool 约束的认知清醒（注释诚实记录权衡）。
- **错误链单出口**：writeError 检查 `Written()` 防双写；默认脱敏（错误细节不泄漏）且有专门防泄漏测试。
- **路由核心**：gin tree 移植正确，TSR/参数/catch-all 匹配行为与 gin 对齐；路径校验默认拦 dot 段防遍历（static 的 `fs.ValidPath` + 400 实测有效）。
- **静态服务**：预压缩变体（br/gz）实现了 Vary 与原始 Content-Type、`q=0` 拒绝识别、变体直接请求不叠加编码等细节。
- **测试基础**：注入时钟的确定性限流测试、恒定时间比较边界、multipart 陷阱、模糊测试（routing_fuzz）覆盖面广。
- **可信代理模型**：netip + 右往左 XFF 回溯的核心实现正确；secure 头的 HSTS 仅 TLS 才发、X-Forwarded-Proto 仅可信代理采信是亮点。

---

## 修复优先级建议

| 优先级 | 条目 |
|---|---|
| P0（安全，立即） | S1 开放重定向、S3 CORS 凭证反射、S4 StatusCoder panic 击穿、S2 SSE 注入 |
| P1（观测与正确性） | S5 inflight 泄漏、S6 日志状态码失真、S7 ErrAbortHandler、S8 X-Real-IP 伪造、M1/M2 413 双轨、M6 Prometheus 格式+基数 |
| P2（行为一致性） | M3/M4 CORS、M5 CSRF null/scheme、M7 Group 拼接、M8 miss 路径、M9/M10 body 入口、M12 Flush、M16 OpenAPI 批量 |
| P3（打磨） | 轻微项 + 测试补齐（先修被测试固化的漏洞断言） |

当前 pre-v1.0 阶段修复成本低，多数修复只涉及局部分支，不影响架构。

---

# 复审报告（修复后二次评审）

复审方法同首轮：不看文档与 git 历史，逐项重放首轮探针测试 + 阅读修复实现 + 回归探测。`go test -race -count=1 ./ghttp/` 通过，`go vet` 干净，`gofmt` 无差异。

## 修复确认（已实测验证）✅

| 编号 | 结论 | 修复方式（实测行为） |
|---|---|---|
| S1 开放重定向 | ✅ 已修 | `//evil.com/`、`/\evil.com/` → 404（`isSafeRedirectTarget` 拒绝）；`/a%3Fx=1/b/` → `Location: /a%3Fx=1/b`（`url.URL` 重编码，`%3F/%23` 保留转义） |
| S2 SSE 注入 | ✅ 已修 | ID/Event 经 `stripSSELineBreaks` 清洗（`id:1data:INJECTED` 成为单行字面量）；Data 按 `\r`/`\n`/`\r\n` 三种终止符拆行；Comment 同样清洗 |
| S3 CORS 凭证反射 | ✅ 已修 | `*`+credentials 时回落为 `ACAO:*` 且**不发** ACAC（凭证被忽略），不再回显任意 Origin |
| S4 StatusCoder 非法值 | ✅ 已修 | 返回 0 → 500 JSON 错误体，无 panic；classifyError 校验 + writeError 二次钳制（纵深防御） |
| S5 inflight 泄漏 | ✅ 已修 | panic 后 `http_requests_in_flight` 回到 0（计数移入 defer） |
| S6 日志状态码失真 | ✅ 已修 | 403 错误路径 Logger 记录 403（`status==0` 时按 `HTTPStatus(err)` 推断，且记账进 defer——panic 请求也留日志） |
| S7 ErrAbortHandler | ✅ 已修 | `panic(http.ErrAbortHandler)` 原样 re-panic（safeChain/serve/Recovery 三处） |
| S8 X-Real-IP 伪造 | ✅ 已修 | XFF 全可信时返回 RemoteIP（10.0.0.1），不再回退 X-Real-IP；"只认第一个存在且非空的转发头" |
| M1 LimitBody 413 | ✅ 已修 | client=413 hook=413 + 完整 JSON 错误体（`ErrRequestEntityTooLarge` 交错误链） |
| M2 MaxBytesError | ✅ 已修 | chunked 超限 → 413（hook 同 413），死分支复活 |
| M3 CORS 预检判定 | ✅ 已修 | 无 ACRM 的 OPTIONS 到达业务 handler（200）；真预检（带 ACRM）仍 204 |
| M4 CORS Vary | ✅ 已修 | 非白名单 Origin 分支也输出 `Vary: Origin` |
| M5 CSRF | ✅ 已修 | `Origin: null` → 403；同源比较带 scheme（复用 `isTLSRequest` 信任策略推导自身 scheme） |
| M6 Metrics | ✅ 已修 | HELP/TYPE 每 family 只出现一次；伪造方法归一（`method_other`）；补 `_count`；标签值转义（`route="/q\"uote"`）；per-code 拆为独立 family `http_requests_by_code_total` |
| M7 Group 拼接 | ✅ 已修 | `Group("/api/")+"/v1/x"` → `/api/v1/x` 命中；`joinRoutePath` 保证连接处恰好一个 `/`（无前导斜杠的 `"nolead"` 也被规范成 `/g2/nolead`） |
| M8 miss 路径 | ✅ 已修 | 自定义 404 返回 error → 走错误链（client=418 hook=418）；`missRequest` 带 owner → miss 路径 ClientIP 遵循可信代理（返回 1.2.3.4） |
| M9 FormBody CT | ✅ 已修 | JSON CT 发到 form 端点 → 415（form 契约声明两种 CT 并参与严格校验） |
| M10 Body(nil) | ✅ 已修 | 注册返回 `ErrMissingCodec`，不再 panic |
| M11 编码失败观测 | ✅ 已修（观测口径） | hook 上报已提交的真实状态（200），与客户端一致；流式取舍保留（属合理决策） |
| M12 Flush | ✅ 已修 | Flush 标记 written（仅在底层真支持 Flusher 时），无 superfluous 告警，hook 与 client 一致为 200 |
| M13 Timeout | ✅ 已修 | 超时 → 504 + 统一 JSON 错误体 + hook 收到 `ErrRequestTimeout`；超时后 goroutine 迟到写被拒（-race 通过） |
| M14 RateLimit 内存 | ✅ 已修 | 新增 `MaxKeys`（默认 10 万）+ 每分片上限 + 触顶时强制 reap；满额新 key 放行的取舍有明确论证（可用性保护而非访问控制） |
| M15 BasicAuth 枚举 | ✅ 已修 | 未知用户对 dummy 值做同样的恒定时间比较，`exists` 参与最终判定防被优化掉 |
| M16 OpenAPI | ✅ 已修 | catch-all 清洗为 `{fp}`；泛型名转义（`rvPage_github.com_..._rvUser`）；Error 冲突自动后缀（Error2）；operationId 去重；内嵌字段遮蔽正确；`*T` 输出 `oneOf[$ref, null]`；ServeWS 登记进 spec |
| M17 WS 文档 | ✅ 部分修 | ServeWS 进 OpenAPI；但"ctx cancels with the request"注释仍在（见遗留 R3） |
| 轻微项 | ✅ 大多已修 | JSON 空体→400、trailing garbage→400、`errors.Is(EOF)`；panic 值进 hook 错误串（`handler panicked: secret-detail-42`）；late `Use()` 打警告日志（atomic.Bool 防竞态）；`Request.reset` 复活并被测试引用；RequestID 限定 `[0-9A-Za-z._-]`；Referrer-Policy 默认改为 `strict-origin-when-cross-origin`；CORS 空 AllowMethods 注释改为与行为一致（"空则不写"）；metrics 405 归属、快照一致性等有注释说明 |

## 新引入的回归 🔴

### R1. 全局链内的 URL 重写中间件失效（匹配提前到链前导致）

**位置**: `mux.go dispatchChained`（`req.resolved = m.resolve(req)` 先于 `safeChain` 执行）+ `dispatchTerminal`（只消费预解析结果，不再自行匹配）

**描述**: 为了让 metrics/限流读到 MatchedRoute，路由匹配从链终端提前到了链之前。副作用是：中间件修改 `req.URL.Path` 后，终端**仍按旧路径的匹配结果**执行——经典的 strip-prefix 网关/URL 重写中间件全部失效。

**实测**:
```
s.Use(重写 /api/v1/* → /*)；注册 GET /users
GET /api/v1/users → 405 Allow:GET   ← 期望 200
GET 请求收到 "405 Method Not Allowed + Allow: GET" 自相矛盾
```
（405 的来源：writeMiss 用重写**后**的路径 `/users` 算 Allow，得到 "GET"，而当前请求就是 GET——matched-by-Allow 却 miss-by-resolve 的矛盾输出。）

**影响**: 旧行为（匹配在终端做）下这类中间件是可用的，属**行为回归**；且失败形态诡异（GET 收到 405+Allow:GET），难排查。

**修复建议**（按侵入度递增）:
1. 至少：`dispatchTerminal` 检测 `req.URL.Path` 与解析时的路径不一致时**重新 resolve**（重写是冷路径，成本可接受）；
2. 或：文档明确"全局中间件内不得改写 URL.Path，重写请用 RawHandle/外层 http.Handler 包装"，并让 writeMiss 在这种不一致时不输出矛盾的 Allow；
3. 或：提供显式的 rewrite 钩子（在 resolve 之前运行）。

## 遗留未修 🟡

### R2. Gzip 中间件仍缺 `Vary: Accept-Encoding`

**位置**: `gzip.go`（本轮未改动）

实测压缩响应 `CE=gzip Vary=[]` 依旧。static 预压缩路径已正确加 Vary（static.go:249,257），动态 gzip 与其不一致——同一缺陷修了一半。共享缓存仍可能把 gzip 响应喂给不支持的客户端。

**建议**: `gzipResponseWriter.WriteHeader` 决定压缩时 `h.Add("Vary", "Accept-Encoding")`（无论最终压不压，只要挂了中间件且响应随 AE 变化就该声明；至少压缩分支必须有）。

### R3. WebSocket 注释仍与实现不符（首轮 M17 的文档部分）

**位置**: `websocket.go:28`（"ctx 随请求取消而取消,可用于协调关闭"）

Hijack 后连接脱离 `http.Server` 管理：Shutdown 不会取消该 ctx，也不等待 WS 连接排空。注释承诺的"协调关闭"实际不存在。本轮只修了 OpenAPI 登记，文档/机制未动。

**建议**: 修正注释（"ctx 不随 Shutdown 取消，长连接需自行监听关闭信号"），或后续提供活跃连接登记 + Shutdown 主动 Close 的 drain 机制。

### R4. 小项

- `BindPlan.needQuery/needHeader` 仍是写而不读的死字段（注释声称"请求期据此跳过 URL.Query() 解析"，但 `compiledParams.serve` 仍无条件 `req.Query()`——注释与实现不符，二选一：真用起来或删掉）。
- 空 path 参数（`/users//posts` → `id=""` 静默零值）行为未变。属 gin 对齐的既定取舍，但与 OpenAPI 中 path 参数 `required:true` 的声明仍矛盾，建议文档标注。
- CSRF 双提交 token 仍未与会话绑定（无 HMAC/`__Host-` 前缀选项）。null Origin 与 scheme 修复后风险已显著降低，剩余属可接受的无状态设计取舍，建议文档写明威胁模型。
- `health.go` 检查仍串行执行；`upload.go` Save/Bytes 仍无大小校验（依赖 LimitBody）——首轮轻微项，未修，可接受。

## 复审总评

24/25 项首轮问题已高质量修复：修复不是打补丁式的，而是带完整双语注释的机理级修正（如 `isSafeRedirectTarget` 解释路由树空段匹配为何造成 `//` 命中、`missRequest` 解释 owner 缺失的后果、BasicAuth dummy 比较解释防编译器优化），并配套了新测试文件（security_fixes_test.go、observability_fixes_test.go、openapi_correctness_test.go、limitbody_413_test.go 等 15 个），此前"测试固化漏洞行为"的断言也已纠正。

需要处理的余项按优先级：
1. **R1 URL 重写回归**（新引入，行为倒退，须修或明确禁止并消除矛盾输出）
2. **R2 gzip Vary**（首轮中等项，漏修）
3. **R3 WS 注释失实**（一行注释的事）
4. **R4 needQuery 死字段**（注释与实现不符）

---

# 复审余项修复记录（第三轮）

R1–R4 已全部修复，`go test -race -count=1 ./ghttp/` 通过，`go vet`/`gofmt` 干净，typed 热路径基准无回归。

| 编号 | 修复方式 | 测试 |
|---|---|---|
| R1 URL 重写回归 | 采用建议 1：`resolvedRoute` 记录解析时的 path/method，`dispatchTerminal` 检测到中间件改写后清空旧 Params/matchedRoute 并重新 `resolve` 一次（冷路径一次额外树查找）；未改写的请求仍复用链前结果，热路径零变化 | `rewrite_middleware_test.go`（strip-prefix 网关、重写后参数/模板刷新、重写到不存在路径→404、method override、无矛盾 405、metrics 类中间件仍读到链前 MatchedRoute，共 6 个子测试） |
| R2 Gzip Vary | 挂载后无条件 `ensureVary(resp.Header(), "Accept-Encoding")`（不论本次是否接受 gzip，两种形态都真实存在）；新增 `ensureVary` 幂等追加（大小写不敏感、识别逗号列表），static 预压缩两处改用同一实现，同挂时不再重复声明 | `gzip_test.go`（压缩/非 gzip 客户端均有 Vary、大小写去重、多值追加幂等、gzip+static 联动恰好一个声明，共 5 个子测试） |
| R3 WS 注释失实 | `WSHandlerFunc` 注释改为如实说明：升级后连接 hijacked，`Shutdown` 不取消 ctx、不等待排空，优雅关闭需业务自行监听外部信号 | 纯文档修正，无行为变化 |
| R4 needQuery 死字段 | 两个 typed 执行器改为 `needQuery=true` 时才调用 `req.Query()`（纯 path/header 端点跳过 `url.ParseQuery`）；删除从未被消费的 `needHeader`。安全性依据：query 步存在 ⇒ `needQuery=true`，nil query 在 `rawSingle`/`collectSlice`/`collectBracketed` 中只做 map 读取（Go 对 nil map 读取安全） | `bind_plan_needquery_test.go`（标志与计划形状一致、纯 path/query/header 三种端点绑定行为不变，共 4 个子测试） |

R4 小项中其余三条（空 path 参数、CSRF 无状态取舍、health 串行/upload 大小）维持原判：属既定设计取舍或可接受的轻微项，未在本轮处理。

变更文件：`mux.go`、`gzip.go`、`static.go`、`websocket.go`、`bind_plan.go`、`typed.go`；新增测试 `rewrite_middleware_test.go`、`gzip_test.go`、`bind_plan_needquery_test.go`；CHANGELOG 已补五条 Fixed 记录。

---

# 第四轮深度评审（聚焦新增代码与修复完整性）

**方法**：以前三轮结论为去重基线，聚焦最近新增代码（autoHEAD、query_lazy、builder、TSR/错误链修复）与修复完整性复核；关键结论均写临时探针实测验证（探针已删除）。基线：`go vet` 干净，`go test -race -count=1 ./ghttp/` 通过。

## 严重 🔴

### S1'. classifyError 调用用户 `HTTPStatus()` 无 panic 防护 —— typed-nil StatusCoder 击穿 ServeHTTP【实测】

**位置**: `error_chain.go:60-77`（`errors.As(err, &sc)` 后直接 `sc.HTTPStatus()`）+ `mux.go`（writeError 运行在 safeChain 的 recover **之外**）

业务代码经典失误 `var e *MyErr; return e`（typed-nil：接口非 nil、内部指针 nil）→ `HTTPStatus()` nil 解引用 panic，发生在所有 recover 之后。实测：`PANIC 逃出 ServeHTTP: runtime error: invalid memory address or nil pointer dereference`。首轮 S4 只校验了 HTTPStatus 的**返回值**范围，没防**方法调用本身** panic——同一漏洞的另一半。

**修复建议**: classifyError 里用带 `recover` 的小函数取状态码，panic 时回退 500。

## 中等 🟡

### M1'. autoHEAD 特性一组缺陷（新功能，四处问题）【实测】

**位置**: `mux.go:359-361, 463-479, 495-497, 723-734`

1. **存在任一显式 HEAD 路由时整体失效**：回退条件是 `t == nil`（整棵 HEAD 树缺失），而 `Health()`/`Ready()`/`Static()`/`File()` 都注册 HEAD 路由——用户一旦使用其中任何一个，autoHEAD 对全站其它路径立即失效。实测：注册 `HEAD /health` 后 `HEAD /x` → 405。应改为"HEAD 树中未命中该 path 时按 GET 回退"。
2. **HEAD 响应丢失 Content-Length 与 Content-Type**：`headResponseWriter.Write` 吞掉 body，标准库因此无法计算 CL、无法做 CT 嗅探。实测：GET `CL=4, CT=text/html`，autoHEAD 的 HEAD `CL=-1, CT=""`——违反 RFC 9110 §9.3.2；标准库原生行为（handler 原样执行、net/http 自动丢体）是 `CL=18, CT=text/html`。正确做法是不吞字节（net/http 对 HEAD 自动丢体）。
3. **405 的 Allow 不含 HEAD**：`allowedMethods` 只扫树，autoHEAD 开启且 GET 命中时 `POST /x` → `Allow: "GET"`。实测确认。
4. **显式注册的 HEAD handler 的 Write 也被吞**：分发只判 `autoHEAD && method==HEAD`，不区分 fallback 与显式命中——显式 HEAD 路由本可依赖标准库计算 CL，也被剥夺（实测显式 HEAD `CL=-1`）。

### M2'. Run 监听失败后 `mux.serving` 未复位，注册永久被拒【实测】

**位置**: `server.go:371-385`（markStarted 同时置 `state=running` 与 `serving=true`）与 `server.go:406-416`（endRun 只回退 `state`）

实测：`Run("非法地址")` 失败返回后再 `RawHandle` → `ErrRegistrationAfterStart`。两个状态源只回退了一个，Server 成半僵尸：IsStarted()=false 却不可注册。**修复**: endRun 回退 idle 时同步 `serving.Store(false)`。

### M3'. form/multipart 解码路径 MaxBytesError 被打平，413 退化 400【实测】

**位置**: `body_decoders.go:115, 162, 182`（`fmt.Errorf("%w: %v", ErrInvalidInput, err)`）

M2 修复只覆盖 `codec.go` 的 `decodeError`，form 路径漏修：LimitBody + chunked 超限表单体 → `*http.MaxBytesError` 被 `%v` 打平 → 客户端收 400（实测）。**修复**: 三处改用 `decodeError(err)`。

### M4'. gzip 中间件下 1xx informational 后最终状态码丢失【实测】

**位置**: `gzip.go:170-181`（WriteHeader 首调即 `decided=true`）vs `request.go:137-141`（外层 Response 对 1xx 特判透传）

handler 先 `WriteHeader(103)` 再 `WriteHeader(201)`：gzip 包装层把 103 当最终决策，201 被丢弃。实测：挂 gzip 后客户端收 200，不挂对照为 201。**修复**: `gzipResponseWriter.WriteHeader` 对 `status < 200` 透传且不置 `decided`。

### M5'. multipart 请求体无总量上限 —— 磁盘耗尽面

**位置**: `body_decoders.go:113-121`、`middleware_csrf.go:335`

`ParseMultipartForm(32MiB)` 只限驻留内存，超出落盘无上限。urlencoded 路径特意自设 10MiB 帽防"换 method 绕过限额"，但换 `CT: multipart/form-data` 即绕过——未挂 LimitBody 的 FormBody 端点可被单请求写满磁盘。CSRF 的 4MiB 帽只查 ContentLength，chunked 也绕过。**修复**: multipart 解析前也包 MaxBytesReader 默认总量帽。

### M6'. formPlan 与 bindPlan 注册期防护不对称 —— 请求期 reflect panic 500

**位置**: `body_decoders.go:249-295`（formPlan.collect）缺 `bind_plan.go:143` 已有的"未导出内嵌指针拒绝"

`struct{ *inner }`（inner 未导出、含 form 字段）注册通过，请求期 `v.Set(reflect.New(...))` panic → 每请求 500。同类：带 tag 的**未导出匿名字段**（跳过条件 `!IsExported() && !Anonymous` 放行匿名）两个 plan 都注册通过、请求期 panic。**修复**: 两处 collect 对称补齐注册期拒绝。

### M7'. RateLimit 默认 IP 维度无 IPv6 前缀聚合，与"满额放行"组合成完整绕过

**位置**: `middleware_ratelimit.go:201-206` + `252-262`

IPv6 下每地址独立一桶：一个 /64 有 2^64 地址可轮换，每个新地址首请求必放行；轮换还可快速打满 MaxKeys（默认 10 万），之后所有新 key 直接放行——限流对攻击者整体失效并 reap 掉合法用户的桶。**修复**: 默认 KeyFunc 对 IPv6 归一到 /64（nginx/cloudflare 惯例）。

### M8'. 表单端点空 Content-Type 仍 fail-open（M9 残留窄化版）

**位置**: `codec.go:274-276` + `body_decoders.go:109-127`

POST 空 CT：ParseForm 不读体不报错 → 零值结构体 + 200，数据静默丢弃；DELETE 空 CT 却走 readAllCapped+ParseQuery 真解析——同一契约两种行为。**修复**: form 契约对空 CT 显式 415，或统一走自读路径。

## 轻微 🔵

| # | 位置 | 问题 |
|---|---|---|
| L1 | `static.go:331-344` | `AcceptsEncoding("*;q=0, gzip", "gzip")` 返回 false【实测】——按出现顺序先命中 `*` 即定案，RFC 9110 语义是具体项优先于通配；另 `q=0.0000`（4 位小数，RFC 限 3 位）被判接受 |
| L2 | `body_decoders.go:234-244` | formPlan 请求期才编译，编译错误（类型不支持/嵌套超深）包成 ErrInvalidInput → 服务端定义错误报成客户端 400，应注册期预编译或 500 |
| L3 | `bind_plan.go:191-219` | `query:",omitempty"`（空名带选项）建出空名绑定步（可被 `?=v` 命中）；同字段并存 query+path+header tag 静默按 query 优先，无冲突报错 |
| L4 | `bind_value.go:302-307, 293-301, 327-342` | ParseBool 不认 HTML checkbox 的 "on"；float 接受 NaN/Inf（可致后续 JSON 编码 500）；切片元素 TrimSpace 而标量不 trim |
| L5 | `codec.go:170-175, 222-234` | JSON 尾随垃圾检测完整物化第二个值（应改 `dec.More()`，无 LimitBody 时是放大点）；XML 尾随 CharData 放行，与 JSON 策略不一致 |
| L6 | `codec.go:252-257` | charset 参数剥弃：`charset=gbk` 按 UTF-8 解，建议文档标注仅支持 UTF-8 |
| L7 | `basic_auth.go:127-129` | ConstantTimeCompare 长度不等立即返回 0，泄漏长度差异（业界普遍接受；可先 SHA-256 归一消除） |
| L8 | `sse.go` | ID/Event/Comment 已剥 `\r\n`，但 NUL 等控制字符未过滤（低危） |
| L9 | `error_chain.go:59-77` | errors.As 使链上任意深度 StatusCoder 恒压过外层 sentinel，行为自洽但优先序未文档化 |

## 性能 ⚡

基准现状：路由热路径零分配（static 24.6ns、param1 29.7ns、5 中间件命中 38.4ns），弱点集中在 typed 端点：

| # | 位置 | 问题与量化 |
|---|---|---|
| P1 | `output.go:96-102` | 默认 JSON 输出每请求新建 bytes.Buffer + Encoder——GetParamsSmall 515ns/208B/6allocs 的最大单项，可 sync.Pool 池化 |
| P2 | `metrics.go:61-70` | countFor 每请求拿一次分片 Mutex，status 码集合基本固定，高 RPS 下是竞争点 |
| P3 | `codec.go:134` | json.NewDecoder 每请求 1 分配，可池化 |
| P4 | `bind_value.go:186,190` | 每 TextUnmarshaler 字段每请求 2 分配（装箱 + []byte 拷贝），量小 |
| P5 | `typed.go` | `&p`/`&b` 装箱逃逸是泛型执行器固有 1-2 alloc，无需处理 |
| P6 | `health.go:115-127` | 检查项串行执行（前三轮已知遗留） |

## 正面确认 ✅

复核前三轮全部安全修复，以下实现完整且质量高：TSR 开放重定向防护、CORS 通配+凭证降级与全分支 Vary、CSRF null origin/带 scheme 同源/恒定时间比较、XFF 多行合并、BasicAuth dummy 比较、ErrAbortHandler 三处透传、Recovery 全部上抛错误链、Timeout 协作式设计、URL 重写后重新 resolve、gzip Vary/Range/池化 Reset、SSE 三种行终止符拆行、惰性 query 与 url.ParseQuery 语义对齐、限流 NaN 防护、endRun 与 Shutdown 竞态处理。`-race` 全绿。

## 修复优先级

P0: S1'（typed-nil 击穿连接）→ P1: M1'/M2'/M3'/M5' → P2: M4'/M6'/M7'/M8' → P3: 轻微项 + 性能项（output buffer 池化收益最大）。

---

# 第四轮修复记录

S1' 与 M1'–M8' 已全部修复，另处理轻微项 L1/L5 与性能项 P1。`go test -race -count=1 ./ghttp/` 通过，`go vet`/`gofmt` 干净；路由命中热路径基准无回归（Static ~25ns、Param1 ~29ns，0 alloc），typed 热路径显著改善（见 P1）。

| 编号 | 修复方式 | 测试 |
|---|---|---|
| S1' typed-nil StatusCoder | `error_chain.go` 新增 `safeHTTPStatus`/`safeErrorMessage`（recover 防护），`classifyError` 经其调用用户实现的 `HTTPStatus()`；panic 回退 500，细节回传路径的 `Error()` 同样防护 | `TestClassifyError_TypedNilStatusCoderFallsBackTo500`（typed-nil 业务错误 + 细节回传开启，断言 500 且 panic 不出 ServeHTTP） |
| M1' autoHEAD 四缺陷 | ① 回退从「按树」改为「按路径」，抽出 `headFallback` 单一实现，`resolve`（链式路径）与 `dispatchRaw`（raw 路径，保留内联快路径避免 resolvedRoute 按值搬运的 ~4ns 回归）共享；② 整体删除吞字节的 `headResponseWriter`——net/http 对 HEAD 原生丢体并按写入字节算 Content-Length、做 CT 嗅探（RFC 9110 §9.3.2），包装层反而丢这两个头；③ `allowedMethods` 在 autoHEAD 开启且 GET 可匹配（且无显式 HEAD 树命中）时向 405 的 Allow 补报 HEAD；④ 回退前 `Params.reset()` + 清 skipped，防失败 HEAD 匹配的残留参数混叠 | `autohead_test.go` 重写为 5 个测试：真实服务器验证 HEAD 元数据与 GET 一致（CL=4、CT 非空、空体）、显式 HEAD 优先、存在显式 HEAD 路由时其余路径回退仍活、405 Allow 含 HEAD、参数路由回退无参数泄漏 |
| M2' Run 失败半僵尸 | `server.go` `endRun` 回退 `stateRunning→stateIdle` 时同步 `mux.serving.Store(false)`（两个状态源成对投影） | `TestRunListenFailureAllowsReRegistration`（Run 失败后 RawHandle 不再报 ErrRegistrationAfterStart） |
| M3' form 超限 400→413 | `body_decoders.go` 三处 `%w: %v` 打平改为 `decodeError(err)`，`*http.MaxBytesError` 可被 `errors.As` 提取映射 413 | `TestFormBodyOversizeChunkedReturns413`（LimitBody + chunked 表单体，真实服务器断言 413） |
| M5' multipart 磁盘耗尽 | 新增 `defaultMaxMultipartBytes = 64 MiB`（2× 内存阈值的容量决策），multipart 分支解析前包 `MaxBytesReader`；已挂 LimitBody 时双层包装取较小值 | `TestFormBodyMultipartOversizeReturns413`（65 MiB multipart 体断言 `ErrRequestEntityTooLarge`） |
| M8' 空 CT fail-open | `formCodec.Decode` 按 mediaType 分派：缺失 CT 显式 415（`ErrUnsupportedMediaType` + 期望列表）；`WithStrictContentType(false)` 下显式声明其它 CT 保留旧的按 urlencoded 解析行为 | `TestFormBodyMissingContentTypeReturns415`；`form_content_type_test.go` 原 fail-open 断言改为期望 415 |
| M4' gzip 1xx 定案 | `gzip.go` `WriteHeader` 对 1xx 透传且不置 `decided`，与外层 `Response` 的同款特判对齐 | `TestGzip1xxInformationalKeepsFinalStatus`（103 后 201，断言最终状态 201 且 gzip 体完整） |
| M6' 两 collect 防护不对称 | `formPlan.collect` 补「未导出内嵌指针拒绝」（与 `BindPlan.collect` 同款）；两处 collect 都新增「带绑定 tag 的未导出匿名字段」注册期拒绝（导出性筛只放行匿名以便展开，作为绑定目标 Set 必 panic） | `TestBindPlanRejectsTaggedUnexportedAnonymous`、`TestFormPlanRejectsUnexportedEmbeddedPointer` |
| M7' IPv6 逐地址分桶 | 新增 `rateLimitIPKey`：IPv6 聚合 /64 前缀（nginx/CDN 惯例，单订户最小分配单元），IPv4 与 4-in-6 原样；默认 KeyFunc 接入，自定义 KeyFunc 不受影响；`RateLimitConfig.KeyFunc` 文档同步 | `TestRateLimitIPKeyAggregatesIPv6`（表驱动 6 例：IPv4/同 64 聚合/异 64 分桶/4-in-6/非法输入） |
| L1 AcceptsEncoding 顺序 | 具名 token 优先于通配符（RFC 9110 §12.5.3），`*` 的判定延迟到扫完全表；`"*;q=0, gzip"` 现正确接受 gzip | `static_precompressed_test.go` 原固化错误行为的用例改正，新增 3 个具名 vs 通配组合用例 |
| L5 尾随内容检测 | JSON 探测从 `Decode(&extra)`（完整物化第二值，CPU/内存放大点）改为 `dec.Token()`（读一个令牌即判定，顶层 `]`/`}` 垃圾直接语法错；`More()` 不可用——对顶层尾随 `]`/`}` 返回 false）；XML 尾随 CharData 只放行纯空白，非空白字符数据按尾随垃圾拒绝 | 既有 `TestJSONTrailingContent` 全部保持通过；`TestXMLTrailingCharDataRejected`（非空白拒绝/纯空白放行） |
| P1 output 缓冲池化 | `output.go` 默认 JSON 路径的 `bytes.Buffer` 经 `sync.Pool` 复用，>64 KiB 不回池防峰值驻留；「先缓冲后提交」的错误链语义不变 | 既有 typed 全量测试覆盖；基准 `TypedGetParamsSmall` 516ns/208B/6allocs → ~450ns/96B/4allocs |

未处理项维持原判：L2/L3/L4/L6/L7/L8/L9（既定取舍或收益不抵改动面）、P2–P6（P5 泛型固有、P6 前三轮已知遗留）。

变更文件：`error_chain.go`、`mux.go`、`server.go`、`body_decoders.go`、`bind_plan.go`、`gzip.go`、`middleware_ratelimit.go`、`static.go`、`codec.go`、`output.go`；测试新增 `review_round4_fixes_test.go`、重写 `autohead_test.go`，更新 `form_content_type_test.go`、`static_precompressed_test.go`；CHANGELOG 补 11 条 Fixed + 1 条 Performance。
