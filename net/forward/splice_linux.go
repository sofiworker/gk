//go:build linux

package forward

import (
	"net"

	"golang.org/x/sys/unix"
)

// SpliceCopy 用 splice(2) 在内核态直接转发 TCP 数据，数据不经过用户态，
// 适合纯转发热路径。仅支持 *net.TCPConn；不感知 conn 的 deadline，
// 错误时调用方应回退 io.Copy。与 io.Copy 一样不传播 FIN：读到源 EOF 返回
// 后，调用方须自行 CloseWrite(dst) 把 EOF 传给对端。
//
// SpliceCopy relays TCP data in the kernel via splice(2) without userspace
// copies, suited for pure-forwarding hot paths. It supports *net.TCPConn only
// and does not honor conn deadlines; callers should fall back to io.Copy on
// error. Like io.Copy it does not propagate FIN: after it returns on source
// EOF the caller must CloseWrite(dst) to relay EOF to the peer.
func SpliceCopy(dst, src *net.TCPConn, chunk int) (int64, error) {
	if chunk <= 0 {
		chunk = 64 * 1024
	}
	var srcFD, dstFD int
	if err := rawFD(src, &srcFD); err != nil {
		return 0, err
	}
	if err := rawFD(dst, &dstFD); err != nil {
		return 0, err
	}
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC); err != nil {
		return 0, err
	}
	defer unix.Close(p[0])
	defer unix.Close(p[1])

	var total int64
	for {
		n, err := unix.Splice(srcFD, nil, p[1], nil, chunk, unix.SPLICE_F_MOVE)
		if err == unix.EAGAIN {
			// Go socket 非阻塞：无数据可读时等源就绪（poll 不消费数据，
			// 与 netpoller 共存安全）。
			// Go sockets are nonblocking: wait for source readiness on
			// EAGAIN (poll consumes nothing; safe beside the netpoller).
			if err := waitPoll(srcFD, unix.POLLIN); err != nil {
				return total, err
			}
			continue
		}
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return total, err
		}
		if n == 0 {
			return total, nil // EOF
		}
		// 管道 → dst，须排空以防管道残留。
		// Pipe → dst; drain fully to avoid pipe residue.
		for n > 0 {
			m, err := unix.Splice(p[0], nil, dstFD, nil, int(n), unix.SPLICE_F_MOVE)
			if err == unix.EAGAIN {
				if err := waitPoll(dstFD, unix.POLLOUT); err != nil {
					return total, err
				}
				continue
			}
			if err != nil {
				if err == unix.EINTR {
					continue
				}
				return total, err
			}
			n -= m
			total += int64(m)
		}
	}
}

// waitPoll 阻塞等待 fd 的指定事件；EINTR 自动重试。
// waitPoll blocks for the given fd events; EINTR is retried.
func waitPoll(fd int, events int16) error {
	pfd := []unix.PollFd{{Fd: int32(fd), Events: events}}
	for {
		if _, err := unix.Poll(pfd, -1); err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		return nil
	}
}

// rawFD 取出 TCPConn 的原始 fd。
// rawFD extracts the raw fd of a TCPConn.
func rawFD(c *net.TCPConn, out *int) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	return rc.Control(func(fd uintptr) {
		*out = int(fd)
	})
}
