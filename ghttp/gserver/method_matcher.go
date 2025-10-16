package gserver

import "sync"

type MethodMatcher struct {
	lengthIndex  map[int]*routeGroup
	segmentIndex map[int]*routeGroup
	radixTree    *CompressedRadixTree
	stats        *MatcherStats
	mu           sync.RWMutex
}

func newMethodMatcher() *MethodMatcher {
	return &MethodMatcher{
		lengthIndex:  make(map[int]*routeGroup),
		segmentIndex: make(map[int]*routeGroup),
		radixTree:    newCompressedRadixTree(),
	}
}

func (mr *MethodMatcher) addRoute(path string, handler ...HandlerFunc) error {
	feature := extractPathFeatures(path)
	entry := &routeEntry{
		path:    path,
		handler: handler,
		feature: feature,
	}
	mr.addToLengthIndex(feature.length, entry)
	mr.addToSegmentIndex(feature.segmentCnt, entry)
	if mr.radixTree == nil {
		mr.radixTree = newCompressedRadixTree()
	}
	return mr.radixTree.insert(path, handler...)
}

func (mr *MethodMatcher) addToLengthIndex(length int, entry *routeEntry) {
	g := mr.lengthIndex[length]
	if g == nil {
		g = &routeGroup{}
		mr.lengthIndex[length] = g
	}
	g.addEntry(entry)
}

func (mr *MethodMatcher) addToSegmentIndex(segCnt int, entry *routeEntry) {
	g := mr.segmentIndex[segCnt]
	if g == nil {
		g = &routeGroup{}
		mr.segmentIndex[segCnt] = g
	}
	g.addEntry(entry)
}

func (mr *MethodMatcher) removeRoute(path string) error {
	feature := extractPathFeatures(path)
	mr.removeFromLengthIndex(feature.length, path)
	mr.removeFromSegmentIndex(feature.segmentCnt, path)

	if mr.radixTree != nil {
		return mr.radixTree.remove(path)
	}
	return nil
}

func (mr *MethodMatcher) removeFromLengthIndex(length int, path string) {
	if g := mr.lengthIndex[length]; g != nil {
		g.removePath(path)
		if g.empty() {
			delete(mr.lengthIndex, length)
		}
	}
}

func (mr *MethodMatcher) removeFromSegmentIndex(segCnt int, path string) {
	if g := mr.segmentIndex[segCnt]; g != nil {
		g.removePath(path)
		if g.empty() {
			delete(mr.segmentIndex, segCnt)
		}
	}
}
