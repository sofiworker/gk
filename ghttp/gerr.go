package ghttp

import (
	"net/http"
	"strconv"

	"github.com/sofiworker/gk/gerr"
)

// GerrStatus maps a gerr.Kind to the canonical HTTP status code.
func GerrStatus(err error) (int, bool) {
	ge, ok := gerr.ErrorOf(err)
	if !ok {
		return 0, false
	}
	return gerrKindStatus(ge.Kind), true
}

// FromGerr converts a gerr.Error into an HTTPError using the Kind -> status
// mapping. The original error stays in the chain, so errors.Is/As still work
// through the returned HTTPError.
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

// ToGerr converts an HTTPError into a gerr.Error using the status -> Kind
// mapping. The HTTP status is preserved as the business Code.
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
