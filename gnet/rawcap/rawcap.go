package rawcap

import "time"

type Config struct {
	SnapLen     int
	Promiscuous bool
	Immediate   bool
	Timeout     time.Duration
	BufferSize  int
}

type Handle interface {
	ReadPacket() (*Packet, error)
	WritePacketData([]byte) error
	SetFilter(filter string) error
	RawHandle() (interface{}, error)
	Stats() *Stats
	Close() error
}
