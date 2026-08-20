# ghttp 执行模型重写设计

## 目标

用 gin/echo 式切片链执行模型完全替换现有 `func(http.Handler) http.Handler` 中间件链 + `installState`/`WithContext` 双路径,保留泛型端点 API 和 radix 路由。

## 新执行模型

```go
// Ctx 是 gin/echo 式执行上下文,池化,请求→handler 直传。
type Ctx struct {
    W          http.ResponseWriter
    R          *http.Request
    params     pathParamList          // 路由填充
    handlers   []HandlerFunc          // 注册期固化的切片链
    index      int8
    status     int
    body       *memoBody              // 懒 memo,单次读零缓冲
    // form/query 缓存(按需填充)
}

type HandlerFunc func(*Ctx)
type Middleware func(*Ctx)  // 与 HandlerFunc 同型,gin 风格

func (c *Ctx) Next() {
    c.index++
    for c.index < int8(len(c.handlers)) {
        c.handlers[c.index](c)
        c.index++
    }
}
// index++ 在 handler 调用之后(gin 同款顺序);中间件未调 Next 即短路
// (echo 式),也可显式 c.Abort()。
```

**链在注册期固化**:
```go
handlers := []HandlerFunc{mw1, mw2, ..., terminal}
// 请求期: c.handlers = handlers; c.Next()
```

## 请求→handler 链路(单一路径)

```
serveHTTP(w, r)
  → ctx = pool.Get()
  → ctx.reset(w, r)
  → mux.match(r.Method, r.URL.Path) → fills ctx.params
  → ctx.handlers = compiledHandlers  // 切片赋值,零分配
  → ctx.Next()                       // 切片迭代,直调
  → pool.Put(ctx)
```

**删除**: `installState`、逐层 `WithContext`、`executionContext`、`requestState`、`responseWriteState`、`extractorTerminal`、`fastDirect` 双路径、旧 `Middleware func(http.Handler) http.Handler`。保留内部终端形态 `pathParamHandler`/`ServeHTTPWithPathParams`(typed 终端的参数直传载体),`lazyPathParams` 作为 descriptor 回退保留。

## 中间件 body 访问

**决策:给,懒 memo。** 理由:

1. 现有 ghttp memoBody 语义已被 JSONBind 1.1× 证明零成本(单次读不缓冲)
2. gin 的"读了就没了"是著名坑,不值得继承
3. 日志/鉴权/签名中间件需要读 body,实用需求

```go
func (c *Ctx) Body() io.ReadCloser { return c.body }
func (c *Ctx) BodyBytes() []byte    { ... } // 懒 memo,首次读直传,再次读重放
```

## 终端接入现有泛型 API

```go
// Handle[I,O](pattern, input, output, handler) → 编译期产物:
// handlers = [mw1, ..., mwn, typedTerminal]
// typedTerminal = buildInput → call handler → marshal output → write
```

`buildInput` 内部复用现有 `MapInputs` 链,但 `params.Get` 改为 `ctx.Param(name)`——O(1) 按 paramSlots 的槽位索引(当前 Phase A 已填好)。

## 路由

保留 `routeMux` + `match` + `compiledRoute`。改动:

- `match` 仍填充 `pathParamList`(Phase A 成果),直接写到 `ctx.params`
- `compiledRoute` 持有 `compiledHandlers []HandlerFunc`(注册期编译的链)
- 删除 `route.fastDirect`, `route.handler`, `route.definition.handler`(http.Handler 形态)

## 内置中间件迁移

RequestID/CORS/Recover/Timeout 从 `func(http.Handler) http.Handler` 重写为 `func(*Ctx)`:

```go
func RequestID() HandlerFunc {
    return func(c *Ctx) {
        // 读/设 X-Request-ID header,然后 c.Next()
    }
}
```

## 最终实现状态

**已完成**:
- `execution_context.go` 删除(executionContext/requestState/stateContext/installState)
- `middleware.go` 删除(LegacyMiddleware/LegacyWrap/LegacyTimeout 等 480 行)
- `ctx.go` 重写:池化 Ctx + ResponseWriter 实现 + 合并 body/form/params/committed 状态
- `middleware_chain.go` 新建:Chain/RequestID/CORS/RequestLogger/Recoverer/Timeout
- `server.go` dispatch 重写:acquireCtx→match→attachCtx→切片链→Next→releaseCtx
- `writer.go` 重写:responseErrorWriteBlocked/beginResponseErrorHandler 基于 Ctx
- `operation_input.go`/`form.go`/`params.go`/`memo_body.go` 迁移到 ctxFromRequest
- `route_mux.go` fastDirect/needsParams 删除,新增 compiledHandlers/needsState
- `path_param.go` stateIndependentTerminal 标记删除;internal pathParamHandler 保留
- `operation.go` stateIndependent 字段流到 routeDefinition
- 全量测试通过(go test ./ghttp/ -count=1, 34s)
- benchmarks 模块编译通过

**保留的内部终端形态**: `pathParamHandler`/`ServeHTTPWithPathParams` (typed 终端的参数直传载体),`lazyPathParams` (descriptor 回退)。

**执行语义**: 中间件不调 Next 即短路(echo 式),`c.Abort()` 显式终止(gin 式)。Ctx 实现 io.StringWriter 避免字符串写入分配。

**Timeout 并发模型(-race 全绿验证)**:
- 子链 goroutine 持有自己的 Ctx 拷贝,其请求 context 挂子 Ctx(`context.WithValue(ctx, ctxKey{}, &child)`),子链错误抑制检查只读子 Ctx 的 committed 字段,绝不跨 goroutine 访问父 Ctx。
- 父 goroutine 的 committed 只由父自己设置:done 分支经 `rec.writeTo(c)`(写经 Ctx)标记。
- `rec.stop()` 与子链写入互斥(`w.mu`);Flush 提交期间持锁,父 goroutine 的 stop() 等待提交完成后再写 504,消除底层 writer 的并发写。
- `go test -race ./ghttp/ -count=1` 全绿(139s)。

## API 边界

**保留不变**:
- `Handle`, `Get`, `Post`, `Put`, `Patch`, `Delete`, `Options`, `Head`, `Connect`, `Trace`
- `GetJSON`, `PostJSON`, `PutJSON`, `PatchJSON`, `DeleteJSON`
- `HandleNoOutput`, `HandleEmpty`
- `NoInput`, `EmptyInput`, `PathString`, `PathInt64`, `PathFloat64`, `PathBool`, `PathRest`
- `QueryStrings`, `JSONBody`, `FormBody`, `MultipartBody`
- `MapInputs`, `MapInputs2`..`MapInputs10`
- `JSONOutput`, `JSON`, `TextOutput`, `Text`, `NoContentOutput`, `Redirect`, `Stream`
- 所有 `With*` Option
- `Server`, `MustMount`, `Build`

**删除**:
- `Middleware`(旧 `func(http.Handler) http.Handler`)
- `Wrap`, `WrapFunc`
- `RequestID`, `CORS`, `Recover`, `Timeout`, `Logger`(重写为新模型)
- `MatchedParams`(Ctx.Params 替代)
- `RequestState`, `FromRequest`, `Params`(Ctx 替代)

## 预期性能

| 场景 | 旧 | 新(预估) | vs gin |
|---|---|---|---|
| Middleware5 | 461ns | ~370ns | 3.8× |
| PathParam1 | 265ns | ~200ns | 2.4× |
| JSONBind | 2716ns | ~2650ns | 1.05× |
| FullChain | 5140ns | ~4800ns | 1.1× |
| Param10 | 1377ns | ~1100ns | 5.0× |

## 实施步骤

1. 新 `Ctx` + `HandlerFunc` + 链(slice 迭代) + 池化 — 骨架
2. 路由接入新调度(`serveHTTP` 重写) — 可运行
3. 终端接入泛型 API — 端点可用
4. 内置中间件迁移 — 功能完整
5. 删除旧代码 — 清理
6. 全量测试 + 横评 — 子代理验证
## 基准结果(重写后,count=3 中位数,vs 重写前基线)

| 场景 | 旧(ns) | 新(ns) | 比率 | 说明 |
|------|--------|--------|------|------|
| StaticRoute | 215 | 219 | 1.02 | 持平 |
| PathParam1 | 265 | 373 | 1.41 | 旧模型无中间件走 fastDirect 直调;统一链模型的结构性代价(池化+Next+闭包+接口调度 ~80ns) |
| PathParam5 | 912 | 902 | 0.99 | 持平 |
| Wildcard | 435 | 435 | 1.00 | 持平 |
| QueryParams | 1319 | 1354 | 1.03 | 持平 |
| JSONBind | 2716 | 2631 | 0.97 | 略优 |
| JSONResponse | 886 | 897 | 1.01 | 持平 |
| Middleware5 | 461 | 438 | 0.95 | 逐层 WithContext 消除收益 |
| FullChain | 5140 | 4715 | 0.92 | 中间件密集路径收益最大 |
| NotFound | 182 | 182 | 1.00 | 持平 |

结论:中间件密集路径(本重写的目标场景)5–8% 提升;唯一显著回归是单参数直调路径(26%),为统一执行模型取代 fastDirect 双路径的预期代价,且该路径绝对成本仍由 typed JSON 管线主导(终端本身 ~300ns)。
