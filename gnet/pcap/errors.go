package pcap

import "errors"

var (
	ErrInvalidMagicNumber  = errors.New("pcap: invalid magic number")
	ErrInvalidFileHeader   = errors.New("pcap: invalid file header")
	ErrInvalidPacketHeader = errors.New("pcap: invalid packet header")
)

