//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package gnet

import (
	"golang.org/x/sys/unix"
)

// acceptConn 接受连接后手动设置非阻塞与 CLOEXEC（BSD 无 accept4）。
// acceptConn accepts then sets nonblock + CLOEXEC manually (BSD lacks accept4).
func acceptConn(fd int) (int, unix.Sockaddr, error) {
	nfd, sa, err := unix.Accept(fd)
	if err != nil {
		return nfd, sa, err
	}
	if err := unix.SetNonblock(nfd, true); err != nil {
		unix.Close(nfd)
		return -1, nil, err
	}
	if _, err := unix.FcntlInt(uintptr(nfd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		unix.Close(nfd)
		return -1, nil, err
	}
	return nfd, sa, nil
}
