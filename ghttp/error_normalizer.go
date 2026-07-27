package ghttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/sofiworker/gk/gerr"
)

const internalServerErrorID = "internal.server_error"

type defaultErrorNormalizer struct {
	mapper         KindStatusMapper
	detailAdapters []ErrorDetailAdapter
}

func newDefaultErrorNormalizer(mapper KindStatusMapper, adapters []ErrorDetailAdapter) ErrorNormalizer {
	if mapper == nil {
		mapper = KindStatusMapperFunc(defaultHTTPStatus)
	}
	return &defaultErrorNormalizer{mapper: mapper, detailAdapters: append([]ErrorDetailAdapter(nil), adapters...)}
}

func (n *defaultErrorNormalizer) NormalizeError(ctx context.Context, err error) NormalizedError {
	if err == nil {
		err = errors.New("nil error")
	}
	normalized := NormalizedError{Cause: err}
	var staged validationStageError
	if errors.As(err, &staged) {
		if result, ok := (defaultValidationErrorAdapter{}).AdaptValidationError(ctx, err); ok {
			normalized.Kind = gerr.KindInvalid
			normalized.Document.MessageID = "request.validation_failed"
			normalized.Document.Details = result.Details
		}
	}
	descriptor, described := gerr.Describe(err)
	if normalized.Document.MessageID != "" {
		// The private validation-stage marker owns the public identity.
	} else if described && allowedPublicID(err, descriptor.ID) {
		normalized.Kind = descriptor.Kind
		normalized.Document.MessageID = descriptor.ID
		normalized.Document.Args, _ = sanitizePublicParams(descriptor.Params)
	} else if errors.Is(err, context.Canceled) {
		normalized.Kind = gerr.KindCanceled
		normalized.Document.MessageID = "request.canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		normalized.Kind = gerr.KindTimeout
		normalized.Document.MessageID = "request.timeout"
	} else {
		normalized.Kind = gerr.KindInternal
		normalized.Document.MessageID = internalServerErrorID
	}
	normalized.Op, normalized.Meta = diagnosticContext(err)
	if normalized.Kind == gerr.KindCanceled {
		normalized.SuppressResponse = true
	}
	if normalized.Document.MessageID == "request.validation_failed" {
		normalized.Document.Status = http.StatusUnprocessableEntity
	} else if status, ok := outerHTTPStatus(err); ok {
		normalized.Document.Status = status
	} else {
		normalized.Document.Status = n.mapper.HTTPStatus(normalized.Kind)
	}
	for _, adapter := range n.detailAdapters {
		if details, ok := adapter.AdaptErrorDetails(ctx, err); ok {
			normalized.Document.Details = details
			break
		}
	}
	return finalizeNormalizedError(normalized)
}

func finalizeNormalizedError(normalized NormalizedError) NormalizedError {
	if normalized.SuppressResponse && normalized.Kind == gerr.KindCanceled {
		return normalized
	}
	if !validHTTPErrorStatus(normalized.Document.Status) || !gerr.ValidMessageID(normalized.Document.MessageID) {
		return safeInternalError(normalized.Cause)
	}
	args, diagnostic := sanitizePublicParams(normalized.Document.Args)
	normalized.Document.Args = args
	if diagnostic && len(args) == 0 && normalized.Document.MessageID == "" {
		return safeInternalError(normalized.Cause)
	}
	for i := range normalized.Document.Details {
		detail := &normalized.Document.Details[i]
		if !gerr.ValidMessageID(detail.MessageID) {
			return safeInternalError(normalized.Cause)
		}
		detail.Args, _ = sanitizePublicParams(detail.Args)
	}
	return normalized
}

func safeInternalError(cause error) NormalizedError {
	return NormalizedError{
		Document: ErrorDocument{Status: http.StatusInternalServerError, MessageID: internalServerErrorID, Args: map[string]any{}},
		Cause:    cause,
		Kind:     gerr.KindInternal,
	}
}

func defaultHTTPStatus(kind gerr.Kind) int {
	switch kind {
	case gerr.KindInvalid:
		return http.StatusBadRequest
	case gerr.KindNotFound:
		return http.StatusNotFound
	case gerr.KindConflict:
		return http.StatusConflict
	case gerr.KindUnauthenticated:
		return http.StatusUnauthorized
	case gerr.KindPermission:
		return http.StatusForbidden
	case gerr.KindRateLimited:
		return http.StatusTooManyRequests
	case gerr.KindUnavailable:
		return http.StatusServiceUnavailable
	case gerr.KindTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

type statusError struct {
	status int
	err    error
}

func WithStatus(err error, status int) error {
	if err == nil {
		return nil
	}
	return &statusError{status: status, err: err}
}

func (e *statusError) Error() string   { return e.err.Error() }
func (e *statusError) Unwrap() error   { return e.err }
func (e *statusError) HTTPStatus() int { return e.status }

func outerHTTPStatus(err error) (int, bool) {
	for err != nil {
		if carrier, ok := err.(HTTPStatusCarrier); ok && validHTTPErrorStatus(carrier.HTTPStatus()) {
			return carrier.HTTPStatus(), true
		}
		if _, many := err.(interface{ Unwrap() []error }); many {
			return 0, false
		}
		one, ok := err.(interface{ Unwrap() error })
		if !ok {
			return 0, false
		}
		err = one.Unwrap()
	}
	return 0, false
}

func validHTTPErrorStatus(status int) bool { return status >= 400 && status <= 599 }

type frameworkErrorMarker interface{ frameworkError() }

func allowedPublicID(err error, id string) bool {
	for _, prefix := range []string{"http.", "request.", "validation.", "internal."} {
		if strings.HasPrefix(id, prefix) {
			_, ok := err.(frameworkErrorMarker)
			return ok
		}
	}
	return true
}

func diagnosticContext(err error) (string, map[string]any) {
	for err != nil {
		if structured, ok := err.(*gerr.Error); ok {
			return structured.Op, cloneDiagnosticMap(structured.Meta)
		}
		if _, many := err.(interface{ Unwrap() []error }); many {
			break
		}
		one, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = one.Unwrap()
	}
	return "", nil
}

func cloneDiagnosticMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
