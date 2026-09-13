# 封装 net/http 客户端：兼容性问题与已知陷阱调研

> 日期：2026-09-11
> 状态：**调研报告**（为 `2026-09-11-ghttp-client-design.md` 提供证据基线）
> 基准：Go 1.27.1（`GOROOT=/root/.local/share/mise/installs/go/1.27.1`）；本仓库 CI 最低支持 Go 1.25.12，Go 1.27 走 preview job（`.github/workflows/go.yml:21-22,52-58`）
> 证据约定：源码一律标注 `文件:行号`，相对 `$GOROOT/src/`；文档标注 URL。凡未能取得一手证据的条目显式标注「未验证」。

---

## 0. 结论摘要

1. **`http.Client` 只有 4 个可组合字段**（`Transport`/`CheckRedirect`/`Jar`/`Timeout`），它的可组合性边界就是这四个。包装层真正该做的是**换掉 `Transport`**，而不是复制 `Client` 的能力。
2. **`Client.Timeout` 不是"每请求超时"，是"每个 `Do` 调用 + 返回后的 body 读取"的整体预算**，且在 `do()` 入口计算一次，**跨全部重定向共享**。它不会被 context 覆盖，而是与 context deadline 取更早者（`client.go:358-378`）。
3. **未读 body 直接 `Close()` 的语义在 Go 1.27 发生了实质变化**：Transport 现在会自动 drain 至多 256 KiB / 50 ms（`transport.go:2420-2440`）。这既是利好也是新坑——Go 1.27 引入了并发 `Read`+`Close` 死锁（golang/go#81404），且 drain 会**掩盖"忘记读 body"的性能债**。
4. **自定义 `DialContext`/`TLSClientConfig` 会静默关闭 HTTP/2**（`transport.go:489`）。这是 `ForceAttemptHTTP2` 存在的唯一理由，也是包装层最容易踩的隐性降级。
5. **包装层绝对不能碰 `http.DefaultTransport` 和 `http.DefaultClient`**：前者被全进程共享，后者无法被用户替换，两者都是"改一处污染全局"的隐蔽通道。
6. **`*url.Error` 是唯一的错误出口**，4xx/5xx 不是 error。包装层若把非 2xx 转成 error，必须提供一个可关闭的开关，否则会破坏 `errors.As(&urlErr)`、`resp.StatusCode` 判断等标准库契约。

---

## 1. `http.Client` 的可组合性边界

`Client` 结构体共 4 个导出字段（`client.go:59-110`）：

| 字段 | 行号 | 语义 |
|---|---|---|
| `Transport RoundTripper` | `client.go:63` | nil 时用 `DefaultTransport`（`client.go:206-211`） |
| `CheckRedirect func(req *Request, via []*Request) error` | `client.go:79` | nil 时用 `defaultCheckRedirect`，上限 10 跳 |
| `Jar CookieJar` | `client.go:90` | nil 时只发送显式设置的 Cookie |
| `Timeout time.Duration` | `client.go:92-101` | 见下 |

`Timeout` 的官方定义（`client.go:92-101`）：

> The timeout includes connection time, any redirects, and reading the response body. The timer remains running after Get, Head, Post, or Do return and will interrupt reading of the Response.Body.

**这解释了为什么它是"整体超时"**：`do()` 在入口处算出一个绝对 deadline 并全程复用：

```go
// client.go:623
deadline = c.deadline()          // client.go:199-204: time.Now().Add(c.Timeout)
```

该 deadline 沿三处生效：

- **连接/请求阶段**：`send()` 调 `setRequestCancel(req, rt, deadline)`（`client.go:264`）；
- **重定向**：同一个 `deadline` 变量贯穿 `for` 循环（`client.go:651-761`），10 次重定向共享一份预算，**不是每跳重置**；
- **body 读取阶段**：成功后 `resp.Body` 被包成 `cancelTimerBody`（`client.go:302-306`），其 `Read` 在超时后返回 `&timeoutError{"... (Client.Timeout or context cancellation while reading body)"}`（`client.go:997-1008`）。

### 与 context 的交互：不是覆盖，是取更早者

`setRequestCancel` 的逻辑（`client.go:358-378`）是关键：

```go
knownTransport := knownRoundTripperImpl(rt, req)
if req.Cancel == nil && knownTransport {
    // If they already had a Request.Context that's
    // expiring sooner, do nothing:
    if !timeBeforeContextDeadline(deadline, oldCtx) {
        return nop, alwaysFalse
    }
    req.ctx, cancelCtx = context.WithDeadline(oldCtx, deadline)
    ...
}
```

即：**Client.Timeout 只在"比用户 context deadline 更早"时才注入 context**，否则完全不碰 ctx（`timeBeforeContextDeadline` 定义在 `client.go:314-321`）。所以"Timeout 覆盖 context"是错误表述——真实语义是 `min(ctxDeadline, clientDeadline)`。

但有两个真实陷阱：

1. **`knownRoundTripperImpl` 是白名单**（`client.go:324-350`）：只认 `*http.Transport`、内置 h2 `http2RoundTripper`，其余靠 `reflect.TypeOf(rt).String() == "*http2.Transport"` 字符串猜。**包装层自定义的 `RoundTripper` 不在名单里**，于是退化为 `req.Cancel` channel + 旧式 `CancelRequest` 接口 + 一条常驻 goroutine（`client.go:395-425`）。这是包装层"套一层 RoundTripper 就多一个 goroutine"的根因。
2. **`didTimeout()` 的判定基于挂钟** `time.Now().After(deadline)`（`client.go:377`）。若 context 先到期，`didTimeout` 仍可能返回 true，导致错误串里出现 `(Client.Timeout exceeded while awaiting headers)` 而实际是 ctx 取消（`client.go:744-750`）。**包装层不要解析错误字符串判断超时来源**，应分别检查 `ctx.Err()` 与 `errors.Is(err, context.DeadlineExceeded)`。

### 包装层设置 `Client.Timeout` 会破坏什么

- 会**限制流式/SSE/大文件下载**：计时器在 `Do` 返回后仍在跑（`client.go:95-96`），长连接读取会在超时点被强制中断。
- 会**绕过用户的 context 预算**（只在更早时生效，晚期 ctx 无效）。
- 会与用户自带的 `*http.Client` **冲突**：若包装层把用户传入的 Client 复制一份再改 Timeout，用户对原 Client 的后续修改（如换 Jar）不会生效；若不复制而直接改，则是**对用户对象的写竞争**（`Client` 文档未声明并发写安全）。
- 推荐做法：**不设 `Client.Timeout`，只透传 context deadline**；若必须提供 `WithTimeout`，应实现为"per-request `context.WithTimeout`"，而不是写 `Client.Timeout`。**这一设计取舍未在标准库中验证对错，属工程判断。**

---

## 2. `Transport` 与连接池

### `DefaultTransport` 的默认参数（`transport.go:47-58`）

| 参数 | 默认值 | 备注 |
|---|---|---|
| `Proxy` | `ProxyFromEnvironment` | 全局缓存，见 §7 |
| `DialContext` | `net.Dialer{Timeout: 30s, KeepAlive: 30s}` | 经 `defaultTransportDialContext` 包装 |
| `ForceAttemptHTTP2` | `true` | 关键：抵消下面 §2.2 的静默降级 |
| `MaxIdleConns` | `100` | 跨 host 总空闲连接 |
| `MaxIdleConnsPerHost` | **未设** → `DefaultMaxIdleConnsPerHost = 2`（`transport.go:60-62`，取值逻辑 `transport.go:1117-1122`） | 高并发单 host 场景的经典瓶颈 |
| `IdleConnTimeout` | `90s` | |
| `TLSHandshakeTimeout` | `10s` | |
| `ExpectContinueTimeout` | `1s` | |

`MaxConnsPerHost` 默认 0 = 不限（`transport.go:218-223`）。

**调参建议**：`MaxIdleConnsPerHost` 只在"单 host 高并发 + 观察到频繁新建连接"时上调（默认 2 会让 100 并发请求退化为反复握手）；`MaxConnsPerHost` 用于保护上游；`DisableKeepAlives=true` 是 Go 1.27 官方给出的"不想要连接复用"的解法（见 §3）。

### 自定义 Dialer 为什么静默禁用 HTTP/2

`ForceAttemptHTTP2` 的文档（`transport.go:301-306`）：

> By default, use of any those fields conservatively disables HTTP/2. To use a custom dialer or TLS config and still attempt HTTP/2 upgrades, set this to true.

实现见 `protocols()`（`transport.go:479-501`）：

```go
case !t.ForceAttemptHTTP2 && (t.TLSClientConfig != nil || t.Dial != nil || t.DialContext != nil || t.hasCustomTLSDialer()):
    // Be conservative and don't automatically enable http2 ... Issue 14275.
```

**包装层的正确姿势**：继承 `http.DefaultTransport.Clone()` 再改字段（`Clone` 会把 `ForceAttemptHTTP2: true` 一起带过来，`transport.go:365`）；或显式 `Protocols.SetHTTP2(true)`。Go 1.24 起 `Transport.Protocols` / `HTTP2Config` 是更明确的开关（`api/go1.24.txt:174-178`），`TLSNextProto` 设为空 map 仍是"关掉 HTTP/2"的文档化写法（`transport.go:480-485`）。

### `Transport.Clone`

- **Go 1.13 引入**（`api/go1.13.txt:207`，`transport.go:343-385`）。
- 语义：**深拷贝全部导出字段**；`TLSClientConfig` 走 `tls.Config.Clone()`，`HTTP2`/`Protocols` 做值拷贝，`TLSNextProto` 用 `maps.Clone`，`ProxyConnectHeader` 用 `Header.Clone()`。
- **不拷贝未导出状态**（连接池、`nextProtoOnce`），所以 Clone 出来的是"全新连接池、同配置"的 Transport——这正是包装层该用的语义。
- 注意：`Clone` 会先执行 `t.nextProtoOnce.Do(t.onceSetNextProtoDefaults)`（`transport.go:344`），即**会触发源 Transport 的 HTTP/2 初始化**。对一个从未用过的全局 Transport 调用 Clone 是安全但非零成本的。

---

## 3. body 与连接复用

### 契约文本

`Response.Body` 文档（`response.go:56-69`）：

> The http Client and Transport guarantee that Body is always non-nil ... It is the caller's responsibility to close Body. The default HTTP client's Transport may not reuse HTTP/1.x "keep-alive" TCP connections if the Body is not read to completion and closed; however, manually reading the body to completion should not be needed in most cases, as closing the body will also cause the body to be read to completion asynchronously, up to a conservative limit.

`Client.Do` 文档（`client.go:564-572`）同义重述，并新增了 `Transport will automatically try to read a Response Body to EOF asynchronously up to a conservative limit when a Body is closed`。

### 「不读直接 Close」在 Go 1.27 的行为变化 —— **不是 Go 1.25**

Go 1.27 发行说明（<https://go.dev/doc/go1.27>，net/http 节）原文：

> HTTP/1 Response.Body now automatically drains any unread content upon being closed, up to a conservative limit, to allow better connection reuse. For most programs, this change should be a no-op, or result in a performance improvement. In rare cases, programs that do not benefit from connection reuse might experience performance degradation if they had been improperly allowing an excessive amount of idle connections to linger; usually by setting Transport.MaxIdleConns to 0 or using different Clients for different requests, thereby bypassing Transport.MaxIdleConns limit. In these cases, setting Transport.DisableKeepAlives to true will disable connection reuse.

实现：`maxPostCloseReadBytes = 256 << 10`（`transport.go:2420`）、`maxPostCloseReadTime = 50 * time.Millisecond`（`transport.go:2424`）、`maybeDrainBody`（`transport.go:2427-2440`，起一个 goroutine 做 `io.CopyN(io.Discard, body, 256KiB+1)` 并以 50 ms 为上限），接线在 `readLoop`：

```go
// transport.go:2604-2613
case bodyEOF := <-waitForBodyRead:
    tryDrain := !bodyEOF && resp.ContentLength <= maxPostCloseReadBytes
    if tryDrain {
        eofc <- struct{}{}
        bodyEOF = maybeDrainBody(body.body)
    }
    alive = alive && bodyEOF && !pc.sawEOF && pc.wroteRequest() && tryPutIdleConn(rc.treq)
```

> **注意**：该段落**不在 Go 1.25/1.26 的 release notes**（已核对 `https://go.dev/doc/go1.25`、Go 1.26 原文检索无命中）。任务描述中的"Go 1.25+"应更正为 **Go 1.27**。对最低支持 1.25 的本仓库而言，这意味着**同一份代码在 1.25 与 1.27 上的连接复用行为不同**，不能依赖自动 drain 来保证复用。

**新引入的坑**：Go 1.27 的自动 drain 与"并发 `Read` + `Close`"会死锁，见 golang/go#81404（已 closed）与 backport #81411（open）：<https://github.com/golang/go/issues/81404>、<https://github.com/golang/go/issues/81411>。包装层**必须避免在另一个 goroutine 仍读 body 时 Close**。

此外 `bodyEOFSignal` 明确禁止关闭后再读：`errReadOnClosedResBody` / `errConcurrentReadOnResBody`（`transport.go:3223-3224`, `3250-3264`）。

### 截断读不破坏复用的正确写法

`io.ReadAll` 的问题：无上限地把整个响应体读进内存。标准库自身在 `MaxBytesReader` 之外**没有提供客户端侧上限**；`Transport.MaxResponseHeaderBytes` 只管响应头（默认 10 MiB，`transport.go:336-341`），**不管 body**。包装层必须自己加 `MaxResponseBodyBytes`。

保持复用的截断读：

```go
lr := io.LimitReader(resp.Body, maxN)
data, err := io.ReadAll(lr)
// 关键：把剩余部分读到 EOF 并丢弃，否则连接不会回到 idle 池
_, _ = io.Copy(io.Discard, resp.Body)
_ = resp.Body.Close()
```

在 Go 1.27 之前，第二段 `io.Copy(io.Discard, ...)` 是**必需**的；在 Go 1.27 上它被自动 drain 部分取代，但自动 drain 上限只有 256 KiB 且受 50 ms 限制，**超过上限的 body 仍会断连**。因此保留显式 drain 是跨版本正确解。`io.Copy` 到 EOF 会读到 `bodyEOFSignal` 的 `io.EOF`，从而走 `fn` 而非 `earlyCloseFn`，这是 `tryPutIdleConn` 成功的前提（`transport.go:2563-2575`）。

### `http.NoBody`

`http.go:189-202`：`NoBody` 是一个 `io.ReadCloser`，`Read` 恒返回 `io.EOF`，`Close` 恒返回 nil，用于**显式表达"请求体为零字节"**。它同时实现 `io.WriterTo`（零分配）。`NewRequestWithContext` 在 `ContentLength == 0 && GetBody != nil` 时把 `Body` 换成 `NoBody` 并让 `GetBody` 也返回 `NoBody`（`request.go:971-974`）。区分 `nil` 与 `NoBody` 很重要：`r.Body == nil` 才表示"完全没有 body"（`request.go:1580-1582`）。

### 请求体的可重放性（`GetBody`）

`GetBody` 契约（`request.go:191-197`）：

> GetBody defines an optional func to return a new copy of Body. It is used for client requests when a redirect requires reading the body more than once. Use of GetBody still requires setting Body. For server requests, it is unused.

`NewRequestWithContext` **只为三种类型**自动设置 `ContentLength` 与 `GetBody`（`request.go:933-962`）：`*bytes.Buffer`、`*bytes.Reader`、`*strings.Reader`；其他类型走 `default:` 分支**什么都不设**（`request.go:956-961`，历史原因见 golang/go#18117）。Go 1.8 引入该机制，见 <https://go.dev/doc/go1.8>（"NewRequest sets Request.GetBody automatically for common body types"）。

**无法重放**：`*os.File`、`io.PipeReader`、`net.Conn`、任何自定义 `io.Reader`（除非自己包一层并设置 `GetBody`）。后果：

- **307/308 重定向**：`redirectBehavior` 在 `ireq.GetBody == nil && ireq.outgoingLength() != 0` 时把 `shouldRedirect` 置 false（`client.go:530-539`），**静默不跟随**并把 3xx 响应原样返回，而不是报错。
- **连接级重试**：`shouldRetryRequest` 对 `nothingWrittenError` 要求 `req.outgoingLength() == 0 || req.GetBody != nil`（`transport.go:868-872`）；`rewindBody` 在 `GetBody == nil` 时返回 `errCannotRewind`（`transport.go:785, 829-831`）。`isReplayable()`（`request.go:1562-1577`）除 `GetBody != nil` 外，还接受 `Idempotency-Key` / `X-Idempotency-Key` 头。

**包装层的硬性要求**：任何"把 `io.Reader` 变成本包请求体"的 API，都必须在能拿到长度时（`[]byte`/`string`/`io.Seeker`/已知大小）自动生成 `GetBody`；否则必须在文档里声明"该 body 不可重放，禁重定向/禁重试"。

---

## 4. 重定向

### 默认策略与 `CheckRedirect` 语义

- 默认 `defaultCheckRedirect`：`if len(via) >= 10 { return errors.New("stopped after 10 redirects") }`（`client.go:845-850`），10 跳上限。
- 触发条件：`CheckRedirect` 在**即将发起新请求前**被调用，`via` 是已发起的请求列表，**最旧在前**（`client.go:65-79`）。
- 跟随的状态码：301/302/303/307/308。`redirectBehavior`（`client.go:514-541`）规定 301/302/303 → 非 GET/HEAD 一律降级为 GET 且不带 body；307/308 → 保留方法与 body（需 `GetBody`）。
- 注意 `redirectMethod` 在 301/302/303 分支里是"先赋原方法、再按需改 GET"，因此**HEAD 会被保留**（`client.go:519-527`）。

### `ErrUseLastResponse` 与"报错停止"的区别

`ErrUseLastResponse` 定义在 `client.go:496-500`，特殊分支在 `client.go:713-718`：

```go
if err == ErrUseLastResponse {
    return resp, nil          // body 未关闭，err 为 nil
}
```

对比 `CheckRedirect` 返回任意其它 error（`client.go:731-738`）：

```go
ue := uerr(err)
ue.(*url.Error).URL = loc
return resp, ue               // resp 非 nil + err 非 nil，且 resp.Body 已被关闭
```

**这正是"resp 与 err 同时非 nil"的唯一情形**，`Client.Do` 文档也如此声明（`client.go:578-580`）。

- **禁止重定向且要读到状态码** → 返回 `http.ErrUseLastResponse`（用户负责 Close body）。
- **禁止重定向且要当错误处理** → 返回自定义 error（resp 非 nil 但 **Body 已关闭**，包装层**不得再 Close/Read**，否则可能触发 `errReadOnClosedResBody` 或与 Go 1.27 并发 drain 冲突）。
- 若 `CheckRedirect` 返回 `ErrUseLastResponse` 之外的 error 且包装层又把它转成自己的 error，**必须先把 `resp.Body.Close()` 的责任厘清**——标准库此时已经关过了。

### 跨域时敏感头的转发规则（Go 1.8+）

标准库在 `client.go:698-703` 判断是否跨 host，然后交给 `shouldCopyHeaderOnRedirect`：

```go
// client.go:1017-1043
ihost, err1 := httpguts.PunycodeHostPort(initial.Hostname())
dhost, err2 := httpguts.PunycodeHostPort(dest.Hostname())
...
return ok1 && ok2 && isDomainOrSubdomain(dhost, ihost)
```

`isDomainOrSubdomain`（`client.go:1045-1066`）：相等即通过；否则要求 `sub` 以 `"." + parent` 结尾。**比较的是 `Hostname()`，即忽略端口**，且用 punycode 规范化（Go 1.8 起支持 IDN，见 <https://go.dev/doc/go1.8>）。注意是**单向**判定：仅当子域名 → 父域名时保留，反向（父→子）会剥离（已验证 `sub.foo.com → foo.com` 剥离）。

被剥离的敏感头（`client.go:824-827`，当前 Go 1.27.1）：

```
"Authorization", "Www-Authenticate", "Cookie", "Cookie2",
"Proxy-Authorization", "Proxy-Authenticate"
```

> **版本时间线**：`Authorization`/`WWW-Authenticate`/`Cookie`/`Cookie2` 自 Go 1.8 起被剥离（<https://go.dev/doc/go1.8>）；`Proxy-Authorization`/`Proxy-Authenticate` 因 CVE-2025-4673 在 Go 1.23.10 / 1.24.4 才加入（issue #79792, #79890, commit `4d1c255f159d`）。因此若项目需支持 Go <1.23.10（本仓库 min=1.25 不受影响），代理认证头在跨域重定向时**不会**被自动剥离。

body 相关头在"POST→GET"时被剥离（`client.go:829-835`）：`Content-Encoding`、`Content-Language`、`Content-Location`、`Content-Type`（对齐 WHATWG Fetch）。该功能在 **Go 1.26 引入**（commit `aced4c79a2b2`）。

`Client.Do` 文档自曝的宽松点（`client.go:594-599`）：

> Note that the Client redirect behavior does not follow the WHATWG Fetch standard. ... by modern standards, Client has a rather permissive behavior. For example, sensitive headers are retained on redirect to a subdomain or to a different scheme on the same host.

**包装层结论**：跨域重定向（含 `http→https` 同 host、`foo.com→sub.foo.com`）**默认会带上 Authorization/Cookie**。安全默认应当是**比标准库更严**：只要 scheme/host/port 任一变化就剥离敏感头，并提供显式开关恢复标准库行为。`CookieJar` 走的是各自 cookie 的 domain 作用域（`client.go:1024-1029` 注释），不受这套规则影响。

---

## 5. 超时分层

| 层 | 字段 | 覆盖范围 | 缺失后果 |
|---|---|---|---|
| DNS+TCP | `net.Dialer.Timeout`（`net/dial.go:127-139`）/ `Dialer.Deadline`（`net/dial.go:141-145`） | 单次拨号（多 IP 时被分摊；OS 另有约 3 min 的 TCP 超时） | 默认无超时，依赖 OS |
| TCP keep-alive | `net.Dialer.KeepAlive`（`net/dial.go:175-185`） | 空闲连接探活 | 默认 15s；负值禁用 |
| Happy Eyeballs | `net.Dialer.FallbackDelay`（`net/dial.go:162-173`） | 首个地址族失败后的回退 | 默认 300ms；负值禁用 |
| TLS | `Transport.TLSHandshakeTimeout`（`transport.go:188-190`） | 握手 | 0 = 无超时（默认 Transport 为 10s，自定义 Transport 若不设就是**真无限**，实现见 `transport.go:1794`） |
| 响应头 | `Transport.ResponseHeaderTimeout`（`transport.go:231-235`） | 请求体写完后等待响应头 | 0 = 无超时；**不含读 body 的时间** |
| 100-continue | `Transport.ExpectContinueTimeout`（`transport.go:237-244`） | 等待服务端对 `Expect: 100-continue` 的响应 | 0 = 立即发 body |
| 空闲连接 | `Transport.IdleConnTimeout`（`transport.go:225-229`） | idle 池中的连接存活 | 0 = 无限 |
| 整请求 | `Client.Timeout`（`client.go:92-101`） | 连接 + 全部重定向 + **读 body**；`Do` 返回后仍计时 | 0 = 无超时 |
| 用户预算 | `context` deadline | 由调用方掌控，粒度最细 | 无 |

**为什么"响应头超时"必须与"整体超时"分开配**：

- `ResponseHeaderTimeout` 的错误是固定的 `errTimeout = &timeoutError{"net/http: timeout awaiting response headers"}`（`transport.go:2935`，使用点 `transport.go:3103-3104`），且它的 `Is` 方法把 `context.DeadlineExceeded` 也算进去（`transport.go:2926-2934`）。它**只管到响应头为止**，因此对"慢但在推进"的流式响应友好：可以设 5s 拿头 + 无上限读流。
- `Client.Timeout` 是**硬预算**，会切断正在下载的 body（`client.go:302-306`, `1006`）。用 `Client.Timeout` 做"防服务端不响应"的保护，会误杀大文件下载和 SSE。
- 正确组合：**`ResponseHeaderTimeout` 防僵死，`context` 承载业务预算，`Client.Timeout` 尽量别用**。同时保留一个粗粒度的 `Dialer.Timeout` 兜底。
- 补充陷阱：HTTPS 走代理时的 **CONNECT 阶段有一个硬编码的 1 分钟超时**（`transport.go:1995` `connectCtx, cancel := testHookProxyConnectTimeout(ctx, 1*time.Minute)`），该值不可配置。

---

## 6. `Dialer` 与自定义拨号

### 签名与优先级

```go
// transport.go:145
DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
```

`Transport.dial` 的优先级（`transport.go:1372-1388`）：`DialContext` → `Dial`（Deprecated）→ `zeroDialer.DialContext`（包级零值 `net.Dialer`，`transport.go:1370`）。两者都返回 `(nil, nil)` 会被显式报错。**为什么必须用 `DialContext` 而不是 `Dial`**：`Dial` 无 ctx，Transport 无法在请求取消时中止拨号，只能泄漏到拨号自然结束（`transport.go:153-155` 的 Deprecated 说明）。

注意 `DialTLSContext`（`transport.go:159-172`）一旦设置，**HTTPS 请求就不再走 `DialContext`，且 `TLSClientConfig`/`TLSHandshakeTimeout` 被忽略**；返回的 conn 必须已完成握手，且要实现 `ConnectionState()` 才能参与 ALPN。包装层如只想换 dialer，**不要**顺手设置 `DialTLSContext`。

### `net.Dialer` 各字段用途

- `Resolver *Resolver`（`net/dial.go:196-197`）：替换 DNS 解析器。**Go 1.8 引入**（`api/go1.8.txt:223`）。
- `Control func(network, address string, c syscall.RawConn) error`（`net/dial.go:206-216`）：建连后、拨号前调用，用于 `SO_BINDTODEVICE`、`SO_MARK`、设置 socket 选项、`TCP_FASTOPEN`。传给它的 network 会被规范化为 `tcp4`/`tcp6`（文档明确说明）。**Go 1.11 引入**（`api/go1.11.txt:445`）。
- `ControlContext`（`net/dial.go:218-228`）：**Go 1.20 引入**（`api/go1.20.txt:270`，发行说明 <https://go.dev/doc/go1.20>）。非 nil 时 `Control` 被忽略。
- `KeepAlive`/`KeepAliveConfig`/`FallbackDelay`：见 §5。
- `Deadline time.Time`（`net/dial.go:141-145`）：**早在 Go 1.0 就存在**（`api/go1.txt:2038`），**不是 Go 1.20+ 新增**——任务描述此处需更正。Go 1.20 新增的是 `ControlContext`。

### 如何既注入自定义 dialer 又保留 HTTP/2 与代理

1. **从 `http.DefaultTransport.Clone()` 出发**，只覆盖 `DialContext`，保留 `ForceAttemptHTTP2: true`（`transport.go:365`）与 `Proxy: ProxyFromEnvironment`。
2. 或新建 `&http.Transport{DialContext: ..., ForceAttemptHTTP2: true, Proxy: http.ProxyFromEnvironment}`；Go 1.24+ 可用 `Protocols` 显式声明。
3. **`Proxy` 与 `DialContext` 的关系**：`DialContext` **拿到的是代理地址，不是目标地址**。`connectMethodForRequest`（`transport.go:1051-1060`）按 `cm.targetScheme` 选定代理并算出 `cm.targetAddr`，而 `dialConn` 调的是 `t.dial(ctx, "tcp", cm.addr())`（`transport.go:1920`），`cm.addr()` 在 `proxyURL != nil` 时返回 `canonicalAddr(cm.proxyURL)`（`transport.go:2208-2213`）。**因此自定义 dialer 不需要、也不应该自己处理代理**——代理是 Transport 层的事。
4. **`ProxyFromEnvironment` 何时被调用**：**每个请求一次**，在 `connectMethodForRequest` 内（`transport.go:1054-1056`），但**环境变量只读一次**（见 §7）。返回非 nil error 会直接**中止请求**（`transport.go:118-120`）。
5. socks5 代理是在握手之后由 Transport 自己在已拨通的 conn 上做 `DialWithConn`（`transport.go:1938-1958`），同样对 `DialContext` 透明。

---

## 7. 代理

### `ProxyFromEnvironment` 的缓存与陷阱

缓存实现（`transport.go:1032-1049`）：

```go
envProxyOnce      sync.Once
envProxyFuncValue func(*url.URL) (*url.URL, error)

func envProxyFunc() func(*url.URL) (*url.URL, error) {
    envProxyOnce.Do(func() {
        envProxyFuncValue = httpproxy.FromEnvironment().ProxyFunc()
    })
    return envProxyFuncValue
}
```

`httpproxy.FromEnvironment()` 在**第一次调用时**快照 `HTTP_PROXY`/`http_proxy`、`HTTPS_PROXY`/`https_proxy`、`NO_PROXY`/`no_proxy`（`src/vendor/golang.org/x/net/http/httpproxy/proxy.go:90-97`），并且 `ProxyFunc()` 会把 `Config` 预处理成 matcher 列表（`proxy.go:116-124` 注释："Changing the contents of cfg will not affect proxy functions created earlier"）。

**结论：环境变量只读一次，且是包级 `sync.Once`，与 Transport 实例无关。** 因此：

- 运行期改 `os.Setenv("HTTPS_PROXY", ...)` 后**新建 Transport 也无效**——因为 `sync.Once` 在包级，不是实例级。（推翻"改环境变量后新建 Transport 就行"的常见说法。）
- 唯一可靠的解法是**显式设置 `Transport.Proxy`** 为自定义函数（如 `http.ProxyURL(u)`，`transport.go:1050-1055`），或使用 `httpproxy.Config{...}.ProxyFunc()` 自建。
- **`pkg.go.dev` 上 `ProxyFromEnvironment` 的文档没有提缓存**（已核对 <https://pkg.go.dev/net/http#ProxyFromEnvironment>），这是纯粹的"源码级陷阱"。

其它要点：

- **CGI 安全开关**：`Config.CGI` 由 `os.Getenv("REQUEST_METHOD") != ""` 推断（`proxy.go:95`）；CGI 环境下若 `HTTP_PROXY` 非空，`proxyForURL` 直接返回错误 `refusing to use HTTP_PROXY value in CGI environment; see golang.org/s/cgihttpproxy`（`proxy.go:133-135`）。
- **localhost 特例**：`ProxyFromEnvironment` 文档只说 `req.URL.Host` 为 `"localhost"` 时返回 nil（`transport.go:1036-1048` 上方注释），但实际实现（`proxy.go:178-186`）**把任何 loopback IP 也视为直连**。文档比实现窄。
- **`NO_PROXY` 支持**：`*`、CIDR、`IP:port`、`[IPv6]:port`、域名后缀（`proxy.go:215-260`）。
- **代理认证**：URL 里带 userinfo 时，Transport 自动生成 `Proxy-Authorization: Basic ...`（`connectMethod.proxyAuth`，`transport.go:1059-1070`）。HTTP 目标走 `mutateHeaderFunc`（`transport.go:1959-1964`）；HTTPS 目标把该头塞进 CONNECT 请求（`transport.go:1967-1990`）。
- **CONNECT 隧道**：`cm.targetScheme == "https"` 分支手写 `CONNECT` 请求、用 `bufio.Reader` 读响应，并加了 1 分钟硬超时（`transport.go:1965-2040`）。可定制项：`ProxyConnectHeader`（静态）与 `GetProxyConnectHeader(ctx, proxyURL, target)`（动态，非 nil 时忽略前者）、`OnProxyConnectResponse`（Go 1.20+，在 200 判定之前调用，返回 error 即失败）。
- **`http.ProxyURL(u)`**：返回恒等于 `u` 的代理函数（`transport.go:1050-1055`），不做任何环境变量或 localhost 判断——**单元测试里固定代理的标准做法**。

---

## 8. 取消与 goroutine 泄漏

- **`client.Do` 返回时 body 的所有权归调用方**：`Client.Do` 文档"the returned Response will contain a non-nil Body which the user is expected to close"（`client.go:564-567`）。**唯一的例外是 `CheckRedirect` 返回非 `ErrUseLastResponse` 错误**，此时 body 已被标准库关闭（`client.go:578-580`, `731-738`）。
- **context 取消后的 body**：`Client.Timeout`/ctx 触发时，`cancelTimerBody` 会在下一次 `Read` 返回 `timeoutError`（`client.go:997-1008`）。此时 `Close()` 仍然必须调用；`Close` 会 `stop()` 停掉定时器（`client.go:1010-1014`）。**忘记 Close 会泄漏定时器 goroutine**。
- **`readLoop` 的退出**是连接级 goroutine 的关键：`select` 同时等 `waitForBodyRead`、`rc.treq.ctx.Done()` 和 `pc.closech`（`transport.go:2595-2617`）；ctx 取消会 `pc.cancelRequest(context.Cause(rc.treq.ctx))`。因此**只要 ctx 有取消路径，读 body 阻塞的 goroutine 是可以被解开的**。
- **`bodyEOFSignal` 的三态**（`transport.go:3204-3264`）：读到 `io.EOF` → `fn(nil)`；提前 `Close` → `earlyCloseFn()`；已 Close 后再 `Read` → `errReadOnClosedResBody`；并发 `Read` → `errConcurrentReadOnResBody`。
- **常见泄漏模式**：
  1. **忘记 `Close`** — 连接永远回不到 idle 池，`MaxIdleConns` 名存实亡（`transport.go:70-75` 明确要求 Transports 复用而非按需创建）。
  2. **忘记 `cancel`** — `context.WithTimeout` 的 `cancel` 未调用会泄漏 ctx 的定时器（这是 `go vet` 的 lostcancel 检查项，非 net/http 特有）。
  3. **body 读到一半阻塞** — 服务端不发数据时不设 `ResponseHeaderTimeout` 会永久挂起；读到一半则需要 ctx/`Client.Timeout` 兜底。
  4. **每次请求新建 Transport** — 每个 Transport 有自己的 idle 池，无法复用，且 `CloseIdleConnections` 无处可调（`transport.go:72-75`）。
  5. **包装层自定义 RoundTripper 触发旧式取消路径** — 见 §1 的 `knownRoundTripperImpl` 白名单，会额外起一条 goroutine（`client.go:395-425`）。
  6. **Go 1.27 自动 drain + 并发 Read/Close** — golang/go#81404 / #81411。
- 相关文档缺失问题：golang/go#60240 "add clear documentation on when to drain & close a Response"（open）、golang/go#51907（open）。

---

## 9. 错误模型

### `*url.Error`

`net/url/url.go:33-54`：

```go
type Error struct {
    Op  string
    URL string
    Err error
}
func (e *Error) Unwrap() error  { return e.Err }
func (e *Error) Error() string  { return fmt.Sprintf("%s %q: %s", e.Op, e.URL, e.Err) }
func (e *Error) Timeout() bool  { t, ok := e.Err.(interface{ Timeout() bool }); return ok && t.Timeout() }
func (e *Error) Temporary() bool
```

- **`errors.As(err, &urlErr)` 可取出 `Op`/`URL`/`Err`**；`Op` 由 `urlErrorOp(method)` 生成（`client.go:543-552`，把方法首字母大写，空方法当 `Get`）。
- **`errors.Is(err, context.DeadlineExceeded)`** 是可靠的超时判定：`timeoutError.Is` 显式返回 `err == context.DeadlineExceeded`（`transport.go:2933`）。
- **`net.Error`**（`net/net.go:425-433`）只有 `Timeout()` 与已废弃的 `Temporary()`。要取底层 `*net.OpError`，用 `errors.As(err, &opErr)`，因为 `url.Error.Unwrap` → `timeoutError`/`*net.OpError` 链路是标准 `Unwrap`。
- **`errors.Is(err, context.Canceled)`**：`net` 包的 `canceledError.Is` 把"operation was canceled"也映射到 `context.Canceled`（`net/net.go:448-454`），所以取消判定同样可靠。
- 其它哨兵：`http.ErrUseLastResponse`（`client.go:500`）、`http.ErrSchemeMismatch`（`client.go:213-214`，在 `send` 里由 `tls.RecordHeaderError` 嗅探转换，`client.go:271-279`）、`http.ErrSkipAltProtocol`（`transport.go:890-891`）。

### 4xx/5xx 不是 error

`Client.Do` 文档（`client.go:561-562`）：

> An error is returned if caused by client policy (such as CheckRedirect), or failure to speak HTTP ... A non-2xx status code doesn't cause an error.

**设计后果**：调用方必须**双重判断**（`err != nil` 且 `resp.StatusCode`）。包装层把非 2xx 转成 error 是常见产品化选择，但会破坏：`errors.As` 取 `*url.Error` 的直觉、`resp.Body` 的关闭责任归属（error 路径上 body 归谁？）、以及"想读 4xx 响应体做错误详情"的用法。

**推荐**：默认保持标准库语义，把状态码判断下沉到 `Response` 对象（`resp.IsSuccess()` / `resp.StatusCode()`）；如果提供"非 2xx 即 error"，则该 error **必须携带 `*http.Response` 并接管 Body 的关闭**，且要允许用户关闭该行为。

### 同时非 nil 的唯一情形

`client.go:578-580`：

> On error, any Response can be ignored. A non-nil Response with a non-nil error only occurs when CheckRedirect fails, and even then the returned Response.Body is already closed.

**如何区分"传输错误"与"HTTP 状态错误"**：传输错误是 `err != nil`（且 `resp == nil`，或 CheckRedirect 场景 `resp != nil` 但 body 已关）；HTTP 状态错误是 `err == nil && resp != nil && resp.StatusCode >= 400`。**没有第三种**。包装层不要用 `resp == nil` 单独做判断——CheckRedirect 场景下 `resp != nil && err != nil`。

补充：`send()` 会校验自定义 RoundTripper 的返回（`client.go:286-301`）：`(nil, nil)` 报错；`ContentLength > 0` 但 `Body == nil` 报错；`Body == nil` 且长度合法时补 `io.NopCloser(strings.NewReader(""))`。`RoundTripper` 的文档则明确"error types returned by RoundTrip are unspecified"（`roundtrip.go:29-31`）——**包装层自定义 RoundTripper 时，返回的 error 不会被自动包成 `*url.Error` 之外的形态，但会经 `uerr` 包装（`client.go:634-650`），`Op` 取首个请求的方法。**

---

## 10. 测试与注入

- **`httptest.Server`**：`NewServer`（`httptest/server.go:266`）/`NewTLSServer`（`httptest/server.go:458`）起真实 loopback 服务，`Server.Client()` 返回配好 TLS 信任与重定向的 `*http.Client`。Go 1.27 新增 `NewTestServer(tb, handler)`（`httptest/server.go:177`，`api/go1.27.txt:284`，基于内存假网络 + `testing/synctest`），是新代码的首选。`NewRecorder()`（`httptest/recorder.go:51`）只适合测 server handler，不能测 client。
- **自定义 `RoundTripper` 做 mock**：实现 `RoundTrip(*http.Request) (*http.Response, error)` 即可拦截一切。必须遵守的契约：返回 `*Response` 的 `Body` 非 nil（否则依赖 `client.go:290-301` 的兜底补空 body）、`StatusCode` 与 `Status` 一致、`Header` 非 nil、`Request` 字段应指回请求。注意 **mock 的 Transport 不在 `knownRoundTripperImpl` 白名单**，会导致 `Client.Timeout` 走旧式取消路径（§1），因此**测试 mock 时不要依赖 `Client.Timeout` 行为与真实 Transport 一致**。
- **为什么必须暴露 RoundTripper 注入点**：
  1. 用户已有自己的 `*http.Transport`（自定义 dialer、代理、mTLS、连接池参数），包装层若不能接收就只能让用户放弃这些配置；
  2. 生态中间件（OpenTelemetry、`httptrace`、重试库、录制回放如 `go-vcr`、熔断器）全部以 `http.RoundTripper` 为接口；
  3. 测试需要无网络 mock；
  4. `http.RoundTripper` 是 Go 生态里**唯一稳定、无争议的传输层抽象**（`roundtrip.go:5-20` 甚至用 `go:linkname` 把 `Transport.RoundTrip` 钉死以防生态破坏，见 golang/go#67401）。
- **包装层建议的注入面**：`WithTransport(http.RoundTripper)`、`WithHTTPClient(*http.Client)`、`WithDialer(*net.Dialer)` / `WithDialContext(func(ctx, network, addr) (net.Conn, error))`、`WithCookieJar(http.CookieJar)`、`WithCheckRedirect(func(*http.Request, []*http.Request) error)`，外加 `RoundTrip(*http.Request) (*http.Response, error)` 让本包对象能被塞回标准库。

---

## 11. 包装层设计检查清单

**必须做**

1. **必须允许注入 `http.RoundTripper`**，并把"换 Transport"作为唯一的传输层扩展点。
2. **必须允许注入完整 `*http.Client`**（用户自带 Jar/CheckRedirect/Transport），且不得在内部复制后静默丢弃用户后续修改。
3. **必须允许注入 `*net.Dialer` / `DialContext`**，并保证注入后 HTTP/2 与代理仍然可用（继承 `ForceAttemptHTTP2: true`）。
4. **必须允许注入 `http.CookieJar`** 并透传到 `Client.Jar`。
5. **必须默认给每个请求挂 `context`**，并让 `ctx` 是唯一的一等取消/超时手段。
6. **必须实现响应体上限（`MaxResponseBodyBytes`）**：`ReadAll` 前先 `io.LimitReader`，超限时返回可判定的错误。
7. **必须在截断读之后把剩余 body drain 到 EOF**（`io.Copy(io.Discard, resp.Body)`）再 `Close`，以保证 Go 1.25/1.26 上的连接复用。
8. **必须保证每条返回路径上 `resp.Body` 都被 `Close`**（包括成功、4xx/5xx、解码失败、用户回调 panic 的场景，用 `defer`）。
9. **必须在能确定长度时自动生成 `Request.GetBody`**（`[]byte`/`string`/`io.Seeker`/已知大小），否则显式禁用该请求的重定向与重试。
10. **必须把 `CheckRedirect` 暴露给用户**，并在包装层默认策略里记录 `via` 链供诊断。
11. **必须默认提供比标准库更严的跨域敏感头策略**（scheme/host/port 任一变化即剥离 `Authorization`/`Cookie`/`Proxy-Authorization`），并允许用户恢复标准库行为。
12. **必须用 `errors.Is/As` 判定超时**（`context.DeadlineExceeded`、`net.Error.Timeout()`、`*url.Error`），不得解析错误字符串。
13. **必须保留 `*url.Error` 的可展开性**：包装错误要 `Unwrap()` 到原始 error，必要时 `errors.Join`。
14. **必须在文档中声明"非 2xx 不是 error"的默认语义**；若提供 `TreatNon2xxAsError`，该开关必须可关闭，且返回的 error 要携带 `*http.Response` 并接管 Body 所有权。
15. **必须为流式/SSE/下载提供"无整体超时"路径**（只配 `ResponseHeaderTimeout` + `ctx`）。
16. **必须实现 `io.Closer` / `CloseIdleConnections()`**，让包装层能释放持有的连接池。
17. **必须能降级为 `*http.Request` / `*http.Response`**，把本包类型交回标准库（`req.HTTPRequest()` / `resp.HTTPResponse()`）。

**必须不做**

18. **不得修改 `http.DefaultTransport` 或 `http.DefaultClient`**（全进程共享，且 `DefaultTransport` 是 `RoundTripper` 接口变量，改写它会让所有依赖方静默换实现）。
19. **不得把 `Client.Timeout` 当作"每请求超时"**，也不得在用户已设 `Client.Timeout` 时覆盖它；包装层的 `WithTimeout` 应落到 per-request context。
20. **不得吞掉 `resp.Body` 未关闭的状态**（如包装 `Response`，则包装类型必须自己 `Close`；如不包装，必须保证调用方拿到可关闭的 body）。
21. **不得在传输错误后仍返回可用的 `resp`**（除 `CheckRedirect` 场景外，`resp` 必须为 nil）。
22. **不得对不可重放的 body 静默重试**（`GetBody == nil` 且 `Body != nil` 时禁止重试；307/308 不允许静默降级为不跟随）。
23. **不得在自定义 `RoundTripper` 里返回 `(nil, nil)` 或 `Body == nil` 且 `ContentLength > 0`** 的响应。
24. **不得依赖 `ProxyFromEnvironment` 的环境变量可变性**（包级 `sync.Once` 只读一次），要给用户显式的 `WithProxy(func(*http.Request) (*url.URL, error))`。
25. **不得设置 `Transport.DisableCompression = true` 后仍假设 `Content-Length` 可用**（透明解压会把 `ContentLength` 置 -1、删除 `Content-Encoding`，`transport.go:2586-2592`）。

---

## 附录：证据索引

**源码**（`$GOROOT/src/`，Go 1.27.1）

- `net/http/client.go:59-110`（Client 定义）、`:176-215`（send/deadline/transport）、`:215-313`（包级 send、cancelTimerBody 装配）、`:314-321`（timeBeforeContextDeadline）、`:324-350`（knownRoundTripperImpl）、`:358-425`（setRequestCancel）、`:496-500`（ErrUseLastResponse）、`:504-541`（checkRedirect/redirectBehavior）、`:543-552`（urlErrorOp）、`:562-602`（Do 文档）、`:609-761`（do 主循环）、`:700-703`（跨 host 判定）、`:713-718`（ErrUseLastResponse 分支）、`:725-727`（2KB slurp）、`:731-738`（resp+err 同时非 nil）、`:749`（headers 阶段 timeout 包装）、`:772-843`（makeHeadersCopier）、`:824-835`（敏感头/body 头列表）、`:845-850`（defaultCheckRedirect）、`:991-1014`（cancelTimerBody）、`:1017-1066`（shouldCopyHeaderOnRedirect/isDomainOrSubdomain）
- `net/http/transport.go:47-62`（DefaultTransport 默认值）、`:116-273`（Proxy/Dial*/TLSClientConfig/ProxyConnectHeader 文档）、`:301-306`（ForceAttemptHTTP2）、`:343-385`（Clone）、`:431-433`（hasCustomTLSDialer）、`:479-501`（protocols 判定 + Issue 14275）、`:1032-1049`（envProxyOnce）、`:1051-1070`（connectMethodForRequest/proxyAuth）、`:1117-1122`（maxIdleConnsPerHost）、`:1370-1388`（dial 优先级）、`:1794`（TLSHandshakeTimeout 应用）、`:1920`（dial 到 proxy 地址）、`:1936-2040`（socks5/CONNECT/代理认证/1min 超时）、`:2208-2213`（cm.addr）、`:2417-2440`（maxPostCloseReadBytes/maxPostCloseReadTime/maybeDrainBody）、`:2555-2617`（readLoop body 装配与 drain）、`:2586-2592`（透明 gzip 解压改写 ContentLength）、`:785-839`（errCannotRewind/rewindBody）、`:841-888`（shouldRetryRequest）、`:2926-2935`（timeoutError/errTimeout）、`:3204-3264`（bodyEOFSignal）
- `net/http/request.go:191-197`（GetBody 契约）、`:890-975`（NewRequestWithContext 自动设置）、`:1562-1587`（isReplayable/outgoingLength）
- `net/http/client.go:118-140`（**RoundTripper 接口契约**：不得解释响应、必须关闭请求 Body）、`net/http/response.go:51-76`（Body 文档）、`net/http/http.go:189-202`（NoBody）、`net/http/roundtrip.go:5-31`（Transport.RoundTrip 的 linkname 锁）、`net/http/httptest/server.go:177,266,458`、`net/http/httptest/recorder.go:51`
- `net/url/url.go:33-54`（url.Error）、`net/net.go:425-433`（net.Error）、`:448-454`（canceledError.Is）、`net/dial.go:126-234`（Dialer 字段）
- `vendor/golang.org/x/net/http/httpproxy/proxy.go:29-97,105-200,207-260`
- `api/go1.13.txt:207`、`api/go1.20.txt:270`、`api/go1.8.txt:223`、`api/go1.11.txt:445`、`api/go1.txt:2038`、`api/go1.24.txt:155-178`、`api/go1.27.txt:284`

**文档与 issue**

- Go 1.27 发行说明（body 自动 drain）：<https://go.dev/doc/go1.27>
- Go 1.25 发行说明（已核对，**无** body drain 条目）：<https://go.dev/doc/go1.25>
- Go 1.8 发行说明（GetBody/重定向头复制）：<https://go.dev/doc/go1.8>
- Go 1.20 发行说明（Dialer.ControlContext）：<https://go.dev/doc/go1.20>
- golang/go#81404 / #81411（Go 1.27 自动 drain 并发 Read+Close 死锁）：<https://github.com/golang/go/issues/81404>、<https://github.com/golang/go/issues/81411>
- golang/go#60240（drain/close 文档缺失）：<https://github.com/golang/go/issues/60240>
- golang/go#51907（`Do` 返回后 body 仍可被读）：<https://github.com/golang/go/issues/51907>
- golang/go#18117（ContentLength 默认 0 的历史包袱，见源码注释）：<https://github.com/golang/go/issues/18117>
- golang/go#67401（`Transport.RoundTrip` linkname 锁定）：<https://github.com/golang/go/issues/67401>
- CGI 代理安全说明：<https://golang.org/s/cgihttpproxy>
- `pkg.go.dev/net/http`（ProxyFromEnvironment **未提缓存**）：<https://pkg.go.dev/net/http#ProxyFromEnvironment>

**明确标注为「未验证」的条目**

- `Client.Timeout` 与用户自定义 `RoundTripper` 组合时的实际行为差异（源码 `knownRoundTripperImpl` 白名单已确证，但"包装层 RoundTripper 下 timeout 语义是否等价"未做运行时实验）。
- "大文件下载被 `Client.Timeout` 误杀"的具体表现（源码 `cancelTimerBody` 已确证会在读 body 时抛 timeoutError，但未构造端到端复现）。
- Go 1.25/1.26 上"未读 body 直接 Close"是否**总是**导致连接不复用（源码显示 `earlyCloseFn` 会 `waitForBodyRead <- false` 使 `alive = false`，逻辑上会关闭连接，但未逐版本对比验证）。
