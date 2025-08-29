package ghttp

import "github.com/valyala/fasthttp"

type noCopy struct{}

// Context 封装 fasthttp.RequestCtx 提供更友好的 API
type Context struct {
	noCopy noCopy
	ctx    *fasthttp.RequestCtx
}
