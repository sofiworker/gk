// Package client 是 ghttp 的 HTTP 客户端：一个建立在标准库 net/http 之上、
// 与 ghttp server 同一套语义与扩展模型的编排层。
// Package client is ghttp's HTTP client: an orchestration layer built on the
// standard library's net/http, sharing the ghttp server's semantics and extension
// model.
//
// # 定位 / Stance
//
// 本包【不】实现传输层。它把 http.RoundTripper 当作唯一组合点：默认用
// http.DefaultTransport 的克隆（不污染全局、不静默禁用 HTTP/2），用户可注入自己的
// *http.Client、http.RoundTripper、*net.Dialer、DialContext、http.CookieJar 与
// CheckRedirect；也可以把整个 Client 退化成 http.RoundTripper（Client.RoundTripper）
// 塞进别人的 http.Client。请求与响应都能与标准库双向互转（Request.HTTPRequest、
// Client.DoHTTP、Response.Raw）。
// This package does NOT implement a transport. It treats http.RoundTripper as the
// single composition point: by default a clone of http.DefaultTransport (never
// mutating the global, never silently disabling HTTP/2), while allowing callers to
// inject their own *http.Client, http.RoundTripper, *net.Dialer, DialContext,
// http.CookieJar and CheckRedirect — or to degrade the whole Client into an
// http.RoundTripper (Client.RoundTripper) for someone else's http.Client. Requests
// and responses convert to and from the standard library both ways
// (Request.HTTPRequest, Client.DoHTTP, Response.Raw).
//
// # 与 resty 的取舍 / Departures from resty
//
// 调用手感借鉴 go-resty/resty（fluent 的 Client.R() 链），但四处语义刻意不同：
//   - 非 2xx 默认【算错误】（返回 *Error，其中带得走整个 *Response），可经
//     WithAllowAllStatus 退回"状态码自己判断"的生态惯例；
//   - 响应体默认【限长】（WithResponseBodyLimit，默认 32 MiB），而不是无上限读入内存；
//   - 不可重放的请求体在重试前【直接拒绝】（ErrBodyNotReplayable），绝不静默重发空体；
//   - 对服务端响应格式【零假设】：没有隐式 envelope，结构化错误体经 WithErrorDecoder 显式接入。
//
// The call feel follows go-resty/resty (a fluent Client.R() chain), with four
// deliberate semantic differences:
//   - non-2xx is an ERROR by default (an *Error carrying the whole *Response);
//     WithAllowAllStatus restores the ecosystem habit of judging status yourself;
//   - response bodies are SIZE-LIMITED by default (WithResponseBodyLimit, 32 MiB),
//     instead of being read into memory unbounded;
//   - an unreplayable request body is REFUSED before retrying
//     (ErrBodyNotReplayable) rather than silently resending an empty body;
//   - ZERO assumptions about the server's response shape: no implicit envelope, and
//     structured error bodies are wired in explicitly via WithErrorDecoder.
//
// # 包边界 / Package boundary
//
// 本包不 import ghttp 父包，因此只使用 client 的程序不会链入 server 框架。真正需要共享
// 的内核（严格 JSON/XML 解码、Content-Type 规范化、日志脱敏、HTTP 状态码契约）位于
// ghttp/internal/*，两侧各自引用；其中 httperr.StatusCoder 经类型别名在本包导出，与 server
// 侧是同一个类型。
// This package does not import the ghttp parent, so a program using only the client
// does not link the server framework. The kernels genuinely worth sharing (strict
// JSON/XML decoding, Content-Type normalization, log sanitization, the HTTP status
// contract) live in ghttp/internal/* and are referenced by both sides; httperr.StatusCoder
// is alias-exported here and is the very same type as on the server side.
//
// # 简洁示例 / Short example
//
//	c := client.New(
//		client.WithBaseURL("https://api.example.com"),
//		client.WithTimeout(5*time.Second),
//		client.WithRetry(client.RetryPolicy{MaxRetries: 3}),
//	)
//
//	var user User
//	resp, err := c.R().
//		SetQueryParam("id", 1).
//		SetResult(&user).
//		Get("/users")
//	if err != nil {
//		return err
//	}
//	_ = resp.StatusCode()
package client
