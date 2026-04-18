package gserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	httpserver "github.com/sofiworker/gk/ghttp/gserver"
)

var (
	ErrNilServer  = errors.New("gserver adapter: nil server")
	ErrNilHandler = errors.New("gserver adapter: nil handler")
	ErrEmptyPath  = errors.New("gserver adapter: empty path")
)

// Register registers a standard http.Handler into gserver route handling.
func Register(s *httpserver.Server, path string, h http.Handler) error {
	switch {
	case s == nil:
		return ErrNilServer
	case h == nil:
		return ErrNilHandler
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return ErrEmptyPath
	}

	s.ANY(path, func(ctx *httpserver.Context) {
		req := buildRequest(ctx)
		h.ServeHTTP(ctx.Writer, req)
	})
	return nil
}

func buildRequest(ctx *httpserver.Context) *http.Request {
	req := &http.Request{
		Method: http.MethodGet,
		Header: make(http.Header),
		URL:    &url.URL{},
		Body:   http.NoBody,
	}

	if ctx == nil {
		return req.WithContext(context.Background())
	}

	req = req.WithContext(ctx.Context())

	fastCtx := ctx.FastContext()
	if fastCtx == nil {
		return req
	}

	if method := string(fastCtx.Method()); method != "" {
		req.Method = method
	}

	req.Host = string(fastCtx.Host())
	req.URL.Path = string(fastCtx.Path())
	req.URL.RawQuery = string(fastCtx.URI().QueryString())
	req.RequestURI = req.URL.RequestURI()

	fastCtx.Request.Header.VisitAll(func(k, v []byte) {
		req.Header.Add(string(k), string(v))
	})

	body := fastCtx.Request.Body()
	if len(body) > 0 {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
	}

	return req
}
