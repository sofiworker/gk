//go:build linux

package gnet

import (
	"golang.org/x/sys/unix"
)

// acceptConn 以非阻塞 + CLOEXEC 接受连接（Linux accept4）。
// acceptConn accepts a conn nonblocking + CLOEXEC (Linux accept4).
func acceptConn(fd int) (int, unix.Sockaddr, error) {
	return unix.Accept4(fd, unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC)
}
