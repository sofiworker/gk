//go:build linux

package rawcap

import (
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type LinuxHandle struct {
	fd      int
	iface   *net.Interface
	cfg     Config
	stats   Stats
	addr    unix.SockaddrLinklayer
	mu      sync.Mutex
	closed  bool
	recvBuf []byte
}

func openLive(interfaceName string, cfg Config) (Handle, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("rawcap: interface %s not found: %w", interfaceName, err)
	}

	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(hostToNetwork16(unix.ETH_P_ALL)))
	if err != nil {
		return nil, fmt.Errorf("rawcap: socket: %w", err)
	}

	handle := &LinuxHandle{
		fd:    fd,
		iface: iface,
		cfg:   cfg,
		addr: unix.SockaddrLinklayer{
			Protocol: hostToNetwork16(unix.ETH_P_ALL),
			Ifindex:  iface.Index,
		},
		recvBuf: make([]byte, cfg.SnapLen),
	}

	if err := handle.configure(); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return handle, nil
}

func (h *LinuxHandle) configure() error {
	if err := unix.Bind(h.fd, &h.addr); err != nil {
		return fmt.Errorf("rawcap: bind: %w", err)
	}

	if h.cfg.BufferSize > 0 {
		if err := unix.SetsockoptInt(h.fd, unix.SOL_SOCKET, unix.SO_RCVBUF, h.cfg.BufferSize); err != nil {
			return fmt.Errorf("rawcap: set buffer size: %w", err)
		}
	}

	if h.cfg.Timeout > 0 {
		tv := unix.NsecToTimeval(h.cfg.Timeout.Nanoseconds())
		if err := unix.SetsockoptTimeval(h.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
			return fmt.Errorf("rawcap: set timeout: %w", err)
		}
	}

	if h.cfg.Promiscuous {
		mreq := &unix.PacketMreq{
			Ifindex: int32(h.iface.Index),
			Type:    unix.PACKET_MR_PROMISC,
		}
		if err := unix.SetsockoptPacketMreq(h.fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, mreq); err != nil {
			return fmt.Errorf("rawcap: set promiscuous: %w", err)
		}
	}

	return nil
}

func (h *LinuxHandle) ReadPacket() (*Packet, error) {
	if h.isClosed() {
		return nil, ErrHandleClosed
	}

	for {
		n, from, err := unix.Recvfrom(h.fd, h.recvBuf, 0)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				return nil, fmt.Errorf("rawcap: read timeout: %w", err)
			}
			if err == unix.EBADF {
				return nil, ErrHandleClosed
			}
			return nil, fmt.Errorf("rawcap: recvfrom: %w", err)
		}

		if n <= 0 {
			continue
		}

		data := make([]byte, n)
		copy(data, h.recvBuf[:n])

		info := &PacketInfo{
			Timestamp:      time.Now().UTC(),
			CaptureLength:  n,
			Length:         n,
			InterfaceIndex: h.iface.Index,
		}

		if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll != nil && ll.Ifindex != 0 {
			info.InterfaceIndex = ll.Ifindex
		}

		h.stats.PacketsReceived++

		return &Packet{
			Data: data,
			Info: info,
		}, nil
	}
}

func (h *LinuxHandle) WritePacketData(data []byte) error {
	if h.isClosed() {
		return ErrHandleClosed
	}
	_, err := unix.Write(h.fd, data)
	return err
}

func (h *LinuxHandle) SetFilter(filter string) error {
	return ErrFilterNotSupported
}

func (h *LinuxHandle) RawHandle() (interface{}, error) {
	if h.isClosed() {
		return nil, ErrHandleClosed
	}
	return h.fd, nil
}

func (h *LinuxHandle) Stats() *Stats {
	stats := h.stats
	if tp, err := unix.GetsockoptTpacketStats(h.fd, unix.SOL_PACKET, unix.PACKET_STATISTICS); err == nil {
		stats.PacketsReceived = uint64(tp.Packets)
		stats.PacketsDropped = uint64(tp.Drops)
	}
	return &stats
}

func (h *LinuxHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}
	h.closed = true

	if h.cfg.Promiscuous {
		mreq := &unix.PacketMreq{
			Ifindex: int32(h.iface.Index),
			Type:    unix.PACKET_MR_PROMISC,
		}
		_ = unix.SetsockoptPacketMreq(h.fd, unix.SOL_PACKET, unix.PACKET_DROP_MEMBERSHIP, mreq)
	}

	return unix.Close(h.fd)
}

func (h *LinuxHandle) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

func hostToNetwork16(i uint16) uint16 {
	return (i<<8)&0xff00 | i>>8
}
