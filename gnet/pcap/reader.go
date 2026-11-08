package pcap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// Reader reads packets from any supported capture format.
// It does not close the underlying reader.
type Reader struct {
	format  Format
	classic *classicReader
	ng      *ngReader
}

// NewReader detects the format from the file signature, without seeking.
// PCAPNG blocks are parsed lazily by ReadPacket or NextBlock.
func NewReader(r io.Reader) (*Reader, error) {
	var signature [4]byte
	if _, err := io.ReadFull(r, signature[:]); err != nil {
		return nil, fmt.Errorf("pcap: read signature: %w", err)
	}
	input := io.MultiReader(bytes.NewReader(signature[:]), r)
	switch binary.BigEndian.Uint32(signature[:]) {
	case uint32(SectionHeaderBlockType):
		return &Reader{format: FormatPCAPNG, ng: newNGReader(input)}, nil
	case MagicNumberMicroseconds, MagicNumberNanoseconds,
		MagicNumberMicrosecondsSwapped, MagicNumberNanosecondsSwapped:
	default:
		return nil, ErrInvalidMagicNumber
	}
	classic, err := newClassicReader(input)
	if err != nil {
		return nil, fmt.Errorf("pcap: read header: %w", err)
	}
	return &Reader{format: FormatPCAP, classic: classic}, nil
}

// Format returns the detected file format.
func (r *Reader) Format() Format { return r.format }

// Header returns the classic PCAP file header, or a zero header for PCAPNG.
func (r *Reader) Header() FileHeader {
	if r.classic != nil {
		return r.classic.Header()
	}
	return FileHeader{}
}

// ReadPacket returns the next packet, skipping non-packet PCAPNG blocks.
func (r *Reader) ReadPacket() (*Packet, error) {
	if r.ng != nil {
		return r.ng.ReadPacket()
	}
	return r.classic.ReadPacket()
}

// InterfaceInfo returns link type, snap length, and timestamp resolution.
// PCAPNG interfaces become available as their description blocks are read.
func (r *Reader) InterfaceInfo(id uint32) (linkType, snapLen uint32, resolution time.Duration, ok bool) {
	if r.ng != nil {
		link, snap, res, found := r.ng.InterfaceInfo(id)
		return uint32(link), snap, res, found
	}
	if id != 0 {
		return 0, 0, 0, false
	}
	header := r.classic.Header()
	return header.Network, header.SnapLen, header.TimestampResolution(), true
}

// CurrentSection returns the most recently read PCAPNG section, or nil.
func (r *Reader) CurrentSection() *SectionHeaderBlock {
	if r.ng == nil {
		return nil
	}
	return r.ng.CurrentSection()
}

// NextBlock reads a PCAPNG block. Classic PCAP returns ErrUnsupportedFormat.
func (r *Reader) NextBlock() (Block, error) {
	if r.ng == nil {
		return nil, fmt.Errorf("%w: blocks require pcapng", ErrUnsupportedFormat)
	}
	return r.ng.NextBlock()
}
