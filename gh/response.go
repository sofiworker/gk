package ghttp

import (
	"bytes"
	"github.com/valyala/fasthttp"
	"reflect"
)

type Response struct {
	fResp      *fasthttp.Response
	RemoteAddr string
	BodyRaw    []byte
	StreamBody bool
	commonBody interface{}
	decoder    Decoder
}

func (r *Response) SetCommonBody(body interface{}) {
	r.commonBody = body
}

func (r *Response) SetDecoder(d Decoder) {
	r.decoder = d
}

func (r *Response) Decode(v interface{}) error {
	if reflect.TypeOf(v).Kind() != reflect.Ptr {
		return ErrDataFormat
	}
	buffer := bytes.NewBuffer(r.BodyRaw)
	return r.decoder.Decode(buffer, v)
}
