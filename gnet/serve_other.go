//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package gnet

import "github.com/sofiworker/gk/gpoller"

// serveImpl 在无 fd 引擎的平台返回 ErrNotSupported。Windows 的标准库
// netpoller 接入路径在 M5 提供（见重构 spec §7）。
//
// serveImpl returns ErrNotSupported on platforms without the fd engine. The
// Windows stdlib-netpoller path arrives in M5 (see the redesign spec, §7).
func (s *Server) serveImpl(addr string) error {
	return gpoller.ErrNotSupported
}
