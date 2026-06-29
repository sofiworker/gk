package ghttp

import (
	"errors"
	"net/http"
)

var (
	ErrConflict   = errors.New("conflict")
	ErrNotFound   = errors.New("not found")
	ErrHandled    = errors.New("response already handled")
	ErrNilContext = errors.New("context is nil")
)

// HTTPError is a structured HTTP error with code and message.
type HTTPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

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
