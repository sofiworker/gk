package replay

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/craft"
	"github.com/sofiworker/gk/gnet/layers"
	"github.com/sofiworker/gk/gnet/packet"
)

// buildIPv4TCPFrame 构造帧并返回字节。
func buildIPv4TCPFrame(t *testing.T, srcIP, dstIP string, payload string) []byte {
	t.Helper()
	eth := &layers.Ethernet{
		DstMAC:    net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		SrcMAC:    net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EtherType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{Version: 4, ID: 1, TTL: 64, Protocol: layers.ProtocolTCP,
		SrcIP: net.ParseIP(srcIP), DstIP: net.ParseIP(dstIP)}
	tcp := &layers.TCP{SrcPort: 12345, DstPort: 80, Seq: 1, Window: 65535}
	buf, err := layers.SerializeLayers(layers.SerializeOptions{FixLengths: true, ComputeChecksums: true},
		eth, ip, tcp, layers.Payload(payload))
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

// countingInjector 记录注入的报文。
type countingInjector struct {
	mu     sync.Mutex
	frames [][]byte
	times  []time.Time
}

func (c *countingInjector) Inject(pkt []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames = append(c.frames, append([]byte(nil), pkt...))
	c.times = append(c.times, time.Now())
	return nil
}

func (c *countingInjector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

func (c *countingInjector) frame(i int) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frames[i]
}

// TestReplayTimestampPacing 覆盖原始时间戳节奏与倍速。
func TestReplayTimestampPacing(t *testing.T) {
	now := time.Now()
	pkts := []*packet.Packet{
		{Data: []byte{1}, Timestamp: now},
		{Data: []byte{2}, Timestamp: now.Add(100 * time.Millisecond)},
		{Data: []byte{3}, Timestamp: now.Add(200 * time.Millisecond)},
	}
	dst := &countingInjector{}
	r, err := New(NewSliceSource(pkts...), dst, WithMultiplier(20))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if dst.count() != 3 {
		t.Fatalf("injected = %d, want 3", dst.count())
	}
	// 200ms / 20 = 10ms，留足余量。
	if elapsed < 8*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("elapsed = %v, want ~10ms", elapsed)
	}
}

// TestReplayPPS 覆盖固定 pps 节奏。
func TestReplayPPS(t *testing.T) {
	var pkts []*packet.Packet
	for i := 0; i < 5; i++ {
		pkts = append(pkts, &packet.Packet{Data: []byte{byte(i)}})
	}
	dst := &countingInjector{}
	r, err := New(NewSliceSource(pkts...), dst, WithPacing(200))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if dst.count() != 5 {
		t.Fatalf("injected = %d, want 5", dst.count())
	}
	// 5 包 @200pps ≈ 25ms。
	if elapsed < 15*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("elapsed = %v, want ~25ms", elapsed)
	}
}

// TestReplayLoop 覆盖循环回放与 ctx 取消。
func TestReplayLoop(t *testing.T) {
	pkts := []*packet.Packet{{Data: []byte{1}}, {Data: []byte{2}}}
	dst := &countingInjector{}
	r, err := New(NewSliceSource(pkts...), dst, WithLoop(3), WithPacing(1000))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if dst.count() != 6 {
		t.Fatalf("injected = %d, want 6 (3 loops × 2)", dst.count())
	}

	// 无限循环 + ctx 取消。
	ctx, cancel := context.WithCancel(context.Background())
	dst2 := &countingInjector{}
	r2, err := New(NewSliceSource(pkts...), dst2, WithLoop(-1), WithPacing(10000))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r2.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Run = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	if dst2.count() == 0 {
		t.Fatal("no packets injected before cancel")
	}
}

// TestReplayRewrite 覆盖 IP+MAC 重写与校验和有效性。
func TestReplayRewrite(t *testing.T) {
	frame := buildIPv4TCPFrame(t, "192.168.1.1", "10.0.0.1", "hello")
	dst := &countingInjector{}
	r, err := New(NewSliceSource(packet.FromBytes(frame)), dst,
		WithRewrite("172.16.0.1", "172.16.0.2", "02:00:00:00:00:01", "02:00:00:00:00:02"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := dst.frame(0)
	// MAC 重写。
	if binary.BigEndian.Uint16(out[0:2]) != 0x0200 || binary.BigEndian.Uint16(out[6:8]) != 0x0200 {
		t.Fatalf("MAC not rewritten: %x", out[:12])
	}
	// IP 重写 + 解码验证。
	ls, err := layers.Decode(out)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	ip := ls[1].(*layers.IPv4)
	if !ip.SrcIP.Equal(net.ParseIP("172.16.0.1")) || !ip.DstIP.Equal(net.ParseIP("172.16.0.2")) {
		t.Fatalf("IPs = %v -> %v", ip.SrcIP, ip.DstIP)
	}
	// IPv4 头校验和有效。
	if layers.InternetChecksum(ip.Contents) != 0 {
		t.Fatalf("IPv4 checksum invalid: %#04x", ip.Checksum)
	}
	// TCP 伪头校验和有效。
	tcp := ls[2].(*layers.TCP)
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], ip.SrcIP.To4())
	copy(pseudo[4:8], ip.DstIP.To4())
	pseudo[9] = 6
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcp.Contents)+len(tcp.Payload())))
	full := append(pseudo, tcp.Contents...)
	full = append(full, tcp.Payload()...)
	if layers.InternetChecksum(full) != 0 {
		t.Fatalf("TCP checksum invalid: %#04x", tcp.Checksum)
	}
}

// TestReplayConnInjector 覆盖经 craft 连接注入的端到端回放。
func TestReplayConnInjector(t *testing.T) {
	frame := buildIPv4TCPFrame(t, "192.168.1.1", "10.0.0.1", "x")
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	go func() {
		buf := make([]byte, len(frame))
		_, _ = io.ReadFull(cli, buf)
	}()

	dst := craft.NewConnInjector(srv)
	r, err := New(NewSliceSource(packet.FromBytes(frame)), dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
