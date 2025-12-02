package packet

import (
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
	"github.com/sofiworker/gk/gnet/pcapng"
	"github.com/sofiworker/gk/gnet/rawcap"
)

// Packet 是统一的捕获数据结构。
type Packet struct {
	Data        []byte
	Timestamp   time.Time
	CaptureLen  int
	OriginalLen int
	InterfaceID int
}

func FromRawcap(pkt *rawcap.Packet) *Packet {
	if pkt == nil {
		return nil
	}
	return &Packet{
		Data:        pkt.Data,
		Timestamp:   pkt.Info.Timestamp,
		CaptureLen:  pkt.Info.CaptureLength,
		OriginalLen: pkt.Info.Length,
		InterfaceID: pkt.Info.InterfaceIndex,
	}
}

func FromPCAP(pkt *pcap.Packet) *Packet {
	if pkt == nil {
		return nil
	}
	return &Packet{
		Data:        pkt.Data,
		Timestamp:   pkt.Timestamp,
		CaptureLen:  len(pkt.Data),
		OriginalLen: int(pkt.Header.OrigLen),
	}
}

func FromPCAPNG(pkt *pcapng.Packet) *Packet {
	if pkt == nil {
		return nil
	}
	return &Packet{
		Data:        pkt.Data,
		Timestamp:   pkt.Timestamp,
		CaptureLen:  int(pkt.CapturedLen),
		OriginalLen: int(pkt.OriginalLen),
		InterfaceID: int(pkt.InterfaceID),
	}
}
