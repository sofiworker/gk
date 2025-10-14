package gserver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

// Matcher 核心接口 - 负责路由匹配
type Matcher interface {
	Match(method, path string) *MatchResult
	AddRoute(method, path string, handler fasthttp.RequestHandler, middleware ...Middleware) error
	AddRoutes(routes []Route) error
	RemoveRoute(method, path string) error
	Routes() []RouteInfo
	Stats() MatcherStats
	Optimize() error
	ResetStats()
}

// 匹配结果
type MatchResult struct {
	Handler    fasthttp.RequestHandler
	Middleware []Middleware
	Params     map[string]string
	RoutePath  string
}

// 路由定义
type Route struct {
	Method     string
	Path       string
	Handler    fasthttp.RequestHandler
	Middleware []Middleware
}

// 路由信息 (只读)
type RouteInfo struct {
	Method     string
	Path       string
	Handler    string
	Middleware []string
}

// 中间件类型
type Middleware func(fasthttp.RequestHandler) fasthttp.RequestHandler

// 匹配器统计
type MatcherStats struct {
	TotalRequests    uint64
	MatchHits        uint64
	MatchMisses      uint64
	AvgMatchTimeNs   uint64
	MemoryUsageBytes uint64
	RoutesCount      int
}

// 上下文键类型
type contextKey string

const ParamsKey contextKey = "routeParams"

// MatcherConfig
type MatcherConfig struct {
	EnableLengthIndex  bool
	EnableSegmentIndex bool
	//EnableFeatureIndex bool
	MaxRoutesPerMethod int
	PrecomputeFeatures bool
	CaseSensitive      bool
}

// 默认配置
var DefaultMatcherConfig = MatcherConfig{
	EnableLengthIndex:  true,
	EnableSegmentIndex: true,
	//EnableFeatureIndex: true,
	MaxRoutesPerMethod: 10000,
	PrecomputeFeatures: true,
	CaseSensitive:      true,
}

// OptimizedMatcher 实现
type OptimizedMatcher struct {
	methodRouters    map[string]*MethodRouter
	globalMiddleware []Middleware
	matchPool        sync.Pool
	paramPool        sync.Pool
	stats            MatcherStats
	mu               sync.RWMutex
	config           MatcherConfig
}

// MethodRouter 实现（合并后的）
type MethodRouter struct {
	lengthIndex  map[int]*routeGroup
	segmentIndex map[int]*routeGroup
	//featureIndex *FeatureIndex
	radixTree   *CompressedRadixTree
	staticCache map[string]*MatchResult
	stats       MatchStats
	config      MatcherConfig
	mu          sync.RWMutex
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
	handler    fasthttp.RequestHandler
	middleware []Middleware
	feature    PathFeature
	node       *CompressedRadixNode
}

// 特征索引类型
type PathFeature struct {
	length     int
	segmentCnt int
	staticBits uint64
	paramBits  uint64
	hash       uint32
}

type InvertedIndexItem struct {
	path     string
	treeNode *CompressedRadixNode
	feature  PathFeature
}

func removeDuplicatesItems(items []*InvertedIndexItem) []*InvertedIndexItem {
	seen := make(map[string]bool)
	res := make([]*InvertedIndexItem, 0, len(items))
	for _, it := range items {
		if !seen[it.path] {
			seen[it.path] = true
			res = append(res, it)
		}
	}
	return res
}

func longestCommonPrefix(a, b string) int {
	i := 0
	for ; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}

// 提取路径特征
func extractPathFeatures(path string) PathFeature {
	segments := strings.Split(path, "/")
	var feature PathFeature
	feature.length = len(path)
	feature.segmentCnt = len(segments)

	for i, seg := range segments {
		if len(seg) == 0 {
			continue
		}
		if seg[0] == ':' || seg[0] == '*' {
			feature.paramBits |= 1 << uint(i)
		} else {
			feature.staticBits |= 1 << uint(i)
		}
	}

	// FNV-1a
	hash := uint32(2166136261)
	for _, b := range []byte(path) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	feature.hash = hash
	return feature
}

// 构造函数与 Matcher 实现
func NewMatcher() Matcher {
	return NewMatcherWithConfig(DefaultMatcherConfig)
}

func NewMatcherWithConfig(config MatcherConfig) Matcher {
	m := &OptimizedMatcher{
		methodRouters: make(map[string]*MethodRouter),
		config:        config,
	}
	m.matchPool.New = func() interface{} {
		return &MatchResult{Params: make(map[string]string, 4)}
	}
	m.paramPool.New = func() interface{} {
		return make(map[string]string, 8)
	}
	return m
}

func (m *OptimizedMatcher) Match(method, path string) *MatchResult {
	start := time.Now()
	//method = strings.ToUpper(method)
	if !m.config.CaseSensitive {
		path = strings.ToLower(path)
	}
	m.mu.RLock()
	methodRouter := m.methodRouters[method]
	m.mu.RUnlock()
	if methodRouter == nil {
		m.recordMiss()
		return nil
	}
	result := methodRouter.match(path)
	elapsed := time.Since(start).Nanoseconds()
	m.recordMatch(result != nil, elapsed)
	return result
}

func (m *OptimizedMatcher) AddRoute(method, path string, handler fasthttp.RequestHandler, middleware ...Middleware) error {
	//method = strings.ToUpper(method)
	m.mu.Lock()
	methodRouter := m.methodRouters[method]
	if methodRouter == nil {
		methodRouter = m.newMethodRouter()
		m.methodRouters[method] = methodRouter
	}
	m.mu.Unlock()
	return methodRouter.addRoute(path, handler, middleware...)
}

func (m *OptimizedMatcher) AddRoutes(routes []Route) error {
	routesByMethod := make(map[string][]Route)
	for _, r := range routes {
		method := strings.ToUpper(r.Method)
		routesByMethod[method] = append(routesByMethod[method], r)
	}
	for method, rs := range routesByMethod {
		m.mu.Lock()
		methodRouter := m.methodRouters[method]
		if methodRouter == nil {
			methodRouter = m.newMethodRouter()
			m.methodRouters[method] = methodRouter
		}
		m.mu.Unlock()
		for _, r := range rs {
			if err := methodRouter.addRoute(r.Path, r.Handler, r.Middleware...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *OptimizedMatcher) RemoveRoute(method, path string) error {
	method = strings.ToUpper(method)
	m.mu.RLock()
	methodRouter := m.methodRouters[method]
	m.mu.RUnlock()
	if methodRouter == nil {
		return fmt.Errorf("no routes for %s", method)
	}
	return methodRouter.removeRoute(path)
}

func (m *OptimizedMatcher) Routes() []RouteInfo {
	var routes []RouteInfo
	m.mu.RLock()
	defer m.mu.RUnlock()
	for method, mr := range m.methodRouters {
		for _, r := range mr.routes() {
			routes = append(routes, RouteInfo{
				Method:     method,
				Path:       r.path,
				Handler:    getHandlerName(r.handler),
				Middleware: getMiddlewareNames(r.middleware),
			})
		}
	}
	return routes
}

func (m *OptimizedMatcher) Stats() MatcherStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.stats
	s.RoutesCount = m.totalRoutes()
	s.MemoryUsageBytes = m.estimateMemoryUsage()
	return s
}

func (m *OptimizedMatcher) Optimize() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, mr := range m.methodRouters {
		if err := mr.optimize(); err != nil {
			return err
		}
	}
	return nil
}

func (m *OptimizedMatcher) ResetStats() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats = MatcherStats{}
}

func (m *OptimizedMatcher) recordMatch(hit bool, elapsed int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats.TotalRequests++
	if hit {
		m.stats.MatchHits++
	} else {
		m.stats.MatchMisses++
	}
	if m.stats.AvgMatchTimeNs == 0 {
		m.stats.AvgMatchTimeNs = uint64(elapsed)
	} else {
		m.stats.AvgMatchTimeNs = uint64(0.9*float64(m.stats.AvgMatchTimeNs) + 0.1*float64(elapsed))
	}
}

func (m *OptimizedMatcher) recordMiss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats.TotalRequests++
	m.stats.MatchMisses++
}

func (m *OptimizedMatcher) estimateMemoryUsage() uint64 {
	var total uint64
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, mr := range m.methodRouters {
		if mr == nil {
			continue
		}
		total += uint64(len(mr.lengthIndex) * 16)
		total += uint64(len(mr.segmentIndex) * 16)
		if mr.radixTree != nil {
			total += mr.radixTree.estimateMemory()
		}
	}
	return total
}

func getHandlerName(handler fasthttp.RequestHandler) string {
	return "handler"
}

func getMiddlewareNames(middleware []Middleware) []string {
	names := make([]string, len(middleware))
	for i := range middleware {
		names[i] = "middleware"
	}
	return names
}

func (m *OptimizedMatcher) newMethodRouter() *MethodRouter {
	return &MethodRouter{
		lengthIndex:  make(map[int]*routeGroup),
		segmentIndex: make(map[int]*routeGroup),
		radixTree:    newCompressedRadixTree(),
		staticCache:  make(map[string]*MatchResult),
		config:       m.config,
	}
}

func (m *OptimizedMatcher) totalRoutes() int {
	total := 0
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, mr := range m.methodRouters {
		if mr != nil && mr.radixTree != nil {
			total += mr.radixTree.Size()
		}
	}
	return total
}

func (crt *CompressedRadixTree) Size() int {
	if crt == nil {
		return 0
	}
	return crt.size
}

// MethodRouter 方法
func (mr *MethodRouter) match(path string) *MatchResult {
	mr.mu.Lock()
	mr.stats.totalRequests++
	mr.mu.Unlock()

	// length filter
	if mr.config.EnableLengthIndex {
		if group := mr.lengthIndex[len(path)]; group == nil || group.empty() {
			return nil
		}
	}
	// segment filter
	if mr.config.EnableSegmentIndex {
		segments := strings.Split(path, "/")
		if group := mr.segmentIndex[len(segments)]; group == nil || group.empty() {
			return nil
		}
	}

	// final precise match
	return mr.radixTree.search(path)
}

func (mr *MethodRouter) addRoute(path string, handler fasthttp.RequestHandler, middleware ...Middleware) error {
	feature := extractPathFeatures(path)
	entry := &routeEntry{
		path:       path,
		handler:    handler,
		middleware: middleware,
		feature:    feature,
	}
	if mr.config.EnableLengthIndex {
		mr.addToLengthIndex(feature.length, entry)
	}
	if mr.config.EnableSegmentIndex {
		mr.addToSegmentIndex(feature.segmentCnt, entry)
	}
	if mr.radixTree == nil {
		mr.radixTree = newCompressedRadixTree()
	}
	return mr.radixTree.insert(path, handler, middleware...)
}

func (mr *MethodRouter) removeRoute(path string) error {
	feature := extractPathFeatures(path)
	if mr.config.EnableLengthIndex {
		mr.removeFromLengthIndex(feature.length, path)
	}
	if mr.config.EnableSegmentIndex {
		mr.removeFromSegmentIndex(feature.segmentCnt, path)
	}
	if mr.radixTree != nil {
		return mr.radixTree.remove(path)
	}
	return nil
}

func (mr *MethodRouter) routes() []*routeEntry {
	var out []*routeEntry
	seen := make(map[string]bool)
	for _, g := range mr.lengthIndex {
		for _, r := range g.routes {
			if !seen[r.path] {
				out = append(out, r)
				seen[r.path] = true
			}
		}
	}
	// fallback: if none, try segment index
	for _, g := range mr.segmentIndex {
		for _, r := range g.routes {
			if !seen[r.path] {
				out = append(out, r)
				seen[r.path] = true
			}
		}
	}
	return out
}

func (mr *MethodRouter) optimize() error {
	// 保留入口，可实现压缩/平衡等操作
	return nil
}

func (mr *MethodRouter) addToLengthIndex(length int, entry *routeEntry) {
	g := mr.lengthIndex[length]
	if g == nil {
		g = &routeGroup{}
		mr.lengthIndex[length] = g
	}
	g.addEntry(entry)
}

func (mr *MethodRouter) addToSegmentIndex(segCnt int, entry *routeEntry) {
	g := mr.segmentIndex[segCnt]
	if g == nil {
		g = &routeGroup{}
		mr.segmentIndex[segCnt] = g
	}
	g.addEntry(entry)
}

func (mr *MethodRouter) removeFromLengthIndex(length int, path string) {
	if g := mr.lengthIndex[length]; g != nil {
		g.removePath(path)
		if g.empty() {
			delete(mr.lengthIndex, length)
		}
	}
}

func (mr *MethodRouter) removeFromSegmentIndex(segCnt int, path string) {
	if g := mr.segmentIndex[segCnt]; g != nil {
		g.removePath(path)
		if g.empty() {
			delete(mr.segmentIndex, segCnt)
		}
	}
}

// MatchStats 简单统计
type MatchStats struct {
	totalRequests uint64
	lengthHits    uint64
	segmentHits   uint64
	featureHits   uint64
	radixHits     uint64
}

// 额外辅助函数
func (crt *CompressedRadixTree) sizeNodes() int {
	if crt == nil {
		return 0
	}
	return crt.size
}
