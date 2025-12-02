package rawcap

import "errors"

var (
	ErrFilterNotSupported = errors.New("rawcap: filter not supported on this platform")
	ErrHandleClosed       = errors.New("rawcap: handle closed")
	ErrUnsupported        = errors.New("rawcap: not supported on this platform")
)
