package pcapng

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

func TestPCAPNGReadWrite(t *testing.T) {
	var buf bytes.Buffer
	writer, err := NewWriter(&buf, WithDefaultTimestampResolution(time.Nanosecond))
	if err != nil {
		t.Fatalf("NewWriter failed: %v", err)
	}
	defer writer.Close()

	ifaceID, err := writer.AddInterface(1, 65535)
	if err != nil {
		t.Fatalf("AddInterface failed: %v", err)
	}

	ts1 := time.Unix(1_710_000_000, 123456789).UTC()
	ts2 := ts1.Add(3 * time.Millisecond)
	payload1 := []byte{0x01, 0x02, 0x03, 0x04}
	payload2 := []byte{0xAA, 0xBB, 0xCC}

	if err := writer.WritePacket(ifaceID, payload1, ts1); err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}
	if err := writer.WritePacket(ifaceID, payload2, ts2); err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}

	reader := NewReader(bytes.NewReader(buf.Bytes()))

	block, err := reader.NextBlock()
	if err != nil {
		t.Fatalf("NextBlock failed: %v", err)
	}
	if _, ok := block.(*SectionHeaderBlock); !ok {
		t.Fatalf("expected SectionHeaderBlock, got %T", block)
	}

	block, err = reader.NextBlock()
	if err != nil {
		t.Fatalf("NextBlock (interface) failed: %v", err)
	}
	idBlock, ok := block.(*InterfaceDescriptionBlock)
	if !ok {
		t.Fatalf("expected InterfaceDescriptionBlock, got %T", block)
	}
	if idBlock.ID != ifaceID {
		t.Fatalf("unexpected interface id: %d", idBlock.ID)
	}

	packet1, err := reader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket failed: %v", err)
	}
	if !packet1.Timestamp.Equal(ts1) {
		t.Fatalf("timestamp mismatch: got %v want %v", packet1.Timestamp, ts1)
	}
	if packet1.CapturedLen != uint32(len(payload1)) {
		t.Fatalf("captured length mismatch: %d", packet1.CapturedLen)
	}
	if !bytes.Equal(packet1.Data, payload1) {
		t.Fatalf("payload mismatch: %x", packet1.Data)
	}

	packet2, err := reader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket second failed: %v", err)
	}
	if !packet2.Timestamp.Equal(ts2) {
		t.Fatalf("timestamp mismatch: got %v want %v", packet2.Timestamp, ts2)
	}
	if packet2.CapturedLen != uint32(len(payload2)) {
		t.Fatalf("captured length mismatch: %d", packet2.CapturedLen)
	}
	if !bytes.Equal(packet2.Data, payload2) {
		t.Fatalf("payload mismatch: %x", packet2.Data)
	}

	if _, err := reader.ReadPacket(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestPCAPNGBigEndianRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	writer, err := NewWriter(&buf, WithByteOrder(binary.BigEndian))
	if err != nil {
		t.Fatalf("NewWriter failed: %v", err)
	}
	defer writer.Close()

	ifaceID, err := writer.AddInterface(1, 0, WithInterfaceTimestampResolution(time.Microsecond))
	if err != nil {
		t.Fatalf("AddInterface failed: %v", err)
	}

	ts := time.Unix(1_720_000_000, 654321000).UTC()
	payload := []byte{0x10, 0x20, 0x30}
	if err := writer.WritePacket(ifaceID, payload, ts); err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}

	reader := NewReader(bytes.NewReader(buf.Bytes()))
	block, err := reader.NextBlock()
	if err != nil {
		t.Fatalf("NextBlock failed: %v", err)
	}
	section, ok := block.(*SectionHeaderBlock)
	if !ok {
		t.Fatalf("expected SectionHeaderBlock, got %T", block)
	}
	if section.ByteOrderMagic != ByteOrderMagicBig {
		t.Fatalf("unexpected byte order magic: %#x", section.ByteOrderMagic)
	}

	if _, err = reader.NextBlock(); err != nil {
		t.Fatalf("NextBlock (interface) failed: %v", err)
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
