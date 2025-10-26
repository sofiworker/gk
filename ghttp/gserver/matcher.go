package gserver

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

type Param struct {
	Key   string
	Value string
}

type MatchResult struct {
	Path        string        `json:"path"`
	Handlers    []HandlerFunc `json:"-"`
	Params      []Param       // 比 map[string]string 更友好给 GC
	QueryValues url.Values    // 可为空；保留原多值语义
}

// GetParam 提供统一访问接口，避免上层反复线性遍历。
func (mr *MatchResult) GetParam(key string) (string, bool) {
	for i := range mr.Params {
		if mr.Params[i].Key == key {
			return mr.Params[i].Value, true
		}
	}
	return "", false
}

func (mr *MatchResult) appendParams(newParams url.Values) {
	if newParams == nil || len(newParams) == 0 {
		return
	}
	for k, arr := range newParams {
		if len(arr) == 0 {
			continue
		}
		val := arr[len(arr)-1] // 取最后一个值，语义和我们之前保持一致

		// 先检查是否存在同名
		replaced := false
		for i := range mr.Params {
			if mr.Params[i].Key == k {
				mr.Params[i].Value = val
				replaced = true
				break
			}
		}
		if !replaced {
			mr.Params = append(mr.Params, Param{Key: k, Value: val})
		}
	}
}

type MatcherStats struct {
	TotalRequests    uint64
	MatchHits        uint64
	MatchMisses      uint64
	TotalMatchTimeNs uint64
	MemoryUsageBytes uint64
	RoutesCount      int
}

type RouteInfo struct {
	Method      string
	Pattern     string
	NumHandlers int
}

// Matcher 核心接口 - 负责路由匹配
type Matcher interface {
	Match(method, path string) *MatchResult
	AddRoute(method, path string, handler ...HandlerFunc) error
	RemoveRoute(method, path string) error
	ListRoutes() []*RouteInfo
	Stats() *MatcherStats
}

func newServerMatcher() Matcher {
	s := &serverMatcher{
		methodMatcher: make(map[string]*MethodMatcher),
		stats:         &MatcherStats{},
	}
	return s
}

type serverMatcher struct {
	methodMatcher map[string]*MethodMatcher
	mu            sync.RWMutex
	stats         *MatcherStats
}

type routeEntry struct {
	path       string
	handlers   []HandlerFunc
	paramNames []string
}

func newRouteEntry(path string, handlers []HandlerFunc) *routeEntry {
	checkPathValid(path)
	return &routeEntry{
		path:       path,
		handlers:   handlers,
		paramNames: extractParamNames(path),
	}
}

//func (r *routeEntry) toResult(params map[string]string) *MatchResult {
//	var paramCopy map[string]string
//	if len(params) > 0 {
//		paramCopy = make(map[string]string, len(params))
//		for k, v := range params {
//			paramCopy[k] = v
//		}
//	}
//	return &MatchResult{
//		Path:     r.path,
//		Handlers: r.handlers,
//		Params:   paramCopy,
//	}
//}

func (r *routeEntry) toResult() *MatchResult {
	return &MatchResult{
		Path:     r.path,
		Handlers: r.handlers,
	}
}

func extractParamNames(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	segments := strings.Split(trimmed, "/")
	var names []string
	for _, segment := range segments {
		if len(segment) == 0 {
			continue
		}
		switch segment[0] {
		case ':', '*':
			names = append(names, segment[1:])
		}
	}
	return names
}

func splitPathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func (s *serverMatcher) Match(method, path string) *MatchResult {
	s.mu.RLock()
	methodRouter := s.methodMatcher[method]
	s.mu.RUnlock()
	if methodRouter == nil {
		return nil
	}
	result := methodRouter.match(path)
	return result
}

func (s *serverMatcher) AddRoute(method, path string, handler ...HandlerFunc) error {
	s.mu.Lock()
	methodMatcher := s.methodMatcher[method]
	if methodMatcher == nil {
		methodMatcher = newMethodMatcher()
		s.methodMatcher[method] = methodMatcher
	}
	s.mu.Unlock()
	return methodMatcher.addRoute(path, handler...)
}

func (s *serverMatcher) RemoveRoute(method, path string) error {
	s.mu.RLock()
	methodRouter := s.methodMatcher[method]
	s.mu.RUnlock()
	if methodRouter == nil {
		return fmt.Errorf("no routes for %s", method)
	}
	return methodRouter.removeRoute(path)
}

func (s *serverMatcher) Stats() *MatcherStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var routeCount int
	for _, mm := range s.methodMatcher {
		mm.mu.RLock()
		routeCount += len(mm.staticGroup)
		routeCount += mm.segmentIndexRouteCount()
		routeCount += mm.radixRouteCount()
		mm.mu.RUnlock()
	}

	return &MatcherStats{
		//TotalRequests:    atomic.LoadUint64(&s.totalRequests),
		//MatchHits:        atomic.LoadUint64(&s.matchHits),
		//MatchMisses:      atomic.LoadUint64(&s.matchMisses),
		//TotalMatchTimeNs: atomic.LoadUint64(&s.totalMatchTimeNs),
		MemoryUsageBytes: 0, // 这里可以在未来加内存估算
		RoutesCount:      routeCount,
	}
}

func (s *serverMatcher) ListRoutes() []*RouteInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*RouteInfo, 0, 32)
	for method, mm := range s.methodMatcher {
		mm.mu.RLock()
		// 静态路由
		for pat, e := range mm.staticGroup {
			out = append(out, &RouteInfo{
				Method:      method,
				Pattern:     pat,
				NumHandlers: len(e.handlers),
			})
		}
		// segmentIndex 和 radixTree 的遍历我们委托内部 helper
		out = append(out, mm.segmentIndexRoutes(method)...)
		out = append(out, mm.radixRoutes(method)...)
		mm.mu.RUnlock()
	}
	return out
}
