package rawcap

import (
	"encoding/binary"
	"io"
)

const (
	MagicNumber  = 0xA1B2C3D4
	VersionMajor = 2
	VersionMinor = 4
)

type FileHeader struct {
	MagicNumber  uint32
	VersionMajor uint16
	VersionMinor uint16
	ThisZone     int32
	SigFigs      uint32
	SnapLen      uint32
	Network      uint32
}

type PacketHeader struct {
	TsSec  uint32
	TsUsec uint32
	CapLen uint32
	Len    uint32
}

type pcapWriter struct {
	w io.Writer
}

func (w *pcapWriter) Close() error {
	//TODO implement me
	panic("implement me")
}

func NewWriter(w io.Writer) (PcapWriter, error) {
	header := FileHeader{
		MagicNumber:  MagicNumber,
		VersionMajor: VersionMajor,
		VersionMinor: VersionMinor,
		SnapLen:      65535,
		Network:      1,
	}

	if err := binary.Write(w, binary.LittleEndian, header); err != nil {
		return nil, err
	}

	return &pcapWriter{w: w}, nil
}

func (w *pcapWriter) WritePacket(pkt Packet) error {
	// 写入PCAP包头
	ts := pkt.Info.Timestamp
	header := PacketHeader{
		TsSec:  uint32(ts.Unix()),
		TsUsec: uint32(ts.Nanosecond() / 1000),
		CapLen: uint32(pkt.Info.CaptureLength),
		Len:    uint32(pkt.Info.Length),
	}

	if err := binary.Write(w.w, binary.LittleEndian, header); err != nil {
		return err
	}

	// 写入包数据
	_, err := w.w.Write(pkt.Data)
	return err
}
