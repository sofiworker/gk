package rawcap

import "time"

const (
	DefaultSnapLen    = 65535
	DefaultBufferSize = 4 << 20 // 4 MiB
)

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

func OpenLive(interfaceName string, cfg Config) (Handle, error) {
	return openLive(interfaceName, normalizeConfig(cfg))
}

func normalizeConfig(cfg Config) Config {
	if cfg.SnapLen <= 0 {
		cfg.SnapLen = DefaultSnapLen
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = DefaultBufferSize
	}
	if cfg.Timeout < 0 {
		cfg.Timeout = 0
	}
	return cfg
}
