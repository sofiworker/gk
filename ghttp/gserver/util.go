package gserver

import (
	"bytes"
	"net/http"
	"net/url"
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
	req.URL = &url.URL{}
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

func ConvertToHTTPRequest(ctx *fasthttp.RequestCtx) (*http.Request, error) {
	req := requestPool.Get().(*http.Request)

	req.Proto = string(ctx.Request.Header.Protocol())
	req.ProtoMajor, req.ProtoMinor, _ = ParseHTTPVersion(req.Proto)

	req.Method = string(ctx.Method())

	req.RequestURI = string(ctx.RequestURI())
	req.ContentLength = int64(ctx.Request.Header.ContentLength())
	req.Host = string(ctx.Host())
	req.RemoteAddr = ctx.RemoteAddr().String()

	fullURI := string(ctx.Request.URI().FullURI())
	u, err := url.Parse(fullURI)
	if err != nil {
		return nil, err
	}
	req.URL = u

	br := bodyReaderPool.Get().(*bodyReader)
	br.Reader = bytes.NewReader(ctx.Request.Body())
	req.Body = br

	ctx.Request.Header.All()(func(key, value []byte) bool {
		req.Header.Add(string(key), string(value))
		return true
	})

	return req, nil
}

func ParseHTTPVersion(vers string) (major, minor int, ok bool) {
	switch vers {
	case "HTTP/1.1":
		return 1, 1, true
	case "HTTP/1.0":
		return 1, 0, true
	case "HTTP/2", "HTTP/2.0":
		return 2, 0, true
	}
	return 0, 0, false
}
