package gserver

import (
	"net/http"

	"github.com/valyala/fasthttp"
)

type respWriter struct {
	ctx *fasthttp.RequestCtx
}

func (r *respWriter) Header() http.Header {
	//TODO implement me
	panic("implement me")
}

func (r *respWriter) Write(bytes []byte) (int, error) {
	//TODO implement me
	panic("implement me")
}

func (r *respWriter) WriteHeader(statusCode int) {
	//TODO implement me
	panic("implement me")
}
