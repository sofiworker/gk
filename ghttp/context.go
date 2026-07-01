package ghttp

import (
	"context"
	"net/http"
)

type requestContextKey struct{}

// Context is the request context interface exposed to handlers.
type Context interface {
	ResponseWriter() http.ResponseWriter
	Request() *http.Request
	Query(key string) string
	DefaultQuery(key, defaultValue string) string
	Path(key string) string
	DefaultPath(key, defaultValue string) string
}

// Query returns a query parameter from a handler context.
func Query(ctx context.Context, key string) string {
	r := requestFromContext(ctx)
	if r == nil {
		return ""
	}
	return r.URL.Query().Get(key)
}

// DefaultQuery returns a query parameter or defaultValue when it is empty.
func DefaultQuery(ctx context.Context, key, defaultValue string) string {
	value := Query(ctx, key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Path returns a path parameter from a handler context.
func Path(ctx context.Context, key string) string {
	r := requestFromContext(ctx)
	if r == nil {
		return ""
	}
	return pathParam(r, key)
}

// DefaultPath returns a path parameter or defaultValue when it is empty.
func DefaultPath(ctx context.Context, key, defaultValue string) string {
	value := Path(ctx, key)
	if value == "" {
		return defaultValue
	}
	return value
}

func contextWithRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, requestContextKey{}, r)
}

func requestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestContextKey{}).(*http.Request)
	return r
}

func queryParam(r *http.Request, key string) string {
	if r == nil {
		return ""
	}
	return r.URL.Query().Get(key)
}

func pathParam(r *http.Request, key string) string {
	if r == nil {
		return ""
	}
	if params := pathParams(r); params != nil {
		return params[key]
	}
	return ""
}
