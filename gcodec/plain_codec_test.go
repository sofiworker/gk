package gcodec

import (
	"errors"
	"testing"
)

func TestPlainCodec(t *testing.T) {
	codec := NewPlainCodec()

	// Test String
	strVal := "Hello World"
	data, err := codec.EncodeBytes(strVal)
	if err != nil {
		t.Fatalf("EncodeBytes string failed: %v", err)
	}
	if string(data) != strVal {
		t.Errorf("expected %s, got %s", strVal, data)
	}
	
	var decodedStr string
	if err := codec.DecodeBytes(data, &decodedStr); err != nil {
		t.Fatalf("DecodeBytes string failed: %v", err)
	}
	if decodedStr != strVal {
		t.Errorf("expected %s, got %s", strVal, decodedStr)
	}

	// Test Byte Slice
	byteVal := []byte("Byte Data")
	data, err = codec.EncodeBytes(byteVal)
	if err != nil {
		t.Fatalf("EncodeBytes bytes failed: %v", err)
	}
	if string(data) != string(byteVal) {
		t.Errorf("expected %s, got %s", byteVal, data)
	}

	var decodedBytes []byte
	if err := codec.DecodeBytes(data, &decodedBytes); err != nil {
		t.Fatalf("DecodeBytes bytes failed: %v", err)
	}
	if string(decodedBytes) != string(byteVal) {
		t.Errorf("expected %s, got %s", byteVal, decodedBytes)
	}

	// Test Error
	errVal := errors.New("some error")
	data, err = codec.EncodeBytes(errVal)
	if err != nil {
		t.Fatalf("EncodeBytes error failed: %v", err)
	}
	if string(data) != "some error" {
		t.Errorf("expected 'some error', got %s", data)
	}

	// Test Interface{}
	var iface interface{}
	if err := codec.DecodeBytes([]byte("test"), &iface); err != nil {
		t.Fatalf("DecodeBytes interface failed: %v", err)
	}
	if iface.(string) != "test" {
		t.Errorf("expected 'test', got %v", iface)
	}
	
	// Test Decode into *string
	var s string
	if err := codec.DecodeBytes([]byte("s"), &s); err != nil {
		t.Fatal(err)
	}
	if s != "s" {
		t.Error("failed decode *string")
	}
}
