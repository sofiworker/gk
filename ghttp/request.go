package ghttp

import (
	"context"
	"net/http"
	"net/url"
	"sync"
)

// Request 是 HTTP 请求上下文。
// Request is the HTTP request context.
//
// 提供对原始 http.Request、路径参数、查询参数等的访问。
// Provides access to the original http.Request, path parameters, query parameters, etc.
type Request struct {
	// Raw 是原始的 http.Request
	// Raw is the original http.Request
	Raw *http.Request

	// Params 是路径参数（从 radix tree 匹配得到）
	// Params are path parameters (matched from radix tree)
	Params Params

	// matched 是匹配到的路由模板（用于日志和观测）
	// matched is the matched route template (for logging and observability)
	matched string

	// match 是完整的路由查找结果，由分发终端使用
	// match is the full lookup result, used by the dispatch terminal
	match routeMatch

	// badPath 标记请求路径未通过校验（dot 段等）
	// badPath marks that the request path failed validation (dot segments, etc.)
	badPath bool

	// query 是惰性解析并缓存的查询参数
	// query is the lazily parsed and cached query
	query     url.Values
	queryOnce sync.Once
}

// Context 返回请求的 context。
// Context returns the request's context.
func (r *Request) Context() context.Context {
	return r.Raw.Context()
}

// Route 返回匹配到的路由模板（如 "/users/:id"）；未匹配时返回空串。
// Route returns the matched route template (e.g. "/users/:id"); empty when unmatched.
func (r *Request) Route() string {
	return r.matched
}

// Query 返回查询参数。首次调用时解析并缓存，之后复用，避免每次访问都重新解析 RawQuery。
// 返回值为共享缓存，调用方不应修改。
// Query returns the query parameters, parsed once and cached so RawQuery is not re-parsed
// on every access. The returned map is shared and must not be modified.
func (r *Request) Query() url.Values {
	r.queryOnce.Do(func() { r.query = r.Raw.URL.Query() })
	return r.query
}

// Params 是路径参数的集合。
// Params is a collection of path parameters.
type Params []Param

// Param 是单个路径参数的键值对。
// Param is a key-value pair for a single path parameter.
type Param struct {
	Key   string
	Value string
}

// Get 返回指定名称的路径参数值。
// Get returns the path parameter value of the specified name.
func (ps Params) Get(name string) string {
	v, _ := ps.Lookup(name)
	return v
}

// Lookup 返回指定名称的路径参数值及其是否存在。
// Lookup returns the path parameter value of the specified name and whether it exists.
func (ps Params) Lookup(name string) (string, bool) {
	for _, p := range ps {
		if p.Key == name {
			return p.Value, true
		}
	}
	return "", false
}

// ByName 返回指定名称的路径参数值（兼容 Gin 风格）。
// ByName returns the path parameter value of the specified name (compatible with Gin style).
func (ps Params) ByName(name string) string {
	return ps.Get(name)
}
