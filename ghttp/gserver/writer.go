package gserver

import (
	"net/http"

	"github.com/valyala/fasthttp"
)

type respWriter struct {
	ctx         *fasthttp.RequestCtx
	header      http.Header
	wroteHeader bool
	statusCode  int
}

func (r *respWriter) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *respWriter) Write(bytes []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if len(bytes) == 0 {
		return 0, nil
	}
	return r.ctx.Write(bytes)
}

func (r *respWriter) WriteHeader(statusCode int) {
	if r.wroteHeader {
		r.ctx.SetStatusCode(statusCode)
		return
	}
	r.wroteHeader = true
	r.statusCode = statusCode
	if r.header != nil {
		r.ctx.Response.Header.Reset()
		for k, values := range r.header {
			for _, v := range values {
				r.ctx.Response.Header.Add(k, v)
			}
		}
	}
	r.ctx.SetStatusCode(statusCode)
}
