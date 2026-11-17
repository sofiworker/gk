package pcap

import (
	"bytes"
	"testing"
	"time"

	"golang.org/x/net/bpf"
)

func TestPCAPNGFilterCopy(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, WithFormat(FormatPCAPNG), WithDefaultTimestampResolution(time.Microsecond))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	ifaceID, err := w.AddInterface(1, 64)
	if err != nil {
		t.Fatalf("AddInterface: %v", err)
	}
	if err := w.WritePacket(&Packet{InterfaceID: ifaceID, Data: []byte{0x41}, Timestamp: time.Unix(0, 0)}); err != nil {
		t.Fatalf("write pkt1: %v", err)
	}
	if err := w.WritePacket(&Packet{InterfaceID: ifaceID, Data: []byte{0x42}, Timestamp: time.Unix(0, 0)}); err != nil {
		t.Fatalf("write pkt2: %v", err)
	}
	_ = w.Close()

	prog := []bpf.Instruction{
		bpf.LoadAbsolute{Off: 0, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x41, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0xffff},
		bpf.RetConstant{Val: 0},
	}

	out := bytes.Buffer{}
	n, err := FilterCopy(bytes.NewReader(buf.Bytes()), &out, prog)
	if err != nil {
		t.Fatalf("FilterCopy: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 packet kept, got %d", n)
	}

	reader, err := NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := reader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if len(pkt.Data) == 0 || pkt.Data[0] != 0x41 {
		t.Fatalf("unexpected packet data: %v", pkt.Data)
	}
}
