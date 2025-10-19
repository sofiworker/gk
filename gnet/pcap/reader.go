package pcap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

type Reader struct {
	r         io.Reader
	header    FileHeader
	byteOrder binary.ByteOrder
	tsUnit    time.Duration
}

func NewReader(r io.Reader) (*Reader, error) {
	var hdrBytes [24]byte
	if _, err := io.ReadFull(r, hdrBytes[:]); err != nil {
		return nil, err
	}

	magic := binary.BigEndian.Uint32(hdrBytes[0:4])
	switch magic {
	case MagicNumberMicroseconds, MagicNumberNanoseconds,
		MagicNumberMicrosecondsSwapped, MagicNumberNanosecondsSwapped:
	default:
		return nil, ErrInvalidMagicNumber
	}

	header := FileHeader{MagicNumber: magic}
	order := header.ByteOrder()

	header.VersionMajor = order.Uint16(hdrBytes[4:6])
	header.VersionMinor = order.Uint16(hdrBytes[6:8])
	header.ThisZone = int32(order.Uint32(hdrBytes[8:12]))
	header.SigFigs = order.Uint32(hdrBytes[12:16])
	header.SnapLen = order.Uint32(hdrBytes[16:20])
	header.Network = order.Uint32(hdrBytes[20:24])

	reader := &Reader{
		r:         r,
		header:    header,
		byteOrder: order,
		tsUnit:    header.TimestampResolution(),
	}
	return reader, nil
}

func (r *Reader) Header() FileHeader {
	return r.header
}

func (r *Reader) ReadPacket() (*Packet, error) {
	var hdrBytes [16]byte
	if _, err := io.ReadFull(r.r, hdrBytes[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrInvalidPacketHeader
		}
		return nil, err
	}

	header := PacketHeader{
		TsSec:   r.byteOrder.Uint32(hdrBytes[0:4]),
		TsUsec:  r.byteOrder.Uint32(hdrBytes[4:8]),
		InclLen: r.byteOrder.Uint32(hdrBytes[8:12]),
		OrigLen: r.byteOrder.Uint32(hdrBytes[12:16]),
	}

	if r.header.SnapLen > 0 && header.InclLen > r.header.SnapLen {
		return nil, fmt.Errorf("pcap: captured length %d exceeds snap length %d", header.InclLen, r.header.SnapLen)
	}

	data := make([]byte, header.InclLen)
	if _, err := io.ReadFull(r.r, data); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrInvalidPacketHeader
		}
		return nil, err
	}

	subSecond := int64(header.TsUsec)
	if r.tsUnit == time.Microsecond {
		subSecond *= int64(time.Microsecond)
	} else {
		subSecond *= int64(time.Nanosecond)
	}

	packet := &Packet{
		Header:    header,
		Data:      data,
		Timestamp: time.Unix(int64(header.TsSec), subSecond).UTC(),
	}
	return packet, nil
}
