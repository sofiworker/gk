package pcapng

type BlockType uint32

const (
	SectionHeaderBlockType        BlockType = 0x0A0D0D0A
	InterfaceDescriptionBlockType BlockType = 0x00000001
	EnhancedPacketBlockType       BlockType = 0x00000006
)

type BlockHeader struct {
	Type        BlockType
	TotalLength uint32
}

type SectionHeaderBlock struct {
	BlockHeader
	ByteOrderMagic uint32
	MajorVersion   uint16
	MinorVersion   uint16
	SectionLength  int64
}

type InterfaceDescriptionBlock struct {
	BlockHeader
	LinkType uint16
	Reserved uint16
	SnapLen  uint32
}

type EnhancedPacketBlock struct {
	BlockHeader
	InterfaceID   uint32
	TimestampHigh uint32
	TimestampLow  uint32
	CapturedLen   uint32
	OriginalLen   uint32
}
