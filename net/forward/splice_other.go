//go:build !linux

package forward

import (
	"io"
	"net"
)

// SpliceCopy 在无 splice 的平台回退为 io.CopyBuffer。
// SpliceCopy falls back to io.CopyBuffer on platforms without splice.
func SpliceCopy(dst, src *net.TCPConn, chunk int) (int64, error) {
	if chunk <= 0 {
		chunk = 32 * 1024
	}
	buf := make([]byte, chunk)
	return io.CopyBuffer(dst, src, buf)
}
