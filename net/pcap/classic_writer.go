package pcap

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

type classicWriter struct {
	w         io.Writer
	buf       *bufio.Writer
	header    FileHeader
	byteOrder binary.ByteOrder
	tsUnit    time.Duration
	closer    io.Closer
}

func newClassicWriter(w io.Writer, cfg writerConfig) (*classicWriter, error) {
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

	writer := &classicWriter{
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

func (w *classicWriter) Header() FileHeader {
	return w.header
}

func (w *classicWriter) WritePacket(pkt *Packet) error {
	header := pkt.Header
	if pkt.CapturedLen != 0 {
		header.InclLen = pkt.CapturedLen
	}
	if pkt.OriginalLen != 0 {
		header.OrigLen = pkt.OriginalLen
	}
	switch {
	case !pkt.Timestamp.IsZero():
		header.SetTimestamp(pkt.Timestamp, w.tsUnit)
	case header.TsSec == 0 && header.TsUsec == 0:
		header.SetTimestamp(time.Now().UTC(), w.tsUnit)
	}

	if uint32(len(pkt.Data)) < header.InclLen {
		return fmt.Errorf("%w: packet data shorter than captured length", ErrInvalidPacket)
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

func (w *classicWriter) Close() error {
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

func (w *classicWriter) writeHeader() error {
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
