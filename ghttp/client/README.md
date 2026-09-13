# ghttp/client

[English](README.en.md) | 中文

`ghttp` 的 HTTP 客户端：建立在标准库 `net/http` 之上的**编排层**，与 `ghttp` server 共用同一套语义与扩展模型。

它**不实现传输层**——只做中间件、编解码、错误映射、重试与可观测性，传输始终交给 `http.RoundTripper`。

```go
c := client.New(
    client.WithBaseURL("https://api.example.com"),
    client.WithTimeout(5*time.Second),
    client.WithRetry(client.RetryPolicy{MaxRetries: 3}),
)

var user User
resp, err := c.R().SetQueryParam("id", 1).SetResult(&user).Get("/users")
```

---

## 设计取向

| 取向 | 说明 |
|---|---|
| **非 2xx 默认算错误** | 返回 `*Error`，其中带得走整个 `*Response`（body 仍可读）。用 `WithAllowAllStatus()` 可退回"状态码自己判断"的生态惯例 |
| **响应体默认限长** | 默认 32 MiB，超出返回 `ErrBodyTooLarge`；`WithUnlimitedResponseBody()` 或流式模式可解除 |
| **不可重放的 body 直接拒绝** | 开启重试但 body 无法重放时，在**发出第一次请求之前**返回 `ErrBodyNotReplayable`，绝不静默重发空 body |
| **对服务端响应格式零假设** | 没有隐式 envelope；结构化错误体经 `WithErrorDecoder` 显式接入 |
| **标准库是第一公民** | 可注入 `*http.Client` / `http.RoundTripper` / `*net.Dialer` / `DialContext` / `http.CookieJar` / `CheckRedirect`，也能把本 Client 退化成 `http.RoundTripper` |

---

## 快速开始

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/sofiworker/gk/ghttp/client"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    c := client.New(
        client.WithBaseURL("https://api.example.com"),
        client.WithHeader("Accept", "application/json"),
    )

    // 方式一：fluent + SetResult（目标类型运行时才知道时用它）
    var user User
    resp, err := c.R().SetQueryParam("id", 1).SetResult(&user).Get("/users")
    if err != nil {
        var ce *client.Error
        if errors.As(err, &ce) {
            fmt.Println("status:", ce.StatusCode, "attempts:", ce.Attempts)
        }
        return
    }
    fmt.Println(resp.StatusCode(), user)

    // 方式二：泛型 sink（Go 1.18+，零方括号）
    var user2 User
    if _, err := client.GetInto(context.Background(), c, "/users", &user2,
        client.WithRequestQuery("id", 1)); err != nil {
        return
    }
}
```

---

## 与标准库互操作

```go
// 注入既有 *http.Client：完全接管传输、Jar、重定向与超时
c := client.NewWithHTTPClient(myHTTPClient, client.WithBaseURL(base))

// 只换传输层
c = client.New(client.WithTransport(myRoundTripper))

// 自定义拨号（绑定本地地址、走自研网络栈）
c = client.New(client.WithDialer(&net.Dialer{LocalAddr: localAddr}))
c = client.New(client.WithDialContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
    return myDialer.DialContext(ctx, network, addr)
}))

// 把本 Client 当 RoundTripper 用（注意：不含中间件/错误映射，见其 godoc）
external := &http.Client{Transport: c.RoundTripper()}

// 本包请求 → 标准库请求
httpReq, err := c.R().SetMethod(http.MethodGet).SetURL("/x").HTTPRequest(ctx)

// 标准库请求 → 本包编排
resp, err := c.DoHTTP(ctx, httpReq)
```

**红线**：本包不修改 `http.DefaultTransport`（默认用它的克隆），不默认设置 `http.Client.Timeout`（它会连读 body 一起计时），不在注入自定义 dialer/TLS 时静默禁用 HTTP/2。

---

## 错误处理

非 2xx 与传输失败统一收敛为 `*Error`：

```go
resp, err := c.R().Get("/users/404")
var ce *client.Error
if errors.As(err, &ce) {
    ce.StatusCode      // 0 表示传输失败（没收到响应）
    ce.Attempts        // 总尝试次数
    ce.Response        // 非 nil 时 body 仍可读
    errors.Is(err, client.ErrUnexpectedStatus) // 是否为"状态码不可接受"
}
```

结构化错误体：

```go
type APIError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

var apiErr APIError
// 方式一：SetError 注册解码目标
c.R().SetError(&apiErr).Get("/x")

// 方式二：全局错误解码器（适合统一错误格式）
c = client.New(client.WithErrorDecoder(func(resp *client.Response, e *client.Error) error {
    return resp.JSON(&apiErr)
}))
```

与 `gerr` 互操作：`(*Error).Kind()` 返回 `gerr.Kind`，`AsGerr(err)` 转入 gerr 世界。

---

## 中间件与 Hook

中间件是洋葱链，与 `ghttp.Server.Use` 同形：

```go
c.Use(func(next client.Handler) client.Handler {
    return func(ctx context.Context, req *client.Request) (*client.Response, error) {
        req.SetHeader("X-Request-ID", newID())      // 前处理
        resp, err := next(ctx, req)
        logLatency(err)                              // 后处理
        return resp, err
    }
})
```

不调用 `next` 即短路（返回缓存响应、mock、本地校验失败）。

Hook 是只读观察者：`OnBeforeRequest` / `OnAfterResponse` / `OnSuccess` / `OnError` / `OnRetry` / `OnPanic`。
`OnPanic` 只观察，默认**继续向上 panic**（不吞掉），与 server 的 `Recovery`"显式选择才恢复"一致。

---

## 重试

```go
c := client.New(
    client.WithBaseURL(base),
    client.WithRetry(client.RetryPolicy{
        MaxRetries:        3,
        RetryDelay:        100 * time.Millisecond,
        RespectRetryAfter: true,          // 尊重服务端 Retry-After（秒数或 HTTP-date）
        OnRetry: func(attempt int, d time.Duration, resp *client.Response, err error) {
            log.Printf("retry %d in %s", attempt, d)
        },
    }),
)
```

默认只重试**幂等方法**（GET/HEAD/PUT/DELETE/OPTIONS/TRACE）+ 传输错误或 `{408,429,500,502,503,504}`。POST/PATCH 需要显式 `RetryNonIdempotent: true`——否则超时后的重试会造成重复副作用。

退避算法复用基础契约层 `gretry`；判定条件留在本包（HTTP 专属）。

请求体可重放性：

| body 来源 | 可否重试 |
|---|---|
| `SetJSON` / `SetForm` / `SetBody`（内存载体） | ✅ 每次尝试重新编码 |
| `SetBodyReader` + `io.Seeker` | ✅ 重试前 `Seek(0,0)` |
| `SetBodyReader` + 普通 `io.Reader` | ❌ 发送前返回 `ErrBodyNotReplayable` |
| multipart 文件路径（`SetFile`） | ✅ 每次尝试重新打开 |
| multipart 非 seekable reader | ❌ 发送前拒绝 |

---

## 上传、下载与流式

```go
// multipart 上传
_, err := c.R().
    SetMultipartFormData(map[string]string{"title": "报告"}).
    SetFile("doc", "/path/to/file.pdf").
    Post("/upload")

// 流式上传
req.SetBodyReader(file, "application/octet-stream").SetContentLength(size)

// 下载落盘（不在内存留副本）
c.R().SetOutputFile("/tmp/out.bin").Get("/big-file")

// 流式响应（大文件、边读边处理）
resp, err := c.R().SetStreamResponse().Get("/stream")
defer resp.Close()
body, err := resp.Body()   // io.ReadCloser
```

响应体三种模式互斥：**内存（默认）** / **流式**（`SetStreamResponse`）/ **落盘**（`SetOutputFile`）。
流式模式下调 `Bytes()` / `String()` 返回 `ErrStreamConsumed`，而不是静默读一个已被消费的流。

---

## Server-Sent Events

```go
stream, err := c.SSE(ctx, "/events")
if err != nil {
    return err
}
defer stream.Close()

for {
    ev, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    fmt.Println(ev.Name, ev.Data)
}
```

自动重连（断线后按退避重连，并可带 `Last-Event-ID` 续传）：

```go
err := c.ConsumeSSE(ctx, "/events", func(ev client.SSEEvent) error {
    fmt.Println(ev.Data)
    return nil
}, client.SSEReconnectPolicy{
    MaxRetries:            5,
    Delay:                 time.Second,
    ResumeFromLastEventID: true,
})
```

心跳注释行（`:` 开头）不产生事件；需要感知心跳请给 `ctx` 设超时。

---

## 泛型入口

泛型只做**薄壳**——状态码、错误、重试、流式仍由非泛型层承担。

```go
// 包级泛型函数（Go 1.18+）：T 从 *T 自动推断，调用处零方括号
var users []User
_, err := client.GetInto(ctx, c, "/users", &users)
_, err = client.PostInto(ctx, c, "/users", createReq, &user)

// 已拿到响应时
var u User
err = client.DecodeInto(resp, &u)

// 返回式（T 只出现在返回值，必须显式实例化）
result, err := client.As[User](resp)
```

```go
//go:build go1.27 的方法级糖
resp, err := c.GetInto(ctx, "/users", &user)                                 // Client 方法，零方括号
resp, err = c.R().SetQueryParam("id", 1).GetInto(ctx, "/users", &user)       // fluent 链 + 方法快捷终结
resp, err = c.R().SetHeader("X-A", "1").PostInto(ctx, "/users", body, &user) // 同族：Post/Put/Patch/Delete/Head/Options
result, err := c.R().SetQueryParam("id", 1).As[User]()                      // 返回式需显式实例化
```

> **不必手写 `SetMethod`**：`r.GetInto` / `r.PostInto` / `r.PutInto` / `r.PatchInto` / `r.DeleteInto` / `r.HeadInto` / `r.OptionsInto` 会自己设好方法与 URL，链上只写参数与头即可；需要任意方法（或 DELETE 带 body）时用 `r.DoInto(ctx, method, url, &dst)`，而 method/URL 已另行设置好的场景才用 `r.Into(ctx, &dst)`。

> Go 的类型推断不看返回类型，因此 `As[User]()` 必须写方括号；sink 形式（`GetInto(ctx, url, &user)`）不需要。这就是主推 sink 的原因。

---

## 可观测性

```go
c := client.New(
    client.WithDebug(true),
    client.WithLogger(myLogger),   // 实现 client.Logger（与 server 的 Logger 同形）
    client.WithTrace(true),
    client.WithDebugBodyLimit(2048),
)

resp, _ := c.R().Get("/x")
ti := resp.Traces()   // DNSLookup / Connect / TLSHandshake / WroteRequest / TTFB / Total
```

debug 日志中的 URL 经与 server 同一套单行化净化（防日志注入）。

---

## 并发与生命周期

- `Client` 构造后只读，**并发安全**；`Use` / `OnXxx` 应在构造期调用。
- `Request` 可变且**不并发安全**：一次请求内构建、终结、丢弃。
- 流式响应必须 `Close`（否则连接与 goroutine 不释放）。
- `Client.CloseIdleConnections()` 关闭连接池中的空闲连接。

---

## 边界与注意事项

- **`WithTimeout` 是整体墙钟上限**，含读 body；大响应/下载请用 `context` 或 `WithResponseHeaderTimeout`。
- **`WithHTTPClient` 是"完全接管"**：之后本包的传输类选项不再生效。
- **`WithDisableRedirects()` 隐含接受 3xx**（不再算错误），因为"禁止跟随重定向"的意图正是自己处理 3xx；用自建 `CheckRedirect` 时需显式 `WithAcceptRedirects()`。
- **`WithProxyFromEnvironment` 只读一次环境变量**（标准库的包级缓存），运行期可变代理请用 `WithProxy`。
- 响应解码与 server 共用严格内核：拒绝 JSON 尾随内容（`{"a":1}GARBAGE`），声明长度却截断时报错。
