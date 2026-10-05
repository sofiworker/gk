// Package ws 实现 RFC 6455 WebSocket 协议的服务端，只依赖标准库。
//
// API 基于 http.ResponseWriter 与 *http.Request，因此既可用于 ghttp 的 Raw 路由
// （ghttp.Response 本身就是 http.ResponseWriter），也可用于任何 net/http 代码：
//
//	conn, err := ws.Upgrade(w, r, ws.WithSubprotocols("chat"))
//	if err != nil {
//		return // 握手失败时 Upgrade 已写出 4xx 响应
//	}
//	defer conn.Close(ws.CloseNormalClosure, "")
//	for {
//		mt, data, err := conn.ReadMessage()
//		if err != nil {
//			return
//		}
//		if err := conn.WriteMessage(mt, data); err != nil {
//			return
//		}
//	}
//
// 并发模型：同一时刻最多一个 goroutine 读（ReadMessage/NextReader），写方法
// （WriteMessage/WriteControl/Ping/Close）内部加锁，可被多个 goroutine 并发调用。
//
// 不支持任何扩展（包括 permessage-deflate）：握手时忽略 Sec-WebSocket-Extensions，
// 收到 RSV 位非零的帧以 1002 关闭连接。
//
// Package ws implements the server side of the RFC 6455 WebSocket protocol using only
// the standard library.
//
// The API is built on http.ResponseWriter and *http.Request, so it works with ghttp Raw
// routes (ghttp.Response is itself an http.ResponseWriter) as well as any net/http code.
//
// Concurrency: at most one goroutine may read (ReadMessage/NextReader) at a time; write
// methods (WriteMessage/WriteControl/Ping/Close) lock internally and may be called from
// multiple goroutines concurrently.
//
// No extensions are supported (including permessage-deflate): Sec-WebSocket-Extensions
// is ignored during the handshake and frames with non-zero RSV bits fail the connection
// with 1002.
package ws
