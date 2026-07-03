package ghttp

import (
	"log"
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

func normalizeRoutePath(path string) string {
	if strings.HasPrefix(path, "/") {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	if containsColonParam(path) {
		log.Printf("[ghttp] WARN route path %q uses deprecated :param syntax; use {param} syntax instead", path)
	}
	return convertBraceParamsToColon(path)
}

func containsColonParam(path string) bool {
	for _, seg := range splitPathSegments(path) {
		if strings.HasPrefix(seg, ":") && len(seg) > 1 {
			return true
		}
	}
	return false
}

func convertBraceParamsToColon(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			if strings.HasSuffix(name, "...") {
				parts[i] = "*" + strings.TrimSuffix(name, "...")
				continue
			}
			parts[i] = ":" + name
		}
	}
	return strings.Join(parts, "/")
}
