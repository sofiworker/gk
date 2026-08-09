# ghttp 框架级 k6 针对性深度测试设计

## 定位

在 `ghttp/testdata/k6/` 增加正式维护的 ghttp 框架级针对性测试套件。测试由 k6、Go 单元测试、raw-socket HTTP 探针与进程资源采样器共同组成，不把 k6 无法构造的底层协议输入伪装为已覆盖。

目标规模为约 70–90 条服务端测试路由、150 条以上运行时检查和 12 类场景。它不是跨框架 benchmark，也不是生产 SLO 证明，而是功能、HTTP 语义、协议、安全边界、稳定性、资源恢复和性能趋势的综合回归工具。

README 和测试文档必须明确：

- 测试对象仅为 ghttp。
- 测试同时验证正确性与性能指标，不能只追求 requests/s。
- 默认完整 profile 在普通开发机上约运行 5–10 分钟。
- 不同机器的绝对吞吐不可直接横向比较；适合比较同一环境中的版本变化。
- 仓库仍处于 pre-v1.0，测试通过不代表可以生产使用。

## 目录

```text
ghttp/testdata/k6/
├── README.md
├── README.en.md
├── go.mod
├── cmd/server/main.go
├── cmd/rawprobe/main.go
├── cmd/resources/main.go
├── internal/app/app.go
├── internal/app/app_test.go
├── fixtures/static/sample.txt
├── scripts/run.ps1
├── scripts/run.sh
├── scenarios/
│   ├── contract_smoke.js
│   ├── routing_matrix.js
│   ├── binding_codec.js
│   ├── error_security.js
│   ├── mixed_api.js
│   ├── stateful_crud.js
│   ├── streaming_sse.js
│   ├── streaming_websocket.js
│   ├── slow_clients.js
│   ├── spike_recovery.js
│   └── resource_soak.js
└── lib/
    ├── config.js
    ├── checks.js
    ├── metrics.js
    └── data.js
```

`go.mod` 使用本地 `replace` 指向仓库根模块，不新增根模块依赖。测试服务和路由函数保持可单测，runner 只负责进程编排。

## 覆盖矩阵

测试服务至少覆盖以下类型，不以少数 CRUD 路径代替广泛覆盖。

### 路由与调度

- 静态、单参数、多参数、catch-all、嵌套 Group。
- 静态与参数优先级。
- GET、POST、PUT、PATCH、DELETE、HEAD、OPTIONS。
- 404、405 与 `Allow`。
- 尾斜杠、重复斜杠、转义斜杠、Unicode、超长路径、非法转义。
- Host、RawPath、原始 query 和转义信息保真。
- 重复注册和冲突路由通过独立构建测试验证，不在已启动服务中制造不确定状态。

### 类型绑定与校验

- path、query、Header、Cookie。
- JSON、XML、form、multipart。
- 指针、数组、slice、嵌套结构、时间、枚举、自定义类型。
- 可选字段、重复 query、边界整数、浮点边界、布尔和 Unicode。
- 空 body、截断 JSON/XML、错误 Content-Type、非法 form、缺失 multipart boundary。
- 小、中、大请求体，以及超过服务限制的请求体。

### 输出

- JSON、XML、text、binary、空响应。
- 200、201、202、204、206、3xx。
- 内容协商与 406。
- 下载、Range、ETag、If-None-Match、Last-Modified。
- gzip 响应。
- OpenAPI 文档和关键 operation/status 一致性。

### 错误系统与安全边界

- 普通 error、wrapped error、joined error、sentinel、gerr、校验错误、panic recovery。
- 400、401、403、404、405、409、412、413、415、422、429、500、503、504。
- JSON error 与 `application/problem+json`。
- 未知错误不得泄漏 stack、密码、token 或内部 sentinel 文本。
- request ID 在成功与错误响应中保持一致。
- 认证缺失、无效 token、权限不足和合法 token。

### Middleware

- 请求 ID、认证、CORS、压缩、恢复、超时。
- middleware 执行顺序和短路。
- context 值传递。
- 响应 Header 合并与覆盖边界。

### 状态与并发

- create → get → update → list → delete → get 404 完整状态链。
- 每个 VU 使用独立 namespace，防止测试数据互相污染。
- 重复创建、重复删除、ETag 乐观锁、版本冲突和幂等请求。
- 并发读写时状态码与响应 schema 保持合法。

### SSE

- 单事件、多事件、事件 ID、event、retry、data 换行。
- 心跳、慢消费者、客户端断连、服务端超时和并发连接。

### WebSocket

- 握手、text echo、binary、ping/pong。
- 多消息、并发连接、客户端关闭、服务端关闭。
- 超大消息、消息洪泛、慢消费者和非法关闭流程。

若公开 `ghttp.WebSocketConn` 无读取前大小限制 API，则测试只报告读后 1009 wire rejection，明确不能证明分配前内存保护；该项作为能力缺口，不修改核心 API。

### 对抗性 HTTP 输入

- CRLF/Header 注入和响应拆分边界。
- 重复 Content-Length、Content-Length/Transfer-Encoding 冲突。
- 超大 Header、Header 数量膨胀、非法 Header 名和值。
- 畸形 URL、NUL、非法转义、异常 Host。
- 超大 multipart、文件数量膨胀、缺失 boundary。
- 慢速上传、请求体提前断开、连接耗尽。
- gzip 高压缩率 payload 和解压大小限制。

k6 负责可由标准 HTTP/WebSocket API 构造的输入。重复 Content-Length、TE/CL、非法 Header 字节等底层报文由 `cmd/rawprobe` 直接通过 TCP 构造，并验证服务不得 panic、泄漏秘密或返回非法 HTTP。

### 生命周期与故障注入

- readiness、liveness 和启动失败。
- graceful shutdown、in-flight request shutdown、SSE/WS 关闭。
- handler 延迟、随机错误、受控 429/503。
- 客户端取消、服务端超时、连接中断。
- spike 后功能、错误率和资源占用恢复。

### OpenAPI 一致性

- 路径、方法、参数位置和 required 属性。
- request/response Content-Type。
- 成功与错误状态集合。
- schema required、数组和对象形状；nullable 若当前公开 ghttp API 无法表达，则以自动化 absence 断言和 capability-gap 报告验收，不修改核心 API或手写虚假 schema。
- 代表性运行时响应与声明 schema 对照。

### 资源与运行时观测

测试服务暴露仅供套件使用的 `/__test/metrics`，返回：

- heap alloc、heap in-use、total alloc、GC 次数和累计 pause。
- goroutine 数、活跃请求、峰值活跃请求。
- 当前与累计 SSE/WebSocket 连接。
- 各状态码、错误类型、panic recovery、timeout、cancel 计数。
- 请求体和响应体累计字节。

runner 额外采集服务进程 CPU、working set、private bytes、handle/thread 数。指标至少在负载前、负载中、峰值、负载后和恢复窗口取样，用于发现 goroutine、连接、heap 或 handle 未恢复趋势。

## 十二类场景与时长

### contract_smoke

约 30–45 秒，低并发遍历全部路由类型和关键失败路径。它是功能回归入口，所有检查必须通过。

### routing_matrix

约 30 秒，集中覆盖 method/path/host/RawPath/404/405/HEAD/OPTIONS 和路由优先级。

### binding_codec

约 45 秒，覆盖全部输入来源、codec、内容协商、边界值和畸形 body。

### error_security

约 45 秒，覆盖错误分类、Problem Details、认证、panic recovery、秘密泄漏与安全 Header。

### mixed_api

约 2–3 分钟，按读多写少的比例混合执行查询、详情、创建、更新、错误和静态资源请求。

### stateful_crud

约 60–90 秒，每个 VU 重复完成独立 CRUD 状态链，验证状态隔离与冲突行为。

### streaming_sse

约 45–60 秒，验证事件语义、首事件时延、心跳、断连和慢消费者。

### streaming_websocket

约 45–60 秒，验证握手、消息、ping/pong、关闭、洪泛和慢消费者。

### slow_clients

约 30 秒，覆盖慢请求、取消、提前断开和服务端 timeout。

### spike_recovery

约 45–60 秒，从低并发快速提升并恢复，观察错误率、拒绝率、尾延迟和恢复窗口。

### resource_soak

约 90–120 秒，以稳定中等并发观察 heap、GC、goroutine、连接、CPU 和 handle 趋势。

### raw_http_adversarial

约 20–30 秒，由 Go rawprobe 执行非法或冲突 HTTP 报文，并在每组后验证健康探针和资源计数。

默认 full runner 将互不干扰的短场景并行执行，状态、streaming、spike、soak 和 rawprobe 分阶段执行，总时长目标为 8–10 分钟。contract_smoke 可单独运行。

## 指标

### k6 内建指标

- `checks`、`http_req_failed`。
- `http_reqs`、`iterations`、`vus`、`vus_max`。
- `http_req_duration` 及 blocked、connecting、tls_handshaking、sending、waiting、receiving。
- `http_req_size`、`http_req_body_size`、`http_resp_size`、`data_sent`、`data_received`。
- `iteration_duration`、`dropped_iterations`。
- WebSocket sessions、connecting、ping、messages sent/received、session duration。

### 自定义指标

- 各路由族成功率和 p95/p99。
- CRUD 完整状态链成功率。
- 错误 schema 正确率。
- 敏感信息泄漏计数。
- 认证、内容协商、Range/ETag、OpenAPI 一致性成功率。
- SSE 首事件时延、事件计数、异常关闭率。
- WebSocket echo 时延、消息丢失率、异常关闭率。
- timeout/cancellation 分类正确率。
- 429/503 等受控拒绝率与 spike 后恢复成功率。
- panic、非法 HTTP 响应和 rawprobe 后健康失败计数。
- heap、goroutine、连接、handle 和 working set 恢复比例。
- GC pause、服务进程 CPU 和峰值 working set。

结果使用 k6 JSON summary 和控制台 summary 保存。测试文档说明每项指标的含义、默认阈值和调整方式。

## 默认阈值

默认阈值用于本地回归门禁，不宣称为生产 SLO：

- contract_smoke `checks` 必须为 100%。
- 未预期请求的 `http_req_failed` 必须低于 0.1%。
- 普通 HTTP p95 默认低于 500ms，p99 默认低于 1000ms。
- 状态链、错误 schema、认证、OpenAPI 和敏感信息检查必须为 100%。
- SSE/WebSocket 功能成功率至少 99%，contract_smoke 中必须为 100%。
- spike 场景允许受控拒绝，但不允许 panic、非法状态码、秘密泄漏或恢复后持续失败。
- raw HTTP 探针结束后健康检查必须为 100%，panic 和秘密泄漏计数必须为 0。
- 恢复窗口结束后，活跃请求与流连接必须归零；goroutine、heap 和 handle 相对基线的增长必须处于可配置容差内。

延迟和并发阈值可通过环境变量覆盖；功能正确性和安全边界阈值不可静默关闭。

## Go 单元测试

测试服务本身必须有表驱动测试，至少覆盖：

- 每类路由的成功和失败路径。
- binding、validation、error、Problem Details。
- middleware 顺序、request ID、认证和 recovery。
- Range/ETag、OpenAPI、SSE 和 WebSocket 基本语义。
- 状态 reset 与并发访问。
- rawprobe 报文构造和响应分类。
- runtime metrics 计数与 reset。

Go 单元测试验证确定性语义，k6 验证真实 TCP、并发、状态流与指标；两者不能互相替代。

## Runner 与失败处理

PowerShell 和 shell runner：

1. 使用实验目录内的 Go cache。
2. 构建并启动测试服务。
3. 轮询就绪探针。
4. 检查 k6 是否安装并记录版本；不自动安装系统软件。
5. 启动资源采样器并记录基线。
6. 执行指定 profile、rawprobe 或 full suite。
7. 等待恢复窗口并记录恢复指标。
8. 保存 summary、资源样本、rawprobe 结果、日志和运行元数据。
9. 无论测试成功、失败或中断都关闭服务与采样器。

服务启动失败、k6 缺失、阈值失败、脚本错误和环境错误必须使用不同退出信息，便于 CI 或使用者判断。

## golangci-lint 徽章

`README.md` 与 `README.en.md` 同时增加 golangci-lint 徽章，链接到 `https://golangci-lint.run/`。徽章状态复用已有 `.github/workflows/go.yml`，alt 文本为 `golangci-lint`，避免展示与仓库实际 lint 状态无关的静态徽章。

## 验证标准

- `go test ./...` 在 `ghttp/testdata/k6` 独立 module 内通过。
- 所有 k6 脚本可被 k6 解析。
- contract_smoke profile 真实运行通过。
- full profile 在 5–10 分钟目标内完成，并输出完整指标。
- rawprobe 全部 case 有明确分类，运行后服务保持健康。
- 资源采样包含基线、峰值和恢复窗口，且连接/活跃请求归零。
- 中英文 README 徽章一致。
- 横评实验仍只在 `.tmp/`，不纳入提交。
- 未经维护者明确授权，不提交、不推送。
