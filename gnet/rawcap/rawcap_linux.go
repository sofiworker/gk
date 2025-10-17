//go:build linux

package rawcap

import (
	"syscall"
	"unsafe"
)

type LinuxHandle struct {
	fd     int
	filter string
}

func NewHandle() (Handle, error) {
	return &LinuxHandle{}, nil
}

func (h *LinuxHandle) SetFilter(filter string) error {
	h.filter = filter
	return nil
}

func (h *LinuxHandle) RawHandle() (interface{}, error) {
	return h, nil
}

func (h *LinuxHandle) ReadPacket() (*Packet, error) {
	return nil, nil
}

func (h *LinuxHandle) WritePacketData(data []byte) error {
	_, err := syscall.Write(h.fd, data)
	return err
}

func (h *LinuxHandle) Stats() *Stats {
	return &Stats{}
}

func (h *LinuxHandle) Close() error {
	return syscall.Close(h.fd)
}

func (h *LinuxHandle) NewRawSocket() (int, error) {
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, syscall.ETH_P_ALL)
	if err != nil {
		return -1, err
	}
	h.fd = fd
	return fd, nil
}

func (h *LinuxHandle) SetupSocket() error {
	err := syscall.SetsockoptInt(h.fd, syscall.IPPROTO_IP, syscall.IP_HDRINCL, 1)
	if err != nil {
		return err
	}
	return nil
}

func (h *LinuxHandle) Bind(idx int) error {
	addr := syscall.SockaddrLinklayer{
		Protocol: syscall.ETH_P_ALL,
		Ifindex:  idx,
	}
	if err := syscall.Bind(h.fd, &addr); err != nil {
		return err
	}
	return nil
}

type RingBuffer struct {
	data     []byte
	ringAddr uintptr
	ringSize int
}

func NewRingBuffer(size int) (*RingBuffer, error) {
	data, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANONYMOUS|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}

	return &RingBuffer{
		data:     data,
		ringAddr: uintptr(unsafe.Pointer(&data[0])),
		ringSize: size,
	}, nil
}
