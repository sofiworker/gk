package ghttp

import (
	"context"
	"net/http"
)

// HandlerFunc is the generic handler signature.
// Req is the parsed request input struct. Resp is the response output struct.
type HandlerFunc[Req, Resp any] func(ctx context.Context, input Req) (Resp, error)

// NoInputHandler is for endpoints with no request body / parameters.
type NoInputHandler[Resp any] func(ctx context.Context) (Resp, error)

// NoOutputHandler is for endpoints that only return an error.
type NoOutputHandler[Req any] func(ctx context.Context, input Req) error

// RawHandler allows direct access to http.ResponseWriter and *http.Request.
type RawHandler func(w http.ResponseWriter, r *http.Request)

// HTTPHandlerFunc is a parsed-input handler that writes the HTTP response itself.
type HTTPHandlerFunc[Req any] func(w http.ResponseWriter, r *http.Request, input Req) error

// StatusCoder lets a typed response explicitly declare its HTTP status.
type StatusCoder interface {
	StatusCode() int
}

// ResponseHeaderWriter lets a typed response explicitly add response headers.
type ResponseHeaderWriter interface {
	WriteResponseHeaders(http.Header)
}

// RedirectFunc resolves a redirect target for a parsed request.
type RedirectFunc[Req any] func(input Req) (string, error)
