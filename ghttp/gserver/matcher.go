package gserver

import (
	"fmt"
	"strings"
	"sync"
)

type MatchResult struct {
	Handlers  []HandlerFunc
	RoutePath string
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
		methodRouters: make(map[string]*MethodMatcher),
		stats:         &MatcherStats{},
	}
	s.matchPool.New = func() interface{} {
		return &MatchResult{}
	}
	return s
}

type serverMatcher struct {
	methodRouters map[string]*MethodMatcher
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
	path    string
	handler []HandlerFunc
	node    *CompressedRadixNode
	feature PathFeature
}

func (s *serverMatcher) Match(method, path string) *MatchResult {
	s.mu.RLock()
	methodRouter := s.methodRouters[method]
	s.mu.RUnlock()
	if methodRouter == nil {
		return nil
	}
	result := methodRouter.match(path)
	return result
}

// MethodMatcher 方法
func (mr *MethodMatcher) match(path string) *MatchResult {
	if group := mr.lengthIndex[len(path)]; group == nil || group.empty() {
		return nil
	}

	segments := strings.Split(path, "/")
	if group := mr.segmentIndex[len(segments)]; group == nil || group.empty() {
		return nil
	}

	return mr.radixTree.search(path)
}

func (s *serverMatcher) AddRoute(method, path string, handler ...HandlerFunc) error {
	s.mu.Lock()
	methodRouter := s.methodRouters[method]
	if methodRouter == nil {
		methodRouter = newMethodMatcher()
		s.methodRouters[method] = methodRouter
	}
	s.mu.Unlock()
	return methodRouter.addRoute(path, handler...)
}

func (s *serverMatcher) RemoveRoute(method, path string) error {
	s.mu.RLock()
	methodRouter := s.methodRouters[method]
	s.mu.RUnlock()
	if methodRouter == nil {
		return fmt.Errorf("no routes for %s", method)
	}
	return methodRouter.removeRoute(path)
}

func (s *serverMatcher) Stats() *MatcherStats {
	return s.stats
}
