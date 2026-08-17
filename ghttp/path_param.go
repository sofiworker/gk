package ghttp

import (
	"net/http"
	"net/url"
	"sync"
)

const maxStackPathParams = 16

type pathParam struct {
	Key   string
	Value string
}

type pathParamList struct {
	values   [maxStackPathParams]pathParam
	overflow []pathParam
	len      int
}

func (ps pathParamList) Get(key string) string {
	for i := 0; i < ps.len; i++ {
		if param := ps.values[i]; param.Key == key {
			return param.Value
		}
	}
	for _, param := range ps.overflow {
		if param.Key == key {
			return param.Value
		}
	}
	return ""
}

func (ps pathParamList) Len() int {
	return ps.len + len(ps.overflow)
}

func (ps pathParamList) Clone() pathParamList {
	cloned := pathParamList{len: ps.len}
	copy(cloned.values[:], ps.values[:])
	if len(ps.overflow) > 0 {
		cloned.overflow = append([]pathParam(nil), ps.overflow...)
	}
	return cloned
}

func (ps *pathParamList) Add(key, value string) {
	if ps.len < len(ps.values) {
		ps.values[ps.len] = pathParam{Key: key, Value: value}
		ps.len++
		return
	}
	ps.overflow = append(ps.overflow, pathParam{Key: key, Value: value})
}

func (ps *pathParamList) Reset() {
	for i := 0; i < ps.len; i++ {
		ps.values[i] = pathParam{}
	}
	ps.len = 0
	ps.overflow = ps.overflow[:0]
}

func (ps *pathParamList) Truncate(n int) {
	if n <= len(ps.values) {
		for i := n; i < ps.len; i++ {
			ps.values[i] = pathParam{}
		}
		ps.len = n
		ps.overflow = ps.overflow[:0]
		return
	}
	ps.len = len(ps.values)
	ps.overflow = ps.overflow[:n-len(ps.values)]
}

type pathParamHandler interface {
	http.Handler
	ServeHTTPWithPathParams(http.ResponseWriter, *http.Request, pathParamList)
}

// directPathValueHandler 接收已匹配的单个末尾路径参数，避免构造参数列表。
// directPathValueHandler receives a matched trailing path value without building a parameter list.
type directPathValueHandler interface {
	ServeHTTPWithPathValue(http.ResponseWriter, *http.Request, string)
}

type directPathValueHandlerFunc func(http.ResponseWriter, *http.Request, string)

func (f directPathValueHandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(w, r, "")
}

func (f directPathValueHandlerFunc) ServeHTTPWithPathParams(w http.ResponseWriter, r *http.Request, params pathParamList) {
	if params.Len() == 0 {
		f(w, r, "")
		return
	}
	f(w, r, params.values[0].Value)
}

func (f directPathValueHandlerFunc) ServeHTTPWithPathValue(w http.ResponseWriter, r *http.Request, value string) {
	f(w, r, value)
}

type pathParamHandlerFunc func(http.ResponseWriter, *http.Request, pathParamList)

func (f pathParamHandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(w, r, pathParamList{})
}

func (f pathParamHandlerFunc) ServeHTTPWithPathParams(w http.ResponseWriter, r *http.Request, params pathParamList) {
	f(w, r, params)
}

// stateIndependentTerminal 标记无需 requestState 的类型化终结器。
// stateIndependentTerminal marks a typed terminal that does not require
// requestState.
type stateIndependentTerminal interface {
	pathParamHandler
	stateIndependentTerminal()
}

type stateIndependentTerminalAdapter struct {
	pathParamHandler
	direct directPathValueHandler
}

func (a stateIndependentTerminalAdapter) directValueHandler() directPathValueHandler { return a.direct }

func (stateIndependentTerminalAdapter) stateIndependentTerminal() {}

func markStateIndependentTerminal(handler http.Handler) http.Handler {
	pathHandler, ok := handler.(pathParamHandler)
	if !ok {
		return handler
	}
	adapter := stateIndependentTerminalAdapter{pathParamHandler: pathHandler}
	if direct, ok := handler.(directPathValueHandler); ok {
		adapter.direct = direct
	}
	return adapter
}

// lazyPathParams 是路径参数的惰性视图:首次按 key 访问时才解码对应段并缓存。
// lazyPathParams is a lazy view of path params: a segment is decoded and cached
// only when its key is first accessed.
// 生命周期与单 goroutine 使用约定同 Params。
// lifetime and single-goroutine contract match Params.
type lazyPathParams struct {
	route *compiledRoute
	path  requestPath
}

// lazyPathParamsPool 复用惰性路径参数源,避免每个参数路由分配一次。
// lazyPathParamsPool reuses lazy path sources instead of allocating per request.
// 生命周期受 Params 的 handler 生命周期契约约束。
// lifetime is bounded by the Params handler-lifetime contract.
var lazyPathParamsPool = sync.Pool{New: func() any { return &lazyPathParams{} }}

// Get 返回并缓存 key 对应参数的解码值;不存在或解码失败返回空。
// Get decodes and caches the param for key; it returns empty when missing or invalid.
// 缓存放在调用方提供的 *pathParamList 上,保持 lazyPathParams 自身小而廉价。
// the cache lives on the caller-provided list, keeping lazyPathParams small.
func (l *lazyPathParams) get(key string, cache *pathParamList) string {
	if l == nil || l.route == nil {
		return ""
	}
	if value := cache.Get(key); value != "" {
		return value
	}
	segmentIndex, ok := l.route.paramPos[key]
	if !ok {
		return ""
	}
	segment := l.route.definition.pattern.segments[segmentIndex]
	raw := l.path.RawAt(segmentIndex)
	if segment.kind == routeSegmentCatchAll {
		raw = l.path.RawJoinFrom(segmentIndex)
	}
	value, err := url.PathUnescape(raw)
	if err != nil {
		return ""
	}
	cache.Add(key, value)
	return value
}

// materialize 解码全部参数并返回深拷贝快照(Detach 语义)。
// materialize decodes all params and returns a detached copy (Detach semantics).
func (l *lazyPathParams) materialize(cache *pathParamList) pathParamList {
	if l == nil || l.route == nil {
		return pathParamList{}
	}
	for _, segment := range l.route.definition.pattern.segments {
		if segment.kind == routeSegmentParameter || segment.kind == routeSegmentCatchAll {
			l.get(segment.value, cache)
		}
	}
	return cache.Clone()
}
