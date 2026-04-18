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
	ErrNilServer   = errors.New("gserver adapter: nil server")
	ErrNilHandler  = errors.New("gserver adapter: nil handler")
	ErrEmptyPath   = errors.New("gserver adapter: empty path")
	ErrInvalidPath = errors.New("gserver adapter: invalid path")
)

// Register registers a standard http.Handler into gserver route handling.
// It projects gserver request context into a lightweight *http.Request shape
// for gws and similar handlers, rather than promising full net/http parity.
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
	if err := validatePath(path); err != nil {
		return err
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

	req.URL.Scheme = string(fastCtx.URI().Scheme())
	req.URL.Host = string(fastCtx.URI().Host())
	req.Host = string(fastCtx.Host())
	req.URL.Path = string(fastCtx.Path())
	req.URL.RawQuery = string(fastCtx.URI().QueryString())
	req.RequestURI = req.URL.RequestURI()
	req.Proto = string(fastCtx.Request.Header.Protocol())
	req.ProtoMajor, req.ProtoMinor = parseProtoVersion(req.Proto)
	if remoteAddr := fastCtx.RemoteAddr(); remoteAddr != nil {
		req.RemoteAddr = remoteAddr.String()
	}

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

func validatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return ErrInvalidPath
	}
	if strings.Contains(path, "?") {
		return ErrInvalidPath
	}
	return nil
}

func parseProtoVersion(proto string) (int, int) {
	major, minor, ok := http.ParseHTTPVersion(proto)
	if !ok {
		return 0, 0
	}
	return major, minor
}
