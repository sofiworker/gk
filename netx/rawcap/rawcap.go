package rawcap

import "time"

type Config struct {
	SnapLen     int
	Promiscuous bool
	Immediate   bool
	Timeout     time.Duration
	BufferSize  int
	Monitor     bool
}

type PacketInfo struct {
	Timestamp      time.Time
	CaptureLength  int
	Length         int
	InterfaceIndex int
}

type Packet struct {
	Data []byte
	Info PacketInfo
}

type Stats struct {
	PacketsReceived  uint64
	PacketsDropped   uint64
	PacketsIfDropped uint64
}

type Handle interface {
	ReadPacket() (Packet, error)
	WritePacketData([]byte) error
	SetBPFFilter(filter string) error
	RawHandle() (interface{}, error)
	Stats() Stats
	Close()
}
