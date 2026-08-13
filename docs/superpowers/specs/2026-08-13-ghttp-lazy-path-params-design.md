# ghttp Lazy Path Params 设计(方案 B 原型)

## 背景与目标

现状:请求进入 `ServeHTTP` 时,`parseRequestPath` 对每个路径段立即 `url.PathUnescape`
(每段一次字符串分配),`route.extract` 物化全部 `pathParamList`,再通过第二次
`context.WithValue + Request.WithContext` 存入 context。实测参数路由 17 次分配、静态
路由 6 次;即使 handler 不读取任何路径参数也全额支付。

目标:路径参数改为“访问时才解码”;去掉参数路由的第二次 context 装箱;安全校验保持急切。
不改变任何公开 API 与外部可观察行为。

## 设计

1. **校验急切、解码惰性**:`parseRequestPath` 不再逐段解码,而是在单次扫描中完成
   `%` 转义合法性校验与 dot 段(含 `%2e` 等编码形态)判定,并记录每段的字节偏移
   (定长数组,零分配)。静态段匹配用“原始段 vs 解码后模式值”的逐步解码比较器,
   保持与今天完全一致的相等语义(大小写不敏感的十六进制转义等价)。
2. **按 key 惰性解码**:路由冻结时在 `compiledRoute` 上预计算 `param名 -> 段索引`。
   `lazyPathParams.Get(key)` 首次访问时按索引取原始子串并 `PathUnescape`,结果缓存进
   `pathParamList`;catch-all 按“原始子串连接后整体解码”,与现状“逐段解码再连接”等价
   (用 `%2F`、孤立 `%` 用例锁死)。
3. **去掉第二次 WithContext**:`requestState` 改为在 context 中存指针,新增 `matched`
   字段;匹配成功后把 `lazyPathParams` 挂到 `requestState`(每请求单 goroutine,无需锁)。
   `Server.MatchedParams`、`paramsFromRequestWithPathParams`、`Params.Path` 统一从
   `requestState` 取惰性源;`Params` 值复制语义靠共享 `*paramsState` 维持;`Detach()` 显式物化。
4. **不变量**:非法转义/dot 段/空段/严格尾斜杠仍返回 400;`pathParamHandler` 接口与
   `PathInt/PathBool/...` 等派生方法不变;`extractorTerminal` 的防御性回退改为现场重建
   `lazyPathParams`,不再依赖 context 中的参数。

## 验证

- `go test ./ghttp/...` 全量通过(含 `server_routing_fuzz_test.go` 模糊测试)。
- 新增 `route_path`/`path_param` 单元测试:比较器等价性、按需解码与缓存、catch-all 与
  `%2F`/`%2f` 语义、`Detach` 物化。
- 前后基准:`BenchmarkRouteComparison`、`BenchmarkProbe_*`,以及 `/tmp/gk-cmp`
  七框架同条件横评(把 `replace` 指向本 worktree)。

## 范围外

- 不改 query/cookie/clientIP 的既有惰性粒度;不做 body 的透明多读缓冲。
- 不提交、不推送;仅在 worktree 内验证。

## 原型实测(2026-08-13,AMD 8845HS,同一 harness 前后对照)

全量 `go test ./ghttp/...`(含 fuzz 种子、legacyrouter)与 `go test -race ./ghttp/` 通过;
新增惰性语义单测通过。
同机同 harness(master vs worktree,`/tmp/gk-cmp`):

| 场景 | master | worktree | 变化 |
| --- | --- | --- | --- |
| 静态路由 typed | 2186ns/3041B/16alloc | 1671ns/2121B/13alloc | -24% ns,-920B,-3 alloc |
| 参数路由(读) typed | 1893ns/2625B/11alloc | 1736ns/2282B/9alloc | -8% ns,-343B,-2 alloc |
| 参数路由(**不读**) typed | 1271ns/1472B/9alloc | 622ns/440B/**5alloc** | **-51% ns**,-1032B,-4 alloc |
| 参数路由(**不读**) raw | 1132ns/1496B/10alloc | 549ns/464B/**6alloc** | **-51% ns**,-1032B,-4 alloc |
| JSON 绑定 raw | 3292ns/2433B/25alloc | 2444ns/1400B/21alloc | -26% ns,-1033B,-4 alloc |
| 静态路由 raw 底座 | 395ns/456B/6alloc | 390ns/464B/6alloc | 持平(+8B 偏移字段) |
| 404 | 1230ns/688B/12alloc | 1265ns/696B/12alloc | 持平(+8B) |

迭代二新增三项:① `needsExtractor` 仅在模式含参数时为真(无参数类型化路由不再建惰性源);
② 结构体无 Params 嵌入且无绑定 tag 时跳过 Params 视图;③ `lazyPathParams` 走 `sync.Pool`
(生命周期仍受“Params 仅 handler 内有效”契约约束,跨请求保留必须 `Detach`,已同步修正既有测试)。

结论:去掉第二次 WithContext 让所有类型化路由 -2~3 分配;“参数路由但不读参数”现已降到与
静态路由相同的分配水平(6/5 alloc),延迟 -51%;读参数场景 -7~8%;静态 raw 底座与 404 持平。
