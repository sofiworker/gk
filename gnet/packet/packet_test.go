package packet

import (
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
	"github.com/sofiworker/gk/gnet/pcapng"
	"github.com/sofiworker/gk/gnet/rawcap"
)

func TestFromRawcap(t *testing.T) {
	ts := time.Now()
	p := &rawcap.Packet{Data: []byte{1, 2}, Info: &rawcap.PacketInfo{Timestamp: ts, CaptureLength: 2, Length: 2, InterfaceIndex: 3}}
	out := FromRawcap(p)
	if out == nil || out.InterfaceID != 3 || out.CaptureLen != 2 || !out.Timestamp.Equal(ts) {
		t.Fatalf("unexpected packet: %+v", out)
	}
}

func TestFromPCAP(t *testing.T) {
	ts := time.Unix(1, 0)
	p := &pcap.Packet{Data: []byte{1}, Header: pcap.PacketHeader{OrigLen: 2}, Timestamp: ts}
	out := FromPCAP(p)
	if out.OriginalLen != 2 || out.CaptureLen != 1 || !out.Timestamp.Equal(ts) {
		t.Fatalf("unexpected packet: %+v", out)
	}
}

func TestFromPCAPNG(t *testing.T) {
	ts := time.Unix(0, 1)
	p := &pcapng.Packet{InterfaceID: 7, Data: []byte{9}, CapturedLen: 1, OriginalLen: 1, Timestamp: ts}
	out := FromPCAPNG(p)
	if out.InterfaceID != 7 || out.CaptureLen != 1 || !out.Timestamp.Equal(ts) {
		t.Fatalf("unexpected packet: %+v", out)
	}
}
