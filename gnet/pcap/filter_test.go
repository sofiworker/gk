package pcap

import (
	"bytes"
	"testing"
	"time"

	"golang.org/x/net/bpf"
)

func TestFilterCopy(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, WithSnapLen(64))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	p1 := &Packet{Data: []byte{0x41, 0x00}, Timestamp: time.Unix(0, 0)} // 'A'
	p2 := &Packet{Data: []byte{0x42, 0x00}, Timestamp: time.Unix(0, 0)} // 'B'
	if err := w.WritePacket(p1); err != nil {
		t.Fatalf("write p1: %v", err)
	}
	if err := w.WritePacket(p2); err != nil {
		t.Fatalf("write p2: %v", err)
	}

	// Filter: first byte == 'A'
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

	r, err := NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	pkt, err := r.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if len(pkt.Data) == 0 || pkt.Data[0] != 0x41 {
		t.Fatalf("unexpected packet data: %v", pkt.Data)
	}
}
