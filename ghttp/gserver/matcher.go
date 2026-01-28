package gserver

import (
	"fmt"
	"strings"
	"sync"
)

type MatchResult struct {
	Path       string            `json:"path"`
	Handlers   []HandlerFunc     `json:"-"`
	PathParams map[string]string `json:"path_params"`
}

type MatcherStats struct {
	TotalRequests    uint64
	MatchHits        uint64
	MatchMisses      uint64
	AvgMatchTimeNs   uint64
	MemoryUsageBytes uint64
	RoutesCount      int
}

// Match 核心接口 - 负责路由匹配
type Match interface {
	Lookup(method, path string) *MatchResult
	AddRoute(method, path string, handler ...HandlerFunc) error
	RemoveRoute(method, path string) error
	Stats() *MatcherStats
}

type serverMatcher struct {
	methodMatcher map[string]*MethodMatcher
	matchPool     sync.Pool
	mu            sync.RWMutex
	stats         *MatcherStats
}

func newServerMatcher() Match {
	s := &serverMatcher{
		methodMatcher: make(map[string]*MethodMatcher),
		stats:         &MatcherStats{},
	}
	s.matchPool.New = func() interface{} {
		return &MatchResult{}
	}
	return s
}

type routeEntry struct {
	path       string
	handlers   []HandlerFunc
	paramNames []string
}

func newRouteEntry(path string, handlers []HandlerFunc) *routeEntry {
	return &routeEntry{
		path:       path,
		handlers:   handlers,
		paramNames: extractParamNames(path),
	}
}

func (r *routeEntry) toResult(params map[string]string) *MatchResult {
	return &MatchResult{
		Path:       r.path,
		Handlers:   r.handlers,
		PathParams: params,
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

func (s *serverMatcher) Lookup(method, path string) *MatchResult {
	s.mu.RLock()
	methodRouter := s.methodMatcher[method]
	s.mu.RUnlock()
	if methodRouter == nil {
		return nil
	}
	result := methodRouter.match(path)
	return result
}

// LookupInto is a fast-path lookup that writes path params into dstParams.
// It avoids per-request map allocations by reusing the provided map.
func (s *serverMatcher) LookupInto(method, path string, dstParams map[string]string) (handlers []HandlerFunc, fullPath string, ok bool) {
	s.mu.RLock()
	methodRouter := s.methodMatcher[method]
	s.mu.RUnlock()
	if methodRouter == nil {
		return nil, "", false
	}
	entry := methodRouter.matchInto(path, dstParams)
	if entry == nil {
		return nil, "", false
	}
	return entry.handlers, entry.path, true
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
	return s.stats
}
