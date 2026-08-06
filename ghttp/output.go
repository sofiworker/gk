package ghttp

import (
	"net/http"
)

// EnvelopeFunc wraps responses before they are written. The contentType and
// codec are the route-negotiated representation; the envelope must not change
// the HTTP status code.
type EnvelopeFunc func(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec Codec)

// DefaultEnvelope wraps responses in {code, msg, data}.
func DefaultEnvelope(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec Codec) {
	if codec == nil {
		codec = &JSONCodec{}
		contentType = MIMEJSON
	}
	w.Header().Set("Content-Type", contentType)

	var envelope struct {
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
		Data interface{} `json:"data,omitempty"`
	}

	if err != nil {
		he := AsError(err)
		if he != nil {
			statusCode = he.Code
			envelope.Code = he.Code
			envelope.Msg = he.Message
		} else {
			envelope.Code = statusCode
			envelope.Msg = http.StatusText(statusCode)
		}
	} else {
		envelope.Msg = "success"
		envelope.Data = resp
	}

	w.WriteHeader(statusCode)
	codec.Marshal(w, &envelope)
}
