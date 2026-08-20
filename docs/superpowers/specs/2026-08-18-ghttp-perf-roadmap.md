# ghttp 性能收敛路线图（StructInput 删除后）

> 状态：第 1、2、4、5、7 步已完成（2026-08-18）。本文档记录 ghttp 服务端在
> 删除 StructInput 老 API 之后的性能收敛计划。基准环境：AMD Ryzen 7 8845HS、
> Go 1.26、标准库 JSON。

## 0. 已完成：删除 StructInput 老 API

删除 `StructInput[T]`、`Body[T]`、`ParseInput`、`WithBodyDecoder` 及全部结构体
tag 解析机器。这是每请求开销的最大单笔来源（旧路径 GET 2015ns/9alloc、
POST 5444ns/32alloc）。删除后 ghttp 只剩显式描述器路径，基线即
`Operation*` 系列基准。

删除后基线（同机实测，`go test -bench=BenchmarkOperation*Reference -count=1`）：

| 场景 | ghttp | web（对照） | 差距 |
|---|---|---|---|
| StaticText | 121.1 ns | 68.5 ns | 1.8× |
| StaticJSON | 256.4 ns | 218.3 ns | 1.2× |
| ParamJSON | 309.0 ns | 218.3 ns | 1.4× |
| NotFound | 122.0 ns | 52.5 ns | 2.3× |
| FiveMW | 437.1 ns | 68.5 ns | 6.4× |

> **FiveMW 说明**：正确性修复（中间件强制状态注入路径）使 FiveMW 从
> 146.9ns 升至 437.1ns。中间件链优化（第 7 步）可将其降至 250ns 以下。

## 0.1 已完成步骤后的实测（count=3）

| 场景 | 删除后基线 | 第 1+2+4+5+7 步后 | 收益 |
|---|---|---|---|
| StaticText | 121.1 ns | ~106 ns | −13% |
| StaticJSON | 256.4 ns | ~230 ns | −10% |
| ParamJSON | 309.0 ns | ~265 ns | −14% |
| NotFound | 122.0 ns | **~84 ns** | **−31%** |
| FiveMW | 437.1 ns | ~333 ns/376B/4alloc | −24% |

与 gin v1.10.0（sonic JSON）外部对比（`/tmp/ghttp-gin-bench`，count=1）：

| 场景 | 优化前 ghttp | 优化后 ghttp | gin | ghttp/gin |
|---|---|---|---|---|
| StaticText | 129.9 ns | 124.7 ns | 78.0 ns | 1.6× |
| StaticJSON | 232.2 ns | 235.2 ns | 635.6 ns | **0.37×（更快）** |
| 200 路由命中 | ~135 ns | 130.9 ns | 95.2 ns | 1.4× |
| 1000 路由命中 | ~135 ns | 133.0 ns | 91.8 ns | 1.4× |
| **404（200 路由）** | **~126 ns** | **92.7 ns** | 43.3 ns | **2.1×（原 3.1×）** |
| **404（1000 路由）** | **~126 ns** | **90.3 ns** | 42.9 ns | **2.1×（原 3.1×）** |
| 单路径参数 | 156 ns | 129.7 ns | 82.2 ns | 1.6× |
| **三参数深路由** | **832.5 ns/704B** | **560.8 ns/64B** | 134.4 ns | 4.2×（原 6.4×） |
| POST JSON body | 2017 ns | 1843 ns | 1414 ns | 1.3× |
| 方法分发 | ~91 ns | 78.7 ns | 28.2 ns | 2.8× |
| 5 层中间件 | 361.6 ns | 341.0 ns | 92.4 ns | 3.7× |

## 1. 逐步修改计划

按"收益/成本比"排序。每步独立可验证，均有对应基准。

### 第 1 步：冻结 ServeHTTP 外层 ✅ 已完成（−15~18ns/请求，全场景）

**修改**：
1. `Server.ServeHTTP` 现在通过 `compiled` 原子指针读取冻结后的
   `compiledState.serveHTTP`（`http.HandlerFunc`），首次请求前 lazy 构建。
2. `hostValidator == nil` → 直接分发闭包；有 host 校验 → 带 defer/recover
   的校验闭包。两者在冻结期选定，不在热路径分支。
3. 消除了 `Server.serveHTTP` 字段的 data race（改为 `compiled` 原子指针
   发布）。

**代价**：原子指针加载 + 函数调用 ≈15-18ns（FiveMW 331→349ns 冷冻前；
NotFound 117→121ns）。接受：race 安全优先。

**验证**：`go test -race ./ghttp/` 全绿；`BenchmarkOperation*Reference` 全场景
下降 2-11%。

### 第 2 步：Codec 注册期解析 ✅ 已完成（错误路径 −75ns/1alloc）

**修改**：
1. `Server` 新增 `resolvedCodecs []responseCodec` 与 `resolvedJSONCodec Codec`
   字段，在 `finalizeRoutes` 冻结期一次性解析。
2. `writeErrorWithCodec` 的 JSON 回退路径优先使用 `resolvedJSONCodec`。
3. `selectResponseCodec` 默认 produces 走 `resolvedCodecs`，跳过每请求
   `Resolve`（含 `mime.ParseMediaType`）。

**验证**：`BenchmarkRequestBody` 1943→1843ns（−5%，错误路径 Accept 协商
减少）；`go test -race ./ghttp/` 全绿。

### 第 3 步：Validator 无 tag 快路径（预期 −130ns，仅启用服务器 Validator 时）

**现状**：`validateOperationInput` 每请求调用 `validator.Validate`，即使输入
结构没有任何 validate tag 也要付出 ~130ns（`BenchmarkProbe_ValidatorNoTags`）。

**修改**：
1. 注册期为每个 Operation 计算 `needsServerValidation`：输入元数据是否携带
   validate 约束（`ValidatedInput` 或带约束描述器）或 `InputFunc` 显式校验。
2. 无约束 → 冻结期直接编译掉 `validateOperationInput` 调用（与现有
   `skipValidation` 字段合并成注册期判定）。

**验证**：新增基准 `BenchmarkOperation*WithValidator`；无 tag 时与
无 Validator 配置的差距应 <5%。

### 第 4 步：404/405 失败路径收敛 ✅ 已完成主体（404 −30%，纯静态路由表）

**修改**：
1. `routeMux` 新增 `hasDynamic` 标志与 `staticPathAllows map[string]string`
   （注册期预计算：静态路径 → 排序后的方法列表，含 GET→HEAD 派生）。
2. `compiledState.ServeHTTP` 纯静态路由表快路径：静态索引未命中且路径无尾
   斜杠时，直接判定 404/405，跳过参数索引与 radix 遍历；判定前先
   `validateRawPath`（空段/dot 段/非法转义仍 400）。
3. 带尾斜杠的路径回退 radix 遍历，保留 strict=false 的尾斜杠容忍语义。

**结果**：`BenchmarkOperationNotFoundReference` 122→**84ns**（−31%）；
外部 `404-200` 130.5→92.7ns、`404-1000` 132→90.3ns（与 gin 差距 3.1×→2.1×）。

**验证**：`go test -race ./ghttp/` 全绿（含 Allow 头断言）。

### 第 5 步：多参数/深层路径解码惰性化 ✅ 已完成主体（deep-param −270ns、−640B/1alloc）

**修改**：
1. `maxStackPathParams` 16→4（`pathParamList` 值尺寸 640B→160B），
   `pathSegmentList` 拆出独立的 `maxStackPathSegments=16` 常量，避免
   5 段路径因槽位缩减产生堆分配。
2. 新增 `stateDirectInputBuilder[T]`：stateIndependent 输入（Path*/Query*/
   Header*/Cookie* 描述器、MapInputs2-7、ValidatedInput）提供
   `buildStateDirect(request, params)` 免堆直编路径，直接按值接收
   request 与 params，**不构造 operationRequest**，消除其按值携带参数列表
   导致的堆逃逸（原 deep-param 704B/4alloc 的 76.85% 主因）。
3. `buildOperationInputValue` 在编译期优先走 stateDirect，无 stateFn 的输入
   （JSONBody/FormBody 等）回退通用 operationRequest 路径。
4. 新增 `lazyPathParams.getOnce`：无缓存惰性解码（中间件路径缺失回退用），
   避免对栈上 params 取址导致逃逸。

**结果**：`BenchmarkRouteParamDeep/ghttp` 832.5ns/704B/4alloc →
**560.8ns/64B/3alloc**（与 gin 差距 6.4×→4.2×）。剩余 3 alloc 为响应串、
Content-Type 头与 writer 输出，均为基准写出开销；剩余 CPU 在 routeMux.match
（20.7%）与 extractMatched 解码（14.8%），属第 4/6 步的收敛范围。

**验证**：`go test -race ./ghttp/` 全绿；`BenchmarkRouteParamDeep` 实测如上。

### 第 6 步：ParamJSON 长尾追平（预期 −80ns，至 ≈250ns）

**现状**：`BenchmarkOperationParamJSONReference` = 326ns vs web 218ns。扣除
JSON 序列化（~170ns）后，ghttp 参数路径固定成本 ~150ns vs web ~50ns。

**修改**：
1. `directPathValueHandlerFunc` 路径去掉中间闭包与 `pathParamList` 空构造
   （`ServeHTTP` 转调 `ServeHTTPWithPathParams` 的空列表路径）。
2. `writeDirectJSON` 的 header 写入合并为一次 `Header().Set`（现为
   `contentType` + `Content-Length` 两次），并复用静态 header 切片
   （web 的 frozen header 思路）。
3. profile 驱动：`go test -bench=BenchmarkOperationParamJSON -cpuprofile`，
   按火焰图逐项削平。

**验证**：`BenchmarkOperationParamJSONReference` 目标 ≤250ns/2alloc。

### 第 7 步：中间件链与状态注入收敛 ✅ 已完成主体（FiveMW −87ns，−24B）

**修改**：
1. `installState` 的两次 context.WithValue 合并为单结构体
   `stateContext{context.Context; state *requestState}`（自实现 `Value`），
   `requestStateFromRequest` 用类型断言快路径读取。
2. `executionContext` 内联 `responseWriteState`（值嵌入为首字段），
   `acquireExecutionContext` 单次池获取，去掉第二个池与两次 release 检查。
3. `writer.go` 的 responseWriteStatePool 移除；bench 适配器改用手动构造。

**结果**：`BenchmarkOperationFiveMiddlewareReference` 437.1ns/400B/4alloc →
**~350ns/376B/4alloc**；外部对比 361.6→341.0ns（gin 92.4ns，3.7×）。
剩余 4 alloc 分析：`Request.WithContext` 浅拷贝（84.91%，net/http 语义必要）、
`&stateContext{}` 逃逸（6.46%）、响应串、Content-Type 头。第 2 点
（中间件链编译期闭包数组）与第 3 点（响应注入按需）留作后续。

**验证**：`go test -race ./ghttp/` 全绿。

### 第 8 步：可插拔 JSON 引擎（可选，预期 JSON 场景 −30~40%）

**现状**：ghttp 与 web 均用标准库 `encoding/json`。web 提供 `UseJSONCodec`
注入点；ghttp 的 `CodecManager.Register` 可注入但热路径仍绕 codecMgr。

**修改**：为 `JSONOutput`/`JSONBody` 增加 `WithJSONCodec`（或 Server 级
`WithJSONMarshaler`），仅影响 JSON 场景；默认保持标准库，不新增强制依赖。

**验证**：注入 sonic/goccy 后 `BenchmarkOperationStaticJSONReference` 对比。

## 2. 目标基线

| 场景 | 删除后基线 | 已达成 | 最终目标 |
|---|---|---|---|
| StaticText | 121.1 ns | ~108 ns | ≤90 ns |
| StaticJSON | 256.4 ns | ~240 ns | ≤220 ns |
| ParamJSON | 309.0 ns | ~275 ns | ≤250 ns |
| NotFound | 122.0 ns | ~120 ns | ≤70 ns |
| FiveMW | 437.1 ns | ~350 ns | ≤250 ns |

## 3. 依赖与顺序约束

- ✅ 第 1 步（ServeHTTP 冻结）先行：同时降低所有场景基线。
- ✅ 第 2 步（Codec 预解析）与第 1 步互不依赖，已并行完成。
- 第 3 步（Validator 快路径）不依赖第 2 步，可独立进行。
- 第 4 步（404/405 收敛）依赖第 1 步的 outcome 直写。
- ✅ 第 5 步（多参数惰性解码）主体完成，剩余 extractMatched 解码收敛
  并入第 6 步。
- 第 6 步（ParamJSON 长尾）依赖第 5 步的提取路径，可合并进行。
- ✅ 第 7 步（中间件收敛）主体完成，中间件闭包数组与按需响应注入留作优化。
- 第 8 步（JSON 引擎）可选，低优先级。

## 4. 未决事项

- 第 3 步（Validator 无 tag 快路径）：仅在启用 `WithValidator` 时生效，
  当前所有基准未启用 Validator，无实际收益；留作后续。
- 第 4 步（404/405）剩余：含动态段的路由表（hasDynamic=true）仍走 radix
  遍历，404 约 172ns（gin 44ns）；可为首段前缀建索引覆盖动态表。
- 第 6 步（ParamJSON）：distill auto body 路径与 directPath 路径的提取
  差异，统一为单次遍历提取。
- 中间件链闭包数组（第 7 步剩余）：`http.Handler` 适配装箱每层 16B/1alloc。
- `legacyrouter` 与 `GHTTP_ROUTER_IMPL` 基准适配器仅用于路由算法对照，保留。
- `.worktrees/` 下三个陈旧 worktree 仍含老 API 代码，属历史分支快照，不影响
  主树构建，未处理。

## 5. 规模化横评发现（2026-08-18，benchmarks/ 模块，14 框架）

横评过程发现并修复两个大请求体缺陷：1. **memoBody.bytes() 扩容越界 panic**：三下标重切 `[:len(buf):next]` 假设
   append 倍增容量,但 Go 对 cap≥256 的切片按 ~1.25× 增长,next 超过实际
   容量即 panic。任何单次读满 512B 缓冲的请求体(如 ≥1KB 的完整 JSON body)
   都会 500。已改为 `append` 后取 `min(next, cap(grown))`,并最终改为
   io.ReadAll 同款标准倍增循环。
2. **大请求体 O(n²) 拷贝**：扩容上限错挂在 32KB、之后每轮 +4096 线性增长,
   100KB body 实测 3.1MB 分配(gin 423KB)。改标准倍增后 800KB,时间
   1240µs→776µs(gin 675µs)。

规模化增长核查(用户重点):ghttp 各维度均呈**亚线性/线性**增长,无非线性
膨胀:

| 维度 | 数据(ghttp vs gin v1.12) | 结论 |
|---|---|---|
| 路由数 | 200→257ns、1000→2141ns、5000→2298ns;与 gin 0.96×/1.02× | 无膨胀,大表持平 |
| 方法数 | 7 方法 2132ns(1.12×)、1400 路由 2115ns(1.08×) | 无膨胀 |
| 路径参数 | 1→265、5→1036、10→1819、20→3655ns(gin 86/152/226/1428) | 亚线性(1→5 参数 +3.9×,+15 参数仅 +2.0×) |
| 中间件层 | 5→467、10→515、20→574、50→880ns | 亚线性(固定注入 ~400ns,每层仅 +5-8ns) |
| 查询参数 | 3→1501ns(3.3×)、10→3567ns(1.01×) | 与 gin 持平,线性 |
| 请求体 | 100KB→752µs(gin 684µs,1.10×) | 修复后与 gin 同量级 |
| 纯路由 | static 78/32、param1 81/38、param5 298/63、miss 160/44ns | radix 常数因子 2-5×,线性 |

`DecodeAt`/`getOnce` 已加无 % 转义快路径(性能无显著变化,stdlib 内部已有
同样扫描,仅为零拷贝语义明确化)。

剩余与 gin v1.12 的主要差距(固定因子,非增长问题):
- PathParam5 6.8×、Param10 7.7×:extractMatched 独立解码 + 组合子链
  (match 43% + extract 22%);可注册期按 paramPos 直索引消解。
- Middleware5 4.6×:状态注入固定成本(installState ~400ns,其中
  Request.WithContext 拷贝 ~40%)。
- NotFound 3.9×:动态路由表仍走完整 radix 遍历,可扩展第 4 步快路径。

### JSON 公平性审计

基准模块 14 框架中,ghttp/gin/echo/web 四个主要对比目标均使用 **stdlib
encoding/json**,无 SIMD 库(sonic/jsoniter)加速。唯一例外是 hertz 使用
sonic,但其 JSONBind 4881ns 仍比 ghttp 2863ns 慢,不影响结论。gin v1.12
通过构建标签(`!sonic`)默认使用 stdlib;echo 使用 DefaultJSONSerializer;
web 代码注释明确"仅依赖标准库 encoding/json"。

## 6. Phase A: match 融合参数提取(2026-08-18 完成)

### 问题
请求路径被遍历三遍:match(radix 遍历 20 段)→extractMatched(独立二次遍历
pattern 20 段 + DecodeAt/RawAt 每参数)→MapInputs 组合子链(10 个 stateFn
串行,每个 params.Get 线性扫描)。三遍合计约 50% 的 Param10 时间。

### 实现
1. **compiledRoute.paramSlots**:注册期构建参数槽位数组(仅参数/catchAll 段,
   按 pattern 段序),替代 extractMatched 的全量 20 段遍历。
2. **fillMatchedParams**:match 命中时一次性遍历 paramSlots,按段偏移直取
   原始字节(无 % 零拷贝),catchAll 段拼接;超 4 参数时预分配 overflow 切片
   避免 append 逐次扩容。
3. **extractMatched 删除**:fastDirect 路径用 result.params 直接传递;状态
   路径用 executionContext.matchedParams 携带,提取终结器防御性回退改为
   fillMatchedParams 按槽位重建。
4. **回退的经验**:O(1) lookup map 实验性尝试已回退(建表+GC 压力 > 线性
   扫描,有据可查)。

### 前后对比(count=3 基准,300ms benchtime,核心场景)

| 场景 | 重构前 | 重构后 | 增益 | 对 gin 比值 |
|---|---|---|---|---|
| Param10 | 1819ns/848B/10a | 1475ns/560B/7a | -19%(-3a) | 8.0×→6.0× |
| Param20 | 3655ns/2256B/14a | 3069ns/1776B/10a | -16%(-4a) | 2.6×→2.1× |
| PathParam5 | 1036ns/208B/4a | 1002ns/208B/4a | -3% | 6.8×→6.5× |
| StaticRoute | 211ns | 220ns | 持平 | — |
| 其余场景 | — | — | 无退化 | — |

### 剩余瓶颈(profile 分析)
- **match radix 遍历(36.5%)**:纯路由常数因子,gin 每段 ~5ns,ghttp ~15ns
- **MapInputs 组合子链(13.3%)**:10 层嵌套闭包调用,需编译期 slot 索引展开
- **PathString stateFn(8.6%)**:per-param 调用链 + params.Get 线性扫描
- Middleware5 的 installState 固定成本(~400ns,其中 WithContext 拷贝 11%)

## 7. Phase B-lite: 组合子链直调(2026-08-18 完成)

### 实现
MapInputs2-10 的 stateFn 链原来通过 stateDirectInputBuilder 接口调用每层
buildStateDirect(虚调用,无法内联)。改为注册期用 directStateFnOf 提取
固化的函数值,链内直调,消除每层 dispatch。

### 结果(count=3)
| 场景 | Phase A 后 | Phase B 后 | 累计 vs 基线 |
|---|---|---|---|
| Param10 | 1475ns | 1438ns | 1819→1438(-21%) |
| Param20 | 3069ns | 2983ns | 3655→2983(-18%) |
| PathParam5 | 1002ns | 936ns | 1036→936(-10%) |

## 8. 质量把关结论(子代理 2026-08-18)

- 全量单测 33.7s/38.9s 通过;-race 90.9s/102.9s 通过,零 DATA RACE;
  build/vet/gofmt 全绿
- 10 框架 × 11 场景横评:核心收益兑现(Param10/Param20),其余场景
  ±2~10%(count=1 噪声量级),无结构性退化
- 六维度规模化核查零膨胀:路由 200→5000 仅 +1.03×(1000→5000)、
  中间件 5→100 层 +3.1×、方法 7→1400 零增长、参数 10→20 比值收窄、
  查询 3→10 与 gin 打平、请求体 1KB→100KB 纯线性
- 基准 harness 既有缺陷修复:BenchmarkMiddleware100/gin 因 gin 每路由
  63 handler 硬上限 panic("too many handlers"),已改为 b.Skip
- 详细报告: /root/test/ghttp_qa_report.md

### 未完成
- 阶段 B 完整体(编译期参数直取):设计见 /tmp/phaseB_design.md,需
  mappedInputs 暴露子输入 + compileOperation 解析槽位;留作后续
- 阶段 C(中间件 installState):WithContext 拷贝是 http.Handler 中间件
  合约固有成本,绕过需 writer 断言快路径,收益 ~15ns,不追
- 阶段 D(404 快路径):172ns 中 80% 是 httptest.Recorder.Header.Clone
  基准工件,writeCachedOutcome 已跳过 Set 规范化,真实收益有限
- 阶段 E(radix 常数因子):per-segment 比较优化
