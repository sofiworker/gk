package gserver

import (
	"strings"
	"sync"
)

type PathFeature struct {
	length     int
	segmentCnt int
	staticBits uint64
	paramBits  uint64
	hash       uint32
}

type MethodMatcher struct {
	staticGroup  map[string]*routeGroup
	lengthIndex  map[int]*routeGroup
	segmentIndex map[int]*routeGroup
	radixTree    *CompressedRadixTree
	stats        *MatcherStats
	mu           sync.RWMutex
}

func newMethodMatcher() *MethodMatcher {
	return &MethodMatcher{
		staticGroup:  make(map[string]*routeGroup),
		lengthIndex:  make(map[int]*routeGroup),
		segmentIndex: make(map[int]*routeGroup),
		radixTree:    newCompressedRadixTree(),
	}
}

func (mr *MethodMatcher) addRoute(path string, handler ...HandlerFunc) error {
	feature := mr.extractPathFeatures(path)
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

func (mr *MethodMatcher) extractPathFeatures(path string) *PathFeature {
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
	return &feature
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
	feature := mr.extractPathFeatures(path)
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
