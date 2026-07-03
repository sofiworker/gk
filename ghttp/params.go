package ghttp

import "net/url"
import "net/http"

// Params stores per-request path, query, header, cookie, and client IP snapshots.
type Params struct {
	path     pathParamList
	query    url.Values
	header   http.Header
	cookies  []*http.Cookie
	clientIP string
}

func newParams(path map[string]string, query url.Values, header http.Header, cookies []*http.Cookie, clientIP string) Params {
	var pathParams pathParamList
	for key, value := range path {
		pathParams.Add(key, value)
	}
	return newParamsFromPathParams(pathParams, query, header, cookies, clientIP)
}

func newParamsFromPathParams(path pathParamList, query url.Values, header http.Header, cookies []*http.Cookie, clientIP string) Params {
	return Params{
		path:     path.Clone(),
		query:    cloneQueryParams(query),
		header:   header.Clone(),
		cookies:  cloneCookies(cookies),
		clientIP: clientIP,
	}
}

func paramsFromRequest(r *http.Request, c *Config) Params {
	return paramsFromRequestWithPathParams(r, c, pathParamList{})
}

func paramsFromRequestWithPathParams(r *http.Request, c *Config, routeParams pathParamList) Params {
	if r == nil {
		return newParams(nil, nil, nil, nil, "")
	}
	clientIP := defaultClientIPResolver(r)
	if c != nil && c.clientIPResolver != nil {
		clientIP = c.clientIPResolver(r)
	}
	if routeParams.Len() > 0 {
		return newParamsFromPathParams(routeParams, r.URL.Query(), r.Header, r.Cookies(), clientIP)
	}
	return newParams(pathParams(r), r.URL.Query(), r.Header, r.Cookies(), clientIP)
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

func cloneCookies(src []*http.Cookie) []*http.Cookie {
	if len(src) == 0 {
		return nil
	}
	dst := make([]*http.Cookie, 0, len(src))
	for _, cookie := range src {
		if cookie == nil {
			continue
		}
		copyCookie := *cookie
		copyCookie.Unparsed = append([]string(nil), cookie.Unparsed...)
		dst = append(dst, &copyCookie)
	}
	return dst
}

// Path returns a path parameter value.
func (p Params) Path(key string) string {
	return p.path.Get(key)
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

// Cookie returns a request cookie value.
func (p Params) Cookie(key string) string {
	for _, cookie := range p.cookies {
		if cookie != nil && cookie.Name == key {
			return cookie.Value
		}
	}
	return ""
}

// DefaultCookie returns a request cookie value or defaultValue when it is empty.
func (p Params) DefaultCookie(key, defaultValue string) string {
	value := p.Cookie(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Cookies returns request cookies.
func (p Params) Cookies() []*http.Cookie {
	return cloneCookies(p.cookies)
}

// ClientIP returns the resolved client IP snapshot.
func (p Params) ClientIP() string {
	return p.clientIP
}
