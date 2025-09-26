package ghttp

import "github.com/valyala/fasthttp"

type noCopy struct{}

type Context struct {
	noCopy noCopy
	ctx    *fasthttp.RequestCtx
}
