package pcapng

import (
	"time"
)

type BlockType uint32

const (
	SectionHeaderBlockType        BlockType = 0x0A0D0D0A
	InterfaceDescriptionBlockType BlockType = 0x00000001
	EnhancedPacketBlockType       BlockType = 0x00000006
)

const (
	ByteOrderMagicLittle uint32 = 0x1A2B3C4D
	ByteOrderMagicBig    uint32 = 0x4D3C2B1A
)

type Option struct {
	Code  uint16
	Value []byte
}

type Block interface {
	BlockType() BlockType
}

type BlockHeader struct {
	Type        BlockType
	TotalLength uint32
}

func (h BlockHeader) BlockType() BlockType {
	return h.Type
}

type SectionHeaderBlock struct {
	BlockHeader
	ByteOrderMagic uint32
	MajorVersion   uint16
	MinorVersion   uint16
	SectionLength  int64
	Options        []Option
}

type InterfaceDescriptionBlock struct {
	BlockHeader
	ID       uint32
	LinkType uint16
	Reserved uint16
	SnapLen  uint32
	Options  []Option
}

type EnhancedPacketBlock struct {
	BlockHeader
	InterfaceID   uint32
	TimestampHigh uint32
	TimestampLow  uint32
	CapturedLen   uint32
	OriginalLen   uint32
	PacketData    []byte
	Options       []Option
}

func (b *EnhancedPacketBlock) BlockType() BlockType {
	return b.Type
}

func (b *EnhancedPacketBlock) Timestamp(resolution time.Duration) time.Time {
	combined := (uint64(b.TimestampHigh) << 32) | uint64(b.TimestampLow)
	switch resolution {
	case time.Nanosecond:
		seconds := int64(combined / 1_000_000_000)
		nanos := int64(combined % 1_000_000_000)
		return time.Unix(seconds, nanos).UTC()
	default:
		seconds := int64(combined / 1_000_000)
		micros := int64(combined % 1_000_000)
		return time.Unix(seconds, micros*1000).UTC()
	}
}
