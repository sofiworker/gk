package ghttp

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

// Params 是 path/query/header/cookie/client IP 的按请求视图。
// Params is a per-request view of path, query, header, cookie and client IP.
// 惰性读取底层请求：query/cookie/client IP 首次访问时解析并缓存。
// it reads lazily: query, cookies and client IP parse on first access.
// header 直接透读；无论 handler 读取多少输入，构造都近乎零分配。
// headers read through; construction is allocation-light regardless of usage.
//
// Params 视图仅在 handler 调用期间有效，且必须单 goroutine 使用。
// a Params view is valid for the handler lifetime and single-goroutine only.
// 需要在 handler 返回后保留或跨 goroutine 共享时，先调用 Detach。
// call Detach to retain values beyond the handler or share across goroutines.
//
// 零值是空的、只读安全的 Params。
// the zero value is an empty, read-safe Params.
type Params struct {
	path  pathParamList
	state *paramsState
}

// paramsState 持有请求引用与惰性缓存，供同一 Params 视图的副本共享。
// paramsState carries the request ref and lazily built caches.
// Detach 后的状态持有所有输入的深拷贝，不再引用请求。
// a detached state owns deep copies and holds no request.
type paramsState struct {
	req       *http.Request
	header    http.Header
	resolver  ClientIPResolver
	lazyPath  *lazyPathParams
	pathCache *pathParamList

	query         url.Values
	queryParsed   bool
	cookies       []*http.Cookie
	cookiesParsed bool
	clientIP      string
	clientIPSet   bool
}

// newParams 从显式值构建不可变快照（Detach 语义）。
// newParams builds an immutable snapshot from explicit values.
// 输入深拷贝，后续修改参数不影响返回的 Params。
// inputs are deep-copied; later mutation does not affect the result.
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
	}
	// 匹配后惰性参数源挂在 requestState 上;这里引用它,Params.Path 走惰性解码。
	// the lazy source is stored on requestState after matching; reference it here
	// so Params.Path resolves through lazy decoding.
	if reqState := requestStateFromRequest(r); reqState != nil && reqState.matched != nil {
		p.state.lazyPath = reqState.matched
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

// queryValues 首次访问时解析请求 query 并缓存。
// queryValues parses the query on first access and caches it.
// 重复读取只付一次解析成本。
// repeated reads parse only once.
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

// cookieList 首次访问时解析请求 cookie 并缓存。
// cookieList parses cookies on first access and caches them.
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

// Detach 返回不再引用底层请求的不可变快照。
// Detach returns an immutable snapshot that no longer references the request.
// 快照可在 handler 返回后保留并跨 goroutine 共享。
// the snapshot is safe to retain and share across goroutines.
// 代价是惰性视图避免的深拷贝，仅由需要它的调用方支付。
// the cost is the deep copy lazy views avoid.
func (p Params) Detach() Params {
	s := p.state
	if s == nil {
		return Params{path: p.path.Clone()}
	}
	var path pathParamList
	if s.lazyPath != nil {
		if s.pathCache == nil {
			s.pathCache = new(pathParamList)
		}
		path = s.lazyPath.materialize(s.pathCache)
	} else {
		path = p.path.Clone()
	}
	return Params{
		path: path,
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

// Path 返回路径参数值。
// Path returns a path parameter value.
func (p Params) Path(key string) string {
	if value := p.path.Get(key); value != "" {
		return value
	}
	if p.state != nil && p.state.lazyPath != nil {
		if p.state.pathCache == nil {
			p.state.pathCache = new(pathParamList)
		}
		return p.state.lazyPath.get(key, p.state.pathCache)
	}
	return ""
}

// DefaultPath 返回路径参数值，为空时返回 defaultValue。
// DefaultPath returns the value or defaultValue when empty.
func (p Params) DefaultPath(key, defaultValue string) string {
	value := p.Path(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// PathInt 将路径参数解析为 int。
// PathInt parses a path parameter as int.
func (p Params) PathInt(key string) (int, error) {
	return strconv.Atoi(p.Path(key))
}

// PathIntDefault 将路径参数解析为 int，为空或非法时返回 defaultValue。
// PathIntDefault parses as int or returns defaultValue when empty/invalid.
func (p Params) PathIntDefault(key string, defaultValue int) int {
	value := p.Path(key)
	if value == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return n
}

// PathBool 将路径参数解析为 bool。
// PathBool parses a path parameter as bool.
func (p Params) PathBool(key string) (bool, error) {
	return strconv.ParseBool(p.Path(key))
}

// PathBoolDefault 将路径参数解析为 bool，为空或非法时返回 defaultValue。
// PathBoolDefault parses as bool or returns defaultValue when empty/invalid.
func (p Params) PathBoolDefault(key string, defaultValue bool) bool {
	value := p.Path(key)
	if value == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return b
}

// Query 返回第一个 query 参数值。
// Query returns the first query parameter value.
func (p Params) Query(key string) string {
	values := p.queryValues()
	if values == nil {
		return ""
	}
	return values.Get(key)
}

// DefaultQuery 返回第一个 query 参数值，为空时返回 defaultValue。
// DefaultQuery returns the first value or defaultValue when empty.
func (p Params) DefaultQuery(key, defaultValue string) string {
	value := p.Query(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// QueryInt 将第一个 query 参数解析为 int。
// QueryInt parses the first query parameter as int.
func (p Params) QueryInt(key string) (int, error) {
	return strconv.Atoi(p.Query(key))
}

// QueryIntDefault 将第一个 query 参数解析为 int，为空或非法时返回 defaultValue。
// QueryIntDefault parses as int or returns defaultValue when empty/invalid.
func (p Params) QueryIntDefault(key string, defaultValue int) int {
	value := p.Query(key)
	if value == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return n
}

// QueryBool 将第一个 query 参数解析为 bool。
// QueryBool parses the first query parameter as bool.
func (p Params) QueryBool(key string) (bool, error) {
	return strconv.ParseBool(p.Query(key))
}

// QueryBoolDefault 将第一个 query 参数解析为 bool，为空或非法时返回 defaultValue。
// QueryBoolDefault parses as bool or returns defaultValue when empty/invalid.
func (p Params) QueryBoolDefault(key string, defaultValue bool) bool {
	value := p.Query(key)
	if value == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return b
}

// QueryList 返回全部 query 参数值。
// QueryList returns all query parameter values.
func (p Params) QueryList(key string) []string {
	values := p.queryValues()[key]
	return append([]string(nil), values...)
}

// Header 返回第一个 header 值。
// Header returns the first header value.
func (p Params) Header(key string) string {
	s := p.state
	if s == nil || s.header == nil {
		return ""
	}
	return s.header.Get(key)
}

// DefaultHeader 返回第一个 header 值，为空时返回 defaultValue。
// DefaultHeader returns the first value or defaultValue when empty.
func (p Params) DefaultHeader(key, defaultValue string) string {
	value := p.Header(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// HeaderList 返回全部 header 值。
// HeaderList returns all header values.
func (p Params) HeaderList(key string) []string {
	s := p.state
	if s == nil {
		return nil
	}
	values := s.header.Values(key)
	return append([]string(nil), values...)
}

// Cookie 返回请求 cookie 值。
// Cookie returns a request cookie value.
func (p Params) Cookie(key string) string {
	for _, cookie := range p.cookieList() {
		if cookie != nil && cookie.Name == key {
			return cookie.Value
		}
	}
	return ""
}

// DefaultCookie 返回请求 cookie 值，为空时返回 defaultValue。
// DefaultCookie returns the cookie value or defaultValue when empty.
func (p Params) DefaultCookie(key, defaultValue string) string {
	value := p.Cookie(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Cookies 返回请求 cookies；返回的切片是副本，可自由修改。
// Cookies returns a copy of request cookies; the slice may be mutated freely.
func (p Params) Cookies() []*http.Cookie {
	return cloneCookies(p.cookieList())
}

// ClientIP 返回客户端 IP，首次访问时解析并缓存。
// ClientIP returns the client IP, resolved and cached on first access.
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

// Request 返回底层 *http.Request。
// Request returns the underlying *http.Request.
// 零值与 Detach 后的视图返回 nil。
// the zero value and detached views return nil.
func (p Params) Request() *http.Request {
	if p.state == nil {
		return nil
	}
	return p.state.req
}

// Method 返回请求方法。
// Method returns the request method.
func (p Params) Method() string {
	r := p.Request()
	if r == nil {
		return ""
	}
	return r.Method
}

// URL 返回请求 URL。
// URL returns the request URL.
func (p Params) URL() *url.URL {
	r := p.Request()
	if r == nil {
		return nil
	}
	return r.URL
}

// ContentType 返回原始 Content-Type 头值,不做媒体类型归一化。
// ContentType returns the raw Content-Type header value without normalization.
func (p Params) ContentType() string {
	r := p.Request()
	if r == nil {
		return ""
	}
	return r.Header.Get("Content-Type")
}

// RawBody 返回请求体原始字节;首次访问读流并缓存,与 RawBody(r) 及
// Body[T].Raw/Decode 共享同一份。Detach 后的视图不持有 body,返回 (nil, nil)。
// RawBody returns the raw body bytes, buffered on first access and shared with
// RawBody(r) and Body[T].Raw/Decode. Detached views hold no body and return
// (nil, nil).
func (p Params) RawBody() ([]byte, error) {
	r := p.Request()
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	if st := requestStateFromRequest(r); st != nil && st.body != nil {
		return st.body.bytes()
	}
	return io.ReadAll(r.Body)
}

// Form 返回合并 query 与表单体的第一个值(静默忽略解析错误)。
// Form returns the first query-merged form value (parse errors are silent).
func (p Params) Form(key string) string {
	values, _ := p.FormValues()
	if values == nil {
		return ""
	}
	return values.Get(key)
}

// PostForm 返回仅来自请求体的表单值(静默忽略解析错误)。
// PostForm returns a body-only form value (parse errors are silent).
func (p Params) PostForm(key string) string {
	values, _ := p.PostFormValues()
	if values == nil {
		return ""
	}
	return values.Get(key)
}

// FormValues 返回合并 query 与表单体的缓存视图。
// FormValues returns the cached query-merged form view.
func (p Params) FormValues() (url.Values, error) {
	form, _, err := formValuesFromRequest(p.Request())
	return form, err
}

// PostFormValues 返回仅来自请求体的缓存视图。
// PostFormValues returns the cached body-only form view.
func (p Params) PostFormValues() (url.Values, error) {
	_, post, err := formValuesFromRequest(p.Request())
	return post, err
}

// MultipartForm 解析并缓存 multipart 表单,见 MultipartForm(r, maxMemory)。
// MultipartForm parses and caches the multipart form; see MultipartForm(r, maxMemory).
func (p Params) MultipartForm(maxMemory int64) (*multipart.Form, error) {
	return MultipartForm(p.Request(), maxMemory)
}
