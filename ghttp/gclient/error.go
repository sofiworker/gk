package gclient

import (
	"fmt"
)

var (
	ErrInvalidPath               = fmt.Errorf("invalid path")
	ErrBaseUrlEmpty              = fmt.Errorf("baseurl is required when path is relative")
	ErrBaseUrlFormat             = fmt.Errorf("invalid baseurl")
	ErrUrlNotAbs                 = fmt.Errorf("resulting url is not absolute")
	ErrDataFormat                = fmt.Errorf("data format error, only ptr data")
	ErrNotFoundMethod            = fmt.Errorf("not found method")
	ErrEnvelopeDataFieldNotFound = fmt.Errorf("envelope data field not found")
)

type HTTPError struct {
	StatusCode int
	Message    string
	Response   *Response
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

type BusinessError struct {
	Code     interface{}
	Message  string
	Response *Response
	Cause    error
}

func (e *BusinessError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Message != "" && e.Code != nil:
		return fmt.Sprintf("business error: code=%v message=%s", e.Code, e.Message)
	case e.Message != "":
		return fmt.Sprintf("business error: %s", e.Message)
	case e.Code != nil:
		return fmt.Sprintf("business error: code=%v", e.Code)
	case e.Cause != nil:
		return e.Cause.Error()
	default:
		return "business error"
	}
}

type WebSocketCloseError struct {
	Code   int
	Reason string
	Err    error
}

func (e *WebSocketCloseError) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Reason != "":
		return fmt.Sprintf("websocket closed: code=%d reason=%s", e.Code, e.Reason)
	case e.Err != nil:
		return fmt.Sprintf("websocket closed: code=%d err=%v", e.Code, e.Err)
	default:
		return fmt.Sprintf("websocket closed: code=%d", e.Code)
	}
}

func (e *WebSocketCloseError) IsCode(code int) bool {
	return e != nil && e.Code == code
}

func (e *WebSocketCloseError) CloseCode() int {
	if e == nil {
		return 0
	}
	return e.Code
}

func (e *WebSocketCloseError) CloseReason() string {
	if e == nil {
		return ""
	}
	return e.Reason
}
