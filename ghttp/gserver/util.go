package gserver

import "path"

func longestCommonPrefix(a, b string) int {
	i := 0
	for ; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}

func JoinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}

	finalPath := path.Join(absolutePath, relativePath)
	if lastChar(relativePath) == '/' && lastChar(finalPath) != '/' {
		return finalPath + "/"
	}
	return finalPath
}

func lastChar(str string) uint8 {
	if str == "" {
		panic("The length of the string can't be 0")
	}
	return str[len(str)-1]
}

func checkPathValid(path string) {
	if path == "" {
		panic("empty path")
	}
	if path[0] != '/' {
		panic("path must begin with '/'")
	}
	for i, c := range []byte(path) {
		switch c {
		case ':':
			if (i < len(path)-1 && path[i+1] == '/') || i == (len(path)-1) {
				panic("wildcards must be named with a non-empty name in path '" + path + "'")
			}
			i++
			for ; i < len(path) && path[i] != '/'; i++ {
				if path[i] == ':' || path[i] == '*' {
					panic("only one wildcard per path segment is allowed, find multi in path '" + path + "'")
				}
			}
		case '*':
			if i == len(path)-1 {
				panic("wildcards must be named with a non-empty name in path '" + path + "'")
			}
			if i > 0 && path[i-1] != '/' {
				panic(" no / before wildcards in path " + path)
			}
			for ; i < len(path); i++ {
				if path[i] == '/' {
					panic("catch-all routes are only allowed at the end of the path in path '" + path + "'")
				}
			}
		}
	}
}
