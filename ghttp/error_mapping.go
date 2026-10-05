package ghttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/sofiworker/gk/gerr"
	"github.com/sofiworker/gk/ghttp/wire"
)

// StatusClientClosedRequest 是客户端在响应前断开（ctx 被取消）时使用的非标准状态码 499。
// StatusClientClosedRequest is the non-standard 499 used when the client went away
// before the response (ctx canceled).
const StatusClientClosedRequest = 499

// StatusFromError 返回 err 对应的 HTTP 状态码。按以下优先级判定：
//  1. 链上的 HTTPError：其 Status；
//  2. *http.MaxBytesError：413；
//  3. wire.ErrInvalidFormat：400；
//  4. *gerr.Error 的 Kind：invalid 400、not_found 404、conflict 409、permission 403、
//     unavailable 503、timeout 504、canceled 499、其余 500；
//  5. context.DeadlineExceeded 504、context.Canceled 499；
//  6. 其他：500。nil 返回 200。
//
// 不读取下游 client 错误的状态码：下游返回 404 不代表本服务应返回 404。
// StatusFromError returns the HTTP status for err with this precedence:
//  1. an HTTPError in the chain: its Status;
//  2. *http.MaxBytesError: 413;
//  3. wire.ErrInvalidFormat: 400;
//  4. the Kind of a *gerr.Error: invalid 400, not_found 404, conflict 409, permission 403,
//     unavailable 503, timeout 504, canceled 499, otherwise 500;
//  5. context.DeadlineExceeded 504, context.Canceled 499;
//  6. anything else: 500. nil yields 200.
//
// A downstream client error's status is deliberately ignored: a downstream 404 does not
// mean this service should answer 404.
func StatusFromError(err error) int {
	status, _ := classifyError(err)
	return status
}

// classifyError 返回状态码，以及可以安全返回给客户端的 HTTPError（如有）。
// classifyError returns the status and, when present, the HTTPError safe to show clients.
func classifyError(err error) (int, *HTTPError) {
	if err == nil {
		return http.StatusOK, nil
	}
	var he HTTPError
	if errors.As(err, &he) {
		return he.Status, &he
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return http.StatusRequestEntityTooLarge, nil
	}
	if errors.Is(err, wire.ErrInvalidFormat) {
		return http.StatusBadRequest, nil
	}
	if ge, ok := gerr.ErrorOf(err); ok && ge.Kind != gerr.KindUnknown {
		return statusFromKind(ge.Kind), nil
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, nil
	case errors.Is(err, context.Canceled):
		return StatusClientClosedRequest, nil
	}
	return http.StatusInternalServerError, nil
}

// statusFromKind 把 gerr.Kind 映射为 HTTP 状态码。
// statusFromKind maps a gerr.Kind to an HTTP status.
func statusFromKind(k gerr.Kind) int {
	switch k {
	case gerr.KindInvalid:
		return http.StatusBadRequest
	case gerr.KindNotFound:
		return http.StatusNotFound
	case gerr.KindConflict:
		return http.StatusConflict
	case gerr.KindPermission:
		return http.StatusForbidden
	case gerr.KindUnavailable:
		return http.StatusServiceUnavailable
	case gerr.KindTimeout:
		return http.StatusGatewayTimeout
	case gerr.KindCanceled:
		return StatusClientClosedRequest
	default:
		return http.StatusInternalServerError
	}
}

// ErrorResponseOf 构造返回给客户端的错误体。只有 HTTPError 的 Message 会原样返回，其余错误只给
// 出状态文本，避免把内部错误字符串泄露给客户端；*gerr.Error 的 Code 会作为业务错误码返回。
// ErrorResponseOf builds the client-facing error body. Only an HTTPError's Message is
// returned verbatim; other errors yield the status text so internal error strings never
// leak. A *gerr.Error's Code is returned as the business error code.
func ErrorResponseOf(err error) ErrorResponse {
	status, he := classifyError(err)
	resp := ErrorResponse{Status: status}
	if he != nil && he.Message != "" {
		resp.Error = he.Message
	} else {
		resp.Error = statusText(status)
	}
	if ge, ok := gerr.ErrorOf(err); ok {
		resp.Code = ge.Code
	}
	return resp
}

// statusText 返回状态文本，499 等非标准状态码也有可读文本。
// statusText returns the status text, including readable text for non-standard codes such as 499.
func statusText(status int) string {
	if status == StatusClientClosedRequest {
		return "client closed request"
	}
	if t := http.StatusText(status); t != "" {
		return t
	}
	return "error"
}
