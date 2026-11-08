package rawcap

import (
	"fmt"
	"io"

	"github.com/sofiworker/gk/gnet/pcap"
)

const (
	MagicNumber  = pcap.MagicNumberMicroseconds
	VersionMajor = 2
	VersionMinor = 4
)

// FileHeader is the classic PCAP file header.
type FileHeader = pcap.FileHeader

type PacketHeader struct {
	TsSec  uint32
	TsUsec uint32
	CapLen uint32
	Len    uint32
}

type pcapWriter struct{ writer *pcap.Writer }

func (w *pcapWriter) Close() error { return w.writer.Close() }

// NewWriter adapts live packets to the shared capture-file writer.
func NewWriter(w io.Writer) (PcapWriter, error) {
	writer, err := pcap.NewWriter(w)
	if err != nil {
		return nil, err
	}
	return &pcapWriter{writer: writer}, nil
}

func (w *pcapWriter) WritePacket(pkt *Packet) error {
	if pkt == nil || pkt.Info == nil {
		return fmt.Errorf("%w: live packet or capture info is nil", pcap.ErrInvalidPacket)
	}
	if pkt.Info.CaptureLength < 0 || pkt.Info.Length < 0 ||
		uint64(pkt.Info.CaptureLength) > uint64(len(pkt.Data)) || uint64(pkt.Info.Length) > 0xffffffff {
		return fmt.Errorf("%w: invalid live packet lengths", pcap.ErrInvalidPacket)
	}
	return w.writer.WritePacket(&pcap.Packet{
		Data: pkt.Data, Timestamp: pkt.Info.Timestamp,
		CapturedLen: uint32(pkt.Info.CaptureLength), OriginalLen: uint32(pkt.Info.Length),
	})
}
