package ghttp

import (
	"net/http"
	"reflect"
	"sync"
)

const missingStatusField = -1

var statusFieldCache sync.Map

// StatusCoder allows a response to provide its HTTP status without reflection.
type StatusCoder interface {
	StatusCode() int
}

// EnvelopeFunc is the function that wraps responses.
type EnvelopeFunc func(ctx Context, statusCode int, resp interface{}, err error, codecMgr *CodecManager)

// DefaultEnvelope wraps responses in {code, msg, data}.
func DefaultEnvelope(ctx Context, statusCode int, resp interface{}, err error, codecMgr *CodecManager) {
	w := ctx.ResponseWriter()
	r := ctx.Request()

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

// resolveStatusCode extracts the HTTP status code from the response.
func resolveStatusCode(resp interface{}) int {
	if statusCoder, ok := resp.(StatusCoder); ok {
		if code := statusCoder.StatusCode(); code != 0 {
			return code
		}
	}

	v := reflect.ValueOf(resp)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return http.StatusOK
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		idx := cachedStatusFieldIndex(v.Type())
		if idx == missingStatusField {
			return http.StatusOK
		}
		statusField := v.Field(idx)
		if statusField.IsValid() && statusField.Kind() == reflect.Int {
			if code := int(statusField.Int()); code != 0 {
				return code
			}
		}
	}
	return http.StatusOK
}

func cachedStatusFieldIndex(t reflect.Type) int {
	if idx, ok := statusFieldCache.Load(t); ok {
		return idx.(int)
	}
	idx := missingStatusField
	if field, ok := t.FieldByName("Status"); ok && field.Type.Kind() == reflect.Int {
		idx = field.Index[0]
	}
	actual, _ := statusFieldCache.LoadOrStore(t, idx)
	return actual.(int)
}
