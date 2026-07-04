package ghttp

import "net/url"
import "net/http"

// Params is a per-request view of path, query, header, cookie, and client IP
// inputs. It reads from the underlying request lazily: query values, cookies,
// and the client IP are parsed on first access and cached, headers are read
// through directly. Construction is allocation-light regardless of how many
// inputs the handler actually reads.
//
// A Params view is valid for the lifetime of the handler invocation and must
// be used from a single goroutine. To retain the values beyond the handler
// return or share them across goroutines, call Detach first to obtain an
// immutable snapshot that no longer references the request.
//
// The zero value is an empty, read-safe Params.
type Params struct {
	path  pathParamList
	state *paramsState
}

// paramsState carries the request reference and the lazily materialized
// caches shared by all copies of one Params view. A detached state owns deep
// copies of every input and holds no request.
type paramsState struct {
	req      *http.Request
	header   http.Header
	resolver ClientIPResolver

	query         url.Values
	queryParsed   bool
	cookies       []*http.Cookie
	cookiesParsed bool
	clientIP      string
	clientIPSet   bool
}

// newParams builds an immutable snapshot from explicit values (detached
// semantics): inputs are deep-copied and later mutation of the arguments does
// not affect the returned Params.
func newParams(path map[string]string, query url.Values, header http.Header, cookies []*http.Cookie, clientIP string) Params {
	var pathParams pathParamList
	for key, value := range path {
		pathParams.Add(key, value)
	}
	return newParamsFromPathParams(pathParams, query, header, cookies, clientIP)
}

func newParamsFromPathParams(path pathParamList, query url.Values, header http.Header, cookies []*http.Cookie, clientIP string) Params {
	return Params{
		path: path.Clone(),
		state: &paramsState{
			header:        header.Clone(),
			query:         cloneQueryParams(query),
			queryParsed:   true,
			cookies:       cloneCookies(cookies),
			cookiesParsed: true,
			clientIP:      clientIP,
			clientIPSet:   true,
		},
	}
}

func paramsFromRequest(r *http.Request, c *Config) Params {
	return paramsFromRequestWithPathParams(r, c, pathParamList{})
}

func paramsFromRequestWithPathParams(r *http.Request, c *Config, routeParams pathParamList) Params {
	if r == nil {
		return Params{}
	}
	resolver := ClientIPResolver(defaultClientIPResolver)
	if c != nil && c.clientIPResolver != nil {
		resolver = c.clientIPResolver
	}
	p := Params{state: &paramsState{
		req:      r,
		header:   r.Header,
		resolver: resolver,
	}}
	if routeParams.Len() > 0 {
		p.path = routeParams
	} else if m := pathParams(r); len(m) > 0 {
		for key, value := range m {
			p.path.Add(key, value)
		}
	}
	return p
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

// queryValues parses the request query on first access and caches the result
// in the shared state, so repeated reads pay the parse only once.
func (p Params) queryValues() url.Values {
	s := p.state
	if s == nil {
		return nil
	}
	if !s.queryParsed {
		if s.req != nil {
			s.query = s.req.URL.Query()
		}
		s.queryParsed = true
	}
	return s.query
}

// cookieList parses the request cookies on first access and caches the result
// in the shared state.
func (p Params) cookieList() []*http.Cookie {
	s := p.state
	if s == nil {
		return nil
	}
	if !s.cookiesParsed {
		if s.req != nil {
			s.cookies = s.req.Cookies()
		}
		s.cookiesParsed = true
	}
	return s.cookies
}

// Detach returns an immutable snapshot of the params that no longer
// references the underlying request. The snapshot is safe to retain after the
// handler returns and to share across goroutines. The cost is the deep copy
// that lazy views avoid, paid only by callers that need it.
func (p Params) Detach() Params {
	s := p.state
	if s == nil {
		return Params{path: p.path.Clone()}
	}
	return Params{
		path: p.path.Clone(),
		state: &paramsState{
			header:        s.header.Clone(),
			query:         cloneQueryParams(p.queryValues()),
			queryParsed:   true,
			cookies:       cloneCookies(p.cookieList()),
			cookiesParsed: true,
			clientIP:      p.ClientIP(),
			clientIPSet:   true,
		},
	}
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
	values := p.queryValues()
	if values == nil {
		return ""
	}
	return values.Get(key)
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
	values := p.queryValues()[key]
	return append([]string(nil), values...)
}

// Header returns the first header value.
func (p Params) Header(key string) string {
	s := p.state
	if s == nil || s.header == nil {
		return ""
	}
	return s.header.Get(key)
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
	s := p.state
	if s == nil {
		return nil
	}
	values := s.header.Values(key)
	return append([]string(nil), values...)
}

// Cookie returns a request cookie value.
func (p Params) Cookie(key string) string {
	for _, cookie := range p.cookieList() {
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

// Cookies returns request cookies. The returned slice is a copy and may be
// mutated freely by the caller.
func (p Params) Cookies() []*http.Cookie {
	return cloneCookies(p.cookieList())
}

// ClientIP returns the client IP, resolved on first access and cached.
func (p Params) ClientIP() string {
	s := p.state
	if s == nil {
		return ""
	}
	if !s.clientIPSet {
		if s.resolver != nil && s.req != nil {
			s.clientIP = s.resolver(s.req)
		}
		s.clientIPSet = true
	}
	return s.clientIP
}
