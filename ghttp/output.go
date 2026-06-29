package ghttp

import (
	"net/http"
	"reflect"
)

// EnvelopeFunc is the function that wraps responses.
type EnvelopeFunc func(ctx *responseContext, statusCode int, resp interface{}, err error, codecMgr *CodecManager)

// responseContext implements Context for internal use.
type responseContext struct {
	w        http.ResponseWriter
	r        *http.Request
	codecMgr *CodecManager
}

func (c *responseContext) ResponseWriter() http.ResponseWriter { return c.w }
func (c *responseContext) Request() *http.Request              { return c.r }

// DefaultEnvelope wraps responses in {code, msg, data}.
func DefaultEnvelope(ctx *responseContext, statusCode int, resp interface{}, err error, codecMgr *CodecManager) {
	w := ctx.w
	r := ctx.r

	accept := r.Header.Get("Accept")
	codec := codecMgr.Negotiate(accept)

	w.Header().Set("Content-Type", codec.ContentTypes()[0])

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
			envelope.Code = 500
			envelope.Msg = err.Error()
		}
	} else {
		envelope.Msg = "success"
		envelope.Data = resp
	}

	w.WriteHeader(statusCode)
	codec.Marshal(w, &envelope)
}

// WithEnvelope sets a custom envelope function.
func (s *Server) WithEnvelope(fn EnvelopeFunc) {
	s.envelope = fn
}

// resolveStatusCode extracts the HTTP status code from the response.
func resolveStatusCode(resp interface{}) int {
	v := reflect.ValueOf(resp)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		statusField := v.FieldByName("Status")
		if statusField.IsValid() && statusField.Kind() == reflect.Int {
			if code := int(statusField.Int()); code != 0 {
				return code
			}
		}
	}
	return http.StatusOK
}
