package pcap

import (
	"encoding/binary"
	"time"
)

type FileHeader struct {
	MagicNumber  uint32
	VersionMajor uint16
	VersionMinor uint16
	ThisZone     uint32
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
}

func (h *PacketHeader) GetTimestamp() time.Time {
	return time.Unix(int64(h.TsSec), int64(h.TsUsec)*1000).UTC()
}

func (h *FileHeader) IsLittleEndian() bool {
	return h.MagicNumber == 0xD4C3B2A1 || h.MagicNumber == 0x1A2B3C4D
}

func (h *FileHeader) GetByteOrder() binary.ByteOrder {
	if h.IsLittleEndian() {
		return binary.LittleEndian
	}
	return binary.BigEndian
}
