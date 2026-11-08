package rawcap

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
)

type writerOutput struct {
	bytes.Buffer
	closes int
}

func (w *writerOutput) Close() error { w.closes++; return nil }

func TestPCAPWriterRoundTrip(t *testing.T) {
	output := &writerOutput{}
	w, err := NewWriter(output)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Unix(1700000000, 123456000).UTC()
	if err := w.WritePacket(&Packet{Data: []byte{1, 2, 3}, Info: &PacketInfo{Timestamp: ts, CaptureLength: 2, Length: 20}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if output.closes != 1 {
		t.Fatalf("close count = %d", output.closes)
	}
	r, err := pcap.NewReader(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := r.ReadPacket()
	if err != nil || !bytes.Equal(pkt.Data, []byte{1, 2}) || !pkt.Timestamp.Equal(ts) || pkt.OriginalLength() != 20 {
		t.Fatalf("packet = %+v, %v", pkt, err)
	}
	if _, err := r.ReadPacket(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestPCAPWriterInvalidPacket(t *testing.T) {
	w, err := NewWriter(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkt := range []*Packet{nil, {}, {Info: &PacketInfo{CaptureLength: -1}}, {Info: &PacketInfo{CaptureLength: 1}}, {Info: &PacketInfo{Length: -1}}} {
		if err := w.WritePacket(pkt); !errors.Is(err, pcap.ErrInvalidPacket) {
			t.Fatalf("error = %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
