# ghttp 框架级 k6 针对性深度测试 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `ghttp/testdata/k6/` 建立约 70–90 条测试路由、150 条以上检查、12 类场景的 ghttp 功能、安全、稳定性、资源与性能趋势测试套件，并在中英文根 README 增加 golangci-lint 徽章。

**Architecture:** 独立 Go module 启动真实 ghttp TCP 服务；Go 单元测试验证确定性语义，k6 验证并发和指标，rawprobe 构造 k6 无法发送的非法 HTTP 报文，resources 采样器记录 Go runtime 与 Windows/Linux 进程资源。runner 统一管理服务生命周期、恢复窗口和结果归档。

**Tech Stack:** Go 1.26.4、仓库本地 ghttp/gerr、Grafana k6、JavaScript ES modules、PowerShell、POSIX shell、标准库 `net/http`/`net`/`runtime/metrics`。

## Global Constraints

- 仓库 pre-v1.0，所有文档必须明确禁止直接用于生产。
- 测试资产正式放入 `ghttp/testdata/k6/`；横评 `.tmp/ghttp-framework-review/` 不纳入提交。
- 根模块不新增依赖；独立测试 module 使用 `replace github.com/sofiworker/gk => ../../..`。
- 代码注释与导出符号使用中英双语，用户可见错误字符串保持英文。
- 不修改 `.gitignore`，不推送；未经维护者明确授权不提交。
- 每个 Go 函数必须有对应单元测试；使用 `gofmt`、`go mod tidy` 和相关 `go test ./...`。
- 默认 full suite 目标运行时间 8–10 分钟；绝对吞吐不表述为生产 SLO。

---

## File Map

- `README.md`、`README.en.md`：golangci-lint 徽章。
- `ghttp/testdata/k6/go.mod`：独立测试 module。
- `ghttp/testdata/k6/internal/app/*.go`：测试服务、路由域、状态存储、middleware、metrics、错误与协议 handler。
- `ghttp/testdata/k6/internal/app/*_test.go`：确定性功能、并发、OpenAPI、SSE、WebSocket 测试。
- `ghttp/testdata/k6/cmd/server/main.go`：服务进程入口。
- `ghttp/testdata/k6/internal/rawprobe/*.go`、`cmd/rawprobe/main.go`：原始 TCP case 与结果分类。
- `ghttp/testdata/k6/internal/resources/*.go`、`cmd/resources/main.go`：跨平台资源采样。
- `ghttp/testdata/k6/lib/*.js`：k6 共享配置、检查、指标、测试数据。
- `ghttp/testdata/k6/scenarios/*.js`：11 个 k6 场景；raw HTTP 为第 12 类场景。
- `ghttp/testdata/k6/scripts/run.ps1`、`run.sh`：完整编排。
- `ghttp/testdata/k6/README.md`、`README.en.md`：运行、指标、限制与结果解释。

---

### Task 1: 独立 module、徽章与测试骨架

**Files:**
- Create: `ghttp/testdata/k6/go.mod`
- Create: `ghttp/testdata/k6/internal/app/app_test.go`
- Modify: `README.md`
- Modify: `README.en.md`

**Interfaces:**
- Produces: module `github.com/sofiworker/gk/ghttp/testdata/k6`，后续任务均依赖。

- [ ] **Step 1: 写失败的 app 构造测试**

```go
func TestNewReturnsHandler(t *testing.T) {
    handler, cleanup := New(Config{})
    t.Cleanup(cleanup)
    if handler == nil {
        t.Fatal("handler is nil")
    }
}
```

- [ ] **Step 2: 运行并确认 RED**

Run: `go test ./internal/app -run TestNewReturnsHandler`

Expected: FAIL，`undefined: New` 或 module 尚未构建。

- [ ] **Step 3: 创建独立 module**

```go
module github.com/sofiworker/gk/ghttp/testdata/k6

go 1.26.4

require github.com/sofiworker/gk v0.0.0

replace github.com/sofiworker/gk => ../../..
```

- [ ] **Step 4: 在两份 README 添加一致徽章**

```markdown
[![golangci-lint](https://github.com/sofiworker/gk/actions/workflows/go.yml/badge.svg)](https://golangci-lint.run/)
```

放在 Go workflow 徽章之后；中英文文件保持相同链接和顺序。

- [ ] **Step 5: 验证范围**

Run: `git diff -- README.md README.en.md`

Expected: 仅各新增一行徽章。

---

### Task 2: 配置、状态存储与 runtime 指标

**Files:**
- Create: `ghttp/testdata/k6/internal/app/config.go`
- Create: `ghttp/testdata/k6/internal/app/app.go`
- Create: `ghttp/testdata/k6/internal/app/store.go`
- Create: `ghttp/testdata/k6/internal/app/metrics.go`
- Create: `ghttp/testdata/k6/internal/app/store_test.go`
- Create: `ghttp/testdata/k6/internal/app/metrics_test.go`

**Interfaces:**
- Produces: `type Config struct { StaticDir string; MaxBodyBytes int64; Secret string }`
- Produces: 最小 `func New(Config) (http.Handler, func())`，先返回空 `http.ServeMux`；Task 3 在其上注册真实 ghttp 路由。
- Produces: `type Store`，方法 `Create`, `Get`, `List`, `Update`, `Delete`, `Reset`。
- Produces: `type RuntimeMetrics`，方法 `BeginRequest`, `EndRequest`, `OpenSSE`, `CloseSSE`, `OpenWebSocket`, `CloseWebSocket`, `Snapshot`, `Reset`。

- [ ] **Step 1: 实现 Config 与最小 New，让 Task 1 RED 转绿**

```go
type Config struct {
    StaticDir    string
    MaxBodyBytes int64
    Secret       string
}

func New(Config) (http.Handler, func()) {
    return http.NewServeMux(), func() {}
}
```

Run: `go test ./internal/app -run TestNewReturnsHandler`

Expected: PASS。该最小实现只解除 package 循环阻断，不提前实现 Task 3 路由行为。

- [ ] **Step 2: 写 Store 状态链和并发测试**

```go
func TestStoreCRUDAndVersionConflict(t *testing.T) { /* create/get/update/delete and stale ETag */ }
func TestStoreConcurrentNamespaces(t *testing.T) { /* 32 goroutines with independent namespace */ }
```

- [ ] **Step 3: 写指标计数与 reset 测试**

```go
func TestRuntimeMetricsTracksAndResets(t *testing.T) {
    metrics := NewRuntimeMetrics()
    done := metrics.BeginRequest()
    metrics.OpenSSE()
    done(200, 10, 20)
    snapshot := metrics.Snapshot()
    // assert active=0, total=1, sse=1, bytes and status count
    metrics.Reset()
}
```

- [ ] **Step 4: 运行并确认 Store/Metrics RED**

Run: `go test ./internal/app -run 'TestStore|TestRuntimeMetrics'`

- [ ] **Step 5: 用 mutex/atomic 实现最小代码**

`Store` 使用 `sync.RWMutex`；每条记录包含 `ID`, `Namespace`, `Name`, `Version`, `UpdatedAt`。指标使用 atomic counter，并通过 `runtime.ReadMemStats`/`runtime.NumGoroutine` 生成 snapshot。

- [ ] **Step 6: 运行 race 测试**

Run: `go test -race ./internal/app -run 'TestStore|TestRuntimeMetrics'`

Expected: PASS，无 data race。

---

### Task 3: 基础 app、路由矩阵和 HTTP 方法语义

**Files:**
- Modify: `ghttp/testdata/k6/internal/app/app.go`
- Create: `ghttp/testdata/k6/internal/app/routes_routing.go`
- Create: `ghttp/testdata/k6/internal/app/middleware.go`
- Create: `ghttp/testdata/k6/internal/app/routing_test.go`

**Interfaces:**
- Consumes: `Config`, `Store`, `RuntimeMetrics`。
- Enhances: Task 2 的最小 `New(Config)`，改为构建真实 ghttp handler。
- Produces: `/health`, `/ready`, `/routes/static`, `/routes/users/{id}`, `/routes/files/{path...}`, `/routes/groups/v1/items/{id}`, `/routes/unicode/{value}`。

- [ ] **Step 1: 写表驱动路由测试**

表中至少包含 GET/HEAD/OPTIONS/404/405、静态优先参数、Unicode、catch-all、尾斜杠和 `Allow` Header。

- [ ] **Step 2: 写冲突注册测试**

使用独立 server 构建函数捕获重复路由和 `/health`/`/health/` 冲突，记录当前 panic 语义。

- [ ] **Step 3: 运行并确认 RED**

Run: `go test ./internal/app -run 'TestRouting|TestDuplicate|TestTrailing'`

- [ ] **Step 4: 使用 ghttp 原生 API 注册路由**

所有成功 handler 返回结构化响应；middleware 记录 request ID、活跃请求、状态和字节数。不得通过外层 net/http mux 重新匹配业务路由。

- [ ] **Step 5: 验证**

Run: `go test ./internal/app -run 'TestRouting|TestDuplicate|TestTrailing'`

Expected: PASS。

---

### Task 4: Binding、codec 与内容协商

**Files:**
- Create: `ghttp/testdata/k6/internal/app/routes_binding.go`
- Create: `ghttp/testdata/k6/internal/app/binding_test.go`
- Create: `ghttp/testdata/k6/internal/app/fixtures.go`

**Interfaces:**
- Produces: `/binding/path/{id}`, `/binding/query`, `/binding/header`, `/binding/cookie`, `/binding/mixed/{id}`。
- Produces: `/codec/json`, `/codec/xml`, `/codec/form`, `/codec/multipart`, `/codec/negotiate`。

- [ ] **Step 1: 写成功输入矩阵**

覆盖 path/query/header/cookie 混合输入、数组、slice、时间、枚举、Unicode、JSON/XML/form/multipart。

- [ ] **Step 2: 写失败矩阵**

覆盖缺失 required、溢出整数、截断 JSON/XML、错误 Content-Type、非法 form、缺失 multipart boundary、超过 `MaxBodyBytes`。

- [ ] **Step 3: 运行并确认 RED**

Run: `go test ./internal/app -run 'TestBinding|TestCodec|TestNegotiation'`

- [ ] **Step 4: 实现类型化 request/response 与校验**

每类输入使用独立 struct，显式 tag；验证错误必须稳定返回 400/413/415/422，不能依赖字符串模糊匹配。

- [ ] **Step 5: 验证**

Run: `go test ./internal/app -run 'TestBinding|TestCodec|TestNegotiation'`

Expected: PASS。

---

### Task 5: 输出、缓存、Range 与 OpenAPI

**Files:**
- Create: `ghttp/testdata/k6/internal/app/routes_output.go`
- Create: `ghttp/testdata/k6/internal/app/output_test.go`
- Create: `ghttp/testdata/k6/internal/app/openapi_test.go`
- Create: `ghttp/testdata/k6/fixtures/static/sample.txt`

**Interfaces:**
- Produces: `/output/json`, `/output/xml`, `/output/text`, `/output/binary`, `/output/empty`, `/output/accepted`, `/output/redirect`。
- Produces: `/files/sample`, `/files/etag`, `/openapi.json`。

- [ ] **Step 1: 写 status/content-type/header 测试**

断言 200/201/202/204/206/3xx、Location、Content-Length、Vary、gzip 和 HEAD body suppression。

- [ ] **Step 2: 写 Range/ETag 条件请求测试**

覆盖 `Range: bytes=0-3`、非法 Range、If-None-Match 304、If-Match 412、Last-Modified。

- [ ] **Step 3: 写 OpenAPI 一致性测试**

解析 `/openapi.json`，验证代表性 path/method/parameter/content/status 与运行时矩阵一致。

- [ ] **Step 4: 运行 RED 后实现**

Run: `go test ./internal/app -run 'TestOutput|TestRange|TestETag|TestOpenAPI'`

- [ ] **Step 5: 验证**

Run: `go test ./internal/app -run 'TestOutput|TestRange|TestETag|TestOpenAPI'`

Expected: PASS。

---

### Task 6: 错误、认证、middleware 与故障注入

**Files:**
- Create: `ghttp/testdata/k6/internal/app/routes_error.go`
- Create: `ghttp/testdata/k6/internal/app/routes_fault.go`
- Create: `ghttp/testdata/k6/internal/app/error_test.go`
- Create: `ghttp/testdata/k6/internal/app/middleware_test.go`

**Interfaces:**
- Produces: `/errors/{kind}`, `/problem/{kind}`, `/auth/{role}`, `/fault/delay`, `/fault/random`, `/fault/limited`, `/fault/panic`。

- [ ] **Step 1: 写错误分类表**

覆盖 ordinary/wrapped/joined/sentinel/gerr/validation/panic 及 400/401/403/404/405/409/412/413/415/422/429/500/503/504。

- [ ] **Step 2: 写敏感信息与 request ID 测试**

响应不得包含配置的 password/token/internal sentinel；成功和错误响应必须回显同一 request ID。

- [ ] **Step 3: 写 middleware 顺序、短路、timeout/recovery 测试**

记录调用序列，验证 before 顺序、after 逆序、auth 短路、panic recovery 和 context cancel。

- [ ] **Step 4: 运行 RED 后实现**

Run: `go test ./internal/app -run 'TestError|TestProblem|TestAuth|TestMiddleware|TestFault'`

- [ ] **Step 5: 验证 race**

Run: `go test -race ./internal/app -run 'TestError|TestProblem|TestAuth|TestMiddleware|TestFault'`

Expected: PASS。

---

### Task 7: Stateful CRUD、幂等与并发一致性

**Files:**
- Create: `ghttp/testdata/k6/internal/app/routes_state.go`
- Create: `ghttp/testdata/k6/internal/app/state_test.go`

**Interfaces:**
- Produces: `/state/{namespace}/items` 和 `/state/{namespace}/items/{id}` CRUD。
- Produces: `Idempotency-Key`、ETag/If-Match 与 `POST /__test/reset`。

- [ ] **Step 1: 写完整状态链测试**

断言 create 201 → get 200 → update 200 → list → delete 204 → get 404。

- [ ] **Step 2: 写幂等、重复删除和 stale ETag 测试**

相同 Idempotency-Key 返回相同资源；重复删除返回稳定状态；过期 If-Match 返回 412。

- [ ] **Step 3: 写 32 goroutine 并发读写测试**

每 goroutine 使用独立 namespace，同时验证响应 schema、版本单调递增和最终 reset。

- [ ] **Step 4: 运行 RED 后实现**

Run: `go test -race ./internal/app -run 'TestState|TestIdempotency|TestConcurrent'`

- [ ] **Step 5: 验证**

Expected: PASS，无 race。

---

### Task 8: SSE、WebSocket 与生命周期

**Files:**
- Create: `ghttp/testdata/k6/internal/app/routes_stream.go`
- Create: `ghttp/testdata/k6/internal/app/stream_test.go`
- Create: `ghttp/testdata/k6/internal/app/lifecycle_test.go`

**Interfaces:**
- Produces: `/sse/once`, `/sse/multi`, `/sse/heartbeat`, `/sse/slow`。
- Produces: `/ws/echo`, `/ws/binary`, `/ws/close`, `/ws/slow`。

- [ ] **Step 1: 写 SSE wire-format 测试**

验证 id/event/retry、多行 data、heartbeat、首事件和取消后连接计数归零。

- [ ] **Step 2: 写 WebSocket 测试**

验证 text/binary echo、ping/pong、多消息、server/client close 和超大消息拒绝。

- [ ] **Step 3: 写 shutdown in-flight 测试**

启动真实 server，保持慢请求/SSE/WS，调用 Shutdown 并验证完成或受控关闭。

- [ ] **Step 4: 运行 RED 后实现**

Run: `go test -race ./internal/app -run 'TestSSE|TestWebSocket|TestShutdown'`

- [ ] **Step 5: 验证**

Expected: PASS，所有活跃连接计数回到 0。

---

### Task 9: raw HTTP 对抗探针

**Files:**
- Create: `ghttp/testdata/k6/internal/rawprobe/cases.go`
- Create: `ghttp/testdata/k6/internal/rawprobe/runner.go`
- Create: `ghttp/testdata/k6/internal/rawprobe/runner_test.go`
- Create: `ghttp/testdata/k6/cmd/rawprobe/main.go`

**Interfaces:**
- Produces: `type Case struct { Name string; Payload []byte; AllowedStatus []int; ExpectClose bool }`
- Produces: `func Run(context.Context, string, []Case) Report`。

- [ ] **Step 1: 写 payload 精确字节测试**

覆盖重复 Content-Length、TE+CL、CRLF Header、非法 Header、NUL URL、非法转义、超大 Header、提前断体。

- [ ] **Step 2: 写本地 TCP fixture 的响应分类测试**

分类为 valid-response、connection-closed、timeout、invalid-response；不得把 closed connection 自动判失败。

- [ ] **Step 3: 运行 RED 后实现**

Run: `go test ./internal/rawprobe`

- [ ] **Step 4: 增加 CLI JSON 输出**

参数：`-addr`, `-timeout`, `-output`；退出码区分 case failure 和环境连接失败。

- [ ] **Step 5: 验证真实服务**

Run: `go run ./cmd/rawprobe -addr 127.0.0.1:18080`

Expected: 每 case 有明确分类，随后 `/health` 正常。

---

### Task 10: 资源采样器

**Files:**
- Create: `ghttp/testdata/k6/internal/resources/sample.go`
- Create: `ghttp/testdata/k6/internal/resources/process_windows.go`
- Create: `ghttp/testdata/k6/internal/resources/process_unix.go`
- Create: `ghttp/testdata/k6/internal/resources/sample_test.go`
- Create: `ghttp/testdata/k6/cmd/resources/main.go`

**Interfaces:**
- Produces: `type Sample struct { At time.Time; CPUSeconds float64; WorkingSet uint64; PrivateBytes uint64; Handles int; Threads int; Runtime app.MetricsSnapshot }`
- Produces: `func Collect(context.Context, int, string) (Sample, error)`。

- [ ] **Step 1: 写 HTTP runtime metrics 解析测试**

使用 httptest 返回固定 JSON，验证全部字段与缺失字段错误。

- [ ] **Step 2: 写当前进程平台采样测试**

Windows 验证 working set/handle/thread 非负；Unix 使用 `/proc` 或系统调用并用 build tags 隔离。

- [ ] **Step 3: 运行 RED 后实现**

Run: `go test ./internal/resources`

- [ ] **Step 4: CLI 周期采样为 JSON Lines**

参数：`-pid`, `-metrics-url`, `-interval`, `-output`；context cancel 时 flush 并正常退出。

- [ ] **Step 5: 验证**

Run: `go test ./internal/resources && go vet ./internal/resources`

Expected: PASS。

---

### Task 11: k6 共享库与 contract/routing/binding/error 场景

**Files:**
- Create: `ghttp/testdata/k6/lib/config.js`
- Create: `ghttp/testdata/k6/lib/checks.js`
- Create: `ghttp/testdata/k6/lib/metrics.js`
- Create: `ghttp/testdata/k6/lib/data.js`
- Create: `ghttp/testdata/k6/scenarios/contract_smoke.js`
- Create: `ghttp/testdata/k6/scenarios/routing_matrix.js`
- Create: `ghttp/testdata/k6/scenarios/binding_codec.js`
- Create: `ghttp/testdata/k6/scenarios/error_security.js`

**Interfaces:**
- Produces: `baseURL()`, `thresholds(profile)`, `checkJSON`, `checkProblem`, `uniqueNamespace`。
- Produces custom metrics: route trend/rate、schema rate、secret leak counter、auth/negotiation/OpenAPI rate。

- [ ] **Step 1: 先写 contract_smoke 导入尚不存在的共享函数**

Run: `k6 inspect scenarios/contract_smoke.js`

Expected: FAIL，missing module/export。

- [ ] **Step 2: 实现共享库**

所有请求加 tag：`route_family`, `operation`, `expected_status`。`checks.js` 必须检查 status、Content-Type、request ID、schema 与秘密字符串。

- [ ] **Step 3: 实现四个场景**

contract_smoke 遍历全部路由族；其余三个集中覆盖 routing、binding/codec、error/security，避免只复用同一个 happy-path 函数。

- [ ] **Step 4: 解析验证**

Run: `k6 inspect scenarios/contract_smoke.js`，并依次 inspect 其余三个脚本。

- [ ] **Step 5: 真实 smoke**

Run: `k6 run -e PROFILE=smoke scenarios/contract_smoke.js`

Expected: checks 100%，秘密泄漏 0。

---

### Task 12: k6 状态、混合、streaming、slow、spike、soak 场景

**Files:**
- Create: `ghttp/testdata/k6/scenarios/mixed_api.js`
- Create: `ghttp/testdata/k6/scenarios/stateful_crud.js`
- Create: `ghttp/testdata/k6/scenarios/streaming_sse.js`
- Create: `ghttp/testdata/k6/scenarios/streaming_websocket.js`
- Create: `ghttp/testdata/k6/scenarios/slow_clients.js`
- Create: `ghttp/testdata/k6/scenarios/spike_recovery.js`
- Create: `ghttp/testdata/k6/scenarios/resource_soak.js`

**Interfaces:**
- Consumes Task 11 的共享库与 Task 7/8 路由。
- Produces CRUD chain、SSE first-event、WS echo、recovery 与 resource trend 指标。

- [ ] **Step 1: stateful_crud 使用每 VU namespace 实现完整状态链**

每次 iteration 必须检查 201→200→200→200→204→404，并记录 `crud_chain_success`。

- [ ] **Step 2: mixed_api 使用确定权重**

读 55%、list/search 15%、create 10%、update 8%、static/cache 5%、expected error 5%、OpenAPI 2%。

- [ ] **Step 3: streaming 使用独立指标**

SSE 记录首事件时延和事件数；WebSocket 记录 echo trend、丢失 rate、异常关闭 counter。

- [ ] **Step 4: slow/spike/soak 定义资源友好 executor**

默认 VU/arrival rate 由环境变量控制；spike 必须包含恢复阶段；soak 默认 90 秒。

- [ ] **Step 5: 全部 inspect**

Run: 对 7 个脚本执行 `k6 inspect`。

Expected: 全部 exit 0，阈值和 scenario 名正确。

---

### Task 13: Server CLI 与双平台 runner

**Files:**
- Create: `ghttp/testdata/k6/cmd/server/main.go`
- Create: `ghttp/testdata/k6/scripts/run.ps1`
- Create: `ghttp/testdata/k6/scripts/run.sh`
- Create: `ghttp/testdata/k6/scripts/runner_test.go`

**Interfaces:**
- Server flags: `-addr`, `-static-dir`, `-max-body-bytes`, `-secret`。
- Runner modes: `smoke`, `full`, 单一 scenario；输出目录参数 `-OutputDir`/`OUTPUT_DIR`。

- [ ] **Step 1: 写 server flag 和 graceful shutdown 测试**

抽取 `run(ctx, args, stdout, stderr) error`，测试非法 addr、就绪、信号取消和端口占用。

- [ ] **Step 2: 写 runner dry-run 测试**

PowerShell/shell 支持 dry-run，输出将执行的 server/resources/k6/rawprobe 命令，不启动进程。

- [ ] **Step 3: 实现 runner 生命周期**

启动 server→等待 ready→启动 sampler→运行场景→rawprobe→恢复窗口→停止 sampler/server。使用 try/finally 或 trap 保证清理。

- [ ] **Step 4: 区分退出原因**

退出码：2 参数；3 缺 k6；4 server；5 k6 threshold；6 rawprobe；7 recovery/resource。

- [ ] **Step 5: smoke 端到端**

Run: `powershell -File scripts/run.ps1 -Profile smoke`

Expected: 生成 metadata、k6 summary、resource JSONL、server log、rawprobe report；退出 0。

---

### Task 14: 文档、阈值解释与最终验证

**Files:**
- Create: `ghttp/testdata/k6/README.md`
- Create: `ghttp/testdata/k6/README.en.md`
- Modify: `docs/superpowers/specs/2026-08-08-ghttp-k6-targeted-test-design.md`（仅在实现偏差需要同步时）

**Interfaces:**
- Documents: 安装 k6、smoke/full 命令、环境变量、全部指标、退出码、结果解释、限制。

- [ ] **Step 1: 写中英文文档**

中文与英文分文件并互链；明确“针对性测试、覆盖广、包含功能/性能/稳定性/安全/资源指标，不是生产 SLO”。

- [ ] **Step 2: 检查路由与检查数量**

提供 Go helper 或脚本统计已注册测试 operation 与 k6 `check` 名称；目标分别为 70–90 和至少 150，重复名称不重复计数。

- [ ] **Step 3: Go 验证**

Run: `go mod tidy && gofmt -w . && go test -race ./... && go vet ./...`

Expected: PASS。

- [ ] **Step 4: k6 验证**

Run: inspect 全部 11 个 JS；运行 smoke；然后运行 full，目标 8–10 分钟。

Expected: 功能/安全阈值通过，生成所有结果文件；性能指标完整，即使环境阈值失败也必须能分类和保存结果。

- [ ] **Step 5: 仓库范围验证**

Run: `git status --short` 与 `git diff -- .gitignore`

Expected: 正式变更仅两份根 README、设计/计划文档和 `ghttp/testdata/k6/`；`.tmp/` 仍未跟踪，`.gitignore` 无变化。

- [ ] **Step 6: 提交门禁**

不执行 `git add`、`git commit` 或 push。等待维护者明确授权并决定是否将设计、计划和正式测试资产一起提交；横评 `.tmp/` 永不纳入该提交。
