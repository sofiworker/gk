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

// pcapng 规范：SHB 的 Byte-Order Magic 字段字节序列固定，
// 1A 2B 3C 4D 表示后续字段为大端，4D 3C 2B 1A 表示小端。
const (
	ByteOrderMagicBig    uint32 = 0x1A2B3C4D
	ByteOrderMagicLittle uint32 = 0x4D3C2B1A
)

// maxBlockLength 是单块分配上限（远超真实捕获块，防恶意文件）。
// maxBlockLength caps per-block allocation (far beyond real captures;
// guards hostile files).
const maxBlockLength = 256 << 20

// UnknownBlock 是未识别的块类型（原样保留，供跳过或透传）。
// UnknownBlock is an unrecognized block type (kept raw for skipping or
// passthrough).
type UnknownBlock struct {
	Type BlockType
	Body []byte
}

// BlockType 实现 Block。
// BlockType implements Block.
func (u *UnknownBlock) BlockType() BlockType { return u.Type }

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
