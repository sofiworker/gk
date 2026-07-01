package gserver

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	ghttpserver "github.com/sofiworker/gk/ghttp"
)

var (
	ErrNilServer   = errors.New("gserver adapter: nil server")
	ErrNilHandler  = errors.New("gserver adapter: nil handler")
	ErrEmptyPath   = errors.New("gserver adapter: empty path")
	ErrInvalidPath = errors.New("gserver adapter: invalid path")
)

// Register registers a standard http.Handler into the main ghttp.Server.
func Register(s *ghttpserver.Server, path string, h http.Handler) error {
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

	// Register on all HTTP methods so the underlying handler
	// (e.g. gws.Handler) can dispatch by method internally.
	if err := registerAllMethods(s, path, h); err != nil {
		return err
	}
	return nil
}

func buildRequest(r *http.Request) *http.Request {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	req := &http.Request{
		Method:     r.Method,
		Header:     r.Header.Clone(),
		URL:        &url.URL{},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Host:       r.Host,
		RequestURI: r.RequestURI,
		Proto:      r.Proto,
		ProtoMajor: r.ProtoMajor,
		ProtoMinor: r.ProtoMinor,
		RemoteAddr: r.RemoteAddr,
	}

	if r.URL != nil {
		req.URL = &url.URL{
			Scheme:   r.URL.Scheme,
			Host:     r.URL.Host,
			Path:     r.URL.Path,
			RawPath:  r.URL.RawPath,
			RawQuery: r.URL.RawQuery,
		}
	}

	req = req.WithContext(r.Context())
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

func registerAllMethods(s *ghttpserver.Server, path string, h http.Handler) error {
	methods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	}
	for _, m := range methods {
		if err := ghttpserver.Route[struct{}, struct{}](s).CUSTOM(m, path).ToHTTP(h); err != nil {
			return err
		}
	}
	return nil
}
