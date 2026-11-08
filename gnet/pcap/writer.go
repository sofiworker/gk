package pcap

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// Writer writes one capture format and closes its underlying io.Closer.
// Concurrent calls require external synchronization.
type Writer struct {
	cfg          writerConfig
	classic      *classicWriter
	ng           *ngWriter
	classicAdded bool
}

// NewWriter defaults to classic PCAP. WithFormat selects another format.
func NewWriter(w io.Writer, opts ...WriterOption) (*Writer, error) {
	cfg := writerConfig{
		format: FormatPCAP, byteOrder: binary.LittleEndian,
		resolution: time.Microsecond, versionMaj: 2, versionMin: 4,
		snapLen: 65535, network: 1, sectionMajor: 1, sectionLength: -1,
	}
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	writer := &Writer{cfg: cfg}
	var err error
	switch cfg.format {
	case FormatPCAP:
		writer.classic, err = newClassicWriter(w, cfg)
	case FormatPCAPNG:
		if cfg.network > 0xffff {
			return nil, fmt.Errorf("%w: pcapng link type %d exceeds uint16", ErrInvalidInterface, cfg.network)
		}
		writer.ng, err = newNGWriter(w, cfg)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, cfg.format)
	}
	if err != nil {
		return nil, fmt.Errorf("pcap: create %s writer: %w", cfg.format, err)
	}
	return writer, nil
}

// Format returns the selected file format.
func (w *Writer) Format() Format { return w.cfg.format }

// Header returns the classic PCAP file header, or a zero header for PCAPNG.
func (w *Writer) Header() FileHeader {
	if w.classic != nil {
		return w.classic.Header()
	}
	return FileHeader{}
}

// AddInterface registers a capture interface. Classic PCAP permits one
// interface matching WithLinkType and WithSnapLen, without interface options.
func (w *Writer) AddInterface(linkType, snapLen uint32, opts ...InterfaceOption) (uint32, error) {
	if w.ng != nil {
		if linkType > 0xffff {
			return 0, fmt.Errorf("%w: pcapng link type %d exceeds uint16", ErrInvalidInterface, linkType)
		}
		return w.ng.AddInterface(uint16(linkType), snapLen, opts...)
	}
	if w.classicAdded || linkType != w.cfg.network || snapLen != w.cfg.snapLen || len(opts) != 0 {
		return 0, fmt.Errorf("%w: classic pcap requires one interface matching the file header", ErrInvalidInterface)
	}
	w.classicAdded = true
	return 0, nil
}

// WritePacket writes a packet with its captured and original lengths.
// For PCAPNG, the first write to interface zero lazily registers an interface
// using WithLinkType, WithSnapLen, and WithTimestampResolution if needed.
func (w *Writer) WritePacket(pkt *Packet) error {
	if pkt == nil {
		return fmt.Errorf("%w: packet is nil", ErrInvalidPacket)
	}
	if w.ng != nil {
		if pkt.InterfaceID == 0 && w.ng.nextInterface == 0 {
			if _, err := w.AddInterface(w.cfg.network, w.cfg.snapLen); err != nil {
				return err
			}
		}
		return w.ng.WritePacket(pkt)
	}
	if pkt.InterfaceID != 0 {
		return fmt.Errorf("%w: %d", ErrUnknownInterface, pkt.InterfaceID)
	}
	return w.classic.WritePacket(pkt)
}

// WritePacketData writes data to interface zero.
func (w *Writer) WritePacketData(data []byte, ts time.Time) error {
	return w.WritePacket(&Packet{Data: data, Timestamp: ts})
}

// Close flushes buffered data and closes the underlying writer if supported.
func (w *Writer) Close() error {
	if w.ng != nil {
		return w.ng.Close()
	}
	return w.classic.Close()
}
