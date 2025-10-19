package pcap

import (
	"encoding/binary"
	"time"
)

const (
	MagicNumberMicroseconds        uint32 = 0xa1b2c3d4
	MagicNumberMicrosecondsSwapped uint32 = 0xd4c3b2a1
	MagicNumberNanoseconds         uint32 = 0xa1b23c4d
	MagicNumberNanosecondsSwapped  uint32 = 0x4d3cb2a1
)

type FileHeader struct {
	MagicNumber  uint32
	VersionMajor uint16
	VersionMinor uint16
	ThisZone     int32
	SigFigs      uint32
	SnapLen      uint32
	Network      uint32
}

type PacketHeader struct {
	TsSec   uint32
	TsUsec  uint32
	InclLen uint32
	OrigLen uint32
}

type Packet struct {
	Header    PacketHeader
	Data      []byte
	Timestamp time.Time
}

func (h *FileHeader) IsLittleEndian() bool {
	switch h.MagicNumber {
	case MagicNumberMicrosecondsSwapped, MagicNumberNanosecondsSwapped:
		return true
	case MagicNumberMicroseconds, MagicNumberNanoseconds:
		return false
	default:
		return false
	}
}

func (h *FileHeader) ByteOrder() binary.ByteOrder {
	if h.IsLittleEndian() {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

func (h *FileHeader) TimestampResolution() time.Duration {
	switch h.MagicNumber {
	case MagicNumberNanoseconds, MagicNumberNanosecondsSwapped:
		return time.Nanosecond
	default:
		return time.Microsecond
	}
}

func (h *PacketHeader) GetTimestamp() time.Time {
	return time.Unix(int64(h.TsSec), int64(h.TsUsec)*1000).UTC()
}

func (h *PacketHeader) SetTimestamp(ts time.Time, resolution time.Duration) {
	h.TsSec = uint32(ts.Unix())
	switch resolution {
	case time.Nanosecond:
		h.TsUsec = uint32(ts.Nanosecond())
	default:
		h.TsUsec = uint32(ts.Nanosecond() / 1000)
	}
}

func (p *Packet) CaptureLength() int {
	return len(p.Data)
}

func (p *Packet) OriginalLength() int {
	if p.Header.OrigLen == 0 {
		return len(p.Data)
	}
	return int(p.Header.OrigLen)
}
