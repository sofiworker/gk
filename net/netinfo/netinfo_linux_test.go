//go:build linux

package netinfo

import (
	"net"
	"testing"
)

// TestConnectionsParsing 覆盖连接表解析（含 IPv4/IPv6 与状态）。
func TestConnectionsParsing(t *testing.T) {
	// 手工构造 /proc 格式行：127.0.0.1:53 监听 + 已建立的 tcp6 连接。
	fields := []string{"0:", "0100007F:0035", "00000000:0000", "0A", "00000000:00000000", "00:00000000", "00000000", "991", "0", "17920", "1"}
	c, ok := parseConnFields(fields, "tcp", true)
	if !ok {
		t.Fatal("parse failed")
	}
	if !c.LocalIP.Equal(net.IPv4(127, 0, 0, 1)) || c.LocalPort != 53 || c.State != "LISTEN" || c.UID != 991 || c.Inode != 17920 {
		t.Fatalf("conn = %+v", c)
	}

	// IPv6 真实 /proc 格式：32 位字内小端（::1 → ...01000000）。
	fields = []string{"0:", "00000000000000000000000001000000:0050", "00000000000000000000000000000000:0000", "0A", "0", "0", "0", "0", "0", "1"}
	c, ok = parseConnFields(fields, "tcp", true)
	if !ok || !c.LocalIP.Equal(net.IPv6loopback) || c.LocalPort != 80 {
		t.Fatalf("ipv6 conn = %+v (ip %v)", c, c.LocalIP)
	}
}

// TestConnectionsLive 覆盖真实 /proc 解析自洽性。
func TestConnectionsLive(t *testing.T) {
	conns, err := Connections()
	if err != nil {
		t.Fatalf("Connections: %v", err)
	}
	for _, c := range conns {
		if c.LocalIP == nil || c.RemoteIP == nil || c.LocalPort == 0 {
			t.Fatalf("bad conn: %+v", c)
		}
	}
}

// TestIOCountersLive 覆盖网卡统计解析。
func TestIOCountersLive(t *testing.T) {
	stats, err := IOCounters()
	if err != nil {
		t.Fatalf("IOCounters: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("no interfaces")
	}
	for _, s := range stats {
		if s.Name == "" {
			t.Fatal("empty interface name")
		}
	}
}

// TestProtoCountersLive 覆盖协议计数解析。
func TestProtoCountersLive(t *testing.T) {
	counters, err := ProtoCounters()
	if err != nil {
		t.Fatalf("ProtoCounters: %v", err)
	}
	if _, ok := counters["Ip.InReceives"]; !ok {
		t.Fatalf("Ip.InReceives missing: %v", counters)
	}
	if _, ok := counters["Tcp.ActiveOpens"]; !ok {
		t.Fatalf("Tcp.ActiveOpens missing: %v", counters)
	}
}
