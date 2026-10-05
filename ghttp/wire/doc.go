// Package wire 提供 HTTP 线格式层的公共原语，供 ghttp（server）、ghttp/client 与用户代码共用。
//
// 范围：
//   - media-type 归一化与比对（MediaType、ContentTypeIn）；
//   - 严格的 JSON/XML 报文体解码（DecodeJSON、DecodeXML、ErrInvalidFormat）；
//   - Server-Sent Events 线格式读写（ReadSSEEvent、AppendSSEField 等）；
//   - 日志字段单行化（LogToken）；
//   - HTTP 状态码契约（StatusCoder）。
//
// 本包只依赖标准库，不 import ghttp 或 ghttp/client，依赖方向始终是两端指向本包。
// 线格式是同一份规范，两端各写一份必然漂移；策略（重试、重连、错误映射到哪个状态码）
// 不在这里，留在各端。
//
// Package wire provides the HTTP wire-format primitives shared by ghttp (server),
// ghttp/client and user code.
//
// Scope:
//   - media-type normalization and matching (MediaType, ContentTypeIn);
//   - strict JSON/XML body decoding (DecodeJSON, DecodeXML, ErrInvalidFormat);
//   - Server-Sent Events wire format (ReadSSEEvent, AppendSSEField, …);
//   - single-line log fields (LogToken);
//   - the HTTP status contract (StatusCoder).
//
// The package depends only on the standard library and imports neither ghttp nor
// ghttp/client; dependencies always point from both sides into this package. A wire
// format is one specification and two implementations would drift; policy (retries,
// reconnection, which status an error maps to) stays on each side.
package wire
