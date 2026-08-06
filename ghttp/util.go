package ghttp

import (
	"path"
	"strings"
)

// JoinPaths joins two URL path segments.
func JoinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}

	finalPath := path.Join(absolutePath, relativePath)
	if strings.HasSuffix(relativePath, "/") && !strings.HasSuffix(finalPath, "/") {
		finalPath += "/"
	}
	return finalPath
}

// splitPathSegments splits a path into segments.
func splitPathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// pathSegmentCount returns the number of segments in a path.
func pathSegmentCount(p string) int {
	if p == "/" || p == "" {
		return 0
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return 0
	}
	count := 1
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			count++
		}
	}
	return count
}

// extractParamNames extracts :param, {param}, and *wildcard names from a path.
func extractParamNames(p string) []string {
	var names []string
	segments := splitPathSegments(p)
	for _, seg := range segments {
		if strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") {
			names = append(names, seg[1:])
			continue
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
			name = strings.TrimSuffix(name, "...")
			names = append(names, name)
		}
	}
	return names
}
