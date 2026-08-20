package ghttp

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// maxStackPathParams 是路径参数列表的栈内槽位:超过 4 个参数的罕见路由改用
// 堆切片。较小的内联数组让 operationRequest 按值携带参数列表时拷贝更少。
// maxStackPathParams is the inline slot count of the path param list; rare
// routes with more than 4 params fall back to a heap slice. The smaller
// inline array shrinks the by-value copy inside operationRequest.
const maxStackPathParams = 4

// maxStackPathSegments 是请求路径段收集的栈内槽位:匹配期间记录全部段
// (含静态段)的字节偏移,常见路径深度都能落在栈内。
// maxStackPathSegments is the inline slot count for request path segments:
// matching records byte offsets of every segment (static included), so common
// path depths stay on the stack.
const maxStackPathSegments = 16

type pathParam struct {
	Key   string
	Value string
}

type pathParamList struct {
	values   [maxStackPathParams]pathParam
	overflow []pathParam
	len      int
	// lastHit 是顺序读游标:映射器(mapper)通常按槽位顺序读参,从上次命中
	// 位置起扫,把 N 次读的 O(N²) 全扫降为近似 O(N)。键唯一保证正确性:
	// 游标段未命中时回退从头全扫。值拷贝会带上游标,不影响语义。
	// lastHit is a sequential-read cursor: mappers usually read params in
	// slot order, so scanning from the previous hit turns O(N²) full scans
	// into ~O(N). Keys are unique, so correctness is preserved: a cursor
	// segment miss falls back to a full scan. Value copies carry the cursor
	// without changing semantics.
	lastHit int
}

func (ps *pathParamList) Get(key string) string {
	if ps.lastHit > 0 && ps.lastHit < ps.len {
		for i := ps.lastHit; i < ps.len; i++ {
			if param := ps.values[i]; param.Key == key {
				ps.lastHit = i + 1
				return param.Value
			}
		}
	}
	for i := 0; i < ps.len; i++ {
		if param := ps.values[i]; param.Key == key {
			ps.lastHit = i + 1
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
	ps.lastHit = 0
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

type pathParamHandlerFunc func(http.ResponseWriter, *http.Request, pathParamList)

func (f pathParamHandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(w, r, pathParamList{})
}

func (f pathParamHandlerFunc) ServeHTTPWithPathParams(w http.ResponseWriter, r *http.Request, params pathParamList) {
	f(w, r, params)
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

// getOnce 解码 key 对应的参数但不写缓存:stateIndependent 直编路径使用,
// 避免对栈上 params 取址导致逃逸。重复访问会重复解码,结果一致。
// getOnce decodes the param for key without caching: used by the
// stateIndependent direct path to avoid taking the address of stack params.
// Repeated access decodes again with identical results.
func (l *lazyPathParams) getOnce(key string) string {
	if l == nil || l.route == nil {
		return ""
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
	if strings.IndexByte(raw, '%') < 0 {
		return raw
	}
	value, err := url.PathUnescape(raw)
	if err != nil {
		return ""
	}
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
