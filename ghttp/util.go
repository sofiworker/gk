package ghttp

import (
	"path"
	"strings"
)

// JoinPaths 拼接两个 URL 路径段。
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
