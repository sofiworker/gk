package ghttp

import (
	"errors"
	"net/http"
)

var (
	ErrConflict    = errors.New("conflict")
	ErrNotFound    = errors.New("not found")
	ErrHandled     = errors.New("response already handled")
	ErrNilContext  = errors.New("context is nil")
	ErrNilListener = errors.New("listener is nil")

	ErrRoutePathInvalid      = errors.New("invalid route path")
	ErrInvalidRequestPath    = errors.New("invalid request path")
	ErrRouteConflict         = errors.New("route conflict")
	ErrRouteBuilderFinalized = errors.New("route builder finalized")
	ErrRouteHandlerNil       = errors.New("route handler is nil")
	ErrRouteMethodAlreadySet = errors.New("route method already set")
	ErrServerFrozen          = errors.New("server frozen")
	ErrOpenAPIDisabled       = errors.New("openapi disabled")
	ErrHandlerPanic          = errors.New("handler panic")

	ErrInvalidParamsUsage   = errors.New("invalid params usage")
	ErrUnsupportedMediaType = errors.New("unsupported media type")
	ErrRequestBodyTooLarge  = errors.New("request body too large")

	ErrRendererNotConfigured = errors.New("renderer is not configured")
	ErrStaticRootRequired    = errors.New("static root cannot be empty")
	ErrRBACNotConfigured     = errors.New("rbac not configured")
	ErrForbidden             = errors.New("forbidden")
)

// HTTPError is a structured HTTP error with code and message.
type HTTPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

// ErrorHandler writes a normalized HTTP error response.
type ErrorHandler func(http.ResponseWriter, *http.Request, *HTTPError)

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.Code)
}

func (e *HTTPError) Unwrap() error { return e.Err }

// ErrorOption configures an HTTPError.
type ErrorOption func(*HTTPError)

func WithCause(err error) ErrorOption {
	return func(e *HTTPError) { e.Err = err }
}

// Err creates a new HTTPError.
func Err(statusCode int, msg string, opts ...ErrorOption) *HTTPError {
	e := &HTTPError{Code: statusCode, Message: msg}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func BadRequest(msg string) *HTTPError {
	return Err(http.StatusBadRequest, msg)
}

func NotFound(msg string) *HTTPError {
	return Err(http.StatusNotFound, msg)
}

func Conflict(msg string) *HTTPError {
	return Err(http.StatusConflict, msg)
}

func InternalError(msg string) *HTTPError {
	return Err(http.StatusInternalServerError, msg)
}

// AsError extracts an HTTPError from an error chain.
func AsError(err error) *HTTPError {
	var he *HTTPError
	if errors.As(err, &he) {
		return he
	}
	return nil
}

func isErrHandled(err error) bool {
	return errors.Is(err, ErrHandled)
}
