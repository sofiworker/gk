package pcapng

import "errors"

var (
	ErrInvalidBlockLength = errors.New("pcapng: invalid block length")
	ErrUnexpectedBlock    = errors.New("pcapng: unexpected block type")
	ErrInvalidSection     = errors.New("pcapng: invalid section header")
)
