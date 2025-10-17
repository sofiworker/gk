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
	staticGroup  map[string][]HandlerFunc // 静态路径表（O(1)）
	segmentIndex map[int][]*routeEntry    // 按段数分组的参数路由
	radixTree    *CompressedRadixTree     // 通配符路由树
	mu           sync.RWMutex
}

func newMethodMatcher() *MethodMatcher {
	return &MethodMatcher{
		staticGroup:  make(map[string][]HandlerFunc),
		segmentIndex: make(map[int][]*routeEntry),
		radixTree:    newCompressedRadixTree(),
	}
}

// 提取路径特征
func (mr *MethodMatcher) extractPathFeature(path string) *PathFeature {
	segments := strings.Split(strings.Trim(path, "/"), "/")
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

	feature := mr.extractPathFeature(path)

	// 优先静态路由
	if !feature.hasParam && !feature.hasWild {
		if _, ok := mr.staticGroup[path]; ok {
			return fmt.Errorf("duplicate static route: %s", path)
		}
		mr.staticGroup[path] = handler
		return nil
	}

	// 通配符走 radix 树
	if feature.hasWild {
		return mr.radixTree.insert(path, handler...)
	}

	// 普通参数路由 -> segment 索引
	entry := &routeEntry{path: path, handler: handler}
	mr.segmentIndex[feature.segmentCnt] = append(mr.segmentIndex[feature.segmentCnt], entry)
	return nil
}

// 匹配逻辑优化版
func (mr *MethodMatcher) match(path string) *MatchResult {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	// 静态匹配（最快）
	if h, ok := mr.staticGroup[path]; ok {
		return &MatchResult{Path: path, Handlers: h}
	}

	// 段数匹配（例如 /user/:id）
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if entries, ok := mr.segmentIndex[len(segments)]; ok {
		for _, e := range entries {
			if params := matchSegments(e.path, path); params != nil {
				return &MatchResult{
					Path:     e.path,
					Handlers: e.handler,
					Params:   params,
				}
			}
		}
	}

	// 最后走通配树匹配
	return mr.radixTree.search(path)
}

func (mr *MethodMatcher) removeRoute(path string) error {
	return nil
}

// 匹配分段路径并提取参数
func matchSegments(pattern, path string) map[string]string {
	pSegs := strings.Split(strings.Trim(pattern, "/"), "/")
	tSegs := strings.Split(strings.Trim(path, "/"), "/")

	if len(pSegs) != len(tSegs) {
		return nil
	}
	params := make(map[string]string)
	for i := range pSegs {
		p := pSegs[i]
		t := tSegs[i]
		if len(p) == 0 {
			continue
		}
		switch p[0] {
		case ':':
			params[p[1:]] = t
		case '*':
			params[p[1:]] = strings.Join(tSegs[i:], "/")
			return params
		default:
			if p != t {
				return nil
			}
		}
	}
	return params
}
