package gserver

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// PathFeature 用于区分路径类型
type PathFeature struct {
	length     int
	segmentCnt int
	hasParam   bool
	hasWild    bool
}

type MethodMatcher struct {
	staticGroup  map[string]*routeEntry       // 静态路径表（O(1)）
	segmentIndex map[int]*CompressedRadixTree // 按段数分组的参数路由
	radixTree    *CompressedRadixTree         // 通配符路由树
	mu           sync.RWMutex
}

func newMethodMatcher() *MethodMatcher {
	return &MethodMatcher{
		staticGroup:  make(map[string]*routeEntry),
		segmentIndex: make(map[int]*CompressedRadixTree),
		radixTree:    newCompressedRadixTree(),
	}
}

// 提取路径特征
func (mr *MethodMatcher) extractPathFeature(path string) *PathFeature {
	segments := splitPathSegments(path)
	f := &PathFeature{
		length:     len(path),
		segmentCnt: len(segments),
	}
	for _, seg := range segments {
		if strings.HasPrefix(seg, ":") {
			f.hasParam = true
		}
		if strings.HasPrefix(seg, "*") {
			f.hasWild = true
		}
	}
	return f
}

// 添加路由
func (mr *MethodMatcher) addRoute(path string, handler ...HandlerFunc) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	entry := newRouteEntry(path, handler)
	feature := mr.extractPathFeature(path)

	// 优先静态路由
	if !feature.hasParam && !feature.hasWild {
		if _, ok := mr.staticGroup[path]; ok {
			return ErrDuplicateRoute
		}
		mr.staticGroup[path] = entry
		return nil
	}

	// 通配符走 radix 树
	if feature.hasWild {
		return mr.radixTree.insert(entry)
	}

	if mr.segmentIndex[feature.segmentCnt] == nil {
		mr.segmentIndex[feature.segmentCnt] = newCompressedRadixTree()
	}
	return mr.segmentIndex[feature.segmentCnt].insert(entry)
}

func (mr *MethodMatcher) match(path string) *MatchResult {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	info := extractPathInfo(path)

	// 静态匹配
	if entry, ok := mr.staticGroup[info.purePath]; ok {
		res := entry.toResult()
		if info.hasQuery {
			qv := ParseQueryParams(info.queryString)
			res.QueryValues = qv
			res.appendParams(qv)
		}
		return res
	}

	// 段数匹配（例如 /user/:id）
	segments := splitPathSegments(info.purePath)
	if entries, ok := mr.segmentIndex[len(segments)]; ok && entries != nil {
		if res := entries.search(info.purePath); res != nil {
			if info.hasQuery {
				qv := ParseQueryParams(info.queryString)
				res.QueryValues = qv
				res.appendParams(qv)
			}
			return res
		}
	}

	// 最后走通配树匹配
	if res := mr.radixTree.search(info.purePath); res != nil {
		if info.hasQuery {
			qv := ParseQueryParams(info.queryString)
			res.QueryValues = qv
			res.appendParams(qv)
		}
		return res
	}
	return nil
}

func (mr *MethodMatcher) removeRoute(path string) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	feature := mr.extractPathFeature(path)

	if !feature.hasParam && !feature.hasWild {
		if _, ok := mr.staticGroup[path]; ok {
			delete(mr.staticGroup, path)
			return nil
		}
		return fmt.Errorf("route not found: %s", path)
	}

	if feature.hasWild {
		return mr.radixTree.remove(path)
	}

	entries := mr.segmentIndex[feature.segmentCnt]
	return entries.remove(path)
}

// ParseQueryParams 解析查询字符串到 url.Values：
//   - 首先尝试 fast path：
//   - 按字节扫描，避免 strings.Split 分配
//   - 解析 k[=v]
//   - 支持 '+' -> ' '，严格的 %XX 解码
//   - 多值参数用 .Add() 保留
//   - 一旦遇到无法安全快速解析的情况（非法 % 编码、分号分隔等）
//     立刻 fallback 到标准库 url.ParseQuery()，保证兼容性
func ParseQueryParams(raw string) url.Values {
	values := make(url.Values)
	if raw == "" {
		return values
	}

	if len(raw) > 4096 {
		// 直接返回空，或者你可以在这里标记一个特殊键告诉后续逻辑被截断了
		return values
	}

	// 遇到分号，我们直接走慢路径，避免在 fast path 里重现所有老式兼容逻辑
	if strings.IndexByte(raw, ';') != -1 {
		std, err := url.ParseQuery(raw)
		if err == nil {
			return std
		}
		return std // 即使报错也可能返回部分可解析内容
	}

	start := 0
	for start <= len(raw) {
		ampRel := strings.IndexByte(raw[start:], '&')
		var pair string
		if ampRel == -1 {
			pair = raw[start:]
			start = len(raw) + 1
		} else {
			pair = raw[start : start+ampRel]
			start = start + ampRel + 1
		}

		if pair == "" {
			continue // 允许 "&&"
		}

		eq := strings.IndexByte(pair, '=')
		var kRaw, vRaw string
		if eq == -1 {
			kRaw = pair
			vRaw = ""
		} else {
			kRaw = pair[:eq]
			vRaw = pair[eq+1:]
		}

		kDec, ok1 := fastDecodeComponent(kRaw)
		if !ok1 {
			return parseQueryFallback(raw, values)
		}
		vDec, ok2 := fastDecodeComponent(vRaw)
		if !ok2 {
			return parseQueryFallback(raw, values)
		}

		values.Add(kDec, vDec)
	}

	return values
}

// parseQueryFallback 使用标准库兜底（非法编码、奇葩情况时走这里）。
// 注意：我们不简单丢弃前面 fast path 已经解析的部分；
// 如果标准库成功，我们直接用标准库结果；
// 如果标准库也报错，我们保留 partial（前面已解析出的内容），避免全丢。
func parseQueryFallback(raw string, partial url.Values) url.Values {
	std, err := url.ParseQuery(raw)
	if err == nil {
		return std
	}
	return partial
}

// fastDecodeComponent: 对查询参数键值做轻量 URL 解码。
// 支持：
//
//	'+' -> ' '
//	'%XX' -> 单字节
//
// 如果发现非法编码（比如 '%GZ' 或末尾只有 '%'），返回 false，
// 触发 fallback。
func fastDecodeComponent(s string) (string, bool) {
	// 快速路径：无 '+' / '%'，直接复用原 string，零分配
	if strings.IndexByte(s, '+') == -1 && strings.IndexByte(s, '%') == -1 {
		return s, true
	}

	var b strings.Builder
	b.Grow(len(s)) // 解码后不会更长

	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '+':
			b.WriteByte(' ')
		case '%':
			if i+2 >= len(s) {
				return "", false
			}
			h1 := hexVal(s[i+1])
			h2 := hexVal(s[i+2])
			if h1 < 0 || h2 < 0 {
				return "", false
			}
			b.WriteByte(byte(h1<<4 | h2))
			i += 2
		default:
			b.WriteByte(c)
		}
	}

	return b.String(), true
}

// hexVal: 单个十六进制字符 -> 数值，非法返回 -1
func hexVal(c byte) int8 {
	switch {
	case c >= '0' && c <= '9':
		return int8(c - '0')
	case c >= 'a' && c <= 'f':
		return int8(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int8(c-'A') + 10
	default:
		return -1
	}
}

type pathInfo struct {
	purePath    string
	hasQuery    bool
	queryString string
}

// extractPathInfo 高性能拆分 path -> 纯路径 + 原始查询串。
// 特性：
//   - 去掉 '#' 之后的内容（fragment）
//   - 零拷贝 substr 切片
//   - 不做 URLDecode
func extractPathInfo(path string) pathInfo {
	if path == "" {
		return pathInfo{}
	}

	// 去掉 fragment (罕见，但自写服务器可能拿到原始 URI)
	if hashIdx := strings.IndexByte(path, '#'); hashIdx != -1 {
		path = path[:hashIdx]
	}

	if qIdx := strings.IndexByte(path, '?'); qIdx != -1 {
		return pathInfo{
			purePath:    path[:qIdx],
			hasQuery:    true,
			queryString: path[qIdx+1:], // 可能是空串
		}
	}

	return pathInfo{
		purePath: path,
	}
}

func (mr *MethodMatcher) segmentIndexRoutes(method string) []*RouteInfo {
	out := make([]*RouteInfo, 0, 8)
	for _, tree := range mr.segmentIndex {
		out = append(out, tree.listRoutes(method)...)
	}
	return out
}
func (mr *MethodMatcher) radixRoutes(method string) []*RouteInfo {
	return mr.radixTree.listRoutes(method)
}

// segmentIndexRouteCount/radixRouteCount: 用于 serverMatcher.Stats()
func (mr *MethodMatcher) segmentIndexRouteCount() (n int) {
	for _, tree := range mr.segmentIndex {
		n += tree.routeCount()
	}
	return
}
func (mr *MethodMatcher) radixRouteCount() int {
	return mr.radixTree.routeCount()
}
