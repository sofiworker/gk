//go:build !linux && !windows

package link

import "errors"

// ErrNotSupported 表示当前平台未实现链路操作。
var ErrNotSupported = errors.New("link: not supported on this platform")

func listLinks() ([]Link, error) {
	return nil, ErrNotSupported
}
