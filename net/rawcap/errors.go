package rawcap

import "errors"

var (
	ErrFilterNotSupported = errors.New("rawcap: filter not supported on this platform")
	// ErrReadTimeout 表示套接字读超时（可重试，非致命）。
	ErrReadTimeout  = errors.New("rawcap: read timeout")
	ErrHandleClosed = errors.New("rawcap: handle closed")
	ErrUnsupported  = errors.New("rawcap: not supported on this platform")
)
