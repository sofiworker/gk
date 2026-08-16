//go:build linux

package craft

import (
	"encoding/binary"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// afpacketInjector 用 AF_PACKET 原始套接字向网卡注入 L2 帧。
// afpacketInjector injects L2 frames into a NIC via an AF_PACKET raw socket.
type afpacketInjector struct {
	fd int
	sa unix.SockaddrLinklayer
}

// NewAFPacketInjector 创建网卡注入器（需 root 或 CAP_NET_RAW）。
// NewAFPacketInjector creates a NIC injector (requires root or CAP_NET_RAW).
func NewAFPacketInjector(iface string) (Injector, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("craft: interface %q: %w", iface, err)
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, 0)
	if err != nil {
		return nil, fmt.Errorf("craft: raw socket: %w", err)
	}
	sa := unix.SockaddrLinklayer{
		Protocol: htons(unix.ETH_P_ALL),
		Ifindex:  ifi.Index,
	}
	if err := unix.Bind(fd, &sa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("craft: bind to %q: %w", iface, err)
	}
	return &afpacketInjector{fd: fd, sa: sa}, nil
}

// Inject 发送帧。
// Inject sends a frame.
func (a *afpacketInjector) Inject(pkt []byte) error {
	return unix.Sendto(a.fd, pkt, 0, &a.sa)
}

// Close 释放原始套接字。
// Close releases the raw socket.
func (a *afpacketInjector) Close() error {
	return unix.Close(a.fd)
}

// htons 主机序转网络序（16 位）。
// htons converts host to network byte order (16-bit).
func htons(v uint16) uint16 {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return binary.LittleEndian.Uint16(b[:])
}
