package pcap

import "errors"

var (
	ErrUnsupportedFormat   = errors.New("pcap: unsupported format")
	ErrUnknownInterface    = errors.New("pcap: unknown interface")
	ErrInvalidInterface    = errors.New("pcap: invalid interface")
	ErrInvalidPacket       = errors.New("pcap: invalid packet")
	ErrInvalidMagicNumber  = errors.New("pcap: invalid magic number")
	ErrInvalidFileHeader   = errors.New("pcap: invalid file header")
	ErrInvalidPacketHeader = errors.New("pcap: invalid packet header")
	ErrInvalidBlockLength  = errors.New("pcapng: invalid block length")
	ErrUnexpectedBlock     = errors.New("pcapng: unexpected block type")
	ErrInvalidSection      = errors.New("pcapng: invalid section header")
)
