package packet

import (
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
	"github.com/sofiworker/gk/gnet/pcap"
	"github.com/sofiworker/gk/gnet/pcapng"
	"github.com/sofiworker/gk/gnet/rawcap"
)

// Packet 是统一的捕获数据结构：既承载原始报文，也缓存 layers 解析结果，
// 供解析（Parse）与构造（craft.Serialize → FromBytes）共用。
//
// Packet is the unified capture data structure: it carries the raw bytes and
// caches the layers parse result, shared by parsing (Parse) and crafting
// (craft.Serialize → FromBytes).
type Packet struct {
	Data        []byte
	Timestamp   time.Time
	CaptureLen  int
	OriginalLen int
	InterfaceID int

	parseMu  sync.Mutex
	parsed   []layers.Layer
	parseErr error
}

// FromBytes 从原始报文构造 Packet（构造产物可直接走解析路径）。
// FromBytes builds a Packet from raw bytes (crafted packets flow straight
// into the parse path).
func FromBytes(data []byte) *Packet {
	return &Packet{
		Data:        data,
		Timestamp:   time.Now(),
		CaptureLen:  len(data),
		OriginalLen: len(data),
	}
}

// Parse 按以太网链解析并缓存结果；未解析时惰性解析。
// Parse decodes the packet as an Ethernet chain and caches the result;
// decoding is lazy.
func (p *Packet) Parse() ([]layers.Layer, error) {
	p.parseMu.Lock()
	defer p.parseMu.Unlock()
	if p.parsed == nil && p.parseErr == nil {
		p.parsed, p.parseErr = layers.Decode(p.Data)
	}
	return p.parsed, p.parseErr
}

// NetworkLayer 返回首个 IPv4/IPv6 层（可能为 nil）。
// NetworkLayer returns the first IPv4/IPv6 layer (may be nil).
func (p *Packet) NetworkLayer() layers.Layer {
	ls, err := p.Parse()
	if err != nil {
		return nil
	}
	for _, l := range ls {
		switch l.(type) {
		case *layers.IPv4, *layers.IPv6:
			return l
		}
	}
	return nil
}

// TransportLayer 返回首个 TCP/UDP/ICMP 层（可能为 nil）。
// TransportLayer returns the first TCP/UDP/ICMP layer (may be nil).
func (p *Packet) TransportLayer() layers.Layer {
	ls, err := p.Parse()
	if err != nil {
		return nil
	}
	for _, l := range ls {
		switch l.(type) {
		case *layers.TCP, *layers.UDP, *layers.ICMP:
			return l
		}
	}
	return nil
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
		OriginalLen: pkt.OriginalLength(), // OrigLen==0 时回退 len(Data)
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
