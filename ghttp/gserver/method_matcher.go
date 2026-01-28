package gserver

import (
	"fmt"
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

	CheckPathValid(path)

	entry := newRouteEntry(path, handler)
	feature := mr.extractPathFeature(path)

	// 优先静态路由
	if !feature.hasParam && !feature.hasWild {
		if _, ok := mr.staticGroup[path]; ok {
			return fmt.Errorf("duplicate static route: %s", path)
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

	// 静态匹配
	if entry, ok := mr.staticGroup[path]; ok {
		return &MatchResult{
			Handlers: entry.handlers,
			Path:     path,
		}
	}

	// 段数匹配（例如 /user/:id）
	segCnt := pathSegmentCount(path)
	if entries, ok := mr.segmentIndex[segCnt]; ok {
		result := entries.search(path)
		if result != nil {
			return result
		}
	}

	// 最后走通配树匹配
	return mr.radixTree.search(path)
}

func (mr *MethodMatcher) matchInto(path string, params map[string]string) *routeEntry {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	if len(params) != 0 {
		for k := range params {
			delete(params, k)
		}
	}

	// Static match
	if entry, ok := mr.staticGroup[path]; ok {
		return entry
	}

	// Segment match
	segCnt := pathSegmentCount(path)
	if entries, ok := mr.segmentIndex[segCnt]; ok {
		if entry := entries.searchInto(path, params); entry != nil {
			return entry
		}
	}

	// Wildcard match
	return mr.radixTree.searchInto(path, params)
}

func pathSegmentCount(path string) int {
	if path == "" {
		return 0
	}
	i := 0
	j := len(path)
	for i < j && path[i] == '/' {
		i++
	}
	for j > i && path[j-1] == '/' {
		j--
	}
	if i >= j {
		return 0
	}
	count := 1
	for k := i; k < j; k++ {
		if path[k] == '/' {
			count++
		}
	}
	return count
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

//// 匹配分段路径并提取参数
//func matchSegments(entry *routeEntry, path string) map[string]string {
//	patternSegs := splitPathSegments(entry.path)
//	targetSegs := splitPathSegments(path)
//
//	if len(patternSegs) != len(targetSegs) {
//		return nil
//	}
//
//	var params map[string]string
//	for i, segment := range patternSegs {
//		if len(segment) == 0 {
//			if targetSegs[i] != "" {
//				return nil
//			}
//			continue
//		}
//
//		if segment[0] == ':' {
//			if params == nil {
//				params = make(map[string]string)
//			}
//			params[segment[1:]] = targetSegs[i]
//			continue
//		}
//
//		if segment != targetSegs[i] {
//			return nil
//		}
//	}
//
//	return params
//}
//
//// 解析查询参数字符串为键值对映射
//func parseQueryParams(queryStr string) map[string]string {
//	params := make(map[string]string)
//	if queryStr == "" {
//		return params
//	}
//
//	pairs := strings.Split(queryStr, "&")
//	for _, pair := range pairs {
//		if eqIdx := strings.IndexByte(pair, '='); eqIdx != -1 {
//			key := pair[:eqIdx]
//			value := pair[eqIdx+1:]
//			params[key] = value
//		} else {
//			params[pair] = ""
//		}
//	}
//	return params
//}
//
//type pathInfo struct {
//	purePath    string
//	hasQuery    bool
//	queryString string
//}

//func extractPathInfo(path string) pathInfo {
//	if idx := strings.IndexByte(path, '?'); idx != -1 {
//		return pathInfo{
//			purePath:    path[:idx],
//			hasQuery:    true,
//			queryString: path[idx+1:],
//		}
//	}
//	return pathInfo{
//		purePath: path,
//		hasQuery: false,
//	}
//}
