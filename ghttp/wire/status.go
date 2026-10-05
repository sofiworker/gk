package wire

import "errors"

// StatusCoder 是携带 HTTP 状态码的错误契约。server 的 HTTPError 与 client 的 *Error 都实现它，
// 调用方可以不依赖具体包、用 StatusOf 统一读取状态码。
//
// 契约本身不包含映射策略：server 不会因为下游 client 错误实现了 StatusCoder 就把下游状态码
// 透传给自己的调用方，是否透传由业务决定。
// StatusCoder is the contract for errors carrying an HTTP status. Both the server's
// HTTPError and the client's *Error implement it, so callers can read the status via
// StatusOf without depending on either package.
//
// The contract carries no mapping policy: the server does not forward a downstream
// client error's status to its own caller merely because it implements StatusCoder;
// whether to forward is the application's decision.
type StatusCoder interface {
	HTTPStatus() int
}

// StatusOf 沿错误链查找第一个 StatusCoder 并返回其状态码；找不到或 err 为 nil 时返回 0。
// StatusOf walks err's chain for the first StatusCoder and returns its status; it
// returns 0 when none is found or err is nil.
func StatusOf(err error) int {
	var sc StatusCoder
	if errors.As(err, &sc) {
		return sc.HTTPStatus()
	}
	return 0
}
