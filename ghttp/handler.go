package ghttp

import (
	"context"
	"net/http"
)

// HandlerFunc is the generic handler signature.
// Req is the parsed request input struct. Resp is the response output struct.
type HandlerFunc[Req, Resp any] func(ctx context.Context, input *Req) (*Resp, error)

// NoInputHandler is for endpoints with no request body / parameters.
type NoInputHandler[Resp any] func(ctx context.Context) (*Resp, error)

// NoOutputHandler is for endpoints that only return an error.
type NoOutputHandler[Req any] func(ctx context.Context, input *Req) error

// RawHandler allows direct access to http.ResponseWriter and *http.Request.
type RawHandler func(w http.ResponseWriter, r *http.Request)
