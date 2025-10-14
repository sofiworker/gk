package gserver

import (
	"net/http"

	"github.com/valyala/fasthttp"
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy noCopy
	ctx    *fasthttp.RequestCtx
	req    *http.Request
}
