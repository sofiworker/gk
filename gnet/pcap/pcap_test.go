package pcap

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

func TestReadWriteRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	writer, err := NewWriter(&buf, WithSnapLen(2048))
	if err != nil {
		t.Fatalf("NewWriter failed: %v", err)
	}
	defer writer.Close()

	ts1 := time.Unix(1_700_000_000, 123456000).UTC()
	ts2 := ts1.Add(1500 * time.Microsecond)

	if err := writer.WritePacket(&Packet{
		Data:      []byte{0x01, 0x02, 0x03},
		Timestamp: ts1,
	}); err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}

	if err := writer.WritePacket(&Packet{
		Header: PacketHeader{
			OrigLen: 4,
			InclLen: 4,
		},
		Data:      []byte{0xAA, 0xBB, 0xCC, 0xDD},
		Timestamp: ts2,
	}); err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	header := reader.Header()
	if header.SnapLen != 2048 {
		t.Fatalf("unexpected snap length: %d", header.SnapLen)
	}

	p1, err := reader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket failed: %v", err)
	}
	if !p1.Timestamp.Equal(ts1) {
		t.Fatalf("unexpected timestamp: got %v want %v", p1.Timestamp, ts1)
	}
	if !bytes.Equal(p1.Data, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("unexpected packet data: %x", p1.Data)
	}

	p2, err := reader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket second failed: %v", err)
	}
	if !p2.Timestamp.Equal(ts2) {
		t.Fatalf("unexpected timestamp: got %v want %v", p2.Timestamp, ts2)
	}
	if p2.Header.InclLen != 4 || p2.Header.OrigLen != 4 {
		t.Fatalf("unexpected lengths: incl=%d orig=%d", p2.Header.InclLen, p2.Header.OrigLen)
	}
	if !bytes.Equal(p2.Data, []byte{0xAA, 0xBB, 0xCC, 0xDD}) {
		t.Fatalf("unexpected packet data: %x", p2.Data)
	}

	if _, err := reader.ReadPacket(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestBigEndianWriter(t *testing.T) {
	var buf bytes.Buffer
	writer, err := NewWriter(&buf, WithByteOrder(binary.BigEndian), WithTimestampResolution(time.Nanosecond))
	if err != nil {
		t.Fatalf("NewWriter failed: %v", err)
	}

	ts := time.Unix(1_710_000_000, 987654321).UTC()
	payload := []byte{0x10, 0x20, 0x30, 0x40}
	if err := writer.WritePacket(&Packet{
		Data:      payload,
		Timestamp: ts,
	}); err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	header := reader.Header()
	if header.IsLittleEndian() {
		t.Fatalf("expected big-endian header")
	}
	if header.TimestampResolution() != time.Nanosecond {
		t.Fatalf("expected nanosecond resolution")
	}

	packet, err := reader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket failed: %v", err)
	}
	if !packet.Timestamp.Equal(ts) {
		t.Fatalf("timestamp mismatch: got %v want %v", packet.Timestamp, ts)
	}
	if !bytes.Equal(packet.Data, payload) {
		t.Fatalf("payload mismatch: %x", packet.Data)
	}
}
