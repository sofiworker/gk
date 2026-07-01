package ghttp

import "net/url"
import "net/http"

// Params stores per-request path, query, header, and client IP snapshots.
type Params struct {
	path     map[string]string
	query    url.Values
	header   http.Header
	clientIP string
}

func newParams(path map[string]string, query url.Values, header http.Header, clientIP string) Params {
	return Params{
		path:     clonePathParams(path),
		query:    cloneQueryParams(query),
		header:   header.Clone(),
		clientIP: clientIP,
	}
}

func clonePathParams(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneQueryParams(src url.Values) url.Values {
	if len(src) == 0 {
		return nil
	}
	dst := make(url.Values, len(src))
	for k, values := range src {
		dst[k] = append([]string(nil), values...)
	}
	return dst
}

// Path returns a path parameter value.
func (p Params) Path(key string) string {
	return p.path[key]
}

// DefaultPath returns a path parameter value or defaultValue when it is empty.
func (p Params) DefaultPath(key, defaultValue string) string {
	value := p.Path(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Query returns the first query parameter value.
func (p Params) Query(key string) string {
	if p.query == nil {
		return ""
	}
	return p.query.Get(key)
}

// DefaultQuery returns the first query parameter value or defaultValue when it is empty.
func (p Params) DefaultQuery(key, defaultValue string) string {
	value := p.Query(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// QueryList returns all query parameter values.
func (p Params) QueryList(key string) []string {
	values := p.query[key]
	return append([]string(nil), values...)
}

// Header returns the first header value.
func (p Params) Header(key string) string {
	if p.header == nil {
		return ""
	}
	return p.header.Get(key)
}

// DefaultHeader returns the first header value or defaultValue when it is empty.
func (p Params) DefaultHeader(key, defaultValue string) string {
	value := p.Header(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// HeaderList returns all header values.
func (p Params) HeaderList(key string) []string {
	values := p.header.Values(key)
	return append([]string(nil), values...)
}

// ClientIP returns the resolved client IP snapshot.
func (p Params) ClientIP() string {
	return p.clientIP
}
