# ghttp/ws

> 开发中（pre-v1.0.0），API 不承诺向后兼容，**禁止直接用于生产**。

RFC 6455 WebSocket 服务端，只依赖标准库，不 import `ghttp` 根包。API 基于 `http.ResponseWriter` 与 `*http.Request`，因此既能用于 `ghttp.Raw` 路由，也能用于任何 net/http 代码。

## 用法

### ghttp Raw 路由

```go
s := ghttp.NewServer()
_ = s.Register(ghttp.Raw(http.MethodGet, "/ws/:room", func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
    conn, err := ws.Upgrade(resp, req.Raw, ws.WithSubprotocols("chat"))
    if err != nil {
        return nil // 握手失败时 Upgrade 已写出 4xx，不要再返回错误让 ghttp 重复写响应
    }
    defer conn.Close(ws.CloseNormalClosure, "")
    for {
        mt, data, err := conn.ReadMessage()
        if err != nil {
            return nil // 连接已接管，返回 nil
        }
        if err := conn.WriteMessage(mt, data); err != nil {
            return nil
        }
    }
}))
```

### net/http

```go
http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
    conn, err := ws.Upgrade(w, r)
    if err != nil {
        return
    }
    defer conn.Close(ws.CloseNormalClosure, "")
    // ...
})
```

### 心跳

```go
conn, _ := ws.Upgrade(w, r, ws.WithPongHandler(func(c *ws.Conn, _ []byte) error {
    return c.SetReadDeadline(time.Now().Add(60 * time.Second))
}))
_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
go func() {
    for range time.Tick(30 * time.Second) {
        if conn.Ping(nil) != nil {
            return
        }
    }
}()
```

## 握手

| 条件 | 失败响应 |
|------|----------|
| 方法不是 GET | 405（`Allow: GET`） |
| HTTP/1.0、`Connection` 不含 `upgrade`、`Upgrade` 不含 `websocket`、`Sec-WebSocket-Key` 不是 base64 编码的 16 字节 | 400（带 `Sec-WebSocket-Version: 13`） |
| `Sec-WebSocket-Version` 不是 13 | 426（带 `Sec-WebSocket-Version: 13`） |
| Origin 校验失败 | 403 |
| 连接无法接管（如 HTTP/2） | 500 |

- 握手失败返回 `*HandshakeError`（`Status`、`Reason`），可用 `errors.Is` 判断 `ErrBadHandshake` / `ErrOriginNotAllowed`；`WithErrorHandler` 可自定义失败响应。
- Origin 默认同源（`SameOrigin`）：缺少 Origin 放行；否则 Origin 的 host 需与请求 Host 一致。`WithCheckOrigin` 覆盖。
- 子协议：`WithSubprotocols` 按服务端偏好顺序，选第一个客户端也提供的；`Conn.Subprotocol()` 返回结果。
- 握手后客户端立即发送、已被 net/http 缓冲的字节不会丢失。
- 辅助函数：`AcceptKey`、`IsWebSocketUpgrade`（中间件可据此跳过压缩/超时）、`Subprotocols`。

## Conn

| 方法 | 说明 |
|------|------|
| `ReadMessage()` / `NextReader()` | 读取下一条数据消息（自动重组分片、处理控制帧）；`NextReader` 流式读取 |
| `WriteMessage(mt, data)` | 写出一条完整消息（单帧，服务端不掩码） |
| `WriteControl(mt, data, deadline)` / `Ping(data)` | 写控制帧，负载 ≤ 125 字节 |
| `Close(code, reason)` | 发送关闭帧后关闭 TCP，可重复调用 |
| `SetReadDeadline` / `SetWriteDeadline` | 读写超时（超时后读/写错误是粘滞的） |
| `Subprotocol` / `RemoteAddr` / `LocalAddr` / `NetConn` | 访问器 |

并发：同一时刻最多一个读 goroutine；写方法内部加锁，可多 goroutine 并发调用，每条消息原子写出，控制帧不会插入数据帧中间。

读侧协议行为：

| 情况 | 处理 | 返回错误 |
|------|------|----------|
| 对端发送关闭帧 | 回复关闭帧并关闭 TCP | `*CloseError`（Code/Text；无状态码时 1005） |
| 未收到关闭帧就断开 | — | `*CloseError{Code: 1006}`，链中含 `io.ErrUnexpectedEOF` |
| 客户端帧未掩码、RSV 非零、保留 opcode、控制帧分片或 > 125 字节、分片顺序错误、非法关闭码 | 以 1002 关闭 | `ErrProtocol` |
| 消息（重组后）超过 `WithReadLimit`（默认 32 MiB） | 以 1009 关闭 | `ErrReadLimit` |
| 文本消息或关闭原因不是合法 UTF-8 | 以 1007 关闭 | `ErrInvalidUTF8` |
| 收到 ping | 默认回复同负载 pong；`WithPingHandler` 覆盖 | — |
| 收到 pong | 默认忽略；`WithPongHandler` 处理 | — |

## 选项

`WithSubprotocols`、`WithCheckOrigin`、`WithReadLimit`、`WithReadBufferSize`、`WithWriteBufferSize`（默认 4096）、`WithPingHandler`、`WithPongHandler`、`WithHandshakeTimeout`、`WithControlTimeout`（自动 pong/close 回复与 `Close` 的超时，默认 5s）、`WithResponseHeader`（101 响应的额外头，如 Set-Cookie）、`WithErrorHandler`。

## 限制

- 不支持任何扩展，包括 **permessage-deflate**：握手忽略 `Sec-WebSocket-Extensions`。
- 只实现服务端；帧编解码与方向无关，为以后的客户端预留。
- 写侧不提供流式 `NextWriter`，每条消息作为单帧写出；`WriteMessage` 不校验文本 UTF-8。
- `Close` 按 RFC 6455 由服务端先关闭 TCP，不等待对端的关闭帧回复。
- 升级成功后 `ghttp.Response` 不知道连接已被接管：Raw handler 升级后应返回 nil，否则 ghttp 的错误处理会尝试在已接管的连接上写响应（net/http 仅记录日志）。
