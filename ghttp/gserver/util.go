package gserver

import (
	"bytes"
	"net/http"
	"path"

	"github.com/valyala/fasthttp"
)

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

func CheckPathValid(path string) {
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
		case '?':
			panic("'?' character is not allowed in path '" + path + "'")
		}
	}
}

func ResetRequest(req *http.Request) {
	// 重置 http.Request 结构体字段
	req.Method = ""
	req.URL = nil
	req.Proto = ""
	req.ProtoMajor = 0
	req.ProtoMinor = 0
	req.Header = make(http.Header)
	req.Body = nil
	req.ContentLength = 0
	req.TransferEncoding = nil
	req.Close = false
	req.Trailer = nil
	req.RemoteAddr = ""
	req.RequestURI = ""
	req.TLS = nil
	req.Form = nil
	req.PostForm = nil
	req.MultipartForm = nil
}

func ConvertToHTTPRequest(ctx *fasthttp.RequestCtx) *http.Request {
	req := requestPool.Get().(*http.Request)

	req.Method = string(ctx.Method())
	req.Proto = "HTTP/1.1"
	req.ProtoMajor = 1
	req.ProtoMinor = 1
	req.RequestURI = string(ctx.RequestURI())
	req.ContentLength = int64(ctx.Request.Header.ContentLength())
	req.Host = string(ctx.Host())
	req.RemoteAddr = ctx.RemoteAddr().String()

	// URL
	req.URL.Scheme = "http" // fasthttp 不直接支持 https，通常在代理后
	if ctx.IsTLS() {
		req.URL.Scheme = "https"
	}
	req.URL.Host = string(ctx.Host())
	req.URL.Path = string(ctx.Path())
	req.URL.RawQuery = string(ctx.URI().QueryString())

	// Body
	br := bodyReaderPool.Get().(*bodyReader)
	br.Reader = bytes.NewReader(ctx.Request.Body())
	req.Body = br

	ctx.Request.Header.VisitAll(func(key, value []byte) {
		req.Header.Set(string(key), string(value))
	})

	return req
}
