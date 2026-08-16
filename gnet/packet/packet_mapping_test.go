package packet

import (
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
	"github.com/sofiworker/gk/gnet/pcap"
	"github.com/sofiworker/gk/gnet/pcapng"
	"github.com/sofiworker/gk/gnet/rawcap"
)

// TestFromBytesAndParse 覆盖构造入口与解析缓存。
// TestFromBytesAndParse covers the crafting entry and the parse cache.
func TestFromBytesAndParse(t *testing.T) {
	p := FromBytes([]byte{0x01})
	if p.CaptureLen != 1 || p.OriginalLen != 1 || p.Timestamp.IsZero() {
		t.Fatalf("FromBytes = %+v", p)
	}
	// 缓存：两次 Parse 结果一致（1 字节数据解析必然失败，缓存错误）。
	err1 := error(nil)
	err2 := error(nil)
	_, err1 = p.Parse()
	_, err2 = p.Parse()
	if err1 == nil || err1 != err2 {
		t.Fatalf("parse cache mismatch: %v vs %v", err1, err2)
	}
}

// TestFromAdapters 覆盖三种来源的字段映射。
// TestFromAdapters covers field mapping from the three adapters.
func TestFromAdapters(t *testing.T) {
	ts := time.Unix(1000, 2000)
	// rawcap
	rp := &rawcap.Packet{
		Data: []byte{1, 2, 3},
		Info: &rawcap.PacketInfo{Timestamp: ts, CaptureLength: 3, Length: 10, InterfaceIndex: 7},
	}
	p := FromRawcap(rp)
	if p.CaptureLen != 3 || p.OriginalLen != 10 || p.InterfaceID != 7 || !p.Timestamp.Equal(ts) {
		t.Fatalf("FromRawcap = %+v", p)
	}

	// pcap
	pp := &pcap.Packet{
		Data:      []byte{4, 5},
		Timestamp: ts,
		Header:    pcap.PacketHeader{OrigLen: 20},
	}
	p = FromPCAP(pp)
	if p.CaptureLen != 2 || p.OriginalLen != 20 || !p.Timestamp.Equal(ts) {
		t.Fatalf("FromPCAP = %+v", p)
	}

	// pcapng
	np := &pcapng.Packet{
		Data:        []byte{6},
		Timestamp:   ts,
		CapturedLen: 1,
		OriginalLen: 30,
		InterfaceID: 2,
	}
	p = FromPCAPNG(np)
	if p.CaptureLen != 1 || p.OriginalLen != 30 || p.InterfaceID != 2 || !p.Timestamp.Equal(ts) {
		t.Fatalf("FromPCAPNG = %+v", p)
	}

	// nil 防御。
	if FromRawcap(nil) != nil || FromPCAP(nil) != nil || FromPCAPNG(nil) != nil {
		t.Fatal("nil adapter should yield nil")
	}
}

// TestNetworkTransportLayer 覆盖层定位（用真实构造报文）。
// TestNetworkTransportLayer covers layer lookup with a real crafted packet.
func TestNetworkTransportLayer(t *testing.T) {
	eth := &layers.Ethernet{DstMAC: make([]byte, 6), SrcMAC: make([]byte, 6), EtherType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, ID: 1, TTL: 64, Protocol: layers.ProtocolTCP,
		SrcIP: []byte{1, 1, 1, 1}, DstIP: []byte{2, 2, 2, 2}}
	tcp := &layers.TCP{SrcPort: 1, DstPort: 2, Seq: 1, Window: 1024}
	buf, err := layers.SerializeLayers(layers.SerializeOptions{FixLengths: true, ComputeChecksums: true},
		eth, ip, tcp, layers.Payload("x"))
	if err != nil {
		t.Fatal(err)
	}
	p := FromBytes(buf)
	if _, ok := p.NetworkLayer().(*layers.IPv4); !ok {
		t.Fatalf("NetworkLayer = %T", p.NetworkLayer())
	}
	if _, ok := p.TransportLayer().(*layers.TCP); !ok {
		t.Fatalf("TransportLayer = %T", p.TransportLayer())
	}
}
