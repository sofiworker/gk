package rawcap

import (
	"net"
	"time"
)

type PacketInfo struct {
	Timestamp      time.Time
	CaptureLength  int
	Length         int
	InterfaceIndex int
}

type Packet struct {
	Data []byte
	Info *PacketInfo
}

type Interface struct {
	Index       int
	Name        string
	Description string
	Addresses   []net.IP
	Netmask     []net.IP
	Broadcast   []net.IP
	Flags       uint32
}

type PcapWriter interface {
	WritePacket(*Packet) error
	Close() error
}

type PlatformSpecific interface {
	SetRingBuffer(size int, blockSize int, numBlocks int) error
	EnableXDP(program []byte, options map[string]interface{}) error
	SetKernelBufferSize(size int) error
}
