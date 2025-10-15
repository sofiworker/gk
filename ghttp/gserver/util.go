package gserver

import "strings"

type PathFeature struct {
	length     int
	segmentCnt int
	staticBits uint64
	paramBits  uint64
	hash       uint32
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

func longestCommonPrefix(a, b string) int {
	i := 0
	for ; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}
