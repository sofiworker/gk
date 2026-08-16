package sniff

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
	"github.com/sofiworker/gk/gnet/packet"
)

// buildTCPPacket 构造 TCP 报文（以太网+IPv4+TCP）。
func buildTCPPacket(t *testing.T, src, dst string, srcPort, dstPort uint16, seq uint32, flags uint16, payload string) *packet.Packet {
	t.Helper()
	eth := &layers.Ethernet{DstMAC: make(net.HardwareAddr, 6), SrcMAC: make(net.HardwareAddr, 6), EtherType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, ID: 1, TTL: 64, Protocol: layers.ProtocolTCP, SrcIP: net.ParseIP(src), DstIP: net.ParseIP(dst)}
	tcp := &layers.TCP{SrcPort: srcPort, DstPort: dstPort, Seq: seq, Flags: flags, Window: 65535}
	buf, err := layers.SerializeLayers(layers.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, ip, tcp, layers.Payload(payload))
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return packet.FromBytes(buf)
}

// recordingAnalyzer 记录流事件。
type recordingAnalyzer struct {
	mu     sync.Mutex
	events []FlowEvent
}

func (a *recordingAnalyzer) OnFlowEvent(ev FlowEvent) {
	a.mu.Lock()
	a.events = append(a.events, ev)
	a.mu.Unlock()
}

func (a *recordingAnalyzer) snapshot() []FlowEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]FlowEvent(nil), a.events...)
}

// TestSnifferTCPOrdered 覆盖顺序 TCP 流的完整事件序列。
func TestSnifferTCPOrdered(t *testing.T) {
	pkts := []*packet.Packet{
		buildTCPPacket(t, "192.168.1.1", "10.0.0.1", 12345, 80, 100, 0, "hello"),
		buildTCPPacket(t, "192.168.1.1", "10.0.0.1", 12345, 80, 105, 0, " world"),
		buildTCPPacket(t, "192.168.1.1", "10.0.0.1", 12345, 80, 111, layers.TCPFlagFIN, ""),
	}
	a := &recordingAnalyzer{}
	s := New(NewSliceSource(pkts...), a)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := a.snapshot()
	if len(events) != 4 { // OpNew + 2×OpData + OpEnd
		t.Fatalf("events = %d, want 4: %+v", len(events), events)
	}
	if events[0].Op != OpNew || events[1].Op != OpData || events[2].Op != OpData || events[3].Op != OpEnd {
		t.Fatalf("op sequence = %v", events)
	}
	if string(events[1].Data) != "hello" || string(events[2].Data) != " world" {
		t.Fatalf("data = %q/%q", events[1].Data, events[2].Data)
	}
	if got := s.tracker.Flows(); len(got) != 1 || got[0].Bytes == 0 {
		t.Fatalf("flows = %+v", got)
	}
}

// TestSnifferTCPOutOfOrder 覆盖乱序段暂存与连续交付。
func TestSnifferTCPOutOfOrder(t *testing.T) {
	pkts := []*packet.Packet{
		buildTCPPacket(t, "192.168.1.1", "10.0.0.1", 12345, 80, 100, 0, "AB"), // next→102
		buildTCPPacket(t, "192.168.1.1", "10.0.0.1", 12345, 80, 104, 0, "DE"), // 乱序：先到 seq=104
		buildTCPPacket(t, "192.168.1.1", "10.0.0.1", 12345, 80, 102, 0, "CD"), // 补 seq=102 → 排空 104
	}
	a := &recordingAnalyzer{}
	s := New(NewSliceSource(pkts...), a)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := a.snapshot()
	var got string
	for _, ev := range events {
		if ev.Op == OpData {
			got += string(ev.Data)
		}
	}
	// "AB"+"CD"+"DE" 的字符串拼接结果（"CD" 尾 D 与 "DE" 头 D 相邻）。
	if got != "ABCDDE" {
		t.Fatalf("reassembled = %q, want ABCDDE", got)
	}
}

// TestSnifferUDP 覆盖 UDP 直接交付与双向流区分。
func TestSnifferUDP(t *testing.T) {
	pkts := []*packet.Packet{
		func() *packet.Packet {
			eth := &layers.Ethernet{DstMAC: make(net.HardwareAddr, 6), SrcMAC: make(net.HardwareAddr, 6), EtherType: layers.EthernetTypeIPv4}
			ip := &layers.IPv4{Version: 4, ID: 1, TTL: 64, Protocol: layers.ProtocolUDP, SrcIP: net.ParseIP("1.1.1.1"), DstIP: net.ParseIP("2.2.2.2")}
			udp := &layers.UDP{SrcPort: 1000, DstPort: 2000}
			buf, err := layers.SerializeLayers(layers.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, ip, udp, layers.Payload("ping"))
			if err != nil {
				t.Fatal(err)
			}
			return packet.FromBytes(buf)
		}(),
	}
	a := &recordingAnalyzer{}
	s := New(NewSliceSource(pkts...), a)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := a.snapshot()
	if len(events) != 2 || events[1].Op != OpData || string(events[1].Data) != "ping" {
		t.Fatalf("events = %+v", events)
	}
	if len(s.tracker.Flows()) != 1 {
		t.Fatalf("flows = %d, want 1", len(s.tracker.Flows()))
	}
}

// TestFlowTrackerExpire 覆盖流老化。
func TestFlowTrackerExpire(t *testing.T) {
	tr := NewFlowTracker()
	pkt := buildTCPPacket(t, "192.168.1.1", "10.0.0.1", 1, 2, 1, 0, "x")
	pkt.Timestamp = time.Now().Add(-time.Minute)
	tr.Track(pkt)
	if len(tr.Expire(30*time.Second)) != 1 {
		t.Fatal("Expire returned no flows")
	}
	if len(tr.Flows()) != 0 {
		t.Fatal("flow not removed after expire")
	}
}

// TestFlowKeyReverse 覆盖五元组反转。
func TestFlowKeyReverse(t *testing.T) {
	k := FlowKey{SrcIP: net.IPv4(1, 1, 1, 1), DstIP: net.IPv4(2, 2, 2, 2), SrcPort: 1, DstPort: 2, Proto: 6}
	r := k.Reverse()
	if !r.SrcIP.Equal(k.DstIP) || r.SrcPort != k.DstPort || r.DstPort != k.SrcPort || r.Proto != 6 {
		t.Fatalf("reverse = %+v", r)
	}
}
