package ghttp

import "fmt"

var (
	ErrInvalidPath    = fmt.Errorf("invalid path")
	ErrBaseUrlEmpty   = fmt.Errorf("baseurl is required when path is relative")
	ErrBaseUrlFormat  = fmt.Errorf("invalid baseurl")
	ErrUrlNotAbs      = fmt.Errorf("resulting url is not absolute")
	ErrDataFormat     = fmt.Errorf("data format error, only ptr data")
	ErrNotFoundMethod = fmt.Errorf("not found method")
)

type HTTPError struct {
	StatusCode int
	Message    string
	Response   *Response
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}
