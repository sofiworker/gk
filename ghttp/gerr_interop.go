package ghttp

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/sofiworker/gk/gerr"
)

// GerrStatus 返回 gerr.Kind 对应的 HTTP 状态码；KindUnknown 与未知 Kind 返回 500。
// GerrStatus returns the HTTP status for a gerr.Kind; KindUnknown and unknown kinds yield 500.
func GerrStatus(k gerr.Kind) int {
	return statusFromKind(k)
}

// KindFromStatus 返回 HTTP 状态码对应的 gerr.Kind，是 GerrStatus 的近似逆映射：
// 4xx 中未单独映射的归为 KindInvalid，5xx 中未单独映射的归为 KindInternal，其余为 KindUnknown。
// KindFromStatus returns the gerr.Kind for an HTTP status, approximately inverting
// GerrStatus: unmapped 4xx become KindInvalid, unmapped 5xx KindInternal, others KindUnknown.
func KindFromStatus(status int) gerr.Kind {
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return gerr.KindNotFound
	case http.StatusConflict, http.StatusPreconditionFailed:
		return gerr.KindConflict
	case http.StatusUnauthorized, http.StatusForbidden:
		return gerr.KindPermission
	case http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusBadGateway:
		return gerr.KindUnavailable
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return gerr.KindTimeout
	case StatusClientClosedRequest:
		return gerr.KindCanceled
	}
	switch {
	case status >= 400 && status < 500:
		return gerr.KindInvalid
	case status >= 500:
		return gerr.KindInternal
	}
	return gerr.KindUnknown
}

// FromGerr 把错误链中的 *gerr.Error 转换为 HTTPError：状态码按 Kind 映射，Message 取 gerr 的
// Message（会返回给客户端，业务应只在其中放可公开的文本），Cause 保留原错误。
// 链中已有 HTTPError 时原样返回它；没有 *gerr.Error 时返回 (HTTPError{}, false)。
// FromGerr converts the *gerr.Error in err's chain into an HTTPError: the status follows
// the Kind, Message is the gerr Message (sent to clients, so keep it publishable) and
// Cause keeps the original error. An HTTPError already in the chain is returned as-is;
// without a *gerr.Error it returns (HTTPError{}, false).
func FromGerr(err error) (HTTPError, bool) {
	var he HTTPError
	if errors.As(err, &he) {
		return he, true
	}
	ge, ok := gerr.ErrorOf(err)
	if !ok {
		return HTTPError{}, false
	}
	return HTTPError{Status: statusFromKind(ge.Kind), Message: ge.Message, Cause: err}, true
}

// ToGerr 把 HTTPError 转换为 *gerr.Error：Kind 由状态码推导，Code 为 "http_<status>"，
// Meta 记录 "http_status"，Cause 链保持不变。err 为 nil 时返回 nil；不含 HTTPError 时按
// StatusFromError 推导状态码。
// ToGerr converts an HTTPError into a *gerr.Error: the Kind derives from the status, Code
// is "http_<status>", Meta records "http_status" and the cause chain is kept. A nil err
// yields nil; without an HTTPError the status comes from StatusFromError.
func ToGerr(err error) *gerr.Error {
	if err == nil {
		return nil
	}
	if ge, ok := gerr.ErrorOf(err); ok {
		return ge
	}
	status, he := classifyError(err)
	msg := statusText(status)
	if he != nil && he.Message != "" {
		msg = he.Message
	}
	return gerr.New(msg,
		gerr.WithKind(KindFromStatus(status)),
		gerr.WithCode("http_"+strconv.Itoa(status)),
		gerr.WithMeta("http_status", status),
		gerr.WithCause(err),
	)
}
