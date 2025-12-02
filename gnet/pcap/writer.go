package pcap

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

type WriterOption func(*writerConfig) error

type writerConfig struct {
	byteOrder  binary.ByteOrder
	resolution time.Duration
	versionMaj uint16
	versionMin uint16
	thisZone   int32
	sigFigs    uint32
	snapLen    uint32
	network    uint32
	bufferSize int
}

type Writer struct {
	w         io.Writer
	buf       *bufio.Writer
	header    FileHeader
	byteOrder binary.ByteOrder
	tsUnit    time.Duration
	closer    io.Closer
}

func NewWriter(w io.Writer, opts ...WriterOption) (*Writer, error) {
	cfg := writerConfig{
		byteOrder:  binary.LittleEndian,
		resolution: time.Microsecond,
		versionMaj: 2,
		versionMin: 4,
		thisZone:   0,
		sigFigs:    0,
		snapLen:    65535,
		network:    1,
	}

	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	magic := selectMagic(cfg.byteOrder, cfg.resolution)
	header := FileHeader{
		MagicNumber:  magic,
		VersionMajor: cfg.versionMaj,
		VersionMinor: cfg.versionMin,
		ThisZone:     cfg.thisZone,
		SigFigs:      cfg.sigFigs,
		SnapLen:      cfg.snapLen,
		Network:      cfg.network,
	}

	writer := &Writer{
		w:         w,
		header:    header,
		byteOrder: cfg.byteOrder,
		tsUnit:    cfg.resolution,
	}

	if closer, ok := w.(io.Closer); ok {
		writer.closer = closer
	}

	if cfg.bufferSize > 0 {
		writer.buf = bufio.NewWriterSize(w, cfg.bufferSize)
		writer.w = writer.buf
	}

	if err := writer.writeHeader(); err != nil {
		return nil, err
	}

	return writer, nil
}

func (w *Writer) Header() FileHeader {
	return w.header
}

func (w *Writer) WritePacket(pkt *Packet) error {
	if pkt == nil {
		return fmt.Errorf("pcap: packet is nil")
	}

	header := pkt.Header
	switch {
	case !pkt.Timestamp.IsZero():
		header.SetTimestamp(pkt.Timestamp, w.tsUnit)
	case header.TsSec == 0 && header.TsUsec == 0:
		header.SetTimestamp(time.Now().UTC(), w.tsUnit)
	}

	if uint32(len(pkt.Data)) < header.InclLen {
		return fmt.Errorf("pcap: packet data shorter than captured length")
	}

	if header.InclLen == 0 {
		header.InclLen = uint32(len(pkt.Data))
	}

	if header.OrigLen == 0 {
		header.OrigLen = uint32(len(pkt.Data))
	}

	var hdrBytes [16]byte
	w.byteOrder.PutUint32(hdrBytes[0:4], header.TsSec)
	w.byteOrder.PutUint32(hdrBytes[4:8], header.TsUsec)
	w.byteOrder.PutUint32(hdrBytes[8:12], header.InclLen)
	w.byteOrder.PutUint32(hdrBytes[12:16], header.OrigLen)

	if _, err := w.w.Write(hdrBytes[:]); err != nil {
		return err
	}

	if _, err := w.w.Write(pkt.Data[:header.InclLen]); err != nil {
		return err
	}
	return nil
}

func (w *Writer) WritePacketData(data []byte, ts time.Time) error {
	packet := &Packet{
		Data:      data,
		Timestamp: ts,
	}
	return w.WritePacket(packet)
}

func (w *Writer) Close() error {
	if w.buf != nil {
		if err := w.buf.Flush(); err != nil {
			return err
		}
	}
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

func (w *Writer) writeHeader() error {
	var hdr [24]byte
	binary.BigEndian.PutUint32(hdr[0:4], w.header.MagicNumber)
	w.byteOrder.PutUint16(hdr[4:6], w.header.VersionMajor)
	w.byteOrder.PutUint16(hdr[6:8], w.header.VersionMinor)
	w.byteOrder.PutUint32(hdr[8:12], uint32(w.header.ThisZone))
	w.byteOrder.PutUint32(hdr[12:16], w.header.SigFigs)
	w.byteOrder.PutUint32(hdr[16:20], w.header.SnapLen)
	w.byteOrder.PutUint32(hdr[20:24], w.header.Network)
	_, err := w.w.Write(hdr[:])
	return err
}

func selectMagic(order binary.ByteOrder, resolution time.Duration) uint32 {
	isNano := resolution == time.Nanosecond
	if order == binary.BigEndian {
		if isNano {
			return MagicNumberNanoseconds
		}
		return MagicNumberMicroseconds
	}

	if isNano {
		return MagicNumberNanosecondsSwapped
	}
	return MagicNumberMicrosecondsSwapped
}

func WithSnapLen(snapLen uint32) WriterOption {
	return func(cfg *writerConfig) error {
		if snapLen == 0 {
			return fmt.Errorf("pcap: snap length must be positive")
		}
		cfg.snapLen = snapLen
		return nil
	}
}

func WithLinkType(linkType uint32) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.network = linkType
		return nil
	}
}

// WithBuffer 启用带缓冲写入以减少系统调用。
func WithBuffer(size int) WriterOption {
	return func(cfg *writerConfig) error {
		if size <= 0 {
			return fmt.Errorf("pcap: buffer size must be positive")
		}
		cfg.bufferSize = size
		return nil
	}
}

func WithByteOrder(order binary.ByteOrder) WriterOption {
	return func(cfg *writerConfig) error {
		if order != binary.BigEndian && order != binary.LittleEndian {
			return fmt.Errorf("pcap: unsupported byte order")
		}
		cfg.byteOrder = order
		return nil
	}
}

func WithTimestampResolution(resolution time.Duration) WriterOption {
	return func(cfg *writerConfig) error {
		switch resolution {
		case time.Microsecond, time.Nanosecond:
			cfg.resolution = resolution
			return nil
		default:
			return fmt.Errorf("pcap: unsupported timestamp resolution %s", resolution)
		}
	}
}

func WithVersion(major, minor uint16) WriterOption {
	return func(cfg *writerConfig) error {
		if major == 0 {
			return fmt.Errorf("pcap: version major must be positive")
		}
		cfg.versionMaj = major
		cfg.versionMin = minor
		return nil
	}
}

func WithTimeZone(zone int32) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.thisZone = zone
		return nil
	}
}

func WithSigFigs(sig uint32) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.sigFigs = sig
		return nil
	}
}
