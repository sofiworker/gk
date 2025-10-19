package gserver

import (
	"fmt"
	"strings"
	"sync"
)

type MatchResult struct {
	Path     string            `json:"path"`
	Handlers []HandlerFunc     `json:"-"`
	Params   map[string]string `json:"params"`
}

type MatcherStats struct {
	TotalRequests    uint64
	MatchHits        uint64
	MatchMisses      uint64
	AvgMatchTimeNs   uint64
	MemoryUsageBytes uint64
	RoutesCount      int
}

// Matcher 核心接口 - 负责路由匹配
type Matcher interface {
	Match(method, path string) *MatchResult
	AddRoute(method, path string, handler ...HandlerFunc) error
	RemoveRoute(method, path string) error
	Stats() *MatcherStats
}

func newServerMatcher() Matcher {
	s := &serverMatcher{
		methodMatcher: make(map[string]*MethodMatcher),
		stats:         &MatcherStats{},
	}
	s.matchPool.New = func() interface{} {
		return &MatchResult{}
	}
	return s
}

type serverMatcher struct {
	methodMatcher map[string]*MethodMatcher
	matchPool     sync.Pool
	mu            sync.RWMutex
	stats         *MatcherStats
}

type routeGroup struct {
	routes []*routeEntry
}

func (rg *routeGroup) addEntry(e *routeEntry) {
	rg.routes = append(rg.routes, e)
}

func (rg *routeGroup) removePath(path string) {
	out := rg.routes[:0]
	for _, r := range rg.routes {
		if r.path != path {
			out = append(out, r)
		}
	}
	rg.routes = out
}

func (rg *routeGroup) empty() bool {
	return len(rg.routes) == 0
}

func (rg *routeGroup) routesCopy() []*routeEntry {
	cp := make([]*routeEntry, len(rg.routes))
	copy(cp, rg.routes)
	return cp
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

func (r *routeEntry) toResult(params map[string]string) *MatchResult {
	var paramCopy map[string]string
	if len(params) > 0 {
		paramCopy = make(map[string]string, len(params))
		for k, v := range params {
			paramCopy[k] = v
		}
	}
	return &MatchResult{
		Path:     r.path,
		Handlers: r.handlers,
		Params:   paramCopy,
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
	return s.stats
}
