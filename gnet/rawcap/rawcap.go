package rawcap

import "time"

const (
	DefaultSnapLen    = 65535
	DefaultBufferSize = 4 << 20 // 4 MiB
)

type Config struct {
	SnapLen     int
	Promiscuous bool
	Timeout     time.Duration
	BufferSize  int

	// Linux 专用性能选项
	TPacketV3 bool // 启用 PACKET_RX_RING/TPACKET_V3
	BlockSize int  // TPacket V3 block 大小
	NumBlocks int  // TPacket V3 block 数量
	FrameSize int  // TPacket V3 frame 大小
}

type Handle interface {
	ReadPacket() (*Packet, error)
	WritePacketData([]byte) error
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
	if cfg.TPacketV3 {
		if cfg.BlockSize <= 0 {
			cfg.BlockSize = 1 << 20 // 1 MiB
		}
		if cfg.NumBlocks <= 0 {
			cfg.NumBlocks = 8
		}
		if cfg.FrameSize <= 0 {
			cfg.FrameSize = 2048
		}
	}
	return cfg
}
