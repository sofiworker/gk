// Package httperr 定义 HTTP 状态码语义的最小跨包共享契约。
// Package httperr defines the minimal cross-package contract for HTTP status
// semantics.
//
// 存在的唯一理由：ghttp(server)与 ghttp/client 都需要"错误自带 HTTP 状态码"这一概念，
// 但两者是独立包——server 把 handler 的错误分类成状态码，client 把响应状态码包装成错误。
// 若各自定义同形接口，用户侧的断言代码就得写两遍；若让 client import server，又会把整个
// server 框架链进 client 二进制。放在 internal 并由两侧以类型别名导出（`type StatusCoder
// = httperr.StatusCoder`），即可保持"同一个类型、零包依赖"。
// The only reason it exists: ghttp (server) and ghttp/client both need the notion
// of "an error carrying its own HTTP status", but they are separate packages — the
// server classifies handler errors into statuses, the client wraps response
// statuses into errors. Defining the same interface twice would force users to
// write assertions twice; letting the client import the server would link the whole
// server framework into client binaries. Living in internal and being alias-exported
// by both sides (`type StatusCoder = httperr.StatusCoder`) keeps it one type with
// zero package dependency.
package httperr

// StatusCoder 让一个错误自带 HTTP 状态码：实现它即可参与状态码判定，而不是被兜底成 500
// （server）或被当成传输错误（client）。
// StatusCoder lets an error carry its own HTTP status: implementing it makes the
// error participate in status decisions instead of falling back to 500 (server) or
// being mistaken for a transport failure (client).
type StatusCoder interface {
	error
	HTTPStatus() int
}
