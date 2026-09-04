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
