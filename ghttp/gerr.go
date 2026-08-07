package ghttp

import (
	"net/http"
	"strconv"

	"github.com/sofiworker/gk/gerr"
)

// GerrStatus 将 gerr.Kind 映射为规范 HTTP 状态码。
// GerrStatus maps a gerr.Kind to the canonical HTTP status code.
func GerrStatus(err error) (int, bool) {
	ge, ok := gerr.ErrorOf(err)
	if !ok {
		return 0, false
	}
	return gerrKindStatus(ge.Kind), true
}

// FromGerr 按 Kind -> status 映射将 gerr.Error 转换为 HTTPError。
// FromGerr converts a gerr.Error into an HTTPError via Kind -> status.
// 原始错误保留在链上，errors.Is/As 仍可穿透。
// the original error stays in the chain, so errors.Is/As still work.
func FromGerr(err error) (*HTTPError, bool) {
	ge, ok := gerr.ErrorOf(err)
	if !ok {
		return nil, false
	}
	status := gerrKindStatus(ge.Kind)
	message := ge.Message
	if message == "" {
		message = ge.Error()
	}
	return Err(status, message, WithCause(err)), true
}

// ToGerr 按 status -> Kind 映射将 HTTPError 转换为 gerr.Error。
// ToGerr converts an HTTPError into a gerr.Error via status -> Kind.
// HTTP 状态码保留为业务 Code。
// the HTTP status is preserved as the business Code.
func ToGerr(err error) (*gerr.Error, bool) {
	he := AsError(err)
	if he == nil {
		return nil, false
	}
	return gerr.New(he.Message,
		gerr.WithKind(httpStatusKind(he.Code)),
		gerr.WithCode(strconv.Itoa(he.Code)),
		gerr.WithCause(he.Err),
	), true
}

func gerrKindStatus(kind gerr.Kind) int {
	switch kind {
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
		return http.StatusRequestTimeout
	default:
		return http.StatusInternalServerError
	}
}

func httpStatusKind(status int) gerr.Kind {
	switch status {
	case http.StatusBadRequest:
		return gerr.KindInvalid
	case http.StatusUnauthorized, http.StatusForbidden:
		return gerr.KindPermission
	case http.StatusNotFound:
		return gerr.KindNotFound
	case http.StatusConflict:
		return gerr.KindConflict
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return gerr.KindTimeout
	case http.StatusServiceUnavailable:
		return gerr.KindUnavailable
	default:
		return gerr.KindInternal
	}
}
