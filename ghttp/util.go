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
	return len(strings.Split(strings.Trim(p, "/"), "/"))
}

// extractParamNames extracts :param and *wildcard names from a path.
func extractParamNames(p string) []string {
	var names []string
	segments := splitPathSegments(p)
	for _, seg := range segments {
		if strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") {
			names = append(names, seg[1:])
		}
	}
	return names
}
