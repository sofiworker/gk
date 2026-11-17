//go:build !linux

package netinfo

import "errors"

// ErrNotSupported 表示当前平台不提供该数据源。
// ErrNotSupported means the platform lacks this data source.
var ErrNotSupported = errors.New("netinfo: not supported on this platform")

// Connections 在非 Linux 平台返回 ErrNotSupported。
// Connections returns ErrNotSupported on non-Linux platforms.
func Connections() ([]ConnInfo, error) {
	return nil, ErrNotSupported
}

// IOCounters 在非 Linux 平台返回 ErrNotSupported。
// IOCounters returns ErrNotSupported on non-Linux platforms.
func IOCounters() ([]IOCountersStat, error) {
	return nil, ErrNotSupported
}

// ProtoCounters 在非 Linux 平台返回 ErrNotSupported。
// ProtoCounters returns ErrNotSupported on non-Linux platforms.
func ProtoCounters() (map[string]uint64, error) {
	return nil, ErrNotSupported
}
